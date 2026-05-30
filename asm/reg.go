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

type RegType uint8

const (
	RegTypeInt RegType = iota
	RegTypeFloat
)

type RegWidth uint8

const (
	WidthUndefined RegWidth = 0
	Width32        RegWidth = 32
	Width64        RegWidth = 64
)

// Value is the runtime payload passed across a Callable boundary. Type and
// width describe how Bits is interpreted by both sides of the ABI.
type Value struct {
	typ   RegType
	width RegWidth
	bits  uint64
}

func I32(v uint32) Value { return Value{typ: RegTypeInt, width: Width32, bits: uint64(v)} }
func I64(v uint64) Value { return Value{typ: RegTypeInt, width: Width64, bits: v} }
func F32(v uint32) Value { return Value{typ: RegTypeFloat, width: Width32, bits: uint64(v)} }
func F64(v uint64) Value { return Value{typ: RegTypeFloat, width: Width64, bits: v} }

func (v Value) RegType() RegType { return v.typ }
func (v Value) Width() RegWidth  { return v.width }
func (v Value) Valid() bool      { return v.width != WidthUndefined }
func (v Value) Bits() uint64     { return v.bits }

func (v Value) String() string {
	switch {
	case v.typ == RegTypeInt && v.width == Width32:
		return "i32"
	case v.typ == RegTypeInt && v.width == Width64:
		return "i64"
	case v.typ == RegTypeFloat && v.width == Width32:
		return "f32"
	case v.typ == RegTypeFloat && v.width == Width64:
		return "f64"
	default:
		return "<invalid>"
	}
}

func (v Value) GoString() string {
	switch {
	case v.typ == RegTypeInt && v.width == Width32:
		return fmt.Sprintf("I32(%d)", uint32(v.bits))
	case v.typ == RegTypeInt && v.width == Width64:
		return fmt.Sprintf("I64(%d)", v.bits)
	case v.typ == RegTypeFloat && v.width == Width32:
		return fmt.Sprintf("F32(%08x)", uint32(v.bits))
	case v.typ == RegTypeFloat && v.width == Width64:
		return fmt.Sprintf("F64(%016x)", v.bits)
	default:
		return "<invalid>"
	}
}

func NewPReg(id uint8, typ RegType, w RegWidth) PReg {
	return PReg{id: id, typ: typ, width: w}
}

func NewVReg(id int32, typ RegType, w RegWidth) VReg {
	return VReg{id: id, typ: typ, width: w}
}

// Compatible reports whether a and b share the same register type and width.
func Compatible(a, b Reg) bool {
	return a.Type() == b.Type() && a.Width() == b.Width()
}

// Compatibles reports whether a and b are element-wise shape-compatible.
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
