package asm

import "fmt"

// rewriter transforms an instruction list whose operands reference virtual
// registers into one whose operands reference physical registers. It owns
// the linear-scan policy: ensure every vreg has a binding, release vregs
// at their last use, and substitute operands.
//
// run is a single API call but the work is split internally into two
// passes — first build the persistent vreg → preg assignment, then
// substitute operands using that assignment. Splitting the passes lets
// rewriteInst see the assignment of any vreg even after a pin-preemption
// has evicted the original holder from the pool.
type rewriter struct {
	pool     *regAlloc
	pins     map[int32]PReg
	last     map[int32]int
	assigned map[int32]PReg
	widths   map[int32]RegWidth
}

func newRewriter(info RegInfo, insts []Instruction, pins map[int32]PReg) *rewriter {
	pool := newRegAlloc(info)
	for i := uint8(0); i < 64; i++ {
		if info.Scratch.Contains(i) {
			pool.exclude(NewPReg(i, RegTypeInt, Width64))
			pool.exclude(NewPReg(i, RegTypeFloat, Width64))
		}
	}
	return &rewriter{
		pool:     pool,
		pins:     pins,
		last:     lastUses(insts),
		assigned: make(map[int32]PReg),
		widths:   make(map[int32]RegWidth),
	}
}

// run produces the rewritten instruction list. It first walks insts to
// build the persistent vreg → preg assignment, then walks them again to
// substitute every VReg / MemOperand base with its assigned PReg.
func (r *rewriter) run(insts []Instruction) ([]Instruction, error) {
	if err := r.assign(insts); err != nil {
		return nil, err
	}
	return r.rewrite(insts), nil
}

// assign walks insts in order, binding vregs as they are first used or
// defined and releasing them once their last use has been processed. The
// persistent assigned map records every vreg's binding; the pool releases
// dead vregs so later vregs can claim their slots. Final pass fills any
// widths still undefined from pin metadata.
func (r *rewriter) assign(insts []Instruction) error {
	for i, inst := range insts {
		for _, v := range inst.Uses() {
			if err := r.ensure(v); err != nil {
				return err
			}
		}
		if dst, ok := inst.Def(); ok {
			if err := r.ensure(dst); err != nil {
				return err
			}
		}
		for _, v := range inst.Uses() {
			if r.last[v.ID()] == i {
				r.pool.free(v)
			}
		}
	}
	for id, pr := range r.pins {
		if w, ok := r.widths[id]; !ok || w == WidthUndefined {
			r.widths[id] = pr.Width()
		}
	}
	return nil
}

// ensure binds v to a preg if it is not already bound. Pinned vregs evict
// any conflicting holder of their target slot before reserving.
func (r *rewriter) ensure(v VReg) error {
	r.recordWidth(v)
	if _, ok := r.pool.bindings[v.ID()]; ok {
		return nil
	}
	if pin, ok := r.pins[v.ID()]; ok {
		if id, busy := r.pool.owner(pin); busy && id != v.ID() {
			r.pool.free(NewVReg(id, pin.Type(), pin.Width()))
		}
		if err := r.pool.reserve(v, pin); err != nil {
			return fmt.Errorf("%w: vreg %v pin %v: %w", ErrConflictingPin, v, pin, err)
		}
		r.assigned[v.ID()] = pin
		return nil
	}
	pr, err := r.pool.alloc(v)
	if err != nil {
		return err
	}
	r.assigned[v.ID()] = pr
	return nil
}

func (r *rewriter) recordWidth(v VReg) {
	if v.Width() == WidthUndefined {
		return
	}
	if _, ok := r.widths[v.ID()]; ok {
		return
	}
	r.widths[v.ID()] = v.Width()
}

// rewrite returns a copy of insts with every VReg / MemOperand-base
// rewritten to its assigned PReg.
func (r *rewriter) rewrite(insts []Instruction) []Instruction {
	out := make([]Instruction, len(insts))
	for i, inst := range insts {
		out[i] = r.rewriteInst(inst)
	}
	return out
}

func (r *rewriter) rewriteInst(inst Instruction) Instruction {
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

// resolve looks up v's persistent preg assignment and produces a PReg
// with v's declared width, falling back to the collected widths map when
// v itself carries WidthUndefined.
func (r *rewriter) resolve(v VReg) (PReg, bool) {
	pr, ok := r.assigned[v.ID()]
	if !ok {
		return PReg{}, false
	}
	w := v.Width()
	if w == WidthUndefined {
		w = r.widths[v.ID()]
	}
	return NewPReg(pr.ID(), pr.Type(), w), true
}

// lastUses returns the highest instruction index at which each vreg is
// referenced (use or def).
func lastUses(insts []Instruction) map[int32]int {
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
