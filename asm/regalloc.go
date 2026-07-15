package asm

import "errors"

// allocator is the assembler's physical-register pool. It tracks which
// physical registers are free, currently bound to a vreg, or off-limits.
// allocator exposes the bookkeeping primitives — picking, reserving, and
// releasing — while leaving instruction-stream policy (linear scan,
// lifetime tracking, pin preemption) to its caller (rewriter).
//
// Three masks govern selection, each indexed by RegType so the integer and
// float banks share one code path:
//
//   - avail: physical registers free to be drawn by alloc.
//   - excluded: registers withheld from auto-alloc but still reservable
//     via Pin (for example, ABI scratch).
//   - blocked: registers that may not be used at all (FP, LR, etc.).
//
// Two maps track the current binding in both directions so callers can ask
// either "what preg does this vreg occupy?" (bindings) or "who owns this
// preg slot?" (owners) without maintaining their own reverse table.
type allocator struct {
	avail    [2]RegMask
	excluded [2]RegMask
	blocked  [2]RegMask

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

func newAllocator(info RegInfo) *allocator {
	return &allocator{
		avail: [2]RegMask{
			RegTypeInt:   info.Allocatable(RegTypeInt),
			RegTypeFloat: info.Allocatable(RegTypeFloat),
		},
		bindings: make(map[int32]PReg),
		owners:   make(map[regKey]int32),
	}
}

// alloc draws a fresh physical register for vreg from the available pool,
// binds them, and returns the assignment. Already-bound vregs return their
// existing binding unchanged.
func (a *allocator) alloc(vreg VReg) (PReg, error) {
	if pr, ok := a.bindings[vreg.ID()]; ok {
		return pr, nil
	}

	typ := vreg.Type()
	mask := a.avail[typ] &^ a.excluded[typ] &^ a.blocked[typ]
	id := mask.First()
	if id == 0xFF {
		return PReg{}, ErrNoRegistersAvailable
	}

	a.avail[typ] = a.avail[typ].Clear(id)
	pr := NewPReg(id, typ, vreg.Width())
	a.bind(vreg, pr)
	return pr, nil
}

// reserve binds vreg to a specific preg, evicting any prior binding for
// vreg. Returns ErrNoRegistersAvailable if preg is blocked.
func (a *allocator) reserve(vreg VReg, preg PReg) error {
	if prev, ok := a.bindings[vreg.ID()]; ok {
		if prev == preg {
			return nil
		}
		a.free(vreg)
	}

	typ := preg.Type()
	if a.blocked[typ].Contains(preg.ID()) {
		return ErrNoRegistersAvailable
	}
	a.avail[typ] = a.avail[typ].Clear(preg.ID())

	a.bind(vreg, preg)
	return nil
}

// free releases vreg's current binding back into the available pool.
func (a *allocator) free(vreg VReg) {
	preg, ok := a.bindings[vreg.ID()]
	if !ok {
		return
	}
	delete(a.bindings, vreg.ID())
	delete(a.owners, regKey{typ: preg.Type(), id: preg.ID()})

	typ := preg.Type()
	if !a.blocked[typ].Contains(preg.ID()) {
		a.avail[typ] = a.avail[typ].Set(preg.ID())
	}
}

// block marks preg permanently unusable (neither alloc nor reserve can
// claim it).
func (a *allocator) block(preg PReg) {
	typ := preg.Type()
	a.blocked[typ] = a.blocked[typ].Set(preg.ID())
	a.avail[typ] = a.avail[typ].Clear(preg.ID())
}

// exclude marks preg unavailable for alloc but still reservable. ABI
// scratch registers use this so lowerers can pin them explicitly.
func (a *allocator) exclude(preg PReg) {
	typ := preg.Type()
	a.excluded[typ] = a.excluded[typ].Set(preg.ID())
}

// owner reports the vreg that currently holds preg's slot, if any. The
// query is width-insensitive — X0 and W0 occupy the same slot.
func (a *allocator) owner(preg PReg) (int32, bool) {
	id, ok := a.owners[regKey{typ: preg.Type(), id: preg.ID()}]
	return id, ok
}

func (a *allocator) bind(vreg VReg, preg PReg) {
	a.bindings[vreg.ID()] = preg
	a.owners[regKey{typ: preg.Type(), id: preg.ID()}] = vreg.ID()
}
