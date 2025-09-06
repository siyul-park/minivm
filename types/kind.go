package types

type Kind byte

const (
	KindI32 Kind = iota
	KindI64
	KindF32
	KindF64
	KindRef
	KindNull
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
	case KindNull:
		return "null"
	default:
		return "unknown"
	}
}
