package types

import (
	"fmt"
	"math"
)

type Boxed uint64

const (
	tagBits     = 3
	tagMask     = (1 << tagBits) - 1
	payloadBits = 52 - tagBits
)

func IsBoxable(payload uint64) bool {
	return payload >= (1 << payloadBits)
}

func NewI32(v int32) Boxed {
	return boxed(uint64(v), KindI32)
}

func NewI64(v int64) Boxed {
	return boxed(uint64(v), KindI64)
}

func NewF32(f float32) Boxed {
	bits := math.Float32bits(f)
	payload := uint64(bits)
	return boxed(payload, KindF32)
}

func NewF64(f float64) Boxed {
	return Boxed(math.Float64bits(f))
}

func NewBool(b bool) Boxed {
	var v uint64
	if b {
		v = 1
	}
	return boxed(v, KindBool)
}

func NewNull() Boxed {
	return boxed(0, KindNull)
}

func NewRef(ptr uintptr) Boxed {
	return boxed(uint64(ptr), KindRef)
}

func boxed(payload uint64, kind Kind) Boxed {
	mantissa := (uint64(kind) << payloadBits) | payload
	u := (uint64(0x7FF) << 52) | mantissa
	return Boxed(u)
}

func (v Boxed) Kind() Kind {
	u := uint64(v)
	if u >= 0x7FF0000000000001 {
		return Kind((u >> payloadBits) & tagMask)
	}
	return KindF64
}

func (v Boxed) I32() int32 {
	return int32(uint64(v) & ((1 << payloadBits) - 1))
}

func (v Boxed) I64() int64 {
	return int64(uint64(v) & ((1 << payloadBits) - 1))
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

func (v Boxed) Ref() uintptr {
	return uintptr(uint64(v) & ((1 << payloadBits) - 1))
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
	case KindBool:
		return v.Bool()
	case KindRef:
		return v.Ref()
	case KindNull:
		return nil
	default:
		panic("unknown kind")
	}
}

func (v Boxed) String() string {
	switch v.Kind() {
	case KindI32, KindI64:
		return fmt.Sprintf("%d", v.Interface())
	case KindF32, KindF64:
		return fmt.Sprintf("%f", v.Interface())
	case KindBool:
		return fmt.Sprintf("%t", v.Interface())
	case KindRef:
		return fmt.Sprintf("ref(%v)", v.Interface())
	case KindNull:
		return "null"
	default:
		return "<invalid>"
	}
}
