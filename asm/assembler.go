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
	returns      map[int][]VReg // return-site stacks, auto-populated by Returns(Index())
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
	a.Emit(Instruction{Op: OpPseudoLabel, Dst: Label(id)})
}

func (a *Assembler) Scratch() PReg {
	mask := a.arch.Scratch
	for range len(a.scratch) {
		_, mask = mask.PopFirst()
	}
	p := NewPReg(mask.First(), RegTypeInt, Width64)
	a.scratch = append(a.scratch, p)
	a.regAlloc.Block(p)
	return p
}

func (a *Assembler) NewVReg(typ RegType, w RegWidth) VReg {
	return a.vregAlloc.Alloc(typ, w)
}

func (a *Assembler) Index() int { return len(a.insts) }

func (a *Assembler) Params(idx int) []VReg {
	if idx == a.Index() {
		return append([]VReg(nil), a.params...)
	}
	return nil
}

// Returns returns the virtual register stack at idx.
// For the live position (idx == Index()), it always reflects the current stack
// and auto-registers it as a return site for signature() and assign().
func (a *Assembler) Returns(idx int) []VReg {
	if idx == a.Index() {
		regs := append([]VReg(nil), a.stack...)
		if a.returns == nil {
			a.returns = make(map[int][]VReg)
		}
		a.returns[idx] = regs
		return regs
	}
	return append([]VReg(nil), a.returns[idx]...)
}

func (a *Assembler) Take(typ RegType, w RegWidth) (VReg, bool) {
	if len(a.stack) == 0 {
		r := a.vregAlloc.Alloc(typ, w)
		a.params = append(a.params, VReg{})
		copy(a.params[1:], a.params[:len(a.params)-1])
		a.params[0] = r
		return r, true
	}
	top := a.stack[len(a.stack)-1]
	if top.Type() != typ || top.Width() != w {
		return VReg{}, false
	}
	a.stack = a.stack[:len(a.stack)-1]
	return top, true
}

func (a *Assembler) Top(i int) (VReg, bool) {
	if len(a.stack) <= i {
		return VReg{}, false
	}
	return a.stack[len(a.stack)-1-i], true
}

func (a *Assembler) Push(r VReg) { a.stack = append(a.stack, r) }

func (a *Assembler) Pop() (VReg, bool) {
	if len(a.stack) == 0 {
		return VReg{}, false
	}
	top := a.stack[len(a.stack)-1]
	a.stack = a.stack[:len(a.stack)-1]
	return top, true
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

// compile strips pseudo-labels, derives the signature, and runs register
// allocation, restoring assembler state afterwards.
func (a *Assembler) compile() (*Signature, []Instruction, error) {
	real := make([]Instruction, 0, len(a.insts))
	for _, inst := range a.insts {
		if inst.Op != OpPseudoLabel {
			real = append(real, inst)
		}
	}

	savedInsts, savedAlloc := a.insts, a.regAlloc.Clone()
	a.insts = real

	sig, err := a.signature()
	if err != nil {
		a.insts, a.regAlloc = savedInsts, savedAlloc
		return nil, nil, err
	}
	assigned, err := a.assign()
	a.insts, a.regAlloc = savedInsts, savedAlloc
	if err != nil {
		return nil, nil, err
	}
	return sig, assigned, nil
}

func (a *Assembler) resolve(phys []Instruction) (code []byte, offsets map[int]int, relocs []Relocation, err error) {
	labelAt := make(map[int]int, len(a.localLabels))
	idx := 0
	for _, inst := range a.insts {
		if inst.Op == OpPseudoLabel {
			if lbl, ok := inst.Dst.(LabelOperand); ok {
				labelAt[lbl.ID] = idx
			}
		} else {
			idx++
		}
	}

	encoded := make([][]byte, len(phys))
	byteOff := make([]int, len(phys)+1)
	for i, inst := range phys {
		if _, ok := inst.Src2.(LabelOperand); ok {
			inst.Src2 = Imm(0)
		}
		b, e := a.arch.Encoder.Encode(inst)
		if e != nil {
			return nil, nil, nil, e
		}
		encoded[i] = b
		byteOff[i+1] = byteOff[i] + len(b)
	}

	localBytes := make(map[int]int, len(labelAt))
	for id, i := range labelAt {
		localBytes[id] = byteOff[i]
	}

	code = make([]byte, 0, byteOff[len(phys)])
	for i, inst := range phys {
		lbl, ok := inst.Src2.(LabelOperand)
		if !ok {
			code = append(code, encoded[i]...)
			continue
		}
		if off, local := localBytes[lbl.ID]; local {
			inst.Src2 = Imm(int64(off - byteOff[i]))
			b, e := a.arch.Encoder.Encode(inst)
			if e != nil {
				return nil, nil, nil, e
			}
			code = append(code, b...)
		} else {
			relocs = append(relocs, Relocation{InstrIdx: i, Offset: byteOff[i], Label: lbl.ID})
			code = append(code, encoded[i]...)
		}
	}
	return code, localBytes, relocs, nil
}

// Link patches cross-object relocations and returns a Caller per object.
// Objects that fail have a nil Caller; the first error is returned.
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
			target := unsafe.Add(entry.chunk.Ptr(), entry.offset)
			src := unsafe.Add(base, r.Offset)
			rel := int64(uintptr(target)) - int64(uintptr(src))

			inst := obj.Instrs[r.InstrIdx]
			inst.Src2 = Imm(rel)
			patched, e := a.arch.Encoder.Encode(inst)
			if e != nil {
				if firstErr == nil {
					firstErr = e
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
		caller, e := a.arch.NewCaller(obj.Sig, obj.Chunk)
		if e != nil {
			if firstErr == nil {
				firstErr = e
			}
			continue
		}
		callers[i] = caller
	}
	return callers, firstErr
}

func (a *Assembler) Abort() { a.reset() }

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
	a.returns = nil
	a.vregAlloc.Reset()
	a.regAlloc.Reset()
}

