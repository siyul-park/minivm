package types

import (
	"fmt"
	"strings"
)

type TypedArray[T int8 | int32 | int64 | float32 | float64] []T

type Array struct {
	Typ   *ArrayType
	Elems []Boxed
}

type ArrayType struct {
	Elem     Type
	ElemKind Kind
}

var (
	TypeI8Array  = NewArrayType(TypeI8)
	TypeI32Array = NewArrayType(TypeI32)
	TypeI64Array = NewArrayType(TypeI64)
	TypeF32Array = NewArrayType(TypeF32)
	TypeF64Array = NewArrayType(TypeF64)
)

var _ Value = TypedArray[int8](nil)
var _ Value = TypedArray[int32](nil)
var _ Value = TypedArray[int64](nil)
var _ Value = TypedArray[float32](nil)
var _ Value = TypedArray[float64](nil)
var _ Traceable = (*Array)(nil)
var _ Type = (*ArrayType)(nil)

func NewArray(typ *ArrayType, elems ...Boxed) *Array {
	return &Array{Typ: typ, Elems: elems}
}

func NewArrayType(elem Type) *ArrayType {
	return &ArrayType{Elem: elem, ElemKind: elem.Kind()}
}

func (a TypedArray[T]) Kind() Kind { return KindRef }

func (a TypedArray[T]) Type() Type {
	var zero T
	switch any(zero).(type) {
	case int8:
		return TypeI8Array
	case int32:
		return TypeI32Array
	case int64:
		return TypeI64Array
	case float32:
		return TypeF32Array
	default:
		return TypeF64Array
	}
}

func (a TypedArray[T]) String() string {
	return formatSlice(a.Type(), len(a), func(i int) string { return formatElem(a[i]) })
}

func (a *Array) Kind() Kind { return KindRef }
func (a *Array) Type() Type { return a.Typ }
func (a *Array) String() string {
	return formatSlice(a.Type(), len(a.Elems), func(i int) string { return a.Elems[i].String() })
}

func (a *Array) Refs() []Ref {
	var refs []Ref
	for _, e := range a.Elems {
		if e.Kind() == KindRef {
			if refs == nil {
				refs = make([]Ref, 0, len(a.Elems))
			}
			refs = append(refs, Ref(e.Ref()))
		}
	}
	return refs
}

func (t *ArrayType) Kind() Kind { return KindRef }

func (t *ArrayType) String() string {
	return "[]" + t.Elem.String()
}

func (t *ArrayType) Cast(other Type) bool {
	return t.Equals(other)
}

func (t *ArrayType) Equals(other Type) bool {
	if t == other {
		return true
	}
	if o, ok := other.(*ArrayType); ok {
		return t.Elem.Equals(o.Elem)
	}
	return false
}

func formatElem[T int8 | int32 | int64 | float32 | float64](v T) string {
	switch x := any(v).(type) {
	case int8:
		return fmt.Sprintf("%d", x)
	case int32:
		return fmt.Sprintf("%d", x)
	case int64:
		return fmt.Sprintf("%d", x)
	default:
		return fmt.Sprintf("%g", x)
	}
}

func formatSlice(typ Type, n int, elem func(int) string) string {
	var sb strings.Builder
	sb.WriteString(typ.String())
	sb.WriteByte('{')
	for i := range n {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(elem(i))
	}
	sb.WriteByte('}')
	return sb.String()
}
