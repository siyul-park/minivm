package asm

import (
	"fmt"
	"math/bits"
)

// Reg is implemented by both physical and virtual registers.
type Reg interface {
	Type() RegType
	Width() RegWidth
	String() string
}

// PReg is a physical register selected by the architecture.
type PReg struct {
	id    uint8
	typ   RegType
	width RegWidth
}

// VReg is a virtual register allocated by the assembler. Its physical
// binding is resolved during Build.
type VReg struct {
	id    int32
	typ   RegType
	width RegWidth
}

// RegType distinguishes the integer and floating-point register banks.
type RegType uint8

const (
	RegTypeInt RegType = iota
	RegTypeFloat
)

// RegWidth declares whether a register holds a 32- or 64-bit lane.
type RegWidth uint8

const (
	WidthUndefined RegWidth = 0
	Width32        RegWidth = 32
	Width64        RegWidth = 64
)

// RegMask is a bitmask of physical register IDs (0..63).
type RegMask uint64

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

// NewPReg constructs a physical register descriptor.
func NewPReg(id uint8, typ RegType, w RegWidth) PReg {
	return PReg{id: id, typ: typ, width: w}
}

// NewVReg constructs a virtual register descriptor.
func NewVReg(id int32, typ RegType, w RegWidth) VReg {
	return VReg{id: id, typ: typ, width: w}
}

// NewRegMask builds a mask from a list of physical register IDs.
func NewRegMask(ids []uint8) RegMask {
	var m RegMask
	for _, id := range ids {
		m |= 1 << id
	}
	return m
}

// NewRegInfo describes an architecture's register banks and reserved IDs.
func NewRegInfo(numInt, numFloat uint8, intReserved, fltReserved, scratch []uint8) RegInfo {
	return RegInfo{
		NumInt:      numInt,
		NumFloat:    numFloat,
		IntReserved: NewRegMask(intReserved),
		FltReserved: NewRegMask(fltReserved),
		Scratch:     NewRegMask(scratch),
	}
}

func (r PReg) ID() uint8       { return r.id }
func (r PReg) Type() RegType   { return r.typ }
func (r PReg) Width() RegWidth { return r.width }

func (r PReg) String() string {
	if r.typ == RegTypeFloat {
		if r.width == Width32 {
			return fmt.Sprintf("s%d", r.id)
		}
		return fmt.Sprintf("d%d", r.id)
	}
	if r.width == Width32 {
		return fmt.Sprintf("w%d", r.id)
	}
	return fmt.Sprintf("x%d", r.id)
}

func (r VReg) ID() int32       { return r.id }
func (r VReg) Type() RegType   { return r.typ }
func (r VReg) Width() RegWidth { return r.width }

func (r VReg) String() string {
	prefix := "vr"
	if r.typ == RegTypeFloat {
		prefix = "vf"
	}
	return fmt.Sprintf("%s%d", prefix, r.id)
}

func (m RegMask) Set(id uint8) RegMask {
	if id < 64 {
		m |= 1 << id
	}
	return m
}

func (m RegMask) Clear(id uint8) RegMask {
	if id < 64 {
		m &^= 1 << id
	}
	return m
}

func (m RegMask) Contains(id uint8) bool {
	return id < 64 && (m&(1<<id)) != 0
}

func (m RegMask) First() uint8 {
	if m == 0 {
		return 0xFF
	}
	return uint8(bits.TrailingZeros64(uint64(m)))
}

func (m RegMask) PopFirst() (uint8, RegMask) {
	if m == 0 {
		return 0xFF, m
	}
	i := uint8(bits.TrailingZeros64(uint64(m)))
	m &^= 1 << i
	return i, m
}

func (m RegMask) Count() int {
	return bits.OnesCount64(uint64(m))
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
