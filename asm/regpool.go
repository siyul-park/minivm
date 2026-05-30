package asm

import "errors"

// regPool is the assembler's physical-register pool. It tracks which
// physical registers are free, currently bound to a vreg, or off-limits.
// regPool exposes the bookkeeping primitives — picking, reserving, and
// releasing — while leaving instruction-stream policy (linear scan,
// lifetime tracking, pin preemption) to its caller.
//
// Three masks govern selection:
//
//   - avail: physical registers free to be drawn by alloc.
//   - excluded: registers withheld from auto-alloc but still reservable
//     via Pin (for example, ABI scratch).
//   - blocked: registers that may not be used at all (FP, LR, etc.).
//
// Two maps track the current binding in both directions so callers can ask
// either "what preg does this vreg occupy?" or "who owns this preg slot?"
// without maintaining their own reverse table.
type regPool struct {
	info        RegInfo
	intAvail    RegMask
	floatAvail  RegMask
	intExcluded RegMask
	fltExcluded RegMask
	intBlocked  RegMask
	fltBlocked  RegMask

	bindings map[int32]PReg   // vreg id → preg
	owners   map[regKey]int32 // preg key (type+id) → vreg id
}

// regKey identifies a physical register slot independent of width — the
// 32-bit (W/S) and 64-bit (X/D) views of the same id share the same slot.
type regKey struct {
	typ RegType
	id  uint8
}

var ErrNoRegistersAvailable = errors.New("no registers available")

func newRegPool(info RegInfo) *regPool {
	return &regPool{
		info:       info,
		intAvail:   info.Allocatable(RegTypeInt),
		floatAvail: info.Allocatable(RegTypeFloat),
		bindings:   make(map[int32]PReg),
		owners:     make(map[regKey]int32),
	}
}

// alloc draws a fresh physical register for vreg from the available pool,
// binds them, and returns the assignment. Already-bound vregs return their
// existing binding unchanged.
func (p *regPool) alloc(vreg VReg) (PReg, error) {
	if pr, ok := p.bindings[vreg.ID()]; ok {
		return pr, nil
	}

	mask := p.intAvail &^ p.intExcluded &^ p.intBlocked
	if vreg.Type() == RegTypeFloat {
		mask = p.floatAvail &^ p.fltExcluded &^ p.fltBlocked
	}

	id := mask.First()
	if id == 0xFF {
		return PReg{}, ErrNoRegistersAvailable
	}

	if vreg.Type() == RegTypeFloat {
		p.floatAvail = p.floatAvail.Clear(id)
	} else {
		p.intAvail = p.intAvail.Clear(id)
	}

	pr := NewPReg(id, vreg.Type(), vreg.Width())
	p.bind(vreg, pr)
	return pr, nil
}

// reserve binds vreg to a specific preg, evicting any prior binding for
// vreg. Returns ErrNoRegistersAvailable if preg is blocked.
func (p *regPool) reserve(vreg VReg, preg PReg) error {
	if prev, ok := p.bindings[vreg.ID()]; ok {
		if prev == preg {
			return nil
		}
		p.free(vreg)
	}

	if preg.Type() == RegTypeFloat {
		if p.fltBlocked.Contains(preg.ID()) {
			return ErrNoRegistersAvailable
		}
		p.floatAvail = p.floatAvail.Clear(preg.ID())
	} else {
		if p.intBlocked.Contains(preg.ID()) {
			return ErrNoRegistersAvailable
		}
		p.intAvail = p.intAvail.Clear(preg.ID())
	}

	p.bind(vreg, preg)
	return nil
}

// free releases vreg's current binding back into the available pool.
func (p *regPool) free(vreg VReg) {
	preg, ok := p.bindings[vreg.ID()]
	if !ok {
		return
	}
	delete(p.bindings, vreg.ID())
	delete(p.owners, keyOf(preg))

	switch preg.Type() {
	case RegTypeFloat:
		if !p.fltBlocked.Contains(preg.ID()) {
			p.floatAvail = p.floatAvail.Set(preg.ID())
		}
	default:
		if !p.intBlocked.Contains(preg.ID()) {
			p.intAvail = p.intAvail.Set(preg.ID())
		}
	}
}

// block marks preg permanently unusable (neither alloc nor reserve can
// claim it).
func (p *regPool) block(preg PReg) {
	switch preg.Type() {
	case RegTypeFloat:
		p.fltBlocked = p.fltBlocked.Set(preg.ID())
		p.floatAvail = p.floatAvail.Clear(preg.ID())
	default:
		p.intBlocked = p.intBlocked.Set(preg.ID())
		p.intAvail = p.intAvail.Clear(preg.ID())
	}
}

// exclude marks preg unavailable for alloc but still reservable. ABI
// scratch registers use this so lowerers can pin them explicitly.
func (p *regPool) exclude(preg PReg) {
	switch preg.Type() {
	case RegTypeFloat:
		p.fltExcluded = p.fltExcluded.Set(preg.ID())
	default:
		p.intExcluded = p.intExcluded.Set(preg.ID())
	}
}

// owner reports the vreg that currently holds preg's slot, if any. The
// query is width-insensitive — X0 and W0 occupy the same slot.
func (p *regPool) owner(preg PReg) (int32, bool) {
	id, ok := p.owners[keyOf(preg)]
	return id, ok
}

func (p *regPool) bind(vreg VReg, preg PReg) {
	p.bindings[vreg.ID()] = preg
	p.owners[keyOf(preg)] = vreg.ID()
}

func keyOf(p PReg) regKey {
	return regKey{typ: p.Type(), id: p.ID()}
}
