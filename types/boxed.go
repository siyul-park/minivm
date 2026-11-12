package types

import (
	"fmt"
	"math"
)

// Boxed is a value packed into one 64-bit word: an f64 as itself, a positive NaN as BoxedNaN, any other kind as a NaN carrying a kind tag and payload.
type Boxed uint64

const (
	// TBits is the width of the kind tag in a boxed NaN.
	TBits = 3
	// TMask selects the kind tag after shifting it down.
	TMask = (1 << TBits) - 1
	// VBits is the width of the payload in a boxed NaN.
	VBits = 52 - TBits
	// VMask selects the payload bits.
	VMask = (1 << VBits) - 1
)

const (
	// BoxedNull is the null reference.
	BoxedNull = Boxed(exponent | uint64(KindRef)<<VBits)
	// BoxedFalse is the i1 false.
	BoxedFalse = Boxed(exponent | uint64(KindI1)<<VBits)
	// BoxedTrue is the i1 true.
	BoxedTrue = Boxed(exponent | uint64(KindI1)<<VBits | 1)
	// BoxedNaN is the f64 every positive NaN boxes as. A positive NaN's bits
	// may hold a kind tag; BoxedNaN's tag is KindF64. A negative NaN never
	// does, so it keeps its bits.
	BoxedNaN = Boxed(exponent | 1)
)

// exponent is the all-ones float64 exponent that marks a boxed non-f64 value.
const exponent = 0x7FF << 52

var _ Value = Boxed(0)

// Tag returns the NaN bit pattern, without payload, that marks a boxed value of kind.
func Tag(kind Kind) uint64 {
	return exponent | uint64(kind)<<VBits
}

// IsBoxable reports whether v fits the inline i64 payload.
func IsBoxable(v int64) bool {
	const (
		minI64 = -1 << (VBits - 1)
		maxI64 = 1<<(VBits-1) - 1
	)
	return minI64 <= v && v <= maxI64
}

// BoxF64 boxes a float64 value. A positive NaN boxes as BoxedNaN, so a NaN
// keeps its sign but not its payload.
func BoxF64(v float64) Boxed {
	u := math.Float64bits(v)
	if int64(u) > exponent {
		return BoxedNaN
	}
	return Boxed(u)
}

// BoxRef boxes a reference index.
func BoxRef(v int) Boxed {
	return Box(uint64(v), KindRef)
}

// BoxWord boxes word, the native word of kind (see Boxed.Word). An i64 word
// outside the inline range is not representable; callers promote it first.
// An unknown kind boxes to zero.
func BoxWord(kind Kind, word uint64) Boxed {
	switch kind {
	case KindI1:
		return BoxI1(uint32(word) != 0)
	case KindI8:
		return BoxI8(int8(uint32(word)))
	case KindI32:
		return BoxI32(int32(uint32(word)))
	case KindI64:
		return BoxI64(int64(word))
	case KindF32:
		return BoxF32(math.Float32frombits(uint32(word)))
	case KindF64:
		return BoxF64(math.Float64frombits(word))
	case KindRef:
		return Boxed(word)
	default:
		return 0
	}
}

// BoxI32 boxes an i32 value.
func BoxI32(v int32) Boxed {
	return Box(uint64(uint32(v)), KindI32)
}

// BoxI8 boxes an i8 value. i8 shares the i32 representation; the kind tag keeps
// the value distinguishable as i8 (Kind, Type, String) while the payload is the
// sign-extended i32 the operators read.
func BoxI8(v int8) Boxed {
	return Box(uint64(uint32(int32(v))), KindI8)
}

// BoxI1 boxes a boolean as i1 (payload 0 or 1), returning the shared singleton
// for each value. i1 shares the i32 representation.
func BoxI1(b bool) Boxed {
	if b {
		return BoxedTrue
	}
	return BoxedFalse
}

// BoxI64 boxes an i64 value; v must satisfy IsBoxable.
func BoxI64(v int64) Boxed {
	return Box(uint64(v)&VMask, KindI64)
}

// BoxF32 boxes an f32 value.
func BoxF32(v float32) Boxed {
	return Box(uint64(math.Float32bits(v)), KindF32)
}

// Box packs payload v with kind into a boxed value.
func Box(v uint64, kind Kind) Boxed {
	m := (uint64(kind) << VBits) | v
	u := exponent | m
	return Boxed(u)
}

// Unbox decodes v into the Value of its kind.
func Unbox(v Boxed) Value {
	switch v.Kind() {
	case KindI32:
		return I32(v.I32())
	case KindI8:
		return I8(v.I8())
	case KindI1:
		return I1(v.Bool())
	case KindI64:
		return I64(v.I64())
	case KindF32:
		return F32(v.F32())
	case KindF64:
		return F64(v.F64())
	case KindRef:
		return Ref(v.Ref())
	default:
		return nil
	}
}

// I8 decodes an int8 value.
func (v Boxed) I8() int8 {
	return int8(v & VMask)
}

// F64 decodes a float64 value.
func (v Boxed) F64() float64 {
	return math.Float64frombits(uint64(v))
}

// Ref returns the referenced value index.
func (v Boxed) Ref() int {
	return int(int32(v & VMask))
}

// Word returns v's native word: i1 as 0 or 1, i8 and i32 as a zero-extended
// 32-bit lane, i64 as the full 64 bits, f32 as its bits in the low 32, and
// f64 and ref as the boxed word itself.
func (v Boxed) Word() uint64 {
	switch v.Kind() {
	case KindI1:
		if v.Bool() {
			return 1
		}
		return 0
	case KindI8, KindI32:
		return uint64(uint32(v.I32()))
	case KindI64:
		return uint64(v.I64())
	case KindF32:
		return uint64(math.Float32bits(v.F32()))
	default:
		return uint64(v)
	}
}

// I32 decodes an int32 value.
func (v Boxed) I32() int32 {
	return int32(v & VMask)
}

// I64 decodes an int64 value.
func (v Boxed) I64() int64 {
	i := int64(v & VMask)
	if i>>(VBits-1) != 0 {
		i |= ^VMask
	}
	return i
}

// F32 decodes a float32 value.
func (v Boxed) F32() float32 {
	return math.Float32frombits(uint32(v & VMask))
}

// Bool reports the boxed boolean value.
func (v Boxed) Bool() bool {
	return (uint64(v) & VMask) != 0
}

func (v Boxed) Type() Type {
	switch v.Kind() {
	case KindI32:
		return TypeI32()
	case KindI8:
		return TypeI8()
	case KindI1:
		return TypeI1()
	case KindI64:
		return TypeI64()
	case KindF32:
		return TypeF32()
	case KindF64:
		return TypeF64()
	case KindRef:
		return TypeAny()
	default:
		return nil
	}
}

func (v Boxed) Kind() Kind {
	u := uint64(v)
	if u>>52 == 0x7FF && u&0x000FFFFFFFFFFFFF != 0 {
		return Kind((u >> VBits) & TMask)
	}
	return KindF64
}

func (v Boxed) String() string {
	switch v.Kind() {
	case KindI32, KindI8:
		return fmt.Sprintf("%d", v.I32())
	case KindI1:
		if v.Bool() {
			return "true"
		}
		return "false"
	case KindI64:
		return fmt.Sprintf("%d", v.I64())
	case KindF32:
		return fmt.Sprintf("%g", v.F32())
	case KindF64:
		return fmt.Sprintf("%g", v.F64())
	case KindRef:
		return fmt.Sprintf("%d", v.Ref())
	default:
		return "<invalid>"
	}
}
