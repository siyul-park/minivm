package asm

import (
	"errors"
	"fmt"
)

// rewriter transforms an instruction list whose operands reference virtual
// registers into one whose operands reference physical registers. It owns
// both the physical-register pool and the register-allocation policy: bind
// each vreg as it is used or defined, release it at its last reference
// (read or write), and — when the bank is exhausted — spill the value whose
// final use is farthest away to a stack slot, reloading it on demand.
//
// Virtual registers are dense, so every per-vreg fact lives in one indexed
// table rather than a map, and owners is its inverse: a bank slot maps to
// the vreg holding it. Slots are width-insensitive, so X0 and W0 name the
// same slot.
//
// run is a single forward pass over the instruction stream. Spill
// eligibility alone consults a control-flow graph built once up front (see
// block.go, dominance.go, uses.go): whether a store actually dominates
// every reload, rather than a flat last-use index whose only question was
// "did a label sit in between." Register-release timing keeps the flat
// index — see dead — since spilling is the one decision where a wrong
// answer merely declines a spill, while a wrong release can silently evict
// an unrelated pinned value. Spilling inserts reload/store instructions and
// a stack frame, so run also rebases the caller's label→index table onto
// the rewritten stream.
type rewriter struct {
	frame Frame

	dom  *dominance
	uses useIndex

	// barriers holds the position of every self-recursive call — a call
	// whose target label is bound at or before the call itself — ascending.
	// See crosses.
	barriers []int

	// hazards indexes, per vreg id, every loop-governed self-referencing
	// redefinition. See crosses and carry.go.
	hazards [][]hazard

	regs   []vreg
	owners [2][bankSize]int32

	avail       [2]RegMask
	allocatable [2]RegMask

	slots int
	freed []int

	out []Instruction
}

// vreg is the rewriter's state for one virtual register.
type vreg struct {
	pin PReg
	reg PReg

	last  int
	guard int
	slot  int

	width RegWidth

	pinned  bool
	mapped  bool
	bound   bool
	spilled bool
}

// MaxSpillSlots caps how many spill slots the allocator may use for one
// build. arm64.SpillBytes derives the arm64 invoke trampoline's spill
// reserve from this constant (see docs/jit-internals.md and
// asm/arm64/abi_arm64.s); changing it without hand-updating the
// trampoline's stack-reserve literal to match arm64.StackReserve fails
// TestARM64_StackReserve (interp/tier_test.go) instead of corrupting the
// native stack at runtime.
const MaxSpillSlots = 512

// bankSize is how many physical register slots one bank can hold, fixed by
// RegMask's width.
const bankSize = 64

var ErrNoRegistersAvailable = errors.New("no registers available")

// newRewriter prepares the register table for insts. count is how many
// virtual registers the assembler handed out; every referenced vreg id must
// fall below it.
func newRewriter(arch Arch, insts []Instruction, pins map[int32]PReg, count int) (*rewriter, error) {
	info := arch.Registers()
	frame := arch.Frame()
	r := &rewriter{
		frame: frame,
		regs:  make([]vreg, count),
		avail: [2]RegMask{
			RegTypeInt:   info.Allocatable(RegTypeInt),
			RegTypeFloat: info.Allocatable(RegTypeFloat),
		},
		allocatable: [2]RegMask{
			RegTypeInt:   info.Allocatable(RegTypeInt) &^ info.Scratch,
			RegTypeFloat: info.Allocatable(RegTypeFloat),
		},
	}
	for i := range r.regs {
		r.regs[i] = vreg{last: -1, guard: -1, slot: -1}
	}
	for i := range r.owners {
		for slot := range r.owners[i] {
			r.owners[i][slot] = -1
		}
	}
	if err := r.scan(insts); err != nil {
		return nil, err
	}
	for id, preg := range pins {
		if err := r.check(id); err != nil {
			return nil, err
		}
		if !info.valid(preg) {
			return nil, fmt.Errorf("%w: pin virtual register %d to %v", ErrInvalidOperand, id, preg)
		}
		r.regs[id].pin = preg
		r.regs[id].pinned = true
		r.regs[id].width = preg.Width()
	}
	return r, nil
}

