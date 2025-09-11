package types

import (
	"fmt"
	"math"
)

type Boxed uint64

var (
	BoxedFalse = BoxI32(0)
	BoxedTrue  = BoxI32(1)
)

const (
	tagBits     = 3
	tagMask     = (1 << tagBits) - 1
	payloadBits = 52 - tagBits
)

var _ Value = Boxed(0)

func IsBoxable(v int64) bool {
	return v >= int64(-(1<<(payloadBits-1))) && v <= int64((1<<(payloadBits-1))-1)
}

func BoxI32(v int32) Boxed {
	return box(uint64(uint32(v)), KindI32)
}

func BoxI64(v int64) Boxed {
	return box(uint64(v&((1<<payloadBits)-1)), KindI64)
}

func BoxF32(v float32) Boxed {
	return box(uint64(math.Float32bits(v)), KindF32)
}

func BoxF64(v float64) Boxed {
	return Boxed(math.Float64bits(v))
}

func BoxRef(v int) Boxed {
	return box(uint64(uint32(v)), KindRef)
}

func BoxBool(b bool) Boxed {
	if b {
		return BoxedTrue
	}
	return BoxedFalse
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
		panic("unknown kind")
	}
}

func box(payload uint64, kind Kind) Boxed {
	mantissa := (uint64(kind) << payloadBits) | payload
	u := (uint64(0x7FF) << 52) | mantissa
	return Boxed(u)
}

func (v Boxed) Kind() Kind {
	if u := uint64(v); u>>52 == 0x7FF && u&0x000FFFFFFFFFFFFF != 0 {
		return Kind((u >> payloadBits) & tagMask)
	}
	return KindF64
}

func (v Boxed) I32() int32 {
	return int32(uint64(v) & ((1 << payloadBits) - 1))
}

func (v Boxed) I64() int64 {
	payload := int64(uint64(v) & ((1 << payloadBits) - 1))
	if payload>>(payloadBits-1) != 0 {
		payload |= ^((1 << payloadBits) - 1)
	}
	return payload
}

func (v Boxed) F32() float32 {
	return math.Float32frombits(uint32(uint64(v) & ((1 << payloadBits) - 1)))
}

func (v Boxed) F64() float64 {
	return math.Float64frombits(uint64(v))
}

func (v Boxed) Bool() bool {
	return (uint64(v) & ((1 << payloadBits) - 1)) != 0
}

func (v Boxed) Ref() int {
	return int(uint64(v) & ((1 << payloadBits) - 1))
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
		return v.Ref()
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
