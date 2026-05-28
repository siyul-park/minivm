package asm

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"unsafe"
)

// Assembler emits target-architecture assembly. It owns only assembly state:
// instructions, virtual registers, scratch registers, labels, and ABI sites.
// External call-convention policy (mapping VRegs to ABI PReg slots) is the
// caller's responsibility; the assembler simply honors Pin/Site declarations.
type Assembler struct {
	arch       *Arch
	buffer     *Buffer
	nextVRegID int32

	insts   []Instruction
	scratch []PReg
	pins    map[int32]PReg
	sites   map[int][]VReg
	err     error

	nextLabel int
	global    map[int]label
	local     map[int]int
	entries   map[int][]VReg
	aliases   map[int]int
}

type label struct {
	chunk  *Chunk
	offset int
}

type program struct {
	insts   []Instruction
	scratch []PReg
	pins    map[int32]PReg
	sites   map[int][]VReg
	labels  map[int]int
	entries map[int][]VReg
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
	ErrUnresolvedLabel = errors.New("unresolved label")
	ErrAliasCycle      = errors.New("label alias cycle")
	ErrConflictingPin  = fmt.Errorf("%w: conflicting pin", ErrInvalidArgs)
)

func NewAssembler(arch *Arch, buffer *Buffer) *Assembler {
	return &Assembler{
		arch:    arch,
		buffer:  buffer,
		pins:    make(map[int32]PReg),
		global:  make(map[int]label),
		aliases: make(map[int]int),
	}
}

// NewVReg allocates a fresh virtual register.
func (a *Assembler) NewVReg(typ RegType, width RegWidth) VReg {
	r := NewVReg(a.nextVRegID, typ, width)
	a.nextVRegID++
	return r
}

// Scratch reserves the next architecture scratch register. Scratch slots
// are pre-blocked from regalloc and surfaced in Signature.Scratch.
func (a *Assembler) Scratch() PReg {
	mask := a.arch.Scratch
	for range len(a.scratch) {
		_, mask = mask.PopFirst()
	}

	p := NewPReg(mask.First(), RegTypeInt, Width64)
	a.scratch = append(a.scratch, p)
	return p
}

// NewLabel reserves a label identifier; bind it later with Bind.
func (a *Assembler) NewLabel() int {
	id := a.nextLabel
	a.nextLabel++
	return id
}

// Bind anchors a label at the current instruction index.
func (a *Assembler) Bind(id int) {
	if a.local == nil {
		a.local = make(map[int]int)
	}
	a.local[id] = len(a.insts)
}

// Entry marks the current instruction position as a callable entry point.
// Live values use the ABI parameter register order at this boundary.
func (a *Assembler) Entry(label int, live []VReg) {
	if label < 0 {
		if a.err == nil {
			a.err = fmt.Errorf("%w: reserved entry label %d", ErrInvalidArgs, label)
		}
		return
	}
	a.Bind(label)
	if a.entries == nil {
		a.entries = make(map[int][]VReg)
	}
	a.entries[label] = slices.Clone(live)
	for i, v := range live {
		_ = a.Pin(v, NewPReg(uint8(i), v.Type(), v.Width()))
	}
}

// Alias defers a label resolution decision until Link.
func (a *Assembler) Alias(label, target int) {
	a.aliases[label] = target
}

// Emit appends one instruction and returns its index.
func (a *Assembler) Emit(inst Instruction) int {
	a.insts = append(a.insts, inst)
	return len(a.insts) - 1
}

// Emits appends multiple instructions.
func (a *Assembler) Emits(insts ...Instruction) {
	a.insts = append(a.insts, insts...)
}

// Index returns the next instruction slot (equivalent to len(insts)).
func (a *Assembler) Index() int {
	return len(a.insts)
}

// Pin binds a VReg to a specific PReg for the lifetime of this compile.
// Returns ErrConflictingPin if v was already pinned to a different PReg;
// the first conflict is also retained and re-surfaced by Compile so callers
// that batch many Pin calls can defer checking.
func (a *Assembler) Pin(v VReg, p PReg) error {
	if existing, ok := a.pins[v.ID()]; ok && existing.ID() != p.ID() {
		err := fmt.Errorf("%w: %v to %v and %v", ErrConflictingPin, v, existing, p)
		if a.err == nil {
			a.err = err
		}
		return err
	}
	a.pins[v.ID()] = p
	return nil
}

// Site declares an ABI boundary at instruction index idx. The given VRegs
// describe the values live across that boundary (entry params for idx=0,
// return values for branch/exit sites). Subsequent calls at the same idx
// overwrite.
func (a *Assembler) Site(idx int, live []VReg) {
	if a.sites == nil {
		a.sites = make(map[int][]VReg)
	}
	a.sites[idx] = slices.Clone(live)
}

