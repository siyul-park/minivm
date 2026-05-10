package asm

import (
	"errors"
	"fmt"
	"unsafe"
)

type Assembler struct {
	arch         *Arch
	vregAlloc    *VRegAlloc
	regAlloc     *RegAlloc
	buffer       *Buffer
	stack        []VReg
	params       []VReg
	insts        []Instruction
	nextLabel    int
	globalLabels map[int]label
	localLabels  map[int]int
	scratch      []PReg
}

type label struct {
	chunk  *Chunk
	offset int
}

var ErrUnresolvedLabel = errors.New("unresolved label")

func NewAssembler(arch *Arch, buffer *Buffer) *Assembler {
	return &Assembler{
		arch:         arch,
		vregAlloc:    NewVRegAlloc(),
		regAlloc:     NewRegAlloc(arch.Registers),
		buffer:       buffer,
		globalLabels: make(map[int]label),
	}
}

func (a *Assembler) NewLabel() int {
	id := a.nextLabel
	a.nextLabel++
	return id
}

func (a *Assembler) Bind(id int) {
	if a.localLabels == nil {
		a.localLabels = make(map[int]int)
	}
	a.localLabels[id] = len(a.insts)
	a.insts = append(a.insts, Instruction{Op: OpPseudoLabel, Dst: Label(id)})
}

