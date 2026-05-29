package asm

type RegInfo struct {
	NumInt      uint8
	NumFloat    uint8
	IntReserved RegMask
	FltReserved RegMask
}

func NewRegInfo(numInt, numFloat uint8, intRes, fltRes []uint8) RegInfo {
	return RegInfo{
		NumInt:      numInt,
		NumFloat:    numFloat,
		IntReserved: NewRegMask(intRes),
		FltReserved: NewRegMask(fltRes),
	}
}

func (ri RegInfo) Allocatable(typ RegType) RegMask {
	var count uint8
	var reserved RegMask

	if typ == RegTypeInt {
		count = ri.NumInt
		reserved = ri.IntReserved
	} else {
		count = ri.NumFloat
		reserved = ri.FltReserved
	}

	mask := RegMask((1 << count) - 1)
	mask &^= reserved

	return mask
}
