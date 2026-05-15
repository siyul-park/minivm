package asm

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"unsafe"
)

type Assembler struct {
	arch   *Arch
	buffer *Buffer
	vregs  *VRegAlloc

	stack   []VReg
	params  []VReg
	insts   []Instruction
	scratch []PReg
	returns map[int][]VReg

	nextLabel int
	global    map[int]label
	local     map[int]int
}

type label struct {
	chunk  *Chunk
	offset int
}

type program struct {
	insts   []Instruction
	params  []VReg
	stack   []VReg
	scratch []PReg
	returns map[int][]VReg
	labels  map[int]int
}

type compiler struct {
	arch *Arch
	regs *RegAlloc
	prog program

	phys map[int32]PReg
	live map[int32]PReg
	own  map[uint8]VReg
}

var (
	ErrUnresolvedLabel      = errors.New("unresolved label")
	ErrConflictingReturnPin = fmt.Errorf("%w: conflicting return register pin", ErrInvalidArgs)
)

func NewAssembler(arch *Arch, buffer *Buffer) *Assembler {
	return &Assembler{
		arch:   arch,
		buffer: buffer,
		vregs:  NewVRegAlloc(),
		global: make(map[int]label),
	}
}

func (a *Assembler) NewLabel() int {
	id := a.nextLabel
	a.nextLabel++
	return id
}

func (a *Assembler) Bind(id int) {
	if a.local == nil {
		a.local = make(map[int]int)
	}
	a.local[id] = len(a.insts)
}

func (a *Assembler) Scratch() PReg {
	mask := a.arch.Scratch
	for range len(a.scratch) {
		_, mask = mask.PopFirst()
	}

	p := NewPReg(mask.First(), RegTypeInt, Width64)
	a.scratch = append(a.scratch, p)
	return p
}

func (a *Assembler) NewVReg(typ RegType, width RegWidth) VReg {
	return a.vregs.Alloc(typ, width)
}

func (a *Assembler) Index() int {
	return len(a.insts)
}

func (a *Assembler) Mark() int {
	idx := a.Index()
	if a.returns == nil {
		a.returns = make(map[int][]VReg)
	}
	a.returns[idx] = slices.Clone(a.stack)
	return idx
}

func (a *Assembler) Params(idx int) []VReg {
	if idx != a.Index() {
		return nil
	}
	return slices.Clone(a.params)
}

func (a *Assembler) Returns(idx int) []VReg {
	if idx == a.Index() {
		return slices.Clone(a.stack)
	}
	return slices.Clone(a.returns[idx])
}

func (a *Assembler) Take(typ RegType, width RegWidth) (VReg, bool) {
	if len(a.stack) == 0 {
		r := a.NewVReg(typ, width)
		a.params = append([]VReg{r}, a.params...)
		return r, true
	}

	r := a.stack[len(a.stack)-1]
	if r.Type() != typ || r.Width() != width {
		return VReg{}, false
	}

	a.stack = a.stack[:len(a.stack)-1]
	return r, true
}

func (a *Assembler) Top(i int) (VReg, bool) {
	if i < 0 || i >= len(a.stack) {
		return VReg{}, false
	}
	return a.stack[len(a.stack)-1-i], true
}

func (a *Assembler) Push(r VReg) {
	a.stack = append(a.stack, r)
}

func (a *Assembler) Pop() (VReg, bool) {
	if len(a.stack) == 0 {
		return VReg{}, false
	}

	r := a.stack[len(a.stack)-1]
	a.stack = a.stack[:len(a.stack)-1]
	return r, true
}

func (a *Assembler) Emit(inst Instruction) int {
	a.insts = append(a.insts, inst)
	return len(a.insts) - 1
}

func (a *Assembler) Emits(insts ...Instruction) {
	a.insts = append(a.insts, insts...)
}

func (a *Assembler) Compile() (*RelocObject, error) {
	p := a.snapshot()

	sig, instrs, err := newCompiler(a.arch, p).compile()
	if err != nil {
		return nil, err
	}

	code, labels, relocs, err := p.resolve(a.arch, instrs)
	if err != nil {
		return nil, err
	}

	if err := a.buffer.Unseal(); err != nil {
		return nil, err
	}

	chunk, err := a.buffer.Append(code)
	if err != nil {
		_ = a.buffer.Seal()
		return nil, err
	}

	if err := a.buffer.Seal(); err != nil {
		return nil, err
	}

	for id, offset := range labels {
		a.global[id] = label{chunk: chunk, offset: offset}
	}

	a.reset()

	return &RelocObject{
		Chunk:  chunk,
		Sig:    sig,
		Instrs: instrs,
		Labels: labels,
		Relocs: relocs,
	}, nil
}

