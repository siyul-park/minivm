package types

import (
	"fmt"
	"slices"
	"strings"
)

// TypedArray is a dense array of a primitive element type.
type TypedArray[T Scalar] []T

// Array is a heap array of boxed elements of a ref or i64-wide element type.
type Array struct {
	Typ   *ArrayType
	elems []Boxed
	binding
}

// ArrayLiteral is an unmaterialized recursive array constant.
type ArrayLiteral struct {
	Typ   *ArrayType
	Elems []Value
}

// ArrayType is the type of arrays with elements of type Elem.
type ArrayType struct {
	Elem     Type
	ElemKind Kind
}

var (
	typeI1Array  = NewArrayType(TypeI1())
	typeI8Array  = NewArrayType(TypeI8())
	typeI32Array = NewArrayType(TypeI32())
	typeI64Array = NewArrayType(TypeI64())
	typeF32Array = NewArrayType(TypeF32())
	typeF64Array = NewArrayType(TypeF64())
)

var _ Value = TypedArray[bool](nil)

var _ Value = TypedArray[int8](nil)

var _ Value = TypedArray[int32](nil)

var _ Value = TypedArray[int64](nil)

var _ Value = TypedArray[float32](nil)

var _ Value = TypedArray[float64](nil)

var _ Value = (*ArrayLiteral)(nil)

var _ Traceable = (*Array)(nil)

var _ Type = (*ArrayType)(nil)

// TypeI1Array returns the array type with i1 elements.
func TypeI1Array() *ArrayType {
	return typeI1Array
}

// TypeI8Array returns the array type with i8 elements.
func TypeI8Array() *ArrayType {
	return typeI8Array
}

// TypeI32Array returns the array type with i32 elements.
func TypeI32Array() *ArrayType {
	return typeI32Array
}

// TypeI64Array returns the array type with i64 elements.
func TypeI64Array() *ArrayType {
	return typeI64Array
}

// TypeF32Array returns the array type with f32 elements.
func TypeF32Array() *ArrayType {
	return typeF32Array
}

// TypeF64Array returns the array type with f64 elements.
func TypeF64Array() *ArrayType {
	return typeF64Array
}

// NewArray returns an array of type typ holding size zero values of its
// element kind. A reference element, which any reports, is the null word,
// uncounted: release skips null, so it is never released. Any other kind keeps
// zero words, which the caller overwrites. A non-nil heap binds it and
// supplies a header and storage it kept for reuse, cleared to zero words.
func NewArray(heap *Heap, typ *ArrayType, size int) *Array {
	var a *Array
	if heap != nil {
		if kept, ok := heap.arrays.get(); ok {
			a = kept
			a.Typ = typ
			a.heap = heap
			if cap(a.elems) < size {
				a.elems = make([]Boxed, size)
			} else {
				a.elems = a.elems[:size]
			}
		}
	}
	if a == nil {
		a = &Array{Typ: typ, elems: make([]Boxed, size), binding: binding{heap}}
	}
	if typ.ElemKind == KindRef {
		for i := range a.elems {
			a.elems[i] = BoxedNull
		}
	}
	return a
}

// NewArrayType returns the array type with element type elem.
func NewArrayType(elem Type) *ArrayType {
	return &ArrayType{Elem: elem, ElemKind: elem.Kind()}
}

// Len returns the number of elements.
//
//gcassert:inline
func (a *Array) Len() int {
	return len(a.elems)
}

// Index returns element i, which a borrows.
//
//gcassert:inline
func (a *Array) Index(i int) Boxed {
	return a.elems[i]
}

// SetIndex takes v as element i and releases the word it replaces. It panics with
// ErrDetached when it must release and a is unbound.
func (a *Array) SetIndex(i int, v Boxed) {
	old := a.elems[i]
	if !owns(old) {
		a.elems[i] = v
		return
	}
	h := a.bound()
	a.elems[i] = v
	h.Release(old)
}

// SwapIndex takes v as element i and returns the word it replaces, which the caller
// now owns.
//
//gcassert:inline
func (a *Array) SwapIndex(i int, v Boxed) Boxed {
	old := a.elems[i]
	a.elems[i] = v
	return old
}

// Fill takes one reference to v and stores it in n elements from index from,
// adding the other n-1 references. It releases v when n is not positive, and
// releases the replaced words once every store is done.
func (a *Array) Fill(v Boxed, from, n int) {
	if n <= 0 {
		if owns(v) {
			a.bound().Release(v)
		}
		return
	}
	var local [8]Boxed
	old := a.replaced(local[:0], from, n)
	var h *Heap
	if v.Kind() == KindRef || len(old) > 0 {
		h = a.bound()
	}
	if v.Kind() == KindRef {
		h.Inc(v.Ref(), n-1)
	}
	for i := from; i < from+n; i++ {
		a.elems[i] = v
	}
	for _, w := range old {
		h.Release(w)
	}
}

