package types

import "fmt"

type I32 int32

type I64 int64
type F32 float32
type F64 float64
type Ref int32

var (
	False = I32(0)
	True  = I32(1)
)

var _ Value = I32(0)
var _ Value = I64(0)
var _ Value = F32(0)
var _ Value = F64(0)
var _ Value = Ref(0)

func Bool(b bool) I32 {
	if b {
		return I32(1)
	}
	return I32(0)
}

func (i I32) Interface() any {
	return int32(i)
}

func (i I32) String() string {
	return fmt.Sprintf("%d", int32(i))
}

func (i I64) Interface() any {
	return int64(i)
}

func (i I64) String() string {
	return fmt.Sprintf("%d", int64(i))
}

func (f F32) Interface() any {
	return float32(f)
}

func (f F32) String() string {
	return fmt.Sprintf("%f", float32(f))
}

func (f F64) Interface() any {
	return float64(f)
}

func (f F64) String() string {
	return fmt.Sprintf("%f", float64(f))
}

func (r Ref) Interface() any {
	return int32(r)
}

func (r Ref) String() string {
	return fmt.Sprintf("%d", int32(r))
}
