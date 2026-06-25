package types

import "fmt"

type I32 int32

type I64 int64
type F32 float32
type F64 float64

// I1 is a boolean value (the i1 type). It shares the i32 representation but
// keeps its own kind/type so it can be pushed, popped, and stored as a distinct
// bool without an extra runtime representation.
type I1 bool

// I8 is an 8-bit integer value (the i8 type / byte). It shares the i32
// representation but keeps its own kind/type.
type I8 int8

type Ref int32

type i1Type struct{}
type i8Type struct{}

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
	TypeI1  = i1Type{}
	TypeI8  = i8Type{}
	TypeI32 = i32Type{}
	TypeI64 = i64Type{}
	TypeF32 = f32Type{}
	TypeF64 = f64Type{}
	TypeRef = refType{}
)

var _ Value = I1(false)
var _ Value = I8(0)
var _ Value = I32(0)
var _ Value = I64(0)
var _ Value = F32(0)
var _ Value = F64(0)
var _ Value = Ref(0)

var _ Type = i1Type{}
var _ Type = i8Type{}
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

func (b I1) Kind() Kind {
	return KindI1
}

func (b I1) Type() Type {
	return TypeI1
}

func (b I1) String() string {
	if b {
		return "true"
	}
	return "false"
}

func (i I8) Kind() Kind {
	return KindI8
}

func (i I8) Type() Type {
	return TypeI8
}

func (i I8) String() string {
	return fmt.Sprintf("%d", int8(i))
}

func (i I32) Kind() Kind {
	return KindI32
}

func (i I32) Type() Type {
	return TypeI32
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

func (i I64) String() string {
	return fmt.Sprintf("%d", int64(i))
}

func (f F32) Kind() Kind {
	return KindF32
}

func (f F32) Type() Type {
	return TypeF32
}

func (f F32) String() string {
	return fmt.Sprintf("%g", float32(f))
}

func (f F64) Kind() Kind {
	return KindF64
}

func (f F64) Type() Type {
	return TypeF64
}

func (f F64) String() string {
	return fmt.Sprintf("%g", float64(f))
}

func (r Ref) Kind() Kind {
	return KindRef
}

func (r Ref) Type() Type {
	return TypeRef
}

func (r Ref) String() string {
	return fmt.Sprintf("%d", r)
}

func (i1Type) Kind() Kind {
	return KindI1
}

func (i1Type) String() string {
	return "i1"
}

func (i1Type) Cast(other Type) bool {
	return other == TypeI1
}

func (i1Type) Equals(other Type) bool {
	return other == TypeI1
}

func (i8Type) Kind() Kind {
	return KindI8
}

func (i8Type) String() string {
	return "i8"
}

func (i8Type) Cast(other Type) bool {
	return other == TypeI8
}

func (i8Type) Equals(other Type) bool {
	return other == TypeI8
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