// scan records, for every referenced vreg, the highest instruction index
// that references it and the first width it declares. A pin overrides the
// declared width afterwards.
func (r *rewriter) scan(insts []Instruction) error {
	for i, inst := range insts {
		uses, n := inst.uses()
		for _, v := range uses[:n] {
			if err := r.note(v, i); err != nil {
				return err
			}
		}
		if dst, ok := inst.def(); ok {
			if err := r.note(dst, i); err != nil {
				return err
			}
		}
	}
	return nil
}

// note records that the instruction at at references v, keeping the highest
// index and the width v declares first.
func (r *rewriter) note(v VReg, at int) error {
	if err := r.check(v.ID()); err != nil {
		return err
	}
	if v.Type() != RegTypeInt && v.Type() != RegTypeFloat {
		return fmt.Errorf("%w: virtual register %d type %d", ErrInvalidOperand, v.ID(), v.Type())
	}
	s := &r.regs[v.ID()]
	s.last = at
	if s.width == WidthUndefined {
		s.width = v.Width()
	}
	return nil
}

// check rejects a vreg the assembler never handed out, so every later
// lookup can index the register table directly.
func (r *rewriter) check(id int32) error {
	if id < 0 || int(id) >= len(r.regs) {
		return fmt.Errorf("%w: virtual register %d", ErrInvalidOperand, id)
	}
	return nil
}

// run produces the rewritten instruction list together with the rebased
// label table. The control-flow graph, dominance, and use index below back
// spill eligibility alone (see crosses); without a frame spilling is
// impossible, and there is no arch to ask Returns/Calls/Jumps of in the
// first place, so building them is skipped entirely.
func (r *rewriter) run(insts []Instruction, labels map[Label]int) ([]Instruction, map[Label]int, error) {
	if r.frame != nil {
		g := buildCFG(insts, labels, r.frame)
		r.dom = newDominance(g)
		r.uses = buildUseIndex(insts, len(r.regs))
		r.barriers = barriers(insts, labels, r.frame)
		r.hazards = carryHazards(insts, g, r.dom, len(r.regs))
	}

	moved := make([]int, len(insts)+1)
	for i, inst := range insts {
		moved[i] = len(r.out)
		if err := r.step(i, inst); err != nil {
			return nil, nil, err
		}
	}
	moved[len(insts)] = len(r.out)

	final, rebased := r.inject(labels, moved)
	return final, rebased, nil
}

// step binds every register the instruction at i touches, appends its
// physical-register form, then releases whatever dies at i.
func (r *rewriter) step(i int, inst Instruction) error {
	uses, n := inst.uses()
	for _, v := range uses[:n] {
		if err := r.use(v, i); err != nil {
			return err
		}
	}
	dst, defines := inst.def()
	if defines {
		if _, err := r.bind(dst, i); err != nil {
			return err
		}
		r.regs[dst.ID()].spilled = false
	}

	r.out = append(r.out, r.substitute(inst))

	for _, v := range uses[:n] {
		if r.dead(v.ID(), i) {
			r.release(v.ID())
		}
	}
	// A def that is never read again is dead after this instruction; the use
	// loop above only frees operands that appear as reads.
	if defines && r.dead(dst.ID(), i) {
		r.release(dst.ID())
	}
	return nil
}

// dead reports whether id's binding may be released right after
// instruction i: scan recorded i as the last instruction referencing id at
// all, by either a read or a write, so nothing later in the stream — not
// even a later definition that reads id's current value in place (a
// multi-instruction immediate load's MOVK chain, say) — still needs it.
//
// This deliberately stays flat rather than asking the per-vreg use index
// (built for spill eligibility; see crosses): that index only tracks reads,
// and a pinned vreg released between two of its writes would be reserved
// back into its fixed physical slot on the next one, silently evicting —
// with no spill, since reserve does not call spill — whatever unrelated
// vreg claimed that slot while it looked free.
func (r *rewriter) dead(id int32, i int) bool {
	return r.regs[id].last == i
}

