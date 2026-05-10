package asm

import "errors"

type RegAlloc struct {
	info         RegInfo
	phys         map[int32]PReg
	intAvail     RegMask
	floatAvail   RegMask
	blockedInt   RegMask
	blockedFloat RegMask
}

var ErrNoRegistersAvailable = errors.New("no registers available")

func NewRegAlloc(info RegInfo) *RegAlloc {
	return &RegAlloc{
		info:         info,
		phys:         make(map[int32]PReg),
		intAvail:     info.Allocatable(RegTypeInt),
		floatAvail:   info.Allocatable(RegTypeFloat),
		blockedInt:   0,
		blockedFloat: 0,
	}
}

func (ra *RegAlloc) Alloc(vreg VReg) (PReg, error) {
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

func (ra *RegAlloc) Reserve(vreg VReg, preg PReg) error {
	p, ok := ra.phys[vreg.ID()]
	if ok && p == preg {
		return nil
	}
	if ok {
		ra.Free(vreg)
	}

	var mask RegMask
	if preg.Type() == RegTypeFloat {
		mask = ra.floatAvail
		if !mask.Contains(preg.ID()) || ra.blockedFloat.Contains(preg.ID()) {
			return ErrNoRegistersAvailable
		}
		ra.floatAvail = mask.Clear(preg.ID())
	} else {
		mask = ra.intAvail
		if !mask.Contains(preg.ID()) || ra.blockedInt.Contains(preg.ID()) {
			return ErrNoRegistersAvailable
		}
		ra.intAvail = mask.Clear(preg.ID())
	}

	ra.phys[vreg.ID()] = preg
	return nil
}

func (ra *RegAlloc) Free(vreg VReg) {
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

func (ra *RegAlloc) Get(vreg VReg) (PReg, bool) {
	p, ok := ra.phys[vreg.ID()]
	return p, ok
}

func (ra *RegAlloc) Block(preg PReg) {
	switch preg.Type() {
	case RegTypeFloat:
		ra.blockedFloat = ra.blockedFloat.Set(preg.ID())
		ra.floatAvail = ra.floatAvail.Clear(preg.ID())
	default:
		ra.blockedInt = ra.blockedInt.Set(preg.ID())
		ra.intAvail = ra.intAvail.Clear(preg.ID())
	}
}

func (ra *RegAlloc) Reset() {
	clear(ra.phys)

	ra.intAvail = ra.info.Allocatable(RegTypeInt)
	ra.floatAvail = ra.info.Allocatable(RegTypeFloat)

	ra.blockedInt = 0
	ra.blockedFloat = 0
}

func (ra *RegAlloc) Clone() *RegAlloc {
	clone := &RegAlloc{
		info:         ra.info,
		phys:         make(map[int32]PReg),
		intAvail:     ra.intAvail,
		floatAvail:   ra.floatAvail,
		blockedInt:   ra.blockedInt,
		blockedFloat: ra.blockedFloat,
	}

	for k, v := range ra.phys {
		clone.phys[k] = v
	}

	return clone
}
