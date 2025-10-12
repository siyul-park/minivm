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