// use ensures v occupies a physical register before the instruction that
// reads it, reloading it first when it was spilled.
func (r *rewriter) use(v VReg, at int) error {
	s := &r.regs[v.ID()]
	reload := s.spilled && !s.bound

	preg, err := r.bind(v, at)
	if err != nil {
		return err
	}
	if reload {
		r.out = append(r.out, r.frame.Reload(preg, s.slot))
	}
	s.spilled = false
	return nil
}

// bind guarantees v holds a physical register and marks it as off-limits to
// eviction while the instruction at at is being rewritten, so satisfying one
// operand never displaces another.
func (r *rewriter) bind(v VReg, at int) (PReg, error) {
	s := &r.regs[v.ID()]
	s.guard = at
	switch {
	case s.bound:
		return s.reg, nil
	case s.pinned:
		r.reserve(v.ID(), s.pin)
		return s.pin, nil
	default:
		return r.obtain(v, at)
	}
}

// reserve hands id a specific physical register, evicting whichever vreg
// currently holds that bank slot.
func (r *rewriter) reserve(id int32, preg PReg) {
	if held := r.owners[preg.Type()][preg.ID()]; held >= 0 && held != id {
		r.free(held)
	}
	r.avail[preg.Type()] = r.avail[preg.Type()].Clear(preg.ID())
	r.own(id, preg)
}

// obtain draws a physical register for v, spilling the value whose last use
// lies farthest ahead when the bank is exhausted and the arch supplies a
// spill frame.
func (r *rewriter) obtain(v VReg, at int) (PReg, error) {
	if preg, ok := r.alloc(v); ok {
		return preg, nil
	}
	if r.frame == nil || v.Type() != RegTypeInt {
		return PReg{}, ErrNoRegistersAvailable
	}
	victim, ok := r.victim(at)
	if !ok {
		return PReg{}, ErrNoRegistersAvailable
	}
	if err := r.spill(victim); err != nil {
		return PReg{}, err
	}
	preg, ok := r.alloc(v)
	if !ok {
		return PReg{}, ErrNoRegistersAvailable
	}
	return preg, nil
}

// alloc claims the lowest free physical register from v's bank.
func (r *rewriter) alloc(v VReg) (PReg, bool) {
	typ := v.Type()
	id := (r.avail[typ] & r.allocatable[typ]).First()
	if id == 0xFF {
		return PReg{}, false
	}
	preg := NewPReg(id, typ, v.Width())
	r.avail[typ] = r.avail[typ].Clear(id)
	r.own(v.ID(), preg)
	return preg, true
}

// victim selects the bound integer vreg whose last use lies farthest ahead
// — the value least likely to be needed soon. Pinned registers, every
// register the instruction at at touches, and every value whose store would
// not dominate its reload are never chosen.
func (r *rewriter) victim(at int) (int32, bool) {
	best := int32(-1)
	last := -1
	for _, id := range r.owners[RegTypeInt] {
		if id < 0 {
			continue
		}
		s := &r.regs[id]
		if s.pinned || s.guard == at || s.last <= last || r.crosses(at, id) {
			continue
		}
		last = s.last
		best = id
	}
	return best, best >= 0
}

// spill writes id's live value to its stack slot and returns the register it
// held to the pool.
func (r *rewriter) spill(id int32) error {
	s := &r.regs[id]
	if s.slot < 0 {
		slot, ok := r.reserveSlot()
		if !ok {
			return ErrNoRegistersAvailable
		}
		s.slot = slot
	}
	r.out = append(r.out, r.frame.Store(s.slot, s.reg))
	r.free(id)
	s.spilled = true
	return nil
}

// reserveSlot hands out a spill slot, reusing one a dead vreg released
// before widening the frame.
func (r *rewriter) reserveSlot() (int, bool) {
	if n := len(r.freed); n > 0 {
		slot := r.freed[n-1]
		r.freed = r.freed[:n-1]
		return slot, true
	}
	if r.slots >= MaxSpillSlots {
		return 0, false
	}
	r.slots++
	return r.slots - 1, true
}

