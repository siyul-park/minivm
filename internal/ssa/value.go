package ssa

import "github.com/siyul-park/minivm/types"

// Value identifies one SSA definition.
type Value int32

// Type identifies an SSA value representation.
type Type uint8

// NoValue identifies an absent value.
const NoValue Value = 0

const (
	TypeI1 Type = iota + 1
	TypeI8
	TypeI32
	TypeI64
	TypeF32
	TypeF64
	TypeRef
	TypeState
)

// TypeOf returns the SSA type for a guest kind.
func TypeOf(kind types.Kind) Type {
	switch kind {
	case types.KindI1:
		return TypeI1
	case types.KindI8:
		return TypeI8
	case types.KindI32:
		return TypeI32
	case types.KindI64:
		return TypeI64
	case types.KindF32:
		return TypeF32
	case types.KindF64:
		return TypeF64
	case types.KindRef:
		return TypeRef
	default:
		return 0
	}
}

// String returns the type name.
func (t Type) String() string {
	switch t {
	case TypeI1:
		return "i1"
	case TypeI8:
		return "i8"
	case TypeI32:
		return "i32"
	case TypeI64:
		return "i64"
	case TypeF32:
		return "f32"
	case TypeF64:
		return "f64"
	case TypeRef:
		return "ref"
	case TypeState:
		return "state"
	default:
		return "invalid"
	}
}
