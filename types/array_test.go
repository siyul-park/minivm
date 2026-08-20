package types_test

import (
	"fmt"
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewArray(t *testing.T) {
	typ := types.NewArrayType(types.TypeAny)
	elems := []types.Boxed{types.BoxRef(1), types.BoxRef(2)}
	array := types.NewArray(typ, elems...)

	require.Same(t, typ, array.Typ)
	require.Equal(t, elems, array.Elems)
}

func TestNewArrayType(t *testing.T) {
	typ := types.NewArrayType(types.TypeI32)
	require.Equal(t, types.TypeI32, typ.Elem)
}

func TestTypedArray_Kind(t *testing.T) {
	tests := []types.Value{types.TypedArray[int8]{}, types.TypedArray[int32]{}, types.TypedArray[int64]{}, types.TypedArray[float32]{}, types.TypedArray[float64]{}}
	for _, val := range tests {
		t.Run(fmt.Sprint(val), func(t *testing.T) {
			require.Equal(t, types.KindRef, val.Kind())
		})
	}
}

func TestTypedArray_Type(t *testing.T) {
	tests := []struct {
		val types.Value
		typ types.Type
	}{
		{val: types.TypedArray[int8]{}, typ: types.TypeI8Array},
		{val: types.TypedArray[int32]{}, typ: types.TypeI32Array},
		{val: types.TypedArray[int64]{}, typ: types.TypeI64Array},
		{val: types.TypedArray[float32]{}, typ: types.TypeF32Array},
		{val: types.TypedArray[float64]{}, typ: types.TypeF64Array},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.typ, tt.val.Type())
		})
	}
}

func TestTypedArray_String(t *testing.T) {
	tests := []struct {
		val types.Value
		str string
	}{
		{val: types.TypedArray[int8]{1, 2, 3}, str: "[]i8{1, 2, 3}"},
		{val: types.TypedArray[int32]{1, 2, 3}, str: "[]i32{1, 2, 3}"},
		{val: types.TypedArray[int64]{1, 2, 3}, str: "[]i64{1, 2, 3}"},
		{val: types.TypedArray[float32]{1, 2, 3}, str: "[]f32{1, 2, 3}"},
		{val: types.TypedArray[float64]{1, 2, 3}, str: "[]f64{1, 2, 3}"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.str, tt.val.String())
		})
	}
}

func TestArray_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewArray(types.NewArrayType(types.TypeAny)).Kind())
}

func TestArray_Type(t *testing.T) {
	typ := types.NewArrayType(types.TypeAny)
	require.Equal(t, typ, types.NewArray(typ).Type())
}

func TestArray_String(t *testing.T) {
	a := types.NewArray(types.NewArrayType(types.TypeAny), types.BoxI32(1), types.BoxI32(2), types.BoxI32(3))
	require.Equal(t, "[]any{1, 2, 3}", a.String())
}

func TestArray_Refs(t *testing.T) {
	t.Run("primitive elements", func(t *testing.T) {
		a := types.NewArray(types.NewArrayType(types.TypeI32), types.BoxI32(1), types.BoxI32(2))

		require.Equal(t, []types.Ref{9}, a.Refs([]types.Ref{9}))
		var refs []types.Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = a.Refs(nil)
		})
		require.Empty(t, refs)
		require.Zero(t, allocs)
	})

	t.Run("reference elements", func(t *testing.T) {
		a := types.NewArray(types.NewArrayType(types.TypeAny), types.BoxRef(1), types.BoxI32(2), types.BoxRef(3))

		require.Equal(t, []types.Ref{9, 1, 3}, a.Refs([]types.Ref{9}))
	})
}

func TestArrayType_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewArrayType(types.TypeI32).Kind())
}

func TestArrayType_String(t *testing.T) {
	require.Equal(t, "[]i32", types.NewArrayType(types.TypeI32).String())
}

func TestArrayType_Cast(t *testing.T) {
	typ := types.NewArrayType(types.TypeI32)

	require.True(t, typ.Cast(types.NewArrayType(types.TypeI32)))
	require.False(t, typ.Cast(types.NewArrayType(types.TypeI64)))
	require.False(t, typ.Cast(types.TypeI32))
}

func TestArrayType_Equals(t *testing.T) {
	typ := types.NewArrayType(types.TypeI32)

	require.True(t, typ.Equals(typ))
	require.True(t, typ.Equals(types.NewArrayType(types.TypeI32)))
	require.False(t, typ.Equals(types.NewArrayType(types.TypeI64)))
	require.False(t, typ.Equals(types.TypeI32))
}

func BenchmarkArray_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		a := types.NewArray(types.NewArrayType(types.TypeI32), types.BoxI32(1), types.BoxI32(2))
		require.Empty(b, a.Refs(nil))

		var refs []types.Ref
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = a.Refs(nil)
		}
		b.StopTimer()
		require.Empty(b, refs)
	})

	b.Run("child refs", func(b *testing.B) {
		a := types.NewArray(types.NewArrayType(types.TypeAny), types.BoxRef(1), types.BoxRef(2))
		require.Equal(b, []types.Ref{1, 2}, a.Refs(nil))

		refs := make([]types.Ref, 0, 2)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = a.Refs(refs[:0])
		}
		b.StopTimer()
		require.Equal(b, []types.Ref{1, 2}, refs)
	})
}
