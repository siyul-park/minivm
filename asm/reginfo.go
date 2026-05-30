package asm

// RegInfo enumerates the integer and float register banks of an
// architecture and the IDs the assembler must avoid (ABI-reserved,
// frame pointer, link register, etc.).
type RegInfo struct {
	NumInt      uint8
	NumFloat    uint8
	IntReserved RegMask
	FltReserved RegMask
	Scratch     RegMask
}

func NewRegInfo(numInt, numFloat uint8, intReserved, fltReserved, scratch []uint8) RegInfo {
	return RegInfo{
		NumInt:      numInt,
		NumFloat:    numFloat,
		IntReserved: NewRegMask(intReserved),
		FltReserved: NewRegMask(fltReserved),
		Scratch:     NewRegMask(scratch),
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
