package asm

import (
	"fmt"
)

type Assembler struct {
	arch      *Arch
	vregAlloc *VRegAlloc
	regAlloc  *RegAlloc
	buffer    *Buffer
	stack     []Register
	params    []Register
	insts     []Instruction
}

func NewAssembler(arch *Arch, buffer *Buffer) *Assembler {
	return &Assembler{
		arch:      arch,
		vregAlloc: NewVRegAlloc(),
		regAlloc:  NewRegAlloc(arch.Registers),
		buffer:    buffer,
	}
}

func (a *Assembler) Push(typ RegType) Register {
	reg := a.vregAlloc.Alloc(typ)
	a.stack = append(a.stack, reg)
	return reg
}

func (a *Assembler) Pop(typ RegType) Register {
	if len(a.stack) == 0 {
		reg := a.vregAlloc.Alloc(typ)
		a.params = append(a.params, reg)
		return reg
	}

	last := a.stack[len(a.stack)-1]
	a.stack = a.stack[:len(a.stack)-1]
	if last.Type() != typ {
		panic(fmt.Sprintf("asm: stack type mismatch: expected %v, got %v", typ, last.Type()))
	}
	return last
}

func (a *Assembler) Emit(inst Instruction) {
	a.insts = append(a.insts, inst)
}

func (a *Assembler) Build() (Caller, error) {
	sig := a.signature()
	instrs, err := a.allocRegs()
	if err != nil {
		return nil, err
	}

	code, err := Encode(a.arch.Encoder, instrs)
	if err != nil {
		return nil, err
	}

	if err := a.buffer.Unseal(); err != nil {
		return nil, err
	}

	chunk, err := a.buffer.Append(code)
	if err != nil {
		return nil, err
	}

	if err := a.buffer.Seal(); err != nil {
		return nil, err
	}

	a.Reset()
	return a.arch.NewCaller(sig, chunk)
}

func (a *Assembler) Reset() {
	a.stack = a.stack[:0]
	a.params = a.params[:0]
	a.insts = a.insts[:0]

	if a.vregAlloc != nil {
		a.vregAlloc.Reset()
	}
	if a.regAlloc != nil {
		a.regAlloc.Reset()
	}
}

func (a *Assembler) signature() *Signature {
	params := make([]RegType, len(a.params))
	for i, reg := range a.params {
		params[i] = reg.Type()
	}
	returns := make([]RegType, len(a.stack))
	for i, reg := range a.stack {
		returns[i] = reg.Type()
	}
	return &Signature{Params: params, Returns: returns}
}

func (a *Assembler) allocRegs() ([]Instruction, error) {
	lastUse := make(map[Register]int)
	for idx, inst := range a.insts {
		for _, reg := range a.srcs(inst) {
			if !reg.IsVirtual() {
				continue
			}
			lastUse[reg] = idx
		}
	}

	intRegs := a.allocatable(RegTypeInt)
	floatRegs := a.allocatable(RegTypeFloat)

	returnRegs := make(map[Register]Register)
	intReturns, floatReturns := 0, 0
	for _, vreg := range a.stack {
		var phys Register
		if vreg.Type() == RegTypeFloat {
			if floatReturns >= len(floatRegs) {
				return nil, fmt.Errorf("asm: too many float returns: %d", len(a.stack))
			}
			phys = floatRegs[floatReturns]
			floatReturns++
		} else {
			if intReturns >= len(intRegs) {
				return nil, fmt.Errorf("asm: too many int returns: %d", len(a.stack))
			}
			phys = intRegs[intReturns]
			intReturns++
		}
		returnRegs[vreg] = phys
	}

	physical := make(map[Register]Register)
	virtual := make(map[Register]Register)
	intParams, floatParams := 0, 0
	for _, vreg := range a.params {
		var phys Register
		if vreg.Type() == RegTypeFloat {
			if floatParams >= len(floatRegs) {
				return nil, fmt.Errorf("asm: too many float params: %d", len(a.params))
			}
			phys = floatRegs[floatParams]
			floatParams++
		} else {
			if intParams >= len(intRegs) {
				return nil, fmt.Errorf("asm: too many int params: %d", len(a.params))
			}
			phys = intRegs[intParams]
			intParams++
		}
		if err := a.reserve(vreg, phys); err != nil {
			return nil, err
		}
		physical[vreg] = phys
		virtual[phys] = vreg
	}

	for idx, inst := range a.insts {
		for _, src := range a.srcs(inst) {
			if !src.IsVirtual() {
				continue
			}
			if _, ok := physical[src]; ok {
				continue
			}
			phys, err := a.regAlloc.Alloc(src)
			if err != nil {
				return nil, err
			}
			physical[src] = phys
			virtual[phys] = src
		}

		dst := a.dst(inst)
		if dst.IsVirtual() {
			if _, ok := physical[dst]; !ok {
				if desired, ok := returnRegs[dst]; ok {
					owner, occupied := virtual[desired]
					if !occupied || owner == dst || (lastUse[owner] == idx && !a.isReturn(owner, returnRegs)) {
						if occupied && owner != dst {
							a.regAlloc.Free(owner)
							delete(virtual, desired)
						}
						if err := a.reserve(dst, desired); err == nil {
							physical[dst] = desired
							virtual[desired] = dst
						} else {
							phys, err := a.regAlloc.Alloc(dst)
							if err != nil {
								return nil, err
							}
							physical[dst] = phys
							virtual[phys] = dst
						}
					} else {
						phys, err := a.regAlloc.Alloc(dst)
						if err != nil {
							return nil, err
						}
						physical[dst] = phys
						virtual[phys] = dst
					}
				} else {
					phys, err := a.regAlloc.Alloc(dst)
					if err != nil {
						return nil, err
					}
					physical[dst] = phys
					virtual[phys] = dst
				}
			}
		}

		for _, src := range a.srcs(inst) {
			if !src.IsVirtual() {
				continue
			}
			if lastUse[src] == idx {
				if !a.isReturn(src, returnRegs) {
					if phys, ok := physical[src]; ok {
						a.regAlloc.Free(src)
						delete(virtual, phys)
						delete(physical, src)
					}
				}
			}
		}
	}

	for vreg, phys := range returnRegs {
		if _, ok := physical[vreg]; ok {
			continue
		}
		if err := a.reserve(vreg, phys); err != nil {
			return nil, err
		}
		physical[vreg] = phys
	}

	rewrite := make([]Instruction, 0, len(a.insts))
	for _, inst := range a.insts {
		rewrite = append(rewrite, a.rewrite(inst, physical))
	}
	return rewrite, nil
}

