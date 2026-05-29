package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoxedArray_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewBoxedArray(NewArrayType(TypeRef)).Kind())
}

func TestBoxedArray_Type(t *testing.T) {
	typ := NewArrayType(TypeRef)
	require.Equal(t, typ, NewBoxedArray(typ).Type())
}

func TestBoxedArray_String(t *testing.T) {
	a := NewBoxedArray(NewArrayType(TypeRef), BoxI32(1), BoxI32(2), BoxI32(3))
	require.Equal(t, "[]ref{1, 2, 3}", a.String())
}

func TestArray_Kind(t *testing.T) {
	tests := []Value{
		Array[int32]{},
		Array[int64]{},
		Array[float32]{},
		Array[float64]{},
	}
	for _, val := range tests {
		t.Run(fmt.Sprint(val), func(t *testing.T) {
			require.Equal(t, KindRef, val.Kind())
		})
	}
}

func TestArray_Type(t *testing.T) {
	tests := []struct {
		val Value
		typ Type
	}{
		{val: Array[int32]{}, typ: TypeI32Array},
		{val: Array[int64]{}, typ: TypeI64Array},
		{val: Array[float32]{}, typ: TypeF32Array},
		{val: Array[float64]{}, typ: TypeF64Array},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.typ, tt.val.Type())
		})
	}
}

func TestArray_String(t *testing.T) {
	tests := []struct {
		val Value
		str string
	}{
		{val: Array[int32]{1, 2, 3}, str: "[]i32{1, 2, 3}"},
		{val: Array[int64]{1, 2, 3}, str: "[]i64{1, 2, 3}"},
		{val: Array[float32]{1, 2, 3}, str: "[]f32{1, 2, 3}"},
		{val: Array[float64]{1, 2, 3}, str: "[]f64{1, 2, 3}"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.str, tt.val.String())
		})
	}
}

func TestBoxedArray_Refs(t *testing.T) {
	t.Run("primitive elements", func(t *testing.T) {
		a := NewBoxedArray(NewArrayType(TypeI32), BoxI32(1), BoxI32(2))

		require.Empty(t, a.Refs())
		var refs []Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = a.Refs()
		})
		require.Empty(t, refs)
		require.Zero(t, allocs)
	})

	t.Run("reference elements", func(t *testing.T) {
		a := NewBoxedArray(NewArrayType(TypeRef), BoxRef(1), BoxI32(2), BoxRef(3))

		require.Equal(t, []Ref{1, 3}, a.Refs())
	})
}

func BenchmarkArray_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		a := NewBoxedArray(NewArrayType(TypeI32), BoxI32(1), BoxI32(2))

		var refs []Ref
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			refs = a.Refs()
		}
		b.StopTimer()
		require.Empty(b, refs)
	})

	b.Run("child refs", func(b *testing.B) {
		a := NewBoxedArray(NewArrayType(TypeRef), BoxRef(1), BoxRef(2))

		var refs []Ref
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			refs = a.Refs()
		}
		b.StopTimer()
		require.Len(b, refs, 2)
	})
}
