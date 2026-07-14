package asm

import (
	"errors"
	"fmt"
)

// rewriter transforms an instruction list whose operands reference virtual
// registers into one whose operands reference physical registers. It owns
// the linear-scan policy: bind each vreg as it is used or defined, release
// vregs at their last use, and — when the physical bank is exhausted —
// spill the value whose next use is farthest away to a stack slot,
// reloading it on demand.
//
// run is a single forward pass. Spilling inserts reload/store instructions
// and a stack frame, so run also remaps the caller's label→index table to
// account for the inserted instructions.
type rewriter struct {
	pool  *regAlloc
	frame Frame
	pins  map[int32]PReg
	last  map[int32]int

	widths   map[int32]RegWidth
	assigned map[int32]PReg
	spilled  map[int32]bool
	slotOf   map[int32]int
	free     []int
	slots    int

	out []Instruction
}

// MaxSpillSlots caps how many spill slots the allocator may use for one
// Code. It sizes the spill area the arm64 invoke trampoline reserves on its
// native stack frame (see docs/jit-internals.md and asm/arm64/abi_arm64.s);
// changing it without updating the trampoline's reserve and interp's
// nativeFrameLimit breaks that arithmetic invariant silently.
const MaxSpillSlots = 512

func newRewriter(arch Arch, insts []Instruction, pins map[int32]PReg) *rewriter {
	info := arch.Registers()
	pool := newRegAlloc(info)
	for i := uint8(0); i < 64; i++ {
		if info.Scratch.Contains(i) {
			pool.exclude(NewPReg(i, RegTypeInt, Width64))
		}
	}
	r := &rewriter{
		pool:     pool,
		frame:    arch.Frame(),
		pins:     pins,
		widths:   make(map[int32]RegWidth),
		assigned: make(map[int32]PReg),
		spilled:  make(map[int32]bool),
		slotOf:   make(map[int32]int),
	}
	r.last = r.scanLastUses(insts)
	for id, pr := range pins {
		r.widths[id] = pr.Width()
	}
	return r
}

// run produces the rewritten instruction list together with the remapped
// label table. It walks insts once, binding and spilling as it goes and
// substituting every operand's vreg with its current physical register,
// then injects the spill frame and returns labels rebased onto the
// rewritten stream.
func (r *rewriter) run(insts []Instruction, labels map[Label]int, entries []Label) ([]Instruction, map[Label]int, error) {
	for i, inst := range insts {
		lbl, ok := inst.Src2.(LabelOperand)
		if !ok {
			continue
		}
		if pos, intra := labels[lbl.ID]; intra && pos <= i {
			r.frame = nil
			break
		}
	}

	newIdx := make([]int, len(insts)+1)
	for i, inst := range insts {
		newIdx[i] = len(r.out)
		protected := make(map[int32]bool)
		for _, v := range inst.Uses() {
			if err := r.use(v, protected); err != nil {
				return nil, nil, err
			}
		}
		if dst, ok := inst.Def(); ok {
			if err := r.define(dst, protected); err != nil {
				return nil, nil, err
			}
		}
		r.out = append(r.out, r.substitute(inst))
		for _, v := range inst.Uses() {
			if r.last[v.ID()] == i {
				r.release(v)
			}
		}
	}
	newIdx[len(insts)] = len(r.out)
	return r.inject(labels, entries, newIdx)
}

// use ensures v occupies a physical register before the instruction that
// reads it. A pinned vreg reserves its slot; a spilled vreg reloads; an
// unbound vreg allocates. protected records every vreg the current
// instruction touches so satisfying one operand never evicts another.
func (r *rewriter) use(v VReg, protected map[int32]bool) error {
	r.note(v)
	if _, ok := r.pool.bindings[v.ID()]; ok {
		protected[v.ID()] = true
		return nil
	}
	if pin, ok := r.pins[v.ID()]; ok {
		return r.pin(v, pin, protected)
	}
	if r.spilled[v.ID()] {
		pr, err := r.obtain(v, protected)
		if err != nil {
			return err
		}
		r.out = append(r.out, r.frame.Reload(pr, r.slotOf[v.ID()]))
		delete(r.spilled, v.ID())
		protected[v.ID()] = true
		return nil
	}
	if _, err := r.obtain(v, protected); err != nil {
		return err
	}
	protected[v.ID()] = true
	return nil
}