func (a *Assembler) allocatable(typ RegType) []Register {
	mask := a.arch.Registers.Allocatable(typ)
	regs := make([]Register, 0, 8)
	for _, id := range mask.List() {
		regs = append(regs, NewReg(id, typ))
		if len(regs) == 8 {
			break
		}
	}
	return regs
}

func (a *Assembler) isReturn(reg Register, returns map[Register]Register) bool {
	_, ok := returns[reg]
	return ok
}

func (a *Assembler) reserve(vreg, phys Register) error {
	if existing, ok := a.regAlloc.phys[vreg]; ok {
		if existing == phys {
			return nil
		}
		return fmt.Errorf("asm: vreg %v already mapped to %v", vreg, existing)
	}

	avail := &a.regAlloc.intAvail
	if phys.Type() == RegTypeFloat {
		avail = &a.regAlloc.floatAvail
	}

	if !avail.Contains(phys.ID()) {
		return fmt.Errorf("asm: physical register %v already reserved", phys)
	}

	avail.Clear(phys.ID())
	a.regAlloc.phys[vreg] = phys
	return nil
}

func (a *Assembler) srcs(inst Instruction) []Register {
	var regs []Register
	if r, ok := a.operands(inst.Src1); ok {
		regs = append(regs, r)
	}
	if r, ok := a.operands(inst.Src2); ok {
		regs = append(regs, r)
	}
	return regs
}

func (a *Assembler) dst(inst Instruction) Register {
	if r, ok := a.operands(inst.Dst); ok {
		return r
	}
	return Register{}
}

func (a *Assembler) operands(op Operand) (Register, bool) {
	switch value := op.(type) {
	case RegOperand:
		return value.Reg, true
	case MemOperand:
		return value.Base, true
	default:
		return Register{}, false
	}
}

func (a *Assembler) rewrite(inst Instruction, mapping map[Register]Register) Instruction {
	return Instruction{
		Op:   inst.Op,
		Dst:  a.rewriteOP(inst.Dst, mapping),
		Src1: a.rewriteOP(inst.Src1, mapping),
		Src2: a.rewriteOP(inst.Src2, mapping),
	}
}

func (a *Assembler) rewriteOP(op Operand, mapping map[Register]Register) Operand {
	switch value := op.(type) {
	case RegOperand:
		if phys, ok := mapping[value.Reg]; ok {
			return RegOperand{phys}
		}
		return value
	case MemOperand:
		base := value.Base
		if phys, ok := mapping[base]; ok {
			base = phys
		}
		return MemOperand{Base: base, Offset: value.Offset}
	default:
		return op
	}
}