func (a *Assembler) Scratch() PReg {
	mask := a.arch.Scratch
	for range len(a.scratch) {
		_, mask = mask.PopFirst()
	}
	id := mask.First()
	preg := NewPReg(id, RegTypeInt, Width64)
	a.scratch = append(a.scratch, preg)
	a.regAlloc.Block(preg)
	return preg
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

func (a *Assembler) Take(typ RegType, w RegWidth) (VReg, bool) {
	if len(a.stack) == 0 {
		reg := a.vregAlloc.Alloc(typ, w)
		a.params = append(a.params, reg)
		return reg, true
	}
	last := a.stack[len(a.stack)-1]
	if last.Type() != typ || last.Width() != w {
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

func (a *Assembler) Emits(insts ...Instruction) {
	a.insts = append(a.insts, insts...)
}

func (a *Assembler) Compile() (*RelocObject, error) {
	sig, instrs, err := a.compile()
	if err != nil {
		return nil, err
	}

	code, offsets, relocs, err := a.resolve(instrs)
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

	for id, off := range offsets {
		a.globalLabels[id] = label{chunk: chunk, offset: off}
	}

	a.reset()

	return &RelocObject{
		Chunk:  chunk,
		Sig:    sig,
		Instrs: instrs,
		Labels: offsets,
		Relocs: relocs,
	}, nil
}

func (a *Assembler) compile() (*Signature, []Instruction, error) {
	stripped := make([]Instruction, 0, len(a.insts))
	for _, inst := range a.insts {
		if inst.Op != OpPseudoLabel {
			stripped = append(stripped, inst)
		}
	}

	saved := a.insts
	snap := a.regAlloc.Clone()
	a.insts = stripped

	sig, err := a.signature()
	if err != nil {
		a.insts = saved
		a.regAlloc = snap
		return nil, nil, err
	}
	assigned, err := a.assign()
	a.insts = saved
	a.regAlloc = snap
	if err != nil {
		return nil, nil, err
	}
	return sig, assigned, nil
}

// Encodes with Imm(0) placeholders in a first pass to measure instruction byte
// sizes without assuming a fixed width, then patches or records relocations.
func (a *Assembler) resolve(physAssigned []Instruction) ([]byte, map[int]int, []Relocation, error) {
	// instrIdx counts only real (non-pseudo) instructions to align with physAssigned.
	labelAt := make(map[int]int)
	instrIdx := 0
	for _, inst := range a.insts {
		if inst.Op == OpPseudoLabel {
			if lbl, ok := inst.Dst.(LabelOperand); ok {
				labelAt[lbl.ID] = instrIdx
			}
		} else {
			instrIdx++
		}
	}

	// Pass 1: encode with placeholder to measure byte sizes.
	encoded := make([][]byte, len(physAssigned))
	byteOffsets := make([]int, len(physAssigned)+1)
	for i, inst := range physAssigned {
		if _, ok := inst.Src2.(LabelOperand); ok {
			inst.Src2 = Imm(0)
		}
		b, err := a.arch.Encoder.Encode(inst)
		if err != nil {
			return nil, nil, nil, err
		}
		encoded[i] = b
		byteOffsets[i+1] = byteOffsets[i] + len(b)
	}

	localBytes := make(map[int]int, len(labelAt))
	for id, idx := range labelAt {
		localBytes[id] = byteOffsets[idx]
	}

	// Pass 2: patch local labels; record relocations for external ones.
	var relocs []Relocation
	code := make([]byte, 0, byteOffsets[len(physAssigned)])
	for i, inst := range physAssigned {
		lbl, hasLabel := inst.Src2.(LabelOperand)
		if !hasLabel {
			code = append(code, encoded[i]...)
			continue
		}
		if off, found := localBytes[lbl.ID]; found {
			inst.Src2 = Imm(int64(off - byteOffsets[i]))
			b, err := a.arch.Encoder.Encode(inst)
			if err != nil {
				return nil, nil, nil, err
			}
			code = append(code, b...)
		} else {
			relocs = append(relocs, Relocation{
				InstrIdx: i,
				Offset:   byteOffsets[i],
				Label:    lbl.ID,
			})
			code = append(code, encoded[i]...)
		}
	}
	return code, localBytes, relocs, nil
}

// Link patches relocations across all objects and returns a Caller per object.
// Objects that fail to link have a nil Caller; the first error is returned.
func (a *Assembler) Link(objects []*RelocObject) ([]Caller, error) {
	if err := a.buffer.Unseal(); err != nil {
		return nil, err
	}

	var firstErr error
	failed := make([]bool, len(objects))

	for i, obj := range objects {
		base := obj.Chunk.Ptr()
		for _, r := range obj.Relocs {
			entry, ok := a.globalLabels[r.Label]
			if !ok {
				if firstErr == nil {
					firstErr = fmt.Errorf("%w: label %d", ErrUnresolvedLabel, r.Label)
				}
				failed[i] = true
				continue
			}
			target := unsafe.Add(
				entry.chunk.Ptr(),
				entry.offset,
			)
			src := unsafe.Add(base, r.Offset)
			off := int64(uintptr(target)) - int64(uintptr(src))
			inst := obj.Instrs[r.InstrIdx]
			inst.Src2 = Imm(off)
			patched, err := a.arch.Encoder.Encode(inst)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				failed[i] = true
				continue
			}
			writeBytes(src, patched)
		}
	}
	if err := a.buffer.Seal(); err != nil {
		return nil, err
	}

	callers := make([]Caller, len(objects))
	for i, obj := range objects {
		if failed[i] {
			continue
		}
		caller, err := a.arch.NewCaller(obj.Sig, obj.Chunk)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		callers[i] = caller
	}
	return callers, firstErr
}

func (a *Assembler) Abort() {
	a.reset()
}

func (a *Assembler) Reset() {
	a.reset()
	a.nextLabel = 0
	a.globalLabels = make(map[int]label)
}

func (a *Assembler) reset() {
	a.stack = a.stack[:0]
	a.params = a.params[:0]
	a.insts = a.insts[:0]
	a.localLabels = nil
	a.scratch = a.scratch[:0]
	if a.vregAlloc != nil {
		a.vregAlloc.Reset()
	}
	if a.regAlloc != nil {
		a.regAlloc.Reset()
	}
}

func (a *Assembler) signature() (*Signature, error) {
	if len(a.params) > a.arch.ABI.MaxParams() {
		return nil, ErrTooManyParams
	}
	if len(a.stack) > a.arch.ABI.MaxReturns() {
		return nil, ErrTooManyReturns
	}

	intRegs := a.allocatable(RegTypeInt)
	floatRegs := a.allocatable(RegTypeFloat)

	params := make([]PReg, len(a.params))
	intP, floatP := 0, 0
	for i, v := range a.params {
		if v.Type() == RegTypeFloat {
			params[i] = NewPReg(floatRegs[floatP].ID(), v.Type(), v.Width())
			floatP++
		} else {
			params[i] = NewPReg(intRegs[intP].ID(), v.Type(), v.Width())
			intP++
		}
	}

	returns := make([]PReg, len(a.stack))
	intR, floatR := 0, 0
	for i, v := range a.stack {
		if v.Type() == RegTypeFloat {
			returns[i] = NewPReg(floatRegs[floatR].ID(), v.Type(), v.Width())
			floatR++
		} else {
			returns[i] = NewPReg(intRegs[intR].ID(), v.Type(), v.Width())
			intR++
		}
	}

	return &Signature{
		Scratch: append([]PReg(nil), a.scratch...),
		Params:  params,
		Returns: returns,
	}, nil
}

func (a *Assembler) assign() ([]Instruction, error) {
	// Compute last-use index for each vreg.
	// dst is registered first so that a write-only vreg is not freed before
	// rewrite. src overwrites dst at the same index, which is correct: a vreg
	// consumed as src at index i stays live until i.
	last := make(map[int32]int)
	for i, inst := range a.insts {
		if dst, ok := a.dst(inst); ok {
			if cur, ok := last[dst.ID()]; !ok || cur < i {
				last[dst.ID()] = i
			}
		}
		for _, v := range a.srcs(inst) {
			last[v.ID()] = i
		}
	}

	intRegs := a.allocatable(RegTypeInt)
	floatRegs := a.allocatable(RegTypeFloat)

	// physical: vreg → preg for the full lifetime, used by rewrite.
	// live: currently allocated subset of physical, used to track frees.
	// virtual: inverse of live (preg id → vreg), used for eviction checks.
	physical := make(map[int32]PReg)
	live := make(map[int32]PReg)
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
		live[v.ID()] = p
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
			live[v.ID()] = p
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
							delete(live, owner.ID())
							delete(virtual, want.ID())
						}

						if err := a.regAlloc.Reserve(dst, want); err == nil {
							physical[dst.ID()] = want
							live[dst.ID()] = want
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
				live[dst.ID()] = p
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
			if p, ok := live[v.ID()]; ok {
				a.regAlloc.Free(v)
				delete(live, v.ID())
				delete(virtual, p.ID())
				// physical is intentionally kept for rewrite
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
		live[vid] = p
	}

	widths := make(map[int32]RegWidth, len(physical))
	for _, v := range a.params {
		widths[v.ID()] = v.Width()
	}
	for _, v := range a.stack {
		widths[v.ID()] = v.Width()
	}
	for _, inst := range a.insts {
		for _, v := range a.srcs(inst) {
			if _, ok := widths[v.ID()]; !ok {
				widths[v.ID()] = v.Width()
			}
		}
		if dst, ok := a.dst(inst); ok {
			if _, ok := widths[dst.ID()]; !ok {
				widths[dst.ID()] = dst.Width()
			}
		}
	}

	// Rewrite vregs to pregs, skipping pseudo-label instructions.
	out := make([]Instruction, 0, len(a.insts))
	for _, inst := range a.insts {
		if inst.Op == OpPseudoLabel {
			continue
		}
		out = append(out, a.rewrite(inst, physical, widths))
	}

	return out, nil
}

func (a *Assembler) allocatable(typ RegType) []PReg {
	mask := a.arch.Registers.Allocatable(typ)
	regs := make([]PReg, 0, mask.Count())
	for !mask.Empty() {
		var id uint8
		id, mask = mask.PopFirst()
		regs = append(regs, NewPReg(id, typ, WidthUndefined))
	}
	return regs
}

func (a *Assembler) srcs(inst Instruction) []VReg {
	var regs []VReg
	if r, ok := a.memBase(inst.Dst); ok {
		regs = append(regs, r)
	}
	if r, ok := a.vreg(inst.Src1); ok {
		regs = append(regs, r)
	}
	if r, ok := a.vreg(inst.Src2); ok {
		regs = append(regs, r)
	}
	return regs
}

func (a *Assembler) dst(inst Instruction) (VReg, bool) {
	if r, ok := inst.Dst.(VRegOperand); ok {
		return r.Reg, true
	}
	return VReg{}, false
}

func (a *Assembler) vreg(op Operand) (VReg, bool) {
	switch v := op.(type) {
	case VRegOperand:
		return v.Reg, true
	case MemOperand:
		return a.memBase(v)
	}
	return VReg{}, false
}

func (a *Assembler) memBase(op Operand) (VReg, bool) {
	if m, ok := op.(MemOperand); ok {
		if b, ok := m.Base.(VRegOperand); ok {
			return b.Reg, true
		}
	}
	return VReg{}, false
}

func (a *Assembler) rewrite(inst Instruction, mapping map[int32]PReg, widths map[int32]RegWidth) Instruction {
	return Instruction{
		Op:   inst.Op,
		Dst:  a.rewriteOP(inst.Dst, mapping, widths),
		Src1: a.rewriteOP(inst.Src1, mapping, widths),
		Src2: a.rewriteOP(inst.Src2, mapping, widths),
	}
}

func (a *Assembler) rewriteOP(op Operand, mapping map[int32]PReg, widths map[int32]RegWidth) Operand {
	switch v := op.(type) {
	case VRegOperand:
		if p, ok := mapping[v.Reg.ID()]; ok {
			w := v.Reg.Width()
			if w == WidthUndefined {
				if ww, ok := widths[v.Reg.ID()]; ok {
					w = ww
				}
			}
			return P(NewPReg(p.ID(), p.Type(), w))
		}
		return v
	case MemOperand:
		base := v.Base
		if vr, ok := base.(VRegOperand); ok {
			if p, ok := mapping[vr.Reg.ID()]; ok {
				w := vr.Reg.Width()
				if w == WidthUndefined {
					if ww, ok := widths[vr.Reg.ID()]; ok {
						w = ww
					}
				}
				base = P(NewPReg(p.ID(), p.Type(), w))
			}
		}
		return Mem(base, v.Offset)
	default:
		return op
	}
}