// define ensures v's destination register exists before the instruction
// that writes it.
func (r *rewriter) define(v VReg, protected map[int32]bool) error {
	r.note(v)
	if _, ok := r.pool.bindings[v.ID()]; ok {
		protected[v.ID()] = true
		return nil
	}
	if pin, ok := r.pins[v.ID()]; ok {
		return r.pin(v, pin, protected)
	}
	if _, err := r.obtain(v, protected); err != nil {
		return err
	}
	protected[v.ID()] = true
	return nil
}

// pin reserves v's pinned physical register, evicting any other vreg that
// currently holds the slot.
func (r *rewriter) pin(v VReg, pin PReg, protected map[int32]bool) error {
	if id, busy := r.pool.owner(pin); busy && id != v.ID() {
		r.pool.free(NewVReg(id, pin.Type(), pin.Width()))
	}
	if err := r.pool.reserve(v, pin); err != nil {
		return fmt.Errorf("%w: vreg %v pin %v: %w", ErrConflictingPin, v, pin, err)
	}
	r.assigned[v.ID()] = pin
	protected[v.ID()] = true
	return nil
}

// obtain returns a physical register for v, drawing from the free pool or,
// when the bank is exhausted and the arch supports a spill frame, evicting
// the farthest-future integer value to a stack slot first.
func (r *rewriter) obtain(v VReg, protected map[int32]bool) (PReg, error) {
	pr, err := r.pool.alloc(v)
	if err == nil {
		r.assigned[v.ID()] = pr
		return pr, nil
	}
	if !errors.Is(err, ErrNoRegistersAvailable) {
		return PReg{}, err
	}
	if r.frame == nil || v.Type() != RegTypeInt {
		return PReg{}, err
	}
	victim, ok := r.victim(protected)
	if !ok {
		return PReg{}, err
	}
	if err := r.spill(victim); err != nil {
		return PReg{}, err
	}
	pr, err = r.pool.alloc(v)
	if err == nil {
		r.assigned[v.ID()] = pr
	}
	return pr, err
}

