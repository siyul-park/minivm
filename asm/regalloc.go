package asm

import "errors"

type RegAlloc struct {
	info       RegInfo
	phys       map[int32]PReg
	intAvail   RegMask
	floatAvail RegMask
}

var ErrNoRegistersAvailable = errors.New("no registers available")

func NewRegAlloc(info RegInfo) *RegAlloc {
	return &RegAlloc{
		info:       info,
		phys:       make(map[int32]PReg),
		intAvail:   info.Allocatable(RegTypeInt),
		floatAvail: info.Allocatable(RegTypeFloat),
	}
}

func (ra *RegAlloc) Alloc(vreg VReg) (PReg, error) {
	if phys, ok := ra.phys[vreg.ID()]; ok {
		return phys, nil
	}

	var mask RegMask
	if vreg.Type() == RegTypeFloat {
		mask = ra.floatAvail
	} else {
		mask = ra.intAvail
	}

	id := mask.First()
	if id == 0xFF {
		return PReg{}, ErrNoRegistersAvailable
	}

	_, mask = mask.PopFirst()

	if vreg.Type() == RegTypeFloat {
		ra.floatAvail = mask
	} else {
		ra.intAvail = mask
	}

	p := NewPReg(id, vreg.Type(), vreg.Width())
	ra.phys[vreg.ID()] = p

	return p, nil
}

func (ra *RegAlloc) Reserve(vreg VReg, preg PReg) error {
	if existing, ok := ra.phys[vreg.ID()]; ok {
		if existing == preg {
			return nil
		}
		return ErrNoRegistersAvailable
	}

	var mask RegMask
	if preg.Type() == RegTypeFloat {
		mask = ra.floatAvail
	} else {
		mask = ra.intAvail
	}

	if !mask.Contains(preg.ID()) {
		return ErrNoRegistersAvailable
	}

	mask = mask.Clear(preg.ID())

	if preg.Type() == RegTypeFloat {
		ra.floatAvail = mask
	} else {
		ra.intAvail = mask
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

	if preg.Type() == RegTypeFloat {
		ra.floatAvail = ra.floatAvail.Set(preg.ID())
	} else {
		ra.intAvail = ra.intAvail.Set(preg.ID())
	}
}

func (ra *RegAlloc) Get(vreg VReg) (PReg, bool) {
	p, ok := ra.phys[vreg.ID()]
	return p, ok
}

func (ra *RegAlloc) Reset() {
	clear(ra.phys)
	ra.intAvail = ra.info.Allocatable(RegTypeInt)
	ra.floatAvail = ra.info.Allocatable(RegTypeFloat)
}