func (a *Assembler) signature() (*Signature, error) {
	if len(a.params) > a.arch.ABI.MaxParams() {
		return nil, ErrTooManyParams
	}
	if len(a.stack) > a.arch.ABI.MaxReturns() {
		return nil, ErrTooManyReturns
	}
	for _, regs := range a.returns {
		if len(regs) > a.arch.ABI.MaxReturns() {
			return nil, ErrTooManyReturns
		}
	}

	pick := func(vregs []VReg) []PReg {
		out := make([]PReg, len(vregs))
		for i, v := range vregs {
			out[i] = NewPReg(uint8(i), v.Type(), v.Width())
		}
		return out
	}

	outputs := make(map[int][]PReg, max(len(a.returns), 1))
	if len(a.returns) == 0 {
		outputs[0] = pick(a.stack)
	} else {
		for idx, regs := range a.returns {
			outputs[idx] = pick(regs)
		}
	}

	return &Signature{
		Scratch: append([]PReg(nil), a.scratch...),
		Inputs:  map[int][]PReg{0: pick(a.params)},
		Outputs: outputs,
	}, nil
}

func (a *Assembler) assign() ([]Instruction, error) {
	last := a.lastRefs()

	physical := make(map[int32]PReg)
	live := make(map[int32]PReg)
	virtual := make(map[uint8]VReg)
	fixed := make(map[int32]PReg)

	pinReturns := func(vregs []VReg) error {
		for i, v := range vregs {
			p := NewPReg(uint8(i), v.Type(), v.Width())
			if existing, ok := fixed[v.ID()]; ok && existing.ID() != p.ID() {
				return fmt.Errorf("%w: conflicting return register pin for %v", ErrInvalidArgs, v)
			}
			fixed[v.ID()] = p
		}
		return nil
	}
	if len(a.returns) == 0 {
		if err := pinReturns(a.stack); err != nil {
			return nil, err
		}
	} else {
		for _, vregs := range a.returns {
			if err := pinReturns(vregs); err != nil {
				return nil, err
			}
		}
	}

	for i, v := range a.params {
		if i >= a.arch.ABI.MaxParams() {
			return nil, ErrTooManyParams
		}

		p := NewPReg(uint8(i), v.Type(), v.Width())
		if err := a.regAlloc.Reserve(v, p); err != nil {
			return nil, err
		}

		physical[v.ID()] = p
		live[v.ID()] = p
		virtual[p.ID()] = v
	}

	for i, inst := range a.insts {
		for _, v := range a.uses(inst) {
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

		if dst, ok := a.def(inst); ok {
			if _, exists := physical[dst.ID()]; !exists {
				want, pinned := fixed[dst.ID()]
				placed := false
				if pinned {
					owner, occupied := virtual[want.ID()]
					_, ownerPinned := fixed[owner.ID()]
					if !occupied || owner.ID() == dst.ID() || (last[owner.ID()] == i && !ownerPinned) {
						if occupied && owner.ID() != dst.ID() {
							a.regAlloc.Free(owner)
							delete(live, owner.ID())
							delete(virtual, want.ID())
						}
						if err := a.regAlloc.Reserve(dst, want); err != nil {
							return nil, err
						}
						physical[dst.ID()] = want
						live[dst.ID()] = want
						virtual[want.ID()] = dst
						placed = true
					}
				}
				if !placed {
					p, err := a.regAlloc.Alloc(dst)
					if err != nil {
						return nil, err
					}
					physical[dst.ID()] = p
					live[dst.ID()] = p
					virtual[p.ID()] = dst
				}
			}
		}

		for _, v := range a.uses(inst) {
			if last[v.ID()] != i {
				continue
			}
			if _, pinned := fixed[v.ID()]; pinned {
				continue
			}
			if p, ok := live[v.ID()]; ok {
				a.regAlloc.Free(v)
				delete(live, v.ID())
				delete(virtual, p.ID())
			}
		}
	}

	for vid, p := range fixed {
		if _, ok := physical[vid]; ok {
			continue
		}
		v := NewVReg(vid, p.Type(), p.Width())
		if err := a.regAlloc.Reserve(v, p); err != nil {
			return nil, err
		}
		physical[vid] = p
		live[vid] = p
	}

	widths := a.vregWidths()

	out := make([]Instruction, 0, len(a.insts))
	for _, inst := range a.insts {
		if inst.Op == OpPseudoLabel {
			continue
		}
		out = append(out, a.rewrite(inst, physical, widths))
	}
	return out, nil
}

func (a *Assembler) lastRefs() map[int32]int {
	last := make(map[int32]int, len(a.insts))
	for i, inst := range a.insts {
		if dst, ok := a.def(inst); ok {
			if cur, ok := last[dst.ID()]; !ok || cur < i {
				last[dst.ID()] = i
			}
		}
		for _, v := range a.uses(inst) {
			last[v.ID()] = i
		}
	}
	return last
}

func (a *Assembler) vregWidths() map[int32]RegWidth {
	widths := make(map[int32]RegWidth)
	set := func(v VReg) {
		if _, ok := widths[v.ID()]; !ok {
			widths[v.ID()] = v.Width()
		}
	}
	for _, v := range a.params {
		set(v)
	}
	for _, v := range a.stack {
		set(v)
	}
	for _, inst := range a.insts {
		for _, v := range a.uses(inst) {
			set(v)
		}
		if dst, ok := a.def(inst); ok {
			set(dst)
		}
	}
	return widths
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

func (a *Assembler) uses(inst Instruction) []VReg {
	var regs []VReg
	if r, ok := a.mbase(inst.Dst); ok {
		regs = append(regs, r)
	}
	for _, op := range []Operand{inst.Src1, inst.Src2, inst.Src3} {
		if r, ok := a.vreg(op); ok {
			regs = append(regs, r)
		}
	}
	return regs
}

func (a *Assembler) def(inst Instruction) (VReg, bool) {
	r, ok := inst.Dst.(VRegOperand)
	return r.Reg, ok
}

func (a *Assembler) vreg(op Operand) (VReg, bool) {
	switch v := op.(type) {
	case VRegOperand:
		return v.Reg, true
	case MemOperand:
		return a.mbase(v)
	}
	return VReg{}, false
}

func (a *Assembler) mbase(op Operand) (VReg, bool) {
	m, ok := op.(MemOperand)
	if !ok {
		return VReg{}, false
	}
	b, ok := m.Base.(VRegOperand)
	return b.Reg, ok
}

func (a *Assembler) rewrite(inst Instruction, mapping map[int32]PReg, widths map[int32]RegWidth) Instruction {
	rw := func(op Operand) Operand { return a.rewriteOp(op, mapping, widths) }
	return Instruction{
		Op:   inst.Op,
		Dst:  rw(inst.Dst),
		Src1: rw(inst.Src1),
		Src2: rw(inst.Src2),
		Src3: rw(inst.Src3),
	}
}

func (a *Assembler) rewriteOp(op Operand, mapping map[int32]PReg, widths map[int32]RegWidth) Operand {
	width := func(v VReg) RegWidth {
		if w := v.Width(); w != WidthUndefined {
			return w
		}
		return widths[v.ID()]
	}
	switch v := op.(type) {
	case VRegOperand:
		if p, ok := mapping[v.Reg.ID()]; ok {
			return P(NewPReg(p.ID(), p.Type(), width(v.Reg)))
		}
	case MemOperand:
		if vr, ok := v.Base.(VRegOperand); ok {
			if p, ok := mapping[vr.Reg.ID()]; ok {
				return Mem(P(NewPReg(p.ID(), p.Type(), width(vr.Reg))), v.Offset)
			}
		}
	}
	return op
}
