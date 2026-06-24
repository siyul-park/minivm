package instr

// Kind classifies an operand value: the four numeric kinds plus a reference.
// It is defined here, the lowest-level package, so instruction metadata can name
// the kinds it pops and pushes; types re-exports it (type Kind = instr.Kind) so
// the value model shares the exact same type.
type Kind byte

const (
	KindF64 Kind = iota
	KindF32
	KindI64
	KindI32
	KindRef
)

// KindAny is the verifier's top element over Kind: a required or produced
// operand whose concrete kind is not statically fixed. It is not a real value
// kind, so it sits outside the iota range and prints as "unknown".
const KindAny Kind = 0xFF

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
