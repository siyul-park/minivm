package asm

import "fmt"

// Reg is implemented by both physical and virtual registers.
type Reg interface {
	Type() RegType
	Width() RegWidth
	String() string
}

// PReg is a physical register selected by the architecture. It packs the
// identifier, bank, and width so register names can be constants.
type PReg uint32

// VReg is a virtual register. Build assigns it a PReg or a spill slot when the
// target is a Frame, and rejects it otherwise.
type VReg struct {
	id    int32
	typ   RegType
	width RegWidth
}

// RegType distinguishes the integer and floating-point register banks.
type RegType uint8

// RegWidth declares whether a register holds a 32- or 64-bit lane.
type RegWidth uint8

const (
	RegTypeInt RegType = iota
	RegTypeFloat
)

const (
	// Width32 is the 32-bit register width.
	Width32 RegWidth = 32
	// Width64 is the 64-bit register width.
	Width64 RegWidth = 64
)

// Bank bases: add a register identifier to name a physical register.
const (
	Int32   PReg = PReg(RegTypeInt)<<pregTypeShift | PReg(Width32)<<pregWidthShift
	Int64   PReg = PReg(RegTypeInt)<<pregTypeShift | PReg(Width64)<<pregWidthShift
	Float32 PReg = PReg(RegTypeFloat)<<pregTypeShift | PReg(Width32)<<pregWidthShift
	Float64 PReg = PReg(RegTypeFloat)<<pregTypeShift | PReg(Width64)<<pregWidthShift
)

const (
	pregTypeShift  = 8
	pregWidthShift = 16
)

// NewPReg constructs a physical register descriptor.
func NewPReg(id uint8, typ RegType, w RegWidth) PReg {
	return PReg(id) | PReg(typ)<<pregTypeShift | PReg(w)<<pregWidthShift
}

// NewVReg constructs a virtual register descriptor.
func NewVReg(id int32, typ RegType, w RegWidth) VReg {
	return VReg{id: id, typ: typ, width: w}
}

// ID returns the register identifier.
func (r PReg) ID() uint8 {
	return uint8(r)
}

// Width returns the register width.
func (r PReg) Width() RegWidth {
	return RegWidth(r >> pregWidthShift)
}

// ID returns the register identifier.
func (r VReg) ID() int32 {
	return r.id
}

// Width returns the register width.
func (r VReg) Width() RegWidth {
	return r.width
}

// Type returns the value type.
func (r PReg) Type() RegType {
	return RegType(r >> pregTypeShift)
}

func (r PReg) String() string {
	if r.Type() == RegTypeFloat {
		if r.Width() == Width32 {
			return fmt.Sprintf("s%d", r.ID())
		}
		return fmt.Sprintf("d%d", r.ID())
	}
	if r.Width() == Width32 {
		return fmt.Sprintf("w%d", r.ID())
	}
	return fmt.Sprintf("x%d", r.ID())
}

// Type returns the value type.
func (r VReg) Type() RegType {
	return r.typ
}

func (r VReg) String() string {
	prefix := "vr"
	if r.typ == RegTypeFloat {
		prefix = "vf"
	}
	return fmt.Sprintf("%s%d", prefix, r.id)
}
