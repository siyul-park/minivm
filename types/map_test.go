package types_test

import (
	"math"
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewTypedMap(t *testing.T) {
	typ := types.NewMapType(types.TypeI32, types.TypeAny)
	m := types.NewTypedMap[int32](typ, 4)
	require.Same(t, typ, m.Typ)
	require.Equal(t, types.BoxedNull, m.Zero)
	require.Zero(t, m.Len())
}
func TestNewMap(t *testing.T) {
	typ := types.NewMapType(types.TypeAny, types.TypeI32)
	m := types.NewMap(typ)
	require.Same(t, typ, m.Typ)
	require.Equal(t, types.BoxI32(0), m.Zero)
	require.Zero(t, m.Len())
}
func TestNewMapForType(t *testing.T) {
	structType := types.NewStructType(types.NewStructField(types.TypeI32))
	tests := []struct {
		typ  *types.MapType
		want any
	}{
		{typ: types.NewMapType(types.TypeI32, types.TypeI32), want: (*types.TypedMap[int32])(nil)},
		{typ: types.NewMapType(types.TypeI64, types.TypeI32), want: (*types.TypedMap[int64])(nil)},
		{typ: types.NewMapType(types.TypeF32, types.TypeI32), want: (*types.TypedMap[float32])(nil)},
		{typ: types.NewMapType(types.TypeF64, types.TypeI32), want: (*types.TypedMap[float64])(nil)},
		{typ: types.NewMapType(types.TypeAny, types.TypeI32), want: (*types.Map)(nil)},
		{typ: types.NewMapType(types.TypeString, types.TypeI32), want: (*types.TypedMap[string])(nil)},
		{typ: types.NewMapType(structType, types.TypeI32), want: (*types.Map)(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.typ.Key.String(), func(t *testing.T) {
			require.IsType(t, tt.want, types.NewMapForType(tt.typ, 0))
		})
	}
}
func TestNewMapWithCapacity(t *testing.T) {
	typ := types.NewMapType(types.TypeI32, types.TypeAny)
	m := types.NewMapWithCapacity(typ, 8)
	require.Same(t, typ, m.Typ)
	require.Equal(t, types.BoxedNull, m.Zero)
	require.Zero(t, m.Len())
}
func TestNewMapIterator(t *testing.T) {
	m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64, types.TypeI32), 0)
	it := types.NewMapIterator(7, m)
	require.Equal(t, types.NewIteratorType(types.TypeI64), it.Type())
	require.True(t, it.Done())
}
func TestNewMapType(t *testing.T) {
	t.Run("string key and i64 value", func(t *testing.T) {
		typ := types.NewMapType(types.TypeString, types.TypeI64)
		require.Equal(t, types.TypeString, typ.Key)
		require.Equal(t, types.TypeI64, typ.Elem)
		require.Equal(t, types.KindRef, typ.KeyKind)
		require.Equal(t, types.KindI64, typ.ElemKind)
		require.False(t, typ.TraceKeys)
		require.True(t, typ.TraceValues)
	})

	t.Run("primitive key and value", func(t *testing.T) {
		typ := types.NewMapType(types.TypeI32, types.TypeI32)
		require.False(t, typ.TraceKeys)
		require.False(t, typ.TraceValues)
	})
}
func TestTypedMap_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewTypedMap[int32](types.NewMapType(types.TypeI32, types.TypeI32), 0).Kind())
}
func TestTypedMap_Type(t *testing.T) {
	typ := types.NewMapType(types.TypeI32, types.TypeI32)
	require.Equal(t, typ, types.NewTypedMap[int32](typ, 0).Type())
}
func TestTypedMap_Get(t *testing.T) {
	t.Run("i32", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32, types.TypeI32), 0)
		m.Set(1, types.BoxI32(2))
		got, ok := m.Get(1)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), got)
		_, ok = m.Get(2)
		require.False(t, ok)
	})

	t.Run("i64 wide key", func(t *testing.T) {
		m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64, types.TypeI32), 0)
		m.Set(1<<50, types.BoxI32(2))
		got, ok := m.Get(1 << 50)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), got)
		_, ok = m.Get(2)
		require.False(t, ok)
	})

	t.Run("f64", func(t *testing.T) {
		m := types.NewTypedMap[float64](types.NewMapType(types.TypeF64, types.TypeI32), 0)
		m.Set(1.5, types.BoxI32(2))
		got, ok := m.Get(1.5)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), got)
	})
}
func TestTypedMap_Set(t *testing.T) {
	t.Run("overwrite returns old", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32, types.TypeI32), 0)
		old, ok := m.Set(1, types.BoxI32(2))
		require.False(t, ok)
		require.Equal(t, types.Boxed(0), old)

		old, ok = m.Set(1, types.BoxI32(3))
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), old)
	})

	t.Run("f32 -0.0 collapses to +0.0", func(t *testing.T) {
		m := types.NewTypedMap[float32](types.NewMapType(types.TypeF32, types.TypeI32), 0)
		m.Set(float32(math.Copysign(0, -1)), types.BoxI32(1))
		m.Set(0, types.BoxI32(2))

		got, ok := m.Get(float32(math.Copysign(0, -1)))
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), got)
		require.Equal(t, 1, m.Len())
	})

	t.Run("f64 -0.0 collapses to +0.0", func(t *testing.T) {
		m := types.NewTypedMap[float64](types.NewMapType(types.TypeF64, types.TypeI32), 0)
		m.Set(math.Copysign(0, -1), types.BoxI32(1))
		m.Set(0, types.BoxI32(2))

		got, ok := m.Get(math.Copysign(0, -1))
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), got)
		require.Equal(t, 1, m.Len())
	})

	t.Run("f64 NaN is not retrievable", func(t *testing.T) {
		m := types.NewTypedMap[float64](types.NewMapType(types.TypeF64, types.TypeI32), 0)
		m.Set(math.NaN(), types.BoxI32(1))
		_, ok := m.Get(math.NaN())
		require.False(t, ok)
	})
}
func TestTypedMap_Delete(t *testing.T) {
	m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32, types.TypeI32), 0)
	m.Set(1, types.BoxI32(2))

	old, ok := m.Delete(1)
	require.True(t, ok)
	require.Equal(t, types.BoxI32(2), old)
	require.Equal(t, 0, m.Len())

	_, ok = m.Delete(1)
	require.False(t, ok)
}
func TestTypedMap_Clear(t *testing.T) {
	m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32, types.TypeI32), 0)
	m.Set(1, types.BoxI32(2))

	var values []types.Boxed
	m.Clear(func(value types.Boxed) {
		values = append(values, value)
	})
	require.Equal(t, []types.Boxed{types.BoxI32(2)}, values)
	require.Equal(t, 0, m.Len())
}
func TestTypedMap_String(t *testing.T) {
	t.Run("i32", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32, types.TypeI32), 0)
		m.Set(1, types.BoxI32(2))
		require.Equal(t, "map[i32]i32{1: 2}", m.String())
	})

	t.Run("i64", func(t *testing.T) {
		m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64, types.TypeI32), 0)
		m.Set(1, types.BoxI32(2))
		require.Equal(t, "map[i64]i32{1: 2}", m.String())
	})

	t.Run("f32", func(t *testing.T) {
		m := types.NewTypedMap[float32](types.NewMapType(types.TypeF32, types.TypeI32), 0)
		m.Set(1, types.BoxI32(2))
		require.Equal(t, "map[f32]i32{1: 2}", m.String())
	})

	t.Run("f64", func(t *testing.T) {
		m := types.NewTypedMap[float64](types.NewMapType(types.TypeF64, types.TypeI32), 0)
		m.Set(1, types.BoxI32(2))
		require.Equal(t, "map[f64]i32{1: 2}", m.String())
	})

	t.Run("fallback key", func(t *testing.T) {
		m := types.NewTypedMap[string](types.NewMapType(types.TypeString, types.TypeI32), 0)
		m.Set("foo", types.BoxI32(2))
		require.Equal(t, "map[string]i32{\"foo\": 2}", m.String())
	})
}
func TestTypedMap_Len(t *testing.T) {
	m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32, types.TypeI32), 0)
	require.Equal(t, 0, m.Len())
	m.Set(1, types.BoxI32(2))
	require.Equal(t, 1, m.Len())
}
func TestTypedMap_Range(t *testing.T) {
	m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64, types.TypeI32), 0)
	m.Set(1, types.BoxI32(2))

	var keys []int64
	m.Range(func(key int64, _ types.Boxed) {
		keys = append(keys, key)
	})
	require.Equal(t, []int64{1}, keys)
}
func TestTypedMap_Refs(t *testing.T) {
	t.Run("inline i64 value", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32, types.TypeI64), 0)
		m.Set(1, types.BoxI64(2))

		var refs []types.Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = m.Refs(nil)
		})
		require.Empty(t, refs)
		require.Equal(t, []types.Ref{9}, m.Refs([]types.Ref{9}))
		require.Zero(t, allocs)
	})

	t.Run("ref value", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32, types.TypeString), 0)
		m.Set(1, types.BoxRef(2))
		require.Equal(t, []types.Ref{9, 2}, m.Refs([]types.Ref{9}))
	})
}
func TestMap_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewMap(types.NewMapType(types.TypeI32, types.TypeI32)).Kind())
}
func TestMap_Type(t *testing.T) {
	typ := types.NewMapType(types.TypeI32, types.TypeI32)
	require.Equal(t, typ, types.NewMap(typ).Type())
}
func TestMap_Get(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeString, types.TypeI32))
	m.Set(types.MapKey{Kind: types.KindRef, Bits: 1}, types.MapEntry{Key: types.BoxRef(1), Value: types.BoxI32(2)})

	entry, ok := m.Get(types.MapKey{Kind: types.KindRef, Bits: 1})
	require.True(t, ok)
	require.Equal(t, types.BoxI32(2), entry.Value)

	_, ok = m.Get(types.MapKey{Kind: types.KindRef, Bits: 2})
	require.False(t, ok)
}
func TestMap_Set(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeI32, types.TypeI32))

	old, ok := m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(2)})
	require.False(t, ok)
	require.Equal(t, types.MapEntry{}, old)

	old, ok = m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(3)})
	require.True(t, ok)
	require.Equal(t, types.BoxI32(2), old.Value)

	entry, ok := m.Get(types.MapKey{Kind: types.KindI32, Bits: 1})
	require.True(t, ok)
	require.Equal(t, types.BoxI32(3), entry.Value)
}
func TestMap_Delete(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeI32, types.TypeI32))
	m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(2)})

	old, ok := m.Delete(types.MapKey{Kind: types.KindI32, Bits: 1})
	require.True(t, ok)
	require.Equal(t, types.BoxI32(2), old.Value)
	require.Equal(t, 0, m.Len())

	_, ok = m.Delete(types.MapKey{Kind: types.KindI32, Bits: 1})
	require.False(t, ok)
}
func TestMap_Clear(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeI32, types.TypeI32))
	m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(2)})

	var entries []types.MapEntry
	m.Clear(func(entry types.MapEntry) {
		entries = append(entries, entry)
	})
	require.Equal(t, []types.MapEntry{{Key: types.BoxI32(1), Value: types.BoxI32(2)}}, entries)
	require.Equal(t, 0, m.Len())
}
func TestMap_String(t *testing.T) {
	t.Run("i32 key", func(t *testing.T) {
		typ := types.NewMapType(types.TypeI32, types.TypeI32)
		m := types.NewMap(typ)
		m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{
			Key:   types.BoxI32(1),
			Value: types.BoxI32(2),
		})
		require.Equal(t, "map[i32]i32{1: 2}", m.String())
	})

	t.Run("empty string key", func(t *testing.T) {
		typ := types.NewMapType(types.TypeString, types.TypeI32)
		m := types.NewMap(typ)
		m.Set(types.MapKey{Kind: types.KindRef, Bits: 1}, types.MapEntry{
			Key:   types.BoxRef(1),
			Value: types.BoxI32(2),
		})
		require.Equal(t, "map[string]i32{1: 2}", m.String())
	})

	t.Run("deterministic", func(t *testing.T) {
		typ := types.NewMapType(types.TypeI32, types.TypeI32)
		m := types.NewMap(typ)
		m.Set(types.MapKey{Kind: types.KindI32, Bits: 2}, types.MapEntry{Key: types.BoxI32(2), Value: types.BoxI32(20)})
		m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(10)})
		require.Equal(t, "map[i32]i32{1: 10, 2: 20}", m.String())
	})
}
func TestMap_Len(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeI32, types.TypeI32))
	require.Equal(t, 0, m.Len())

	m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(2)})
	require.Equal(t, 1, m.Len())
}
func TestMap_Range(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeI32, types.TypeI32))
	m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(2)})

	var keys []types.MapKey
	m.Range(func(key types.MapKey, _ types.MapEntry) {
		keys = append(keys, key)
	})
	require.Equal(t, []types.MapKey{{Kind: types.KindI32, Bits: 1}}, keys)
}
func TestMap_Refs(t *testing.T) {
	t.Run("inline i64 value", func(t *testing.T) {
		m := types.NewMap(types.NewMapType(types.TypeI32, types.TypeI64))
		m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI64(2)})

		var refs []types.Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = m.Refs(nil)
		})
		require.Empty(t, refs)
		require.Equal(t, []types.Ref{9}, m.Refs([]types.Ref{9}))
		require.Zero(t, allocs)
	})

	t.Run("ref key and value", func(t *testing.T) {
		typ := types.NewMapType(types.TypeAny, types.TypeAny)
		m := types.NewMap(typ)
		m.Set(types.MapKey{Kind: types.KindRef, Bits: 1}, types.MapEntry{
			Key:   types.BoxRef(1),
			Value: types.BoxRef(2),
		})
		require.Equal(t, []types.Ref{9, 1, 2}, m.Refs([]types.Ref{9}))
	})

	t.Run("spilled i64 value", func(t *testing.T) {
		typ := types.NewMapType(types.TypeI32, types.TypeI64)
		m := types.NewMap(typ)
		m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{
			Key:   types.BoxI32(1),
			Value: types.BoxRef(2),
		})
		require.Equal(t, []types.Ref{9, 2}, m.Refs([]types.Ref{9}))
	})
}
func TestMapIterator_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewMapIterator(1, types.NewMap(types.NewMapType(types.TypeAny, types.TypeI32))).Kind())
}
func TestMapIterator_Type(t *testing.T) {
	m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64, types.TypeI32), 0)
	require.Equal(t, types.NewIteratorType(types.TypeI64), types.NewMapIterator(1, m).Type())
}
func TestMapIterator_String(t *testing.T) {
	require.Equal(t, "map.iterator", types.NewMapIterator(1, types.NewMap(types.NewMapType(types.TypeAny, types.TypeI32))).String())
}
func TestMapIterator_Next(t *testing.T) {
	t.Run("typed key", func(t *testing.T) {
		m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64, types.TypeI32), 0)
		m.Set(1<<50, types.BoxI32(2))
		it := types.NewMapIterator(7, m)
		require.True(t, it.Next())
		require.Equal(t, types.I64(1<<50), it.Current())
		require.False(t, it.Next())
	})

	t.Run("generic ref key", func(t *testing.T) {
		m := types.NewMap(types.NewMapType(types.TypeString, types.TypeI32))
		m.Set(types.MapKey{Kind: types.KindRef, Bits: 9}, types.MapEntry{Key: types.BoxRef(9), Value: types.BoxI32(2)})
		it := types.NewMapIterator(7, m)
		require.True(t, it.Next())
		require.Equal(t, types.BoxRef(9), it.Current())
	})
}
func TestMapIterator_Current(t *testing.T) {
	m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32, types.TypeI32), 0)
	m.Set(3, types.BoxI32(4))
	it := types.NewMapIterator(1, m)
	require.Equal(t, types.BoxedNull, it.Current())
	require.True(t, it.Next())
	require.Equal(t, types.I32(3), it.Current())
}
func TestMapIterator_Done(t *testing.T) {
	m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32, types.TypeI32), 0)
	m.Set(3, types.BoxI32(4))
	it := types.NewMapIterator(1, m)
	require.True(t, it.Done())
	require.True(t, it.Next())
	require.False(t, it.Done())
	require.False(t, it.Next())
	require.True(t, it.Done())
}
func TestMapIterator_Refs(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeString, types.TypeI32))
	m.Set(types.MapKey{Kind: types.KindRef, Bits: 9}, types.MapEntry{Key: types.BoxRef(9), Value: types.BoxI32(2)})
	it := types.NewMapIterator(7, m)
	require.True(t, it.Next())
	require.Equal(t, []types.Ref{5, 7, 9}, it.Refs([]types.Ref{5}))
}
func TestMapKey_String(t *testing.T) {
	tests := []struct {
		key types.MapKey
		str string
	}{
		{types.MapKey{Kind: types.KindI32, Bits: 1}, "1"},
		{types.MapKey{Kind: types.KindI64, Bits: 1}, "1"},
		{types.MapKey{Kind: types.KindF32, Bits: uint64(math.Float32bits(1))}, "1"},
		{types.MapKey{Kind: types.KindF64, Bits: math.Float64bits(1)}, "1"},
		{types.MapKey{Kind: types.KindRef, Bits: 1}, "1"},
		{types.MapKey{Kind: types.KindText, Text: "a"}, "\"a\""},
		{types.MapKey{Kind: types.Kind(255)}, "<invalid>"},
	}
	for _, tt := range tests {
		t.Run(tt.key.Kind.String(), func(t *testing.T) {
			require.Equal(t, tt.str, tt.key.String())
		})
	}
}
func TestMapType_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewMapType(types.TypeI32, types.TypeI32).Kind())
}
func TestMapType_String(t *testing.T) {
	require.Equal(t, "map[i32]string", types.NewMapType(types.TypeI32, types.TypeString).String())
}
func TestMapType_Cast(t *testing.T) {
	typ := types.NewMapType(types.TypeI32, types.TypeI32)

	require.True(t, typ.Cast(typ))
	require.True(t, typ.Cast(types.NewMapType(types.TypeI32, types.TypeI32)))
	require.False(t, typ.Cast(types.NewMapType(types.TypeI64, types.TypeI32)))
	require.False(t, typ.Cast(types.TypeI32))
}
func TestMapType_Equals(t *testing.T) {
	typ := types.NewMapType(types.TypeI32, types.TypeI32)

	require.True(t, typ.Equals(typ))
	require.True(t, typ.Equals(types.NewMapType(types.TypeI32, types.TypeI32)))
	require.False(t, typ.Equals(types.NewMapType(types.TypeI32, types.TypeI64)))
	require.False(t, typ.Equals(types.TypeI32))
}
func TestMapKey_Value(t *testing.T) {
	tests := []struct {
		key   types.MapKey
		entry types.MapEntry
		want  types.Value
	}{
		{types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{}, types.I32(1)},
		{types.MapKey{Kind: types.KindI64, Bits: 1}, types.MapEntry{}, types.I64(1)},
		{types.MapKey{Kind: types.KindF32, Bits: uint64(math.Float32bits(1))}, types.MapEntry{}, types.F32(1)},
		{types.MapKey{Kind: types.KindF64, Bits: math.Float64bits(1)}, types.MapEntry{}, types.F64(1)},
		{types.MapKey{Kind: types.KindRef, Bits: 1}, types.MapEntry{}, types.Ref(1)},
		{types.MapKey{Kind: types.KindText, Text: "a"}, types.MapEntry{}, types.String("a")},
		{types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(2)}, types.BoxI32(2)},
	}
	for _, tt := range tests {
		t.Run(tt.key.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.key.Value(tt.entry))
		})
	}
}

func BenchmarkTypedMap_Refs(b *testing.B) {
	m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32, types.TypeI64), 0)
	m.Set(1, types.BoxI64(2))
	require.Empty(b, m.Refs(nil))

	var refs []types.Ref
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		refs = m.Refs(nil)
	}
	b.StopTimer()
	require.Empty(b, refs)
}

func BenchmarkMap_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		m := types.NewMap(types.NewMapType(types.TypeI32, types.TypeI32))
		m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(2)})
		require.Empty(b, m.Refs(nil))

		var refs []types.Ref
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = m.Refs(nil)
		}
		b.StopTimer()
		require.Empty(b, refs)
	})

	b.Run("child refs", func(b *testing.B) {
		m := types.NewMap(types.NewMapType(types.TypeAny, types.TypeAny))
		m.Set(types.MapKey{Kind: types.KindRef, Bits: 1}, types.MapEntry{Key: types.BoxRef(1), Value: types.BoxRef(2)})
		require.Equal(b, []types.Ref{1, 2}, m.Refs(nil))

		refs := make([]types.Ref, 0, 2)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = m.Refs(refs[:0])
		}
		b.StopTimer()
		require.Equal(b, []types.Ref{1, 2}, refs)
	})
}