// release returns id's register and spill slot at its last use.
func (r *rewriter) release(id int32) {
	r.free(id)
	s := &r.regs[id]
	s.spilled = false
	if s.slot >= 0 {
		r.freed = append(r.freed, s.slot)
		s.slot = -1
	}
}

// free returns id's bank slot to the available pool, leaving the recorded
// assignment intact for resolve.
func (r *rewriter) free(id int32) {
	s := &r.regs[id]
	if !s.bound {
		return
	}
	typ := s.reg.Type()
	r.owners[typ][s.reg.ID()] = -1
	r.avail[typ] = r.avail[typ].Set(s.reg.ID())
	s.bound = false
}

// own records that id holds preg's bank slot.
func (r *rewriter) own(id int32, preg PReg) {
	r.free(id)
	s := &r.regs[id]
	s.reg = preg
	s.mapped = true
	s.bound = true
	r.owners[preg.Type()][preg.ID()] = id
}

// substitute returns a copy of inst with every VReg — including a
// MemOperand base — replaced by its bound physical register.
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
		if preg, ok := r.resolve(v.Reg); ok {
			return P(preg)
		}
	case MemOperand:
		vbase, isVReg := v.Base.(VRegOperand)
		if !isVReg {
			break
		}
		if preg, ok := r.resolve(vbase.Reg); ok {
			return Mem(P(preg), v.Offset)
		}
	}
	return op
}

// resolve names the physical register standing in for v, viewed at v's
// declared width or, when v carries none, at the width first recorded for
// it.
//
// It reads the most recent assignment rather than only the live binding: one
// instruction may write a pinned register that one of its own sources also
// occupies (a self-move such as MOV SP, SP), and binding the destination
// evicts that source before substitution reads it. The recorded assignment
// still names the correct physical register for the instruction being
// emitted.
func (r *rewriter) resolve(v VReg) (PReg, bool) {
	s := r.regs[v.ID()]
	if !s.mapped {
		return PReg{}, false
	}
	width := v.Width()
	if width == WidthUndefined {
		width = s.width
	}
	return NewPReg(s.reg.ID(), s.reg.Type(), width), true
}

// inject finalizes the rewritten stream. With no spills the body passes
// through and labels rebase onto the shifted instruction indices. Otherwise
// a frame prologue is prepended, a frame epilogue precedes every return, and
// labels are rebased across both the per-instruction shifts and the inserted
// frame instructions. Internal branches therefore skip the prologue, so a
// back-edge cannot reserve the frame twice.
//
// Resume follows only a call whose target label is bound here: a call
// resolved elsewhere never runs this epilogue, so re-reserving the spill
// area after it would double-adjust SP.
func (r *rewriter) inject(labels map[Label]int, moved []int) ([]Instruction, map[Label]int) {
	rebased := make(map[Label]int, len(labels))
	if r.slots == 0 {
		for id, idx := range labels {
			rebased[id] = moved[idx]
		}
		return r.out, rebased
	}

	enter := r.frame.Enter(r.slots)
	leave := r.frame.Leave(r.slots)
	resume := r.frame.Resume(r.slots)

	final := make([]Instruction, 0, len(enter)+len(r.out)+len(leave))
	final = append(final, enter...)
	framed := make([]int, len(r.out)+1)
	for i, inst := range r.out {
		framed[i] = len(final)
		if r.frame.Returns(inst.Op) {
			final = append(final, leave...)
		}
		final = append(final, inst)
		if lbl, ok := inst.Src2.(LabelOperand); ok && r.frame.Calls(inst.Op) {
			if _, bound := labels[lbl.ID]; bound {
				final = append(final, resume...)
			}
		}
	}
	framed[len(r.out)] = len(final)

	for id, idx := range labels {
		rebased[id] = framed[moved[idx]]
	}
	return final, rebased
}
