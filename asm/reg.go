package asm

import "fmt"

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

// NewPReg constructs a physical register descriptor.
func NewPReg(id uint8, typ RegType, w RegWidth) PReg {
	return PReg{id: id, typ: typ, width: w}
}

// NewVReg constructs a virtual register descriptor.
func NewVReg(id int32, typ RegType, w RegWidth) VReg {
	return VReg{id: id, typ: typ, width: w}
}

// Compatible reports whether a and b share the same register type and width.
func Compatible(a, b Reg) bool {
	return a.Type() == b.Type() && a.Width() == b.Width()
}

// Compatibles reports whether a and b are element-wise shape-compatible:
// equal length with matching type and width at every position.
func Compatibles[A, B Reg](a []A, b []B) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !Compatible(a[i], b[i]) {
			return false
		}
	}
	return true
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
