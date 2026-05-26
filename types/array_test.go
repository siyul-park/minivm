package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArray_Kind(t *testing.T) {
	tests := []struct {
		val  Value
		kind Kind
	}{
		{
			val:  I32Array{},
			kind: KindRef,
		},
		{
			val:  I64Array{},
			kind: KindRef,
		},
		{
			val:  F32Array{},
			kind: KindRef,
		},
		{
			val:  F64Array{},
			kind: KindRef,
		},
		{
			val:  NewArray(NewArrayType(TypeRef)),
			kind: KindRef,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.kind, tt.val.Kind())
		})
	}
}

func TestArray_Type(t *testing.T) {
	tests := []struct {
		val Value
		typ Type
	}{
		{
			val: I32Array{},
			typ: TypeI32Array,
		},
		{
			val: I64Array{},
			typ: TypeI64Array,
		},
		{
			val: F32Array{},
			typ: TypeF32Array,
		},
		{
			val: F64Array{},
			typ: TypeF64Array,
		},
		{
			val: NewArray(NewArrayType(TypeRef)),
			typ: NewArrayType(TypeRef),
		},
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
		{
			val: I32Array{1, 2, 3},
			str: "[]i32{1, 2, 3}",
		},
		{
			val: I64Array{1, 2, 3},
			str: "[]i64{1, 2, 3}",
		},
		{
			val: F32Array{1, 2, 3},
			str: "[]f32{1, 2, 3}",
		},
		{
			val: F64Array{1, 2, 3},
			str: "[]f64{1, 2, 3}",
		},
		{
			val: NewArray(NewArrayType(TypeRef), BoxI32(1), BoxI32(2), BoxI32(3)),
			str: "[]ref{1, 2, 3}",
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.str, tt.val.String())
		})
	}
}

func TestArray_Refs(t *testing.T) {
	t.Run("primitive elements", func(t *testing.T) {
		a := NewArray(NewArrayType(TypeI32), BoxI32(1), BoxI32(2))

		require.Empty(t, a.Refs())
		var refs []Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = a.Refs()
		})
		require.Empty(t, refs)
		require.Zero(t, allocs)
	})

	t.Run("reference elements", func(t *testing.T) {
		a := NewArray(NewArrayType(TypeRef), BoxRef(1), BoxI32(2), BoxRef(3))

		require.Equal(t, []Ref{1, 3}, a.Refs())
	})
}

func BenchmarkArray_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		a := NewArray(NewArrayType(TypeI32), BoxI32(1), BoxI32(2))

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
		a := NewArray(NewArrayType(TypeRef), BoxRef(1), BoxRef(2))

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
