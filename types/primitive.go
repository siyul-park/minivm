package types

import "fmt"

type I32 int32

type I64 int64
type F32 float32
type F64 float64

type Ref int32

type i32Type struct{}

type i64Type struct{}
type f32Type struct{}
type f64Type struct{}
type refType struct{}

var (
	Null  = Ref(0)
	False = I32(0)
	True  = I32(1)
)

var (
	TypeI32 = i32Type{}
	TypeI64 = i64Type{}
	TypeF32 = f32Type{}
	TypeF64 = f64Type{}
	TypeRef = refType{}
)

var _ Value = I32(0)
var _ Value = I64(0)
var _ Value = F32(0)
var _ Value = F64(0)
var _ Value = Ref(0)

var _ Type = i32Type{}
var _ Type = i64Type{}
var _ Type = f32Type{}
var _ Type = f64Type{}
var _ Type = refType{}

func Bool(b bool) I32 {
	if b {
		return I32(1)
	}
	return I32(0)
}

func (i I32) Kind() Kind {
	return KindI32
}

func (i I32) Type() Type {
	return TypeI32
}

func (i I32) Interface() any {
	return int32(i)
}

func (i I32) String() string {
	return fmt.Sprintf("%d", int32(i))
}

func (i I64) Kind() Kind {
	return KindI64
}

func (i I64) Type() Type {
	return TypeI64
}

func (i I64) Interface() any {
	return int64(i)
}

func (i I64) String() string {
	return fmt.Sprintf("%d", int64(i))
}

func (f F32) Kind() Kind {
	return KindF32
}

func (f F32) Type() Type {
	return TypeF32
}

func (f F32) Interface() any {
	return float32(f)
}

func (f F32) String() string {
	return fmt.Sprintf("%f", float32(f))
}

func (f F64) Kind() Kind {
	return KindF64
}

func (f F64) Type() Type {
	return f64Type{}
}

func (f F64) Interface() any {
	return float64(f)
}

func (f F64) String() string {
	return fmt.Sprintf("%f", float64(f))
}

func (r Ref) Kind() Kind {
	return KindRef
}

func (r Ref) Type() Type {
	return TypeRef
}

func (r Ref) Interface() any {
	if r == 0 {
		return nil
	}
	return int(r)
}

func (r Ref) String() string {
	return fmt.Sprintf("%d", r)
}

func (i32Type) Kind() Kind {
	return KindI32
}

func (i32Type) String() string {
	return "i32"
}

func (i32Type) Cast(other Type) bool {
	return other == TypeI32
}

func (i32Type) Equals(other Type) bool {
	return other == TypeI32
}

func (i64Type) Kind() Kind {
	return KindI64
}

func (i64Type) String() string {
	return "i64"
}

func (i64Type) Cast(other Type) bool {
	return other == TypeI64
}

func (i64Type) Equals(other Type) bool {
	return other == TypeI64
}

func (f32Type) Kind() Kind {
	return KindF32
}

func (f32Type) String() string {
	return "f32"
}

func (f32Type) Cast(other Type) bool {
	return other == TypeF32
}

func (f32Type) Equals(other Type) bool {
	return other == TypeF32
}

func (f64Type) Kind() Kind {
	return KindF64
}

func (f64Type) String() string {
	return "f64"
}

func (f64Type) Cast(other Type) bool {
	return other == TypeF64
}

func (f64Type) Equals(other Type) bool {
	return other == TypeF64
}

func (refType) Kind() Kind {
	return KindRef
}

func (refType) String() string {
	return "ref"
}

func (refType) Cast(_ Type) bool {
	return true
}

func (refType) Equals(other Type) bool {
	return other == TypeRef
}
