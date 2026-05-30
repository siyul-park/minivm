package asm

import "fmt"

// scanner is the linear-scan register-allocation policy that orchestrates
// regPool over an instruction list. For each instruction it ensures every
// vreg referenced has a binding (auto-allocated, or reserved when pinned),
// then releases vregs whose last use has passed.
//
// scanner produces two outputs:
//
//   - assigned: the persistent vreg → preg map (survives free).
//   - widths: the effective RegWidth per vreg, back-filled from defining
//     sites and pin metadata so undefined-width operand uses can resolve.
type scanner struct {
	pool     *regPool
	pins     map[int32]PReg
	last     map[int32]int
	assigned map[int32]PReg
	widths   map[int32]RegWidth
}

func newScanner(info RegInfo, insts []Instruction, pins map[int32]PReg) *scanner {
	pool := newRegPool(info)
	for i := uint8(0); i < 64; i++ {
		if info.Scratch.Contains(i) {
			pool.exclude(NewPReg(i, RegTypeInt, Width64))
			pool.exclude(NewPReg(i, RegTypeFloat, Width64))
		}
	}
	return &scanner{
		pool:     pool,
		pins:     pins,
		last:     lastUses(insts),
		assigned: make(map[int32]PReg),
		widths:   make(map[int32]RegWidth),
	}
}

// run walks insts in order, binding vregs as they are first used or
// defined and releasing them once their last use has been processed.
func (s *scanner) run(insts []Instruction) error {
	for i, inst := range insts {
		for _, v := range inst.Uses() {
			if err := s.ensure(v); err != nil {
				return err
			}
		}
		if dst, ok := inst.Def(); ok {
			if err := s.ensure(dst); err != nil {
				return err
			}
		}
		for _, v := range inst.Uses() {
			if s.last[v.ID()] == i {
				s.pool.free(v)
			}
		}
	}
	s.backfill()
	return nil
}

// ensure binds v to a preg if it is not already bound. Pinned vregs evict
// any conflicting holder of their target slot before reserving.
func (s *scanner) ensure(v VReg) error {
	s.record(v)
	if _, ok := s.pool.bindings[v.ID()]; ok {
		return nil
	}
	if pin, ok := s.pins[v.ID()]; ok {
		if id, busy := s.pool.owner(pin); busy && id != v.ID() {
			s.pool.free(NewVReg(id, pin.Type(), pin.Width()))
		}
		if err := s.pool.reserve(v, pin); err != nil {
			return fmt.Errorf("%w: vreg %v pin %v: %w", ErrConflictingPin, v, pin, err)
		}
		s.assigned[v.ID()] = pin
		return nil
	}
	pr, err := s.pool.alloc(v)
	if err != nil {
		return err
	}
	s.assigned[v.ID()] = pr
	return nil
}

func (s *scanner) record(v VReg) {
	if v.Width() == WidthUndefined {
		return
	}
	if _, ok := s.widths[v.ID()]; ok {
		return
	}
	s.widths[v.ID()] = v.Width()
}

func (s *scanner) backfill() {
	for id, pr := range s.pins {
		if w, ok := s.widths[id]; !ok || w == WidthUndefined {
			s.widths[id] = pr.Width()
		}
	}
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
