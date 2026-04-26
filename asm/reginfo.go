package asm

type RegInfo struct {
	NumInt      int
	NumFloat    int
	IntReserved RegMask
	FltReserved RegMask
}

func NewRegInfo(numInt, numFloat int, intRes, fltRes []uint8) *RegInfo {
	return &RegInfo{
		NumInt:      numInt,
		NumFloat:    numFloat,
		IntReserved: NewRegMask(intRes),
		FltReserved: NewRegMask(fltRes),
	}
}

func (ri *RegInfo) IsReserved(reg Register) bool {
	if reg.Type() == RegTypeInt {
		return ri.IntReserved.Contains(reg.ID())
	}
	return ri.FltReserved.Contains(reg.ID())
}

func (ri *RegInfo) Allocatable(typ RegType) RegMask {
	var mask RegMask
	var count int
	var reserved RegMask

	if typ == RegTypeInt {
		count = ri.NumInt
		reserved = ri.IntReserved
	} else {
		count = ri.NumFloat
		reserved = ri.FltReserved
	}

	for i := uint8(0); i < uint8(count); i++ {
		if !reserved.Contains(i) {
			mask.Set(i)
		}
	}
	return mask
}
