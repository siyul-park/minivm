package asm

import (
	"errors"
)

type RegAlloc struct {
	info       *RegInfo
	phys       map[Register]Register
	intAvail   RegMask
	floatAvail RegMask
}

var (
	ErrRegisterNotVirtual   = errors.New("register is not virtual")
	ErrNoRegistersAvailable = errors.New("no registers available")
)

func NewRegAlloc(info *RegInfo) *RegAlloc {
	ra := &RegAlloc{
		info: info,
		phys: make(map[Register]Register),
	}

	ra.intAvail = info.Allocatable(RegTypeInt)
	ra.floatAvail = info.Allocatable(RegTypeFloat)

	return ra
}

func (ra *RegAlloc) Alloc(vreg Register) (Register, error) {
	if !vreg.IsVirtual() {
		return Register{}, ErrRegisterNotVirtual
	}

	if phys, ok := ra.phys[vreg]; ok {
		return phys, nil
	}

	avail := &ra.intAvail
	if vreg.Type() == RegTypeFloat {
		avail = &ra.floatAvail
	}

	id := avail.First()
	if id == InvalidRegID {
		return Register{}, ErrNoRegistersAvailable
	}

	avail.Clear(id)
	phys := NewReg(id, vreg.Type())
	ra.phys[vreg] = phys

	return phys, nil
}

func (ra *RegAlloc) Reserve(vreg, phys Register) error {
	if existing, ok := ra.phys[vreg]; ok {
		if existing == phys {
			return nil
		}
		return ErrNoRegistersAvailable
	}

	avail := &ra.intAvail
	if phys.Type() == RegTypeFloat {
		avail = &ra.floatAvail
	}

	if !avail.Contains(phys.ID()) {
		return ErrNoRegistersAvailable
	}

	avail.Clear(phys.ID())
	ra.phys[vreg] = phys
	return nil
}

func (ra *RegAlloc) Free(vreg Register) {
	phys, ok := ra.phys[vreg]
	if !ok {
		return
	}

	delete(ra.phys, vreg)

	if phys.Type() == RegTypeInt {
		ra.intAvail.Set(phys.ID())
	} else {
		ra.floatAvail.Set(phys.ID())
	}
}

func (ra *RegAlloc) Get(vreg Register) (Register, bool) {
	phys, ok := ra.phys[vreg]
	return phys, ok
}

func (ra *RegAlloc) Reset() {
	clear(ra.phys)
	ra.intAvail = ra.info.Allocatable(RegTypeInt)
	ra.floatAvail = ra.info.Allocatable(RegTypeFloat)
}
