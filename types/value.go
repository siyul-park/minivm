package types

import "github.com/siyul-park/minivm/instr"

type Value interface {
	Kind() Kind
	Type() Type
	String() string
}

type Type interface {
	Kind() Kind
	String() string
	Cast(other Type) bool
	Equals(other Type) bool
}

type Traceable interface {
	Value
	Refs() []Ref
}

type Iterator interface {
	Value
	Next() bool
	Current() Value
	Done() bool
}

func Zero(kind Kind) Boxed {
	switch kind {
	case KindI32:
		return BoxI32(0)
	case KindI64:
		return BoxI64(0)
	case KindF32:
		return BoxF32(0)
	case KindF64:
		return BoxF64(0)
	case KindRef:
		return BoxedNull
	default:
		return 0
	}
}

func IsNull(v Value) bool {
	switch v := v.(type) {
	case Ref:
		return v == Null
	case Boxed:
		return v == BoxedNull
	default:
		return false
	}
}

// Kinds projects each Type in ts to its Kind. Returns nil for an empty
// input so callers can use the result as an optional table.
func Kinds(ts []Type) []Kind {
	if len(ts) == 0 {
		return nil
	}
	out := make([]Kind, len(ts))
	for i, t := range ts {
		out[i] = t.Kind()
	}
	return out
}

// Kind is the operand value classification. It is defined in instr (the lowest
// package) and aliased here so instruction metadata and the value model share
// one type; its methods (String, IsNumeric, Size) live on instr.Kind.
type Kind = instr.Kind

const (
	KindF64 = instr.KindF64
	KindF32 = instr.KindF32
	KindI64 = instr.KindI64
	KindI32 = instr.KindI32
	KindRef = instr.KindRef
)
