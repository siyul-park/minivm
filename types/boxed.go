package types

import (
	"fmt"
	"math"
)

type Boxed uint64

const (
	TBits = 3
	TMask = (1 << TBits) - 1
	VBits = 52 - TBits
	VMask = (1 << VBits) - 1
)

var (
	BoxedNull  = BoxRef(0)
	BoxedFalse = Box(0, KindI1)
	BoxedTrue  = Box(1, KindI1)
)

var _ Value = Boxed(0)

func Tag(kind Kind) uint64 {
	return uint64(0x7FF)<<52 | uint64(kind)<<VBits
}

func IsBoxable(v int64) bool {
	const (
		minI64 = -1 << (VBits - 1)
		maxI64 = 1<<(VBits-1) - 1
	)
	return minI64 <= v && v <= maxI64
}

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

func BoxI64(v int64) Boxed {
	return Box(uint64(v)&VMask, KindI64)
}

func BoxF32(v float32) Boxed {
	return Box(uint64(math.Float32bits(v)), KindF32)
}

func BoxF64(v float64) Boxed {
	return Boxed(math.Float64bits(v))
}

func BoxRef(v int) Boxed {
	return Box(uint64(v), KindRef)
}

func Box(v uint64, kind Kind) Boxed {
	m := (uint64(kind) << VBits) | v
	u := (uint64(0x7FF) << 52) | m
	return Boxed(u)
}

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

func (v Boxed) Kind() Kind {
	u := uint64(v)
	if u>>52 == 0x7FF && u&0x000FFFFFFFFFFFFF != 0 {
		return Kind((u >> VBits) & TMask)
	}
	return KindF64
}

func (v Boxed) Type() Type {
	switch v.Kind() {
	case KindI32:
		return TypeI32
	case KindI8:
		return TypeI8
	case KindI1:
		return TypeI1
	case KindI64:
		return TypeI64
	case KindF32:
		return TypeF32
	case KindF64:
		return TypeF64
	case KindRef:
		return TypeRef
	default:
		return nil
	}
}

func (v Boxed) I32() int32 {
	return int32(v & VMask)
}

func (v Boxed) I8() int8 {
	return int8(v & VMask)
}

func (v Boxed) I64() int64 {
	i := int64(v & VMask)
	if i>>(VBits-1) != 0 {
		i |= ^VMask
	}
	return i
}

func (v Boxed) F32() float32 {
	return math.Float32frombits(uint32(v & VMask))
}

func (v Boxed) F64() float64 {
	return math.Float64frombits(uint64(v))
}

func (v Boxed) Bool() bool {
	return (uint64(v) & VMask) != 0
}

func (v Boxed) Ref() int {
	return int(int32(v & VMask))
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
