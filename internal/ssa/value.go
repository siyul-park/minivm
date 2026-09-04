package ssa

import "github.com/siyul-park/minivm/types"

// Value names one SSA value: a block parameter or an instruction result. It
// is a dense index into the function that defines it, assigned once and never
// redefined, so a pass reasons about values with slices rather than pointer
// graphs.
type Value int32

// Type is the vocabulary a Value is typed in: minivm's own value kinds plus
// the interpreter state a deoptimizing instruction resumes into, which is a
// value here (see Frame) but not a value the guest can hold. The zero Type is
// invalid, so a value the builder never typed is rejected rather than read as
// i1.
type Type uint8

// NoValue is the absent Value, and the zero Value, so an instruction or
// terminator that names no interpreter state needs no field for it. Real
// values are numbered from one.
const NoValue Value = 0

const (
	TypeI1 Type = iota + 1
	TypeI8
	TypeI32
	TypeI64
	TypeF32
	TypeF64
	TypeRef
	// TypeState is the interpreter state an instruction deoptimizes into.
	// Only OpState produces it and only an Instruction.State or a
	// Terminator.State may name it.
	TypeState
)

// TypeOf returns the Type mirroring kind, or the invalid zero Type for a kind
// with no representation here (types.KindAny).
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
