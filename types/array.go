package types

import (
	"fmt"
	"strings"
)

type Array[T int32 | int64 | float32 | float64] []T

type BoxedArray struct {
	Typ   *ArrayType
	Elems []Boxed
}

type ArrayType struct {
	Elem     Type
	ElemKind Kind
}

var (
	TypeI32Array = NewArrayType(TypeI32)
	TypeI64Array = NewArrayType(TypeI64)
	TypeF32Array = NewArrayType(TypeF32)
	TypeF64Array = NewArrayType(TypeF64)
)

var _ Value = Array[int32](nil)
var _ Value = Array[int64](nil)
var _ Value = Array[float32](nil)
var _ Value = Array[float64](nil)
var _ Traceable = (*BoxedArray)(nil)
var _ Type = (*ArrayType)(nil)

func NewBoxedArray(typ *ArrayType, elems ...Boxed) *BoxedArray {
	return &BoxedArray{Typ: typ, Elems: elems}
}

func NewArrayType(elem Type) *ArrayType {
	return &ArrayType{Elem: elem, ElemKind: elem.Kind()}
}

func (a Array[T]) Kind() Kind { return KindRef }

func (a Array[T]) Type() Type {
	var zero T
	switch any(zero).(type) {
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

func (a Array[T]) String() string {
	return formatSlice(a.Type(), len(a), func(i int) string { return formatElem(a[i]) })
}

func (a *BoxedArray) Kind() Kind { return KindRef }
func (a *BoxedArray) Type() Type { return a.Typ }
func (a *BoxedArray) String() string {
	return formatSlice(a.Type(), len(a.Elems), func(i int) string { return a.Elems[i].String() })
}

func (a *BoxedArray) Refs() []Ref {
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

func formatElem[T int32 | int64 | float32 | float64](v T) string {
	switch x := any(v).(type) {
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
