package instr

// Kind classifies an operand value: the four numeric kinds plus a reference.
// It is defined here, the lowest-level package, so instruction metadata can name
// the kinds it pops and pushes; types re-exports it (type Kind = instr.Kind) so
// the value model shares the exact same type.
type Kind byte

// Tag values are runtime-only (never serialized: constants round-trip as text),
// so they are laid out for fast classification rather than for stability. The
// kinds that share the i32 representation — i32, i8, i1 — all carry bit 0b100,
// so "computes as i32" is a single mask (see reprI32 / IsI32Repr).
const (
	KindF64 Kind = iota // 0b000
	KindF32             // 0b001
	KindI64             // 0b010
	KindRef             // 0b011
	KindI32             // 0b100
	KindI8              // 0b101
	KindI1              // 0b110
	// 0b111 reserved — must stay outside the i32 representation group.
)

// KindAny is the verifier's top element over Kind: a required or produced
// operand whose concrete kind is not statically fixed. It is not a real value
// kind, so it sits outside the iota range and prints as "unknown".
const KindAny Kind = 0xFF

// reprI32 is the tag bit shared by every Kind in the i32 representation
// (i32, i8, i1): a 32-bit integer slot, an i32 payload, and the i32.* operators.
// The shared bit makes the representation test a single mask. This mirrors the
// JVM "computational type": boolean/byte/short/char/int all compute as int.
const reprI32 = KindI32 // 0b100

func (k Kind) String() string {
	switch k {
	case KindI32:
		return "i32"
	case KindI8:
		return "i8"
	case KindI1:
		return "i1"
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

// IsNumeric reports whether k is one of the numeric kinds (i1, i8, i32, i64,
// f32, f64).
func (k Kind) IsNumeric() bool {
	switch k {
	case KindI1, KindI8, KindI32, KindI64, KindF32, KindF64:
		return true
	default:
		return false
	}
}

// Repr returns the kind k is computed and stored as: i1 and i8 reduce to i32,
// every other kind is its own representation.
func (k Kind) Repr() Kind {
	if k&reprI32 != 0 {
		return KindI32
	}
	return k
}

func (k Kind) Size() int {
	switch k {
	case KindI1, KindI8:
		return 1
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
