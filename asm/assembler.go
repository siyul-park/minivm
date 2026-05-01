package asm

type Assembler struct {
	arch      *Arch
	vregAlloc *VRegAlloc
	regAlloc  *RegAlloc
	buffer    *Buffer
	stack     []VReg
	params    []VReg
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

func (a *Assembler) Params() []VReg {
	return append([]VReg(nil), a.params...)
}

func (a *Assembler) Returns() []VReg {
	return append([]VReg(nil), a.stack...)
}

func (a *Assembler) NewVReg(typ RegType, w RegWidth) VReg {
	return a.vregAlloc.Alloc(typ, w)
}

func (a *Assembler) Take(typ RegType) (VReg, bool) {
	if len(a.stack) == 0 {
		reg := a.vregAlloc.Alloc(typ, Width64)
		a.params = append(a.params, reg)
		return reg, true
	}
	last := a.stack[len(a.stack)-1]
	if last.Type() != typ {
		return VReg{}, false
	}
	a.stack = a.stack[:len(a.stack)-1]
	return last, true
}

func (a *Assembler) Top(i int) (VReg, bool) {
	if len(a.stack) <= i {
		return VReg{}, false
	}
	return a.stack[len(a.stack)-1-i], true
}

func (a *Assembler) Push(reg VReg) {
	a.stack = append(a.stack, reg)
}

func (a *Assembler) Pop() (VReg, bool) {
	if len(a.stack) == 0 {
		return VReg{}, false
	}
	last := a.stack[len(a.stack)-1]
	a.stack = a.stack[:len(a.stack)-1]
	return last, true
}

func (a *Assembler) Emit(inst Instruction) int {
	a.insts = append(a.insts, inst)
	return len(a.insts) - 1
}

func (a *Assembler) Build() (Caller, error) {
	sig, err := a.signature()
	if err != nil {
		return nil, err
	}
	instrs, err := a.assign()
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

func (a *Assembler) signature() (*Signature, error) {
	if len(a.stack) > a.arch.ABI.MaxReturns() {
		return nil, ErrTooManyReturns
	}
	if len(a.params) > a.arch.ABI.MaxParams() {
		return nil, ErrTooManyParams
	}
	params := make([]RegType, len(a.params))
	for i, reg := range a.params {
		params[i] = reg.Type()
	}
	returns := make([]RegType, len(a.stack))
	for i, reg := range a.stack {
		returns[i] = reg.Type()
	}
	return &Signature{Params: params, Returns: returns}, nil
}

func (a *Assembler) assign() ([]Instruction, error) {
	last := make(map[int32]int)
	for i, inst := range a.insts {
		for _, v := range a.srcs(inst) {
			last[v.ID()] = i
		}
	}

	intRegs := a.allocatable(RegTypeInt, Width64)
	floatRegs := a.allocatable(RegTypeFloat, Width64)

	physical := make(map[int32]PReg)
	virtual := make(map[uint8]VReg)
	fixed := make(map[int32]PReg)

	intR, floatR := 0, 0
	for _, v := range a.stack {
		var p PReg
		if v.Type() == RegTypeFloat {
			p = floatRegs[floatR]
			floatR++
		} else {
			p = intRegs[intR]
			intR++
		}
		fixed[v.ID()] = p
	}

	intP, floatP := 0, 0
	for _, v := range a.params {
		var p PReg

		if v.Type() == RegTypeFloat {
			if floatP >= a.arch.ABI.MaxParams() {
				return nil, ErrTooManyParams
			}
			p = floatRegs[floatP]
			floatP++
		} else {
			if intP >= a.arch.ABI.MaxParams() {
				return nil, ErrTooManyParams
			}
			p = intRegs[intP]
			intP++
		}

		if err := a.regAlloc.Reserve(v, p); err != nil {
			return nil, err
		}

		physical[v.ID()] = p
		virtual[p.ID()] = v
	}

	for i, inst := range a.insts {
		for _, v := range a.srcs(inst) {
			if _, ok := physical[v.ID()]; ok {
				continue
			}

			p, err := a.regAlloc.Alloc(v)
			if err != nil {
				return nil, err
			}

			physical[v.ID()] = p
			virtual[p.ID()] = v
		}

		if dst, ok := a.dst(inst); ok {
			if _, exists := physical[dst.ID()]; !exists {
				if want, ok := fixed[dst.ID()]; ok {
					owner, occupied := virtual[want.ID()]
					fix := false
					if owner.ID() != 0 {
						if _, ok := fixed[owner.ID()]; ok {
							fix = true
						}
					}
					if !occupied || owner.ID() == dst.ID() || (last[owner.ID()] == i && !fix) {
						if occupied && owner.ID() != dst.ID() {
							a.regAlloc.Free(owner)
							delete(virtual, want.ID())
						}

						if err := a.regAlloc.Reserve(dst, want); err == nil {
							physical[dst.ID()] = want
							virtual[want.ID()] = dst
							continue
						}
					}
				}

				p, err := a.regAlloc.Alloc(dst)
				if err != nil {
					return nil, err
				}

				physical[dst.ID()] = p
				virtual[p.ID()] = dst
			}
		}

		for _, v := range a.srcs(inst) {
			if last[v.ID()] != i {
				continue
			}
			if _, ok := fixed[v.ID()]; ok {
				continue
			}
			if p, ok := physical[v.ID()]; ok {
				a.regAlloc.Free(v)
				delete(physical, v.ID())
				delete(virtual, p.ID())
			}
		}
	}

	for vid, p := range fixed {
		if _, ok := physical[vid]; ok {
			continue
		}
		if err := a.regAlloc.Reserve(NewVReg(vid, p.Type(), p.Width()), p); err != nil {
			return nil, err
		}
		physical[vid] = p
	}

	out := make([]Instruction, 0, len(a.insts))
	for _, inst := range a.insts {
		out = append(out, a.rewrite(inst, physical))
	}

	return out, nil
}

func (a *Assembler) allocatable(typ RegType, w RegWidth) []PReg {
	mask := a.arch.Registers.Allocatable(typ)
	regs := make([]PReg, 0, mask.Count())
	for !mask.Empty() {
		var id uint8
		id, mask = mask.PopFirst()
		regs = append(regs, NewPReg(id, typ, w))
	}
	return regs
}

func (a *Assembler) srcs(inst Instruction) []VReg {
	var regs []VReg
	if r, ok := a.vreg(inst.Src1); ok {
		regs = append(regs, r)
	}
	if r, ok := a.vreg(inst.Src2); ok {
		regs = append(regs, r)
	}
	return regs
}

func (a *Assembler) dst(inst Instruction) (VReg, bool) {
	return a.vreg(inst.Dst)
}

func (a *Assembler) vreg(op Operand) (VReg, bool) {
	switch v := op.(type) {
	case VRegOperand:
		return v.Reg, true
	case MemOperand:
		if b, ok := v.Base.(VRegOperand); ok {
			return b.Reg, true
		}
	}
	return VReg{}, false
}

func (a *Assembler) rewrite(inst Instruction, mapping map[int32]PReg) Instruction {
	return Instruction{
		Op:   inst.Op,
		Dst:  a.rewriteOP(inst.Dst, mapping),
		Src1: a.rewriteOP(inst.Src1, mapping),
		Src2: a.rewriteOP(inst.Src2, mapping),
	}
}

func (a *Assembler) rewriteOP(op Operand, mapping map[int32]PReg) Operand {
	switch v := op.(type) {
	case VRegOperand:
		if p, ok := mapping[v.Reg.ID()]; ok {
			return P(p)
		}
		return v
	case MemOperand:
		base := v.Base
		if vr, ok := base.(VRegOperand); ok {
			if p, ok := mapping[vr.Reg.ID()]; ok {
				base = P(p)
			}
		}
		return Mem(base, v.Offset)

	default:
		return op
	}
}
