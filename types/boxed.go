package types

import (
	"fmt"
	"math"
)

type Boxed uint64

var (
	BoxedNull  = BoxRef(0)
	BoxedFalse = BoxI32(0)
	BoxedTrue  = BoxI32(1)
)

const (
	tBits = 3
	tMask = (1 << tBits) - 1
	vBits = 52 - tBits
	vMask = (1 << vBits) - 1
)

var _ Value = Boxed(0)

func IsBoxable(v int64) bool {
	return uint64(v+vMask) <= 2*vMask
}

func BoxI32(v int32) Boxed {
	return Box(uint64(uint32(v)), KindI32)
}

func BoxI64(v int64) Boxed {
	return Box(uint64(v)&vMask, KindI64)
}

func BoxF32(v float32) Boxed {
	return Box(uint64(math.Float32bits(v)), KindF32)
}

func BoxF64(v float64) Boxed {
	return Boxed(math.Float64bits(v))
}

func BoxRef(v int) Boxed {
	return Box(uint64(uint32(v)), KindRef)
}

func BoxBool(b bool) Boxed {
	if b {
		return BoxedTrue
	}
	return BoxedFalse
}

func Box(v uint64, kind Kind) Boxed {
	m := (uint64(kind) << vBits) | v
	u := (uint64(0x7FF) << 52) | m
	return Boxed(u)
}

func Unbox(v Boxed) Value {
	switch v.Kind() {
	case KindI32:
		return I32(v.I32())
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
		return Kind((u >> vBits) & tMask)
	}
	return KindF64
}

func (v Boxed) Type() Type {
	switch v.Kind() {
	case KindI32:
		return TypeI32
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
	return int32(v & vMask)
}

func (v Boxed) I64() int64 {
	i := int64(v & vMask)
	if i>>(vBits-1) != 0 {
		i |= ^vMask
	}
	return i
}

func (v Boxed) F32() float32 {
	return math.Float32frombits(uint32(v & vMask))
}

func (v Boxed) F64() float64 {
	return math.Float64frombits(uint64(v))
}

func (v Boxed) Bool() bool {
	return (uint64(v) & vMask) != 0
}

func (v Boxed) Ref() int {
	return int(uint64(v) & vMask)
}

func (v Boxed) Interface() any {
	switch v.Kind() {
	case KindI32:
		return v.I32()
	case KindI64:
		return v.I64()
	case KindF32:
		return v.F32()
	case KindF64:
		return v.F64()
	case KindRef:
		addr := v.Ref()
		if addr == 0 {
			return nil
		}
		return addr
	default:
		return nil
	}
}

func (v Boxed) String() string {
	switch v.Kind() {
	case KindI32, KindI64:
		return fmt.Sprintf("%d", v.Interface())
	case KindF32, KindF64:
		return fmt.Sprintf("%f", v.Interface())
	case KindRef:
		return fmt.Sprintf("%d", v.Interface())
	default:
		return "<invalid>"
	}
}
