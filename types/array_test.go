package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewArray(t *testing.T) {
	typ := NewArrayType(TypeRef)
	elems := []Boxed{BoxRef(1), BoxRef(2)}
	array := NewArray(typ, elems...)

	require.Same(t, typ, array.Typ)
	require.Equal(t, elems, array.Elems)
}

func TestNewArrayType(t *testing.T) {
	typ := NewArrayType(TypeI32)
	require.Equal(t, TypeI32, typ.Elem)
}

func TestTypedArray_Kind(t *testing.T) {
	tests := []Value{
		TypedArray[int8]{},
		TypedArray[int32]{},
		TypedArray[int64]{},
		TypedArray[float32]{},
		TypedArray[float64]{},
	}
	for _, val := range tests {
		t.Run(fmt.Sprint(val), func(t *testing.T) {
			require.Equal(t, KindRef, val.Kind())
		})
	}
}

func TestTypedArray_Type(t *testing.T) {
	tests := []struct {
		val Value
		typ Type
	}{
		{val: TypedArray[int8]{}, typ: TypeI8Array},
		{val: TypedArray[int32]{}, typ: TypeI32Array},
		{val: TypedArray[int64]{}, typ: TypeI64Array},
		{val: TypedArray[float32]{}, typ: TypeF32Array},
		{val: TypedArray[float64]{}, typ: TypeF64Array},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.typ, tt.val.Type())
		})
	}
}

func TestTypedArray_String(t *testing.T) {
	tests := []struct {
		val Value
		str string
	}{
		{val: TypedArray[int8]{1, 2, 3}, str: "[]i8{1, 2, 3}"},
		{val: TypedArray[int32]{1, 2, 3}, str: "[]i32{1, 2, 3}"},
		{val: TypedArray[int64]{1, 2, 3}, str: "[]i64{1, 2, 3}"},
		{val: TypedArray[float32]{1, 2, 3}, str: "[]f32{1, 2, 3}"},
		{val: TypedArray[float64]{1, 2, 3}, str: "[]f64{1, 2, 3}"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.str, tt.val.String())
		})
	}
}

func TestArray_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewArray(NewArrayType(TypeRef)).Kind())
}

func TestArray_Type(t *testing.T) {
	typ := NewArrayType(TypeRef)
	require.Equal(t, typ, NewArray(typ).Type())
}

func TestArray_String(t *testing.T) {
	a := NewArray(NewArrayType(TypeRef), BoxI32(1), BoxI32(2), BoxI32(3))
	require.Equal(t, "[]ref{1, 2, 3}", a.String())
}

func TestArray_Refs(t *testing.T) {
	t.Run("primitive elements", func(t *testing.T) {
		a := NewArray(NewArrayType(TypeI32), BoxI32(1), BoxI32(2))

		require.Equal(t, []Ref{9}, a.Refs([]Ref{9}))
		var refs []Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = a.Refs(nil)
		})
		require.Empty(t, refs)
		require.Zero(t, allocs)
	})

	t.Run("reference elements", func(t *testing.T) {
		a := NewArray(NewArrayType(TypeRef), BoxRef(1), BoxI32(2), BoxRef(3))

		require.Equal(t, []Ref{9, 1, 3}, a.Refs([]Ref{9}))
	})
}

func TestArrayType_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewArrayType(TypeI32).Kind())
}

func TestArrayType_String(t *testing.T) {
	require.Equal(t, "[]i32", NewArrayType(TypeI32).String())
}

func TestArrayType_Cast(t *testing.T) {
	typ := NewArrayType(TypeI32)

	require.True(t, typ.Cast(NewArrayType(TypeI32)))
	require.False(t, typ.Cast(NewArrayType(TypeI64)))
	require.False(t, typ.Cast(TypeI32))
}

func TestArrayType_Equals(t *testing.T) {
	typ := NewArrayType(TypeI32)

	require.True(t, typ.Equals(typ))
	require.True(t, typ.Equals(NewArrayType(TypeI32)))
	require.False(t, typ.Equals(NewArrayType(TypeI64)))
	require.False(t, typ.Equals(TypeI32))
}

func BenchmarkArray_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		a := NewArray(NewArrayType(TypeI32), BoxI32(1), BoxI32(2))
		require.Empty(b, a.Refs(nil))

		var refs []Ref
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = a.Refs(nil)
		}
		b.StopTimer()
		require.Empty(b, refs)
	})

	b.Run("child refs", func(b *testing.B) {
		a := NewArray(NewArrayType(TypeRef), BoxRef(1), BoxRef(2))
		require.Equal(b, []Ref{1, 2}, a.Refs(nil))

		refs := make([]Ref, 0, 2)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = a.Refs(refs[:0])
		}
		b.StopTimer()
		require.Equal(b, []Ref{1, 2}, refs)
	})
}
