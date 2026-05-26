package types

import (
	"fmt"
	"strings"
)

type I32Array []int32

type I64Array []int64

type F32Array []float32

type F64Array []float64

type Array struct {
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

var _ Value = I32Array(nil)
var _ Value = I64Array(nil)
var _ Value = F32Array(nil)
var _ Value = F64Array(nil)
var _ Traceable = (*Array)(nil)
var _ Type = (*ArrayType)(nil)

func (a I32Array) Kind() Kind { return KindRef }
func (a I32Array) Type() Type { return TypeI32Array }
func (a I32Array) String() string {
	return formatSlice(TypeI32Array, len(a), func(i int) string { return fmt.Sprintf("%d", a[i]) })
}

func (a I64Array) Kind() Kind { return KindRef }
func (a I64Array) Type() Type { return TypeI64Array }
func (a I64Array) String() string {
	return formatSlice(TypeI64Array, len(a), func(i int) string { return fmt.Sprintf("%d", a[i]) })
}

func (a F32Array) Kind() Kind { return KindRef }
func (a F32Array) Type() Type { return TypeF32Array }
func (a F32Array) String() string {
	return formatSlice(TypeF32Array, len(a), func(i int) string { return fmt.Sprintf("%g", a[i]) })
}

func (a F64Array) Kind() Kind { return KindRef }
func (a F64Array) Type() Type { return TypeF64Array }
func (a F64Array) String() string {
	return formatSlice(TypeF64Array, len(a), func(i int) string { return fmt.Sprintf("%g", a[i]) })
}

func NewArray(typ *ArrayType, elems ...Boxed) *Array {
	return &Array{Typ: typ, Elems: elems}
}

func (a *Array) Kind() Kind     { return KindRef }
func (a *Array) Type() Type     { return a.Typ }
func (a *Array) String() string { return joinElems(a.Type(), a.Elems) }

func (a *Array) Refs() []Ref {
	var refs []Ref
	for _, e := range a.Elems {
		if e.Kind() == KindRef {
			refs = append(refs, Ref(e.Ref()))
		}
	}
	return refs
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

func joinElems[T interface{ String() string }](typ Type, elems []T) string {
	var sb strings.Builder
	sb.WriteString(typ.String())
	sb.WriteByte('{')
	for i, e := range elems {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(e.String())
	}
	sb.WriteByte('}')
	return sb.String()
}

func NewArrayType(elem Type) *ArrayType {
	return &ArrayType{Elem: elem, ElemKind: elem.Kind()}
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
