package asm

// regAlloc is the assembler's internal linear-scan allocator. It is
// package-private because callers should not be reaching past Assembler.Pin.
//
// Three masks govern selection:
//
//   - avail: physical registers free to be drawn by alloc.
//   - excluded: registers withheld from auto-alloc but still reservable
//     via Pin (for example, ABI scratch).
//   - blocked: registers that may not be used at all (FP, LR, etc.).
type regAlloc struct {
	info        RegInfo
	phys        map[int32]PReg
	intAvail    RegMask
	floatAvail  RegMask
	intExcluded RegMask
	fltExcluded RegMask
	intBlocked  RegMask
	fltBlocked  RegMask
}

func newRegAlloc(info RegInfo) *regAlloc {
	return &regAlloc{
		info:       info,
		phys:       make(map[int32]PReg),
		intAvail:   info.Allocatable(RegTypeInt),
		floatAvail: info.Allocatable(RegTypeFloat),
	}
}

func (ra *regAlloc) alloc(vreg VReg) (PReg, error) {
	if phys, ok := ra.phys[vreg.ID()]; ok {
		return phys, nil
	}

	mask := ra.intAvail &^ ra.intExcluded &^ ra.intBlocked
	if vreg.Type() == RegTypeFloat {
		mask = ra.floatAvail &^ ra.fltExcluded &^ ra.fltBlocked
	}

	id := mask.First()
	if id == 0xFF {
		return PReg{}, ErrNoRegistersAvailable
	}

	if vreg.Type() == RegTypeFloat {
		ra.floatAvail = ra.floatAvail.Clear(id)
	} else {
		ra.intAvail = ra.intAvail.Clear(id)
	}

	p := NewPReg(id, vreg.Type(), vreg.Width())
	ra.phys[vreg.ID()] = p
	return p, nil
}

func (ra *regAlloc) reserve(vreg VReg, preg PReg) error {
	p, ok := ra.phys[vreg.ID()]
	if ok && p == preg {
		return nil
	}
	if ok {
		ra.free(vreg)
	}

	if preg.Type() == RegTypeFloat {
		if ra.fltBlocked.Contains(preg.ID()) {
			return ErrNoRegistersAvailable
		}
		ra.floatAvail = ra.floatAvail.Clear(preg.ID())
	} else {
		if ra.intBlocked.Contains(preg.ID()) {
			return ErrNoRegistersAvailable
		}
		ra.intAvail = ra.intAvail.Clear(preg.ID())
	}

	ra.phys[vreg.ID()] = preg
	return nil
}

func (ra *regAlloc) free(vreg VReg) {
	preg, ok := ra.phys[vreg.ID()]
	if !ok {
		return
	}
	delete(ra.phys, vreg.ID())

	switch preg.Type() {
	case RegTypeFloat:
		if !ra.fltBlocked.Contains(preg.ID()) {
			ra.floatAvail = ra.floatAvail.Set(preg.ID())
		}
	default:
		if !ra.intBlocked.Contains(preg.ID()) {
			ra.intAvail = ra.intAvail.Set(preg.ID())
		}
	}
}

func (ra *regAlloc) block(preg PReg) {
	switch preg.Type() {
	case RegTypeFloat:
		ra.fltBlocked = ra.fltBlocked.Set(preg.ID())
		ra.floatAvail = ra.floatAvail.Clear(preg.ID())
	default:
		ra.intBlocked = ra.intBlocked.Set(preg.ID())
		ra.intAvail = ra.intAvail.Clear(preg.ID())
	}
}

func (ra *regAlloc) exclude(preg PReg) {
	switch preg.Type() {
	case RegTypeFloat:
		ra.fltExcluded = ra.fltExcluded.Set(preg.ID())
	default:
		ra.intExcluded = ra.intExcluded.Set(preg.ID())
	}
}