func (a *Assembler) Link(objects []*RelocObject) ([]Caller, error) {
	if err := a.buffer.Unseal(); err != nil {
		return nil, err
	}

	failed := make([]bool, len(objects))
	var firstErr error

	for i, obj := range objects {
		base := obj.Chunk.Ptr()

		for _, reloc := range obj.Relocs {
			target, ok := a.global[reloc.Label]
			if !ok {
				failed[i] = true
				if firstErr == nil {
					firstErr = fmt.Errorf("%w: label %d", ErrUnresolvedLabel, reloc.Label)
				}
				continue
			}

			src := unsafe.Add(base, reloc.Offset)
			dst := unsafe.Add(target.chunk.Ptr(), target.offset)
			rel := int64(uintptr(dst)) - int64(uintptr(src))

			inst := obj.Instrs[reloc.InstrIdx]
			inst.Src2 = Imm(rel)

			code, err := a.arch.Encoder.Encode(inst)
			if err != nil {
				failed[i] = true
				if firstErr == nil {
					firstErr = err
				}
				continue
			}

			writeBytes(src, code)
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
	a.global = make(map[int]label)
}

func (a *Assembler) reset() {
	a.stack = a.stack[:0]
	a.params = a.params[:0]
	a.insts = a.insts[:0]
	a.scratch = a.scratch[:0]
	a.returns = nil
	a.local = nil
	a.vregs.Reset()
}

func (a *Assembler) snapshot() program {
	return program{
		insts:   slices.Clone(a.insts),
		params:  slices.Clone(a.params),
		stack:   slices.Clone(a.stack),
		scratch: slices.Clone(a.scratch),
		returns: cloneReturns(a.returns),
		labels:  maps.Clone(a.local),
	}
}

func newCompiler(arch *Arch, p program) *compiler {
	c := &compiler{
		arch: arch,
		regs: NewRegAlloc(arch.Registers),
		prog: p,
		phys: make(map[int32]PReg),
		live: make(map[int32]PReg),
		own:  make(map[uint8]VReg),
	}

	for _, p := range p.scratch {
		c.regs.Block(p)
	}

	return c
}

func (c *compiler) compile() (*Signature, []Instruction, error) {
	if len(c.prog.params) > c.arch.ABI.MaxParams() {
		return nil, nil, ErrTooManyParams
	}

	for _, regs := range c.prog.returnSites() {
		if len(regs) > c.arch.ABI.MaxReturns() {
			return nil, nil, ErrTooManyReturns
		}
	}

	if err := c.reserveParams(); err != nil {
		return nil, nil, err
	}

	fixed, err := c.fixedReturns()
	if err != nil {
		return nil, nil, err
	}

	instrs, err := c.assign(fixed)
	if err != nil {
		return nil, nil, err
	}

	sig := c.signature()
	return sig, instrs, nil
}

func (c *compiler) reserveParams() error {
	for i, v := range c.prog.params {
		if i >= c.arch.ABI.MaxParams() {
			return ErrTooManyParams
		}

		p := NewPReg(uint8(i), v.Type(), v.Width())
		if err := c.regs.Reserve(v, p); err != nil {
			return err
		}

		c.bind(v, p)
	}

	return nil
}

func (c *compiler) fixedReturns() (map[int32]PReg, error) {
	fixed := make(map[int32]PReg)

	for _, regs := range c.prog.returnSites() {
		for i, v := range regs {
			p := NewPReg(uint8(i), v.Type(), v.Width())

			if existing, ok := fixed[v.ID()]; ok && existing.ID() != p.ID() {
				return nil, ErrConflictingReturnPin
			}

			fixed[v.ID()] = p
		}
	}

	return fixed, nil
}

func (c *compiler) assign(fixed map[int32]PReg) ([]Instruction, error) {
	last := c.lastRefs()

	for i, inst := range c.prog.insts {
		for _, v := range c.uses(inst) {
			if err := c.ensure(v); err != nil {
				return nil, err
			}
		}

		if dst, ok := c.def(inst); ok {
			if err := c.ensurePlaced(dst, fixed, last, i); err != nil {
				return nil, err
			}
		}

		for _, v := range c.uses(inst) {
			if last[v.ID()] != i {
				continue
			}
			if _, pinned := fixed[v.ID()]; pinned {
				continue
			}
			c.free(v)
		}
	}

	for id, p := range fixed {
		if _, ok := c.phys[id]; ok {
			continue
		}

		v := NewVReg(id, p.Type(), p.Width())
		if err := c.regs.Reserve(v, p); err != nil {
			return nil, err
		}

		c.bind(v, p)
	}

	return c.rewriteAll(), nil
}

func (c *compiler) ensure(v VReg) error {
	if _, ok := c.phys[v.ID()]; ok {
		return nil
	}

	p, err := c.regs.Alloc(v)
	if err != nil {
		return err
	}

	c.bind(v, p)
	return nil
}

func (c *compiler) ensurePlaced(v VReg, fixed map[int32]PReg, last map[int32]int, idx int) error {
	if _, ok := c.phys[v.ID()]; ok {
		return nil
	}

	p, err := c.place(v, fixed, last, idx)
	if err != nil {
		return err
	}

	c.bind(v, p)
	return nil
}

func (c *compiler) place(v VReg, fixed map[int32]PReg, last map[int32]int, idx int) (PReg, error) {
	want, pinned := fixed[v.ID()]
	if !pinned {
		return c.regs.Alloc(v)
	}

	owner, occupied := c.own[want.ID()]
	_, ownerPinned := fixed[owner.ID()]

	if occupied && owner.ID() != v.ID() && (last[owner.ID()] != idx || ownerPinned) {
		return c.regs.Alloc(v)
	}

	if occupied && owner.ID() != v.ID() {
		c.free(owner)
	}

	if err := c.regs.Reserve(v, want); err != nil {
		return PReg{}, err
	}

	return want, nil
}

func (c *compiler) bind(v VReg, p PReg) {
	c.phys[v.ID()] = p
	c.live[v.ID()] = p
	c.own[p.ID()] = v
}

func (c *compiler) free(v VReg) {
	p, ok := c.live[v.ID()]
	if !ok {
		return
	}

	c.regs.Free(v)
	delete(c.live, v.ID())
	delete(c.own, p.ID())
}

func (c *compiler) signature() *Signature {
	inputs := map[int][]PReg{
		0: abiRegs(c.prog.params),
	}

	outputs := make(map[int][]PReg)
	for idx, regs := range c.prog.returnSites() {
		outputs[idx] = abiRegs(regs)
	}

	return &Signature{
		Scratch: slices.Clone(c.prog.scratch),
		Inputs:  inputs,
		Outputs: outputs,
	}
}

func (c *compiler) lastRefs() map[int32]int {
	last := make(map[int32]int)

	for i, inst := range c.prog.insts {
		if dst, ok := c.def(inst); ok {
			last[dst.ID()] = i
		}
		for _, v := range c.uses(inst) {
			last[v.ID()] = i
		}
	}

	return last
}

func (c *compiler) rewriteAll() []Instruction {
	widths := c.widths()

	out := make([]Instruction, 0, len(c.prog.insts))
	for _, inst := range c.prog.insts {
		out = append(out, c.rewrite(inst, widths))
	}

	return out
}

func (c *compiler) rewrite(inst Instruction, widths map[int32]RegWidth) Instruction {
	return Instruction{
		Op:   inst.Op,
		Dst:  c.rewriteOp(inst.Dst, widths),
		Src1: c.rewriteOp(inst.Src1, widths),
		Src2: c.rewriteOp(inst.Src2, widths),
		Src3: c.rewriteOp(inst.Src3, widths),
	}
}

func (c *compiler) rewriteOp(op Operand, widths map[int32]RegWidth) Operand {
	switch v := op.(type) {
	case VRegOperand:
		p, ok := c.phys[v.Reg.ID()]
		if !ok {
			return op
		}
		return P(c.withWidth(p, v.Reg, widths))

	case MemOperand:
		base, ok := v.Base.(VRegOperand)
		if !ok {
			return op
		}

		p, ok := c.phys[base.Reg.ID()]
		if !ok {
			return op
		}

		return Mem(P(c.withWidth(p, base.Reg, widths)), v.Offset)

	default:
		return op
	}
}

func (c *compiler) withWidth(p PReg, v VReg, widths map[int32]RegWidth) PReg {
	width := v.Width()
	if width == WidthUndefined {
		width = widths[v.ID()]
	}
	return NewPReg(p.ID(), p.Type(), width)
}

func (c *compiler) widths() map[int32]RegWidth {
	widths := make(map[int32]RegWidth)

	set := func(v VReg) {
		if _, ok := widths[v.ID()]; !ok {
			widths[v.ID()] = v.Width()
		}
	}

	for _, v := range c.prog.params {
		set(v)
	}
	for _, v := range c.prog.stack {
		set(v)
	}
	for _, regs := range c.prog.returns {
		for _, v := range regs {
			set(v)
		}
	}
	for _, inst := range c.prog.insts {
		if dst, ok := c.def(inst); ok {
			set(dst)
		}
		for _, v := range c.uses(inst) {
			set(v)
		}
	}

	return widths
}

func (c *compiler) uses(inst Instruction) []VReg {
	var regs []VReg

	if r, ok := c.mbase(inst.Dst); ok {
		regs = append(regs, r)
	}

	for _, op := range []Operand{inst.Src1, inst.Src2, inst.Src3} {
		if r, ok := c.vreg(op); ok {
			regs = append(regs, r)
		}
	}

	return regs
}

func (c *compiler) def(inst Instruction) (VReg, bool) {
	dst, ok := inst.Dst.(VRegOperand)
	return dst.Reg, ok
}

func (c *compiler) vreg(op Operand) (VReg, bool) {
	switch v := op.(type) {
	case VRegOperand:
		return v.Reg, true
	case MemOperand:
		return c.mbase(v)
	default:
		return VReg{}, false
	}
}

func (c *compiler) mbase(op Operand) (VReg, bool) {
	mem, ok := op.(MemOperand)
	if !ok {
		return VReg{}, false
	}

	base, ok := mem.Base.(VRegOperand)
	if !ok {
		return VReg{}, false
	}

	return base.Reg, true
}

func (p program) returnSites() map[int][]VReg {
	if len(p.returns) != 0 {
		return p.returns
	}

	return map[int][]VReg{
		0: p.stack,
	}
}

func (p program) resolve(arch *Arch, phys []Instruction) ([]byte, map[int]int, []Relocation, error) {
	encoded := make([][]byte, len(phys))
	offsets := make([]int, len(phys)+1)

	for i, inst := range phys {
		if _, ok := inst.Src2.(LabelOperand); ok {
			inst.Src2 = Imm(0)
		}

		code, err := arch.Encoder.Encode(inst)
		if err != nil {
			return nil, nil, nil, err
		}

		encoded[i] = code
		offsets[i+1] = offsets[i] + len(code)
	}

	labels := make(map[int]int, len(p.labels))
	for id, idx := range p.labels {
		labels[id] = offsets[idx]
	}

	code := make([]byte, 0, offsets[len(phys)])
	var relocs []Relocation

	for i, inst := range phys {
		lbl, ok := inst.Src2.(LabelOperand)
		if !ok {
			code = append(code, encoded[i]...)
			continue
		}

		target, local := labels[lbl.ID]
		if !local {
			relocs = append(relocs, Relocation{
				InstrIdx: i,
				Offset:   offsets[i],
				Label:    lbl.ID,
			})
			code = append(code, encoded[i]...)
			continue
		}

		inst.Src2 = Imm(int64(target - offsets[i]))

		patch, err := arch.Encoder.Encode(inst)
		if err != nil {
			return nil, nil, nil, err
		}

		code = append(code, patch...)
	}

	return code, labels, relocs, nil
}

func abiRegs(vregs []VReg) []PReg {
	regs := make([]PReg, len(vregs))
	for i, v := range vregs {
		regs[i] = NewPReg(uint8(i), v.Type(), v.Width())
	}
	return regs
}

func cloneReturns(in map[int][]VReg) map[int][]VReg {
	if len(in) == 0 {
		return nil
	}

	out := make(map[int][]VReg, len(in))
	for idx, regs := range in {
		out[idx] = slices.Clone(regs)
	}
	return out
}
