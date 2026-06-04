package asm

import "errors"

// regAlloc is the assembler's physical-register pool. It tracks which
// physical registers are free, currently bound to a vreg, or off-limits.
// regAlloc exposes the bookkeeping primitives — picking, reserving, and
// releasing — while leaving instruction-stream policy (linear scan,
// lifetime tracking, pin preemption) to its caller (rewriter).
//
// Three masks govern selection:
//
//   - avail: physical registers free to be drawn by alloc.
//   - excluded: registers withheld from auto-alloc but still reservable
//     via Pin (for example, ABI scratch).
//   - blocked: registers that may not be used at all (FP, LR, etc.).
//
// Two maps track the current binding in both directions so callers can ask
// either "what preg does this vreg occupy?" (bindings) or "who owns this
// preg slot?" (owners) without maintaining their own reverse table.
type regAlloc struct {
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

func newRegAlloc(info RegInfo) *regAlloc {
	return &regAlloc{
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
func (a *regAlloc) alloc(vreg VReg) (PReg, error) {
	if pr, ok := a.bindings[vreg.ID()]; ok {
		return pr, nil
	}

	mask := a.intAvail &^ a.intExcluded &^ a.intBlocked
	if vreg.Type() == RegTypeFloat {
		mask = a.floatAvail &^ a.fltExcluded &^ a.fltBlocked
	}

	id := mask.First()
	if id == 0xFF {
		return PReg{}, ErrNoRegistersAvailable
	}

	if vreg.Type() == RegTypeFloat {
		a.floatAvail = a.floatAvail.Clear(id)
	} else {
		a.intAvail = a.intAvail.Clear(id)
	}

	pr := NewPReg(id, vreg.Type(), vreg.Width())
	a.bind(vreg, pr)
	return pr, nil
}

// reserve binds vreg to a specific preg, evicting any prior binding for
// vreg. Returns ErrNoRegistersAvailable if preg is blocked.
func (a *regAlloc) reserve(vreg VReg, preg PReg) error {
	if prev, ok := a.bindings[vreg.ID()]; ok {
		if prev == preg {
			return nil
		}
		a.free(vreg)
	}

	if preg.Type() == RegTypeFloat {
		if a.fltBlocked.Contains(preg.ID()) {
			return ErrNoRegistersAvailable
		}
		a.floatAvail = a.floatAvail.Clear(preg.ID())
	} else {
		if a.intBlocked.Contains(preg.ID()) {
			return ErrNoRegistersAvailable
		}
		a.intAvail = a.intAvail.Clear(preg.ID())
	}

	a.bind(vreg, preg)
	return nil
}

// free releases vreg's current binding back into the available pool.
func (a *regAlloc) free(vreg VReg) {
	preg, ok := a.bindings[vreg.ID()]
	if !ok {
		return
	}
	delete(a.bindings, vreg.ID())
	delete(a.owners, regKey{typ: preg.Type(), id: preg.ID()})

	switch preg.Type() {
	case RegTypeFloat:
		if !a.fltBlocked.Contains(preg.ID()) {
			a.floatAvail = a.floatAvail.Set(preg.ID())
		}
	default:
		if !a.intBlocked.Contains(preg.ID()) {
			a.intAvail = a.intAvail.Set(preg.ID())
		}
	}
}

// block marks preg permanently unusable (neither alloc nor reserve can
// claim it).
func (a *regAlloc) block(preg PReg) {
	switch preg.Type() {
	case RegTypeFloat:
		a.fltBlocked = a.fltBlocked.Set(preg.ID())
		a.floatAvail = a.floatAvail.Clear(preg.ID())
	default:
		a.intBlocked = a.intBlocked.Set(preg.ID())
		a.intAvail = a.intAvail.Clear(preg.ID())
	}
}

// exclude marks preg unavailable for alloc but still reservable. ABI
// scratch registers use this so lowerers can pin them explicitly.
func (a *regAlloc) exclude(preg PReg) {
	switch preg.Type() {
	case RegTypeFloat:
		a.fltExcluded = a.fltExcluded.Set(preg.ID())
	default:
		a.intExcluded = a.intExcluded.Set(preg.ID())
	}
}

// owner reports the vreg that currently holds preg's slot, if any. The
// query is width-insensitive — X0 and W0 occupy the same slot.
func (a *regAlloc) owner(preg PReg) (int32, bool) {
	id, ok := a.owners[regKey{typ: preg.Type(), id: preg.ID()}]
	return id, ok
}

func (a *regAlloc) bind(vreg VReg, preg PReg) {
	a.bindings[vreg.ID()] = preg
	a.owners[regKey{typ: preg.Type(), id: preg.ID()}] = vreg.ID()
}