// victim selects the bound integer vreg whose last use lies farthest ahead
// — the value least likely to be needed soon. Pinned, protected, and
// non-integer bindings are never chosen.
func (r *rewriter) victim(protected map[int32]bool) (int32, bool) {
	best := int32(-1)
	bestLast := -1
	for id, pr := range r.pool.bindings {
		if pr.Type() != RegTypeInt {
			continue
		}
		if _, pinned := r.pins[id]; pinned {
			continue
		}
		if protected[id] {
			continue
		}
		if l := r.last[id]; l > bestLast {
			bestLast = l
			best = id
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

// spill writes the victim's live value to its stack slot and frees the
// register it held.
func (r *rewriter) spill(id int32) error {
	pr := r.pool.bindings[id]
	slot, ok := r.slotFor(id)
	if !ok {
		return ErrNoRegistersAvailable
	}
	r.out = append(r.out, r.frame.Store(slot, pr))
	r.pool.free(NewVReg(id, pr.Type(), pr.Width()))
	r.spilled[id] = true
	return nil
}

// slotFor returns id's stable spill slot, assigning a fresh one on first
// spill.
func (r *rewriter) slotFor(id int32) (int, bool) {
	if s, ok := r.slotOf[id]; ok {
		return s, true
	}
	var s int
	if n := len(r.free); n > 0 {
		s = r.free[n-1]
		r.free = r.free[:n-1]
	} else {
		if r.slots >= MaxSpillSlots {
			return 0, false
		}
		s = r.slots
		r.slots++
	}
	r.slotOf[id] = s
	return s, true
}

// release returns v's register to the pool at its last use and clears any
// spill bookkeeping.
func (r *rewriter) release(v VReg) {
	if _, ok := r.pool.bindings[v.ID()]; ok {
		r.pool.free(v)
	}
	delete(r.spilled, v.ID())
	if s, ok := r.slotOf[v.ID()]; ok {
		r.free = append(r.free, s)
		delete(r.slotOf, v.ID())
	}
}

// note records v's declared width the first time it is seen so operands
// carrying WidthUndefined can resolve to the right physical-register view.
func (r *rewriter) note(v VReg) {
	if w := v.Width(); w != WidthUndefined {
		if _, ok := r.widths[v.ID()]; !ok {
			r.widths[v.ID()] = w
		}
	}
}

// inject finalizes the rewritten stream. With no spills the body passes
// through and labels rebase onto the (1:1) instruction indices. Otherwise a
// frame prologue is prepended, a frame epilogue precedes every return, and
// labels are rebased across both the per-instruction shifts and the
// inserted frame instructions. Resume is injected only after a call whose
// target label is bound in this Code: an external call (resolved later by
// Link) never runs this Code's epilogue, so re-reserving the spill area
// after it would double-adjust SP.
func (r *rewriter) inject(labels map[Label]int, entries []Label, newIdx []int) ([]Instruction, map[Label]int, error) {
	if r.slots == 0 || r.frame == nil {
		out := make(map[Label]int, len(labels))
		for id, idx := range labels {
			out[id] = newIdx[idx]
		}
		return r.out, out, nil
	}

	enter := r.frame.Enter(r.slots)
	leave := r.frame.Leave(r.slots)
	entry := make(map[Label]bool, len(entries))
	for _, id := range entries {
		if labels[id] != 0 {
			return nil, nil, fmt.Errorf("%w: entry=%d offset=%d", ErrEntryRequiresFrame, id, labels[id])
		}
		entry[id] = true
	}
	final := make([]Instruction, 0, len(enter)+len(r.out)+len(leave))
	final = append(final, enter...)

	bodyToFinal := make([]int, len(r.out)+1)
	for bi, inst := range r.out {
		bodyToFinal[bi] = len(final)
		if r.frame.Returns(inst.Op) {
			final = append(final, leave...)
		}
		final = append(final, inst)
		if lbl, isLabel := inst.Src2.(LabelOperand); isLabel && r.frame.Calls(inst.Op) {
			if _, intra := labels[lbl.ID]; intra {
				final = append(final, r.frame.Resume(r.slots)...)
			}
		}
	}
	bodyToFinal[len(r.out)] = len(final)

	// Internal branches skip the prologue so a back-edge cannot reserve the
	// frame again. An external entry bound at instruction zero is different:
	// its callable must start at byte zero and execute Enter before the body.
	out := make(map[Label]int, len(labels))
	for id, idx := range labels {
		if entry[id] {
			out[id] = 0
			continue
		}
		out[id] = bodyToFinal[newIdx[idx]]
	}
	return final, out, nil
}

// substitute returns a copy of inst with every VReg / MemOperand base
// replaced by its currently bound physical register.
func (r *rewriter) substitute(inst Instruction) Instruction {
	return Instruction{
		Op:   inst.Op,
		Dst:  r.rewriteOp(inst.Dst),
		Src1: r.rewriteOp(inst.Src1),
		Src2: r.rewriteOp(inst.Src2),
		Src3: r.rewriteOp(inst.Src3),
	}
}

func (r *rewriter) rewriteOp(op Operand) Operand {
	switch v := op.(type) {
	case VRegOperand:
		if pr, ok := r.resolve(v.Reg); ok {
			return P(pr)
		}
	case MemOperand:
		base, isVReg := v.Base.(VRegOperand)
		if !isVReg {
			break
		}
		if pr, ok := r.resolve(base.Reg); ok {
			return Mem(P(pr), v.Offset)
		}
	}
	return op
}

// resolve looks up v's physical register and produces a PReg with v's
// declared width, falling back to the collected widths map when v itself
// carries WidthUndefined.
//
// It prefers the live binding but falls back to the last-recorded
// assignment: a single instruction may write a pinned register that one of
// its own sources also occupies (a self-move such as MOV SP, SP), and
// binding the destination evicts that source before substitution reads it.
// The recorded assignment still names the correct physical register for
// the instruction being emitted.
func (r *rewriter) resolve(v VReg) (PReg, bool) {
	pr, ok := r.pool.bindings[v.ID()]
	if !ok {
		pr, ok = r.assigned[v.ID()]
	}
	if !ok {
		return PReg{}, false
	}
	w := v.Width()
	if w == WidthUndefined {
		w = r.widths[v.ID()]
	}
	return NewPReg(pr.ID(), pr.Type(), w), true
}

// scanLastUses returns the highest instruction index at which each vreg is
// referenced (use or def).
func (*rewriter) scanLastUses(insts []Instruction) map[int32]int {
	last := make(map[int32]int)
	for i, inst := range insts {
		if dst, ok := inst.Def(); ok {
			last[dst.ID()] = i
		}
		for _, v := range inst.Uses() {
			last[v.ID()] = i
		}
	}
	return last
}
