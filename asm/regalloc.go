package asm

// regAlloc is the assembler's internal linear-scan allocator. It is private
// because callers should not be reaching past Assembler.Pin.
type regAlloc struct {
	info         RegInfo
	phys         map[int32]PReg
	intAvail     RegMask
	floatAvail   RegMask
	blockedInt   RegMask
	blockedFloat RegMask
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

	mask := ra.intAvail &^ ra.blockedInt
	if vreg.Type() == RegTypeFloat {
		mask = ra.floatAvail &^ ra.blockedFloat
	}

	id := mask.First()
	if id == 0xFF {
		return PReg{}, ErrNoRegistersAvailable
	}

	_, newMask := mask.PopFirst()

	if vreg.Type() == RegTypeFloat {
		ra.floatAvail = newMask
	} else {
		ra.intAvail = newMask
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
		if !ra.floatAvail.Contains(preg.ID()) || ra.blockedFloat.Contains(preg.ID()) {
			return ErrNoRegistersAvailable
		}
		ra.floatAvail = ra.floatAvail.Clear(preg.ID())
	} else {
		if !ra.intAvail.Contains(preg.ID()) || ra.blockedInt.Contains(preg.ID()) {
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
		if !ra.blockedFloat.Contains(preg.ID()) {
			ra.floatAvail = ra.floatAvail.Set(preg.ID())
		}
	default:
		if !ra.blockedInt.Contains(preg.ID()) {
			ra.intAvail = ra.intAvail.Set(preg.ID())
		}
	}
}

func (ra *regAlloc) block(preg PReg) {
	switch preg.Type() {
	case RegTypeFloat:
		ra.blockedFloat = ra.blockedFloat.Set(preg.ID())
		ra.floatAvail = ra.floatAvail.Clear(preg.ID())
	default:
		ra.blockedInt = ra.blockedInt.Set(preg.ID())
		ra.intAvail = ra.intAvail.Clear(preg.ID())
	}
}
