package types

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

type Kind byte

const (
	KindF64 Kind = iota
	KindF32
	KindI64
	KindI32
	KindRef
)

func (k Kind) String() string {
	switch k {
	case KindI32:
		return "i32"
	case KindI64:
		return "i64"
	case KindF32:
		return "f32"
	case KindF64:
		return "f64"
	case KindRef:
		return "ref"
	default:
		return "unknown"
	}
}

// IsNumeric reports whether k is one of the numeric kinds (i32, i64, f32, f64).
func (k Kind) IsNumeric() bool {
	switch k {
	case KindI32, KindI64, KindF32, KindF64:
		return true
	default:
		return false
	}
}

func (k Kind) Size() int {
	switch k {
	case KindI32:
		return 4
	case KindI64:
		return 8
	case KindF32:
		return 4
	case KindF64:
		return 8
	case KindRef:
		return 4
	default:
		return 0
	}
}