// Compile finalizes the current instruction list into a RelocObject, runs
// register allocation, encodes the result into the buffer, and resets per-
// compile state. Labels declared by NewLabel/Bind remain globally resolvable
// across subsequent Compile calls via Link.
func (a *Assembler) Compile() (*RelocObject, error) {
	if a.err != nil {
		return nil, a.err
	}

	p := a.snapshot()

	sig, params, instrs, err := newCompiler(a.arch, p).compile()
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

	var entries map[int]Entry
	if len(params) > 0 {
		entries = make(map[int]Entry, len(params))
		for label, regs := range params {
			entries[label] = Entry{Offset: labels[label], Params: regs}
		}
	}

	return &RelocObject{
		Chunk:   chunk,
		Sig:     sig,
		Entries: entries,
		Instrs:  instrs,
		Relocs:  relocs,
	}, nil
}

// Link resolves cross-object label references and constructs a Caller for
// each successfully linked object. Objects with unresolved labels yield a
// nil Caller in the corresponding slot; the first error encountered is
// returned alongside the partial result.
func (a *Assembler) Link(objects []*RelocObject) ([]Caller, error) {
	if err := a.buffer.Unseal(); err != nil {
		return nil, err
	}

	failed := make([]bool, len(objects))
	var firstErr error

	for i, obj := range objects {
		base := obj.Chunk.Ptr()

		for _, reloc := range obj.Relocs {
			target, err := a.resolveLabel(reloc.Label)
			if err != nil {
				failed[i] = true
				if firstErr == nil {
					firstErr = err
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

		caller, err := a.arch.ABI.NewCaller(obj.Sig, obj.Chunk)
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

// CallerAt creates a caller for a callable label within a linked object.
func (a *Assembler) CallerAt(obj *RelocObject, label int) (Caller, error) {
	entry, ok := obj.Entries[label]
	if !ok {
		return nil, fmt.Errorf("%w: entry label %d", ErrUnresolvedLabel, label)
	}
	chunk, err := obj.Chunk.Slice(entry.Offset)
	if err != nil {
		return nil, err
	}
	sig := &Signature{
		Params:  entry.Params,
		Returns: obj.Sig.Returns,
		Scratch: obj.Sig.Scratch,
	}
	return a.arch.ABI.NewCaller(sig, chunk)
}

// Abort discards the current Compile's pending state.
func (a *Assembler) Abort() {
	a.reset()
}

// Reset clears all assembler state including globally tracked labels.
func (a *Assembler) Reset() {
	a.reset()
	a.nextLabel = 0
	a.global = make(map[int]label)
	a.aliases = make(map[int]int)
}

func (a *Assembler) reset() {
	a.insts = a.insts[:0]
	a.scratch = a.scratch[:0]
	a.pins = make(map[int32]PReg)
	a.sites = nil
	a.err = nil
	a.local = nil
	a.entries = nil
	a.nextVRegID = 0
}

func (a *Assembler) snapshot() program {
	sites := a.sites
	if len(sites) > 0 {
		out := make(map[int][]VReg, len(sites))
		for idx, regs := range sites {
			out[idx] = slices.Clone(regs)
		}
		sites = out
	}

	var entries map[int][]VReg
	if len(a.entries) > 0 {
		entries = make(map[int][]VReg, len(a.entries))
		for label, regs := range a.entries {
			entries[label] = slices.Clone(regs)
		}
	}

	return program{
		insts:   slices.Clone(a.insts),
		scratch: slices.Clone(a.scratch),
		pins:    maps.Clone(a.pins),
		sites:   sites,
		labels:  maps.Clone(a.local),
		entries: entries,
	}
}

func (a *Assembler) resolveLabel(id int) (label, error) {
	seen := make(map[int]bool)
	for {
		if seen[id] {
			return label{}, fmt.Errorf("%w: label %d", ErrAliasCycle, id)
		}
		seen[id] = true
		if target, ok := a.aliases[id]; ok {
			id = target
			continue
		}
		target, ok := a.global[id]
		if !ok {
			return label{}, fmt.Errorf("%w: label %d", ErrUnresolvedLabel, id)
		}
		return target, nil
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

func (c *compiler) compile() (*Signature, map[int][]PReg, []Instruction, error) {
	if entry, ok := c.prog.sites[0]; ok && len(entry) > c.arch.ABI.MaxParams() {
		return nil, nil, nil, ErrTooManyParams
	}
	for _, entry := range c.prog.entries {
		if len(entry) > c.arch.ABI.MaxParams() {
			return nil, nil, nil, ErrTooManyParams
		}
	}

	for idx, regs := range c.prog.sites {
		if idx == 0 {
			continue
		}
		if len(regs) > c.arch.ABI.MaxReturns() {
			return nil, nil, nil, ErrTooManyReturns
		}
	}

	instrs, err := c.assign()
	if err != nil {
		return nil, nil, nil, err
	}

	sig, entries := c.signature()
	return sig, entries, instrs, nil
}

func (c *compiler) assign() ([]Instruction, error) {
	last := c.lastRefs()
	exitPins := c.exitPins()

	// Reserve entry-site pins upfront so subsequent allocation cannot
	// claim their physical slots before they are used.
	for _, v := range c.prog.sites[0] {
		p, ok := c.prog.pins[v.ID()]
		if !ok {
			continue
		}
		if _, bound := c.phys[v.ID()]; bound {
			continue
		}
		if err := c.regs.Reserve(v, p); err != nil {
			return nil, err
		}
		c.bind(v, p)
	}

	for i, inst := range c.prog.insts {
		for _, v := range c.uses(inst) {
			if err := c.ensure(v); err != nil {
				return nil, err
			}
		}

		if dst, ok := c.def(inst); ok {
			if err := c.ensurePlaced(dst, exitPins, last, i); err != nil {
				return nil, err
			}
		}

		for _, v := range c.uses(inst) {
			if last[v.ID()] != i {
				continue
			}
			if exitPins[v.ID()] {
				continue
			}
			c.free(v)
		}
	}

	for id, p := range c.prog.pins {
		if _, ok := c.phys[id]; ok {
			continue
		}
		if !exitPins[id] {
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

// exitPins returns the set of VRegs whose pin must be honored at a return
// site (idx > 0). Entry-site (idx 0) pins only matter on function entry and
// can be reclaimed once their last use passes.
func (c *compiler) exitPins() map[int32]bool {
	out := make(map[int32]bool)
	for idx, regs := range c.prog.sites {
		if idx == 0 {
			continue
		}
		for _, v := range regs {
			if _, ok := c.prog.pins[v.ID()]; ok {
				out[v.ID()] = true
			}
		}
	}
	return out
}

func (c *compiler) ensure(v VReg) error {
	if _, ok := c.phys[v.ID()]; ok {
		return nil
	}

	if pinned, ok := c.prog.pins[v.ID()]; ok {
		if err := c.regs.Reserve(v, pinned); err != nil {
			return err
		}
		c.bind(v, pinned)
		return nil
	}

	p, err := c.regs.Alloc(v)
	if err != nil {
		return err
	}

	c.bind(v, p)
	return nil
}

func (c *compiler) ensurePlaced(v VReg, exitPins map[int32]bool, last map[int32]int, idx int) error {
	if _, ok := c.phys[v.ID()]; ok {
		return nil
	}

	p, err := c.place(v, exitPins, last, idx)
	if err != nil {
		return err
	}

	c.bind(v, p)
	return nil
}

func (c *compiler) place(v VReg, exitPins map[int32]bool, last map[int32]int, idx int) (PReg, error) {
	want, pinned := c.prog.pins[v.ID()]
	if !pinned {
		return c.regs.Alloc(v)
	}

	owner, occupied := c.own[want.ID()]
	ownerPinned := exitPins[owner.ID()]

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

func (c *compiler) signature() (*Signature, map[int][]PReg) {
	params := c.pregs(c.prog.sites[0])

	entries := make(map[int][]PReg, len(c.prog.entries))
	for label, regs := range c.prog.entries {
		entries[label] = c.pregs(regs)
	}

	returns := make(map[int][]PReg)
	for idx, regs := range c.prog.sites {
		if idx != 0 {
			returns[idx] = c.pregs(regs)
		}
	}

	sig := &Signature{
		Params:  params,
		Scratch: slices.Clone(c.prog.scratch),
		Returns: returns,
	}
	return sig, entries
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

		width := v.Reg.Width()
		if width == WidthUndefined {
			width = widths[v.Reg.ID()]
		}

		return P(NewPReg(p.ID(), p.Type(), width))

	case MemOperand:
		base, ok := v.Base.(VRegOperand)
		if !ok {
			return op
		}

		p, ok := c.phys[base.Reg.ID()]
		if !ok {
			return op
		}

		width := base.Reg.Width()
		if width == WidthUndefined {
			width = widths[base.Reg.ID()]
		}

		return Mem(P(NewPReg(p.ID(), p.Type(), width)), v.Offset)

	default:
		return op
	}
}

func (c *compiler) widths() map[int32]RegWidth {
	widths := make(map[int32]RegWidth)

	set := func(v VReg) {
		if _, ok := widths[v.ID()]; !ok {
			widths[v.ID()] = v.Width()
		}
	}

	for _, regs := range c.prog.sites {
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

func (c *compiler) pregs(vregs []VReg) []PReg {
	regs := make([]PReg, len(vregs))
	for i, v := range vregs {
		if p, ok := c.prog.pins[v.ID()]; ok {
			regs[i] = p
			continue
		}
		regs[i] = NewPReg(uint8(i), v.Type(), v.Width())
	}
	return regs
}