// Copy copies n elements of src from index from to a at index at, which may be
// the same array. It retains the copied words, and releases the replaced words
// once the copy is done.
func (a *Array) Copy(at int, src *Array, from, n int) {
	if n <= 0 {
		return
	}
	words := src.elems[from : from+n]
	var local [8]Boxed
	old := a.replaced(local[:0], at, n)
	var h *Heap
	if len(old) > 0 {
		h = a.bound()
	}
	for _, w := range words {
		if w.Kind() == KindRef {
			if h == nil {
				h = a.bound()
			}
			h.Retain(w)
		}
	}
	copy(a.elems[at:at+n], words)
	for _, w := range old {
		h.Release(w)
	}
}

// Append takes vs and adds them after the last element.
func (a *Array) Append(vs ...Boxed) {
	a.elems = append(a.elems, vs...)
}

// Delete removes element i, shifts the later elements left, and returns
// the removed word, which the caller now owns. The vacated word becomes null.
func (a *Array) Delete(i int) Boxed {
	old := a.elems[i]
	n := len(a.elems) - 1
	copy(a.elems[i:], a.elems[i+1:])
	a.elems[n] = BoxedNull
	a.elems = a.elems[:n]
	return old
}

// Kind returns KindRef.
func (a TypedArray[T]) Kind() Kind {
	return KindRef
}

// Type returns the array type matching the element type.
func (a TypedArray[T]) Type() Type {
	var zero T
	switch any(zero).(type) {
	case bool:
		return TypeI1Array()
	case int8:
		return TypeI8Array()
	case int32:
		return TypeI32Array()
	case int64:
		return TypeI64Array()
	case float32:
		return TypeF32Array()
	default:
		return TypeF64Array()
	}
}

// Clone returns a copy of a with its own backing storage.
func (a TypedArray[T]) Clone() TypedArray[T] {
	return slices.Clone(a)
}

func (a TypedArray[T]) String() string {
	return formatSlice(a.Type(), len(a), func(i int) string { return formatElem(a[i]) })
}

func (a *Array) Kind() Kind {
	return KindRef
}

func (a *Array) Type() Type {
	return a.Typ
}

func (a *Array) Refs(dst []Ref) []Ref {
	for _, e := range a.elems {
		if e.Kind() == KindRef {
			dst = append(dst, Ref(e.Ref()))
		}
	}
	return dst
}

// Clone returns a copy of a with its own element slice. A bound copy retains
// every reference it holds, null included, so both arrays own theirs. An
// unbound copy shares its words.
func (a *Array) Clone() *Array {
	c := &Array{Typ: a.Typ, elems: slices.Clone(a.elems), binding: a.binding}
	if a.heap != nil {
		for _, e := range c.elems {
			a.heap.Retain(e)
		}
	}
	return c
}

func (a *Array) String() string {
	return formatSlice(a.Type(), len(a.elems), func(i int) string { return a.elems[i].String() })
}

// Kind reports the reference kind.
func (a *ArrayLiteral) Kind() Kind {
	return KindRef
}

// Type returns the array literal type.
func (a *ArrayLiteral) Type() Type {
	return a.Typ
}

// String returns the canonical array literal.
func (a *ArrayLiteral) String() string {
	return formatSlice(a.Type(), len(a.Elems), func(i int) string { return a.Elems[i].String() })
}

func (t *ArrayType) Kind() Kind {
	return KindRef
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

func (t *ArrayType) String() string {
	return "[]" + t.Elem.String()
}

func formatElem[T Scalar](v T) string {
	switch x := any(v).(type) {
	case bool:
		return I1(x).String()
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

func (a TypedArray[T]) clone() Value {
	return a.Clone()
}

// replaced appends the words of a that owns a reference in [from, from+n) to
// dst.
func (a *Array) replaced(dst []Boxed, from, n int) []Boxed {
	for _, w := range a.elems[from : from+n] {
		if owns(w) {
			dst = append(dst, w)
		}
	}
	return dst
}

// owns reports whether releasing w changes a count: w is a reference but not
// null.
func owns(w Boxed) bool {
	return w.Kind() == KindRef && w.Ref() != 0
}

func (a *Array) clone() Value {
	return a.Clone()
}
