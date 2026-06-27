package types

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMap_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewMap(NewMapType(TypeI32, TypeI32)).Kind())
}

func TestMap_Type(t *testing.T) {
	typ := NewMapType(TypeI32, TypeI32)
	require.Equal(t, typ, NewMap(typ).Type())
}

func TestMap_String(t *testing.T) {
	t.Run("i32 key", func(t *testing.T) {
		typ := NewMapType(TypeI32, TypeI32)
		m := NewMap(typ)
		m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{
			Key:   BoxI32(1),
			Value: BoxI32(2),
		})
		require.Equal(t, "map[i32]i32{1: 2}", m.String())
	})

	t.Run("empty string key", func(t *testing.T) {
		typ := NewMapType(TypeString, TypeI32)
		m := NewMap(typ)
		m.Set(MapKey{Kind: KindRef, Bits: 1}, MapEntry{
			Key:   BoxRef(1),
			Value: BoxI32(2),
		})
		require.Equal(t, "map[string]i32{1: 2}", m.String())
	})

	t.Run("deterministic", func(t *testing.T) {
		typ := NewMapType(TypeI32, TypeI32)
		m := NewMap(typ)
		m.Set(MapKey{Kind: KindI32, Bits: 2}, MapEntry{Key: BoxI32(2), Value: BoxI32(20)})
		m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{Key: BoxI32(1), Value: BoxI32(10)})
		require.Equal(t, "map[i32]i32{1: 10, 2: 20}", m.String())
	})
}

func TestMap_Len(t *testing.T) {
	m := NewMap(NewMapType(TypeI32, TypeI32))
	require.Equal(t, 0, m.Len())

	m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{Key: BoxI32(1), Value: BoxI32(2)})
	require.Equal(t, 1, m.Len())
}

func TestMap_Get(t *testing.T) {
	m := NewMap(NewMapType(TypeString, TypeI32))
	m.Set(MapKey{Kind: KindRef, Bits: 1}, MapEntry{Key: BoxRef(1), Value: BoxI32(2)})

	entry, ok := m.Get(MapKey{Kind: KindRef, Bits: 1})
	require.True(t, ok)
	require.Equal(t, BoxI32(2), entry.Value)

	_, ok = m.Get(MapKey{Kind: KindRef, Bits: 2})
	require.False(t, ok)
}

func TestMap_Set(t *testing.T) {
	m := NewMap(NewMapType(TypeI32, TypeI32))

	old, ok := m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{Key: BoxI32(1), Value: BoxI32(2)})
	require.False(t, ok)
	require.Equal(t, MapEntry{}, old)

	old, ok = m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{Key: BoxI32(1), Value: BoxI32(3)})
	require.True(t, ok)
	require.Equal(t, BoxI32(2), old.Value)

	entry, ok := m.Get(MapKey{Kind: KindI32, Bits: 1})
	require.True(t, ok)
	require.Equal(t, BoxI32(3), entry.Value)
}

func TestMap_Delete(t *testing.T) {
	m := NewMap(NewMapType(TypeI32, TypeI32))
	m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{Key: BoxI32(1), Value: BoxI32(2)})

	old, ok := m.Delete(MapKey{Kind: KindI32, Bits: 1})
	require.True(t, ok)
	require.Equal(t, BoxI32(2), old.Value)
	require.Equal(t, 0, m.Len())

	_, ok = m.Delete(MapKey{Kind: KindI32, Bits: 1})
	require.False(t, ok)
}

func TestMap_Range(t *testing.T) {
	m := NewMap(NewMapType(TypeI32, TypeI32))
	m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{Key: BoxI32(1), Value: BoxI32(2)})

	var keys []MapKey
	m.Range(func(key MapKey, _ MapEntry) {
		keys = append(keys, key)
	})
	require.Equal(t, []MapKey{{Kind: KindI32, Bits: 1}}, keys)
}

func TestMap_Clear(t *testing.T) {
	m := NewMap(NewMapType(TypeI32, TypeI32))
	m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{Key: BoxI32(1), Value: BoxI32(2)})

	var entries []MapEntry
	m.Clear(func(entry MapEntry) {
		entries = append(entries, entry)
	})
	require.Equal(t, []MapEntry{{Key: BoxI32(1), Value: BoxI32(2)}}, entries)
	require.Equal(t, 0, m.Len())
}

func TestMapIterator(t *testing.T) {
	t.Run("typed map key", func(t *testing.T) {
		m := NewTypedMap[int64](NewMapType(TypeI64, TypeI32), 0)
		m.Set(1<<50, BoxI32(2))

		iter := NewMapIterator(Ref(7), m)
		require.True(t, iter.Done())
		require.Equal(t, NewIteratorType(TypeI64), iter.Type())
		require.True(t, iter.Next())
		require.Equal(t, I64(1<<50), iter.Current())
		require.False(t, iter.Next())
		require.True(t, iter.Done())
		require.Equal(t, []Ref{7}, iter.Refs())
	})

	t.Run("generic map ref key", func(t *testing.T) {
		m := NewMap(NewMapType(TypeString, TypeI32))
		m.Set(MapKey{Kind: KindRef, Bits: 9}, MapEntry{Key: BoxRef(9), Value: BoxI32(2)})

		iter := NewMapIterator(Ref(7), m)
		require.Equal(t, NewIteratorType(TypeString), iter.Type())
		require.True(t, iter.Next())
		require.Equal(t, BoxRef(9), iter.Current())
		require.Equal(t, []Ref{7, 9}, iter.Refs())
	})
}

func TestMap_Refs(t *testing.T) {
	t.Run("inline i64 value", func(t *testing.T) {
		m := NewMap(NewMapType(TypeI32, TypeI64))
		m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{Key: BoxI32(1), Value: BoxI64(2)})

		var refs []Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = m.Refs()
		})
		require.Empty(t, refs)
		require.Zero(t, allocs)
	})

	t.Run("ref key and value", func(t *testing.T) {
		typ := NewMapType(TypeRef, TypeRef)
		m := NewMap(typ)
		m.Set(MapKey{Kind: KindRef, Bits: 1}, MapEntry{
			Key:   BoxRef(1),
			Value: BoxRef(2),
		})
		require.Equal(t, []Ref{1, 2}, m.Refs())
	})

	t.Run("spilled i64 value", func(t *testing.T) {
		typ := NewMapType(TypeI32, TypeI64)
		m := NewMap(typ)
		m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{
			Key:   BoxI32(1),
			Value: BoxRef(2),
		})
		require.Equal(t, []Ref{2}, m.Refs())
	})
}

func TestNewMapForType(t *testing.T) {
	structType := NewStructType(NewStructField(TypeI32))
	tests := []struct {
		name string
		typ  *MapType
		want any
	}{
		{name: "i32", typ: NewMapType(TypeI32, TypeI32), want: (*TypedMap[int32])(nil)},
		{name: "i64", typ: NewMapType(TypeI64, TypeI32), want: (*TypedMap[int64])(nil)},
		{name: "f32", typ: NewMapType(TypeF32, TypeI32), want: (*TypedMap[float32])(nil)},
		{name: "f64", typ: NewMapType(TypeF64, TypeI32), want: (*TypedMap[float64])(nil)},
		{name: "ref", typ: NewMapType(TypeRef, TypeI32), want: (*Map)(nil)},
		{name: "string", typ: NewMapType(TypeString, TypeI32), want: (*Map)(nil)},
		{name: "struct", typ: NewMapType(structType, TypeI32), want: (*Map)(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.IsType(t, tt.want, NewMapForType(tt.typ, 0))
		})
	}
}

func TestTypedMap_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewTypedMap[int32](NewMapType(TypeI32, TypeI32), 0).Kind())
}

func TestTypedMap_Type(t *testing.T) {
	typ := NewMapType(TypeI32, TypeI32)
	require.Equal(t, typ, NewTypedMap[int32](typ, 0).Type())
}

func TestTypedMap_Len(t *testing.T) {
	m := NewTypedMap[int32](NewMapType(TypeI32, TypeI32), 0)
	require.Equal(t, 0, m.Len())
	m.Set(1, BoxI32(2))
	require.Equal(t, 1, m.Len())
}

func TestTypedMap_Get(t *testing.T) {
	t.Run("i32", func(t *testing.T) {
		m := NewTypedMap[int32](NewMapType(TypeI32, TypeI32), 0)
		m.Set(1, BoxI32(2))
		got, ok := m.Get(1)
		require.True(t, ok)
		require.Equal(t, BoxI32(2), got)
		_, ok = m.Get(2)
		require.False(t, ok)
	})

	t.Run("i64 wide key", func(t *testing.T) {
		m := NewTypedMap[int64](NewMapType(TypeI64, TypeI32), 0)
		m.Set(1<<50, BoxI32(2))
		got, ok := m.Get(1 << 50)
		require.True(t, ok)
		require.Equal(t, BoxI32(2), got)
		_, ok = m.Get(2)
		require.False(t, ok)
	})

	t.Run("f64", func(t *testing.T) {
		m := NewTypedMap[float64](NewMapType(TypeF64, TypeI32), 0)
		m.Set(1.5, BoxI32(2))
		got, ok := m.Get(1.5)
		require.True(t, ok)
		require.Equal(t, BoxI32(2), got)
	})
}

func TestTypedMap_Set(t *testing.T) {
	t.Run("overwrite returns old", func(t *testing.T) {
		m := NewTypedMap[int32](NewMapType(TypeI32, TypeI32), 0)
		old, ok := m.Set(1, BoxI32(2))
		require.False(t, ok)
		require.Equal(t, Boxed(0), old)

		old, ok = m.Set(1, BoxI32(3))
		require.True(t, ok)
		require.Equal(t, BoxI32(2), old)
	})

	t.Run("f32 -0.0 collapses to +0.0", func(t *testing.T) {
		m := NewTypedMap[float32](NewMapType(TypeF32, TypeI32), 0)
		m.Set(float32(math.Copysign(0, -1)), BoxI32(1))
		m.Set(0, BoxI32(2))

		got, ok := m.Get(float32(math.Copysign(0, -1)))
		require.True(t, ok)
		require.Equal(t, BoxI32(2), got)
		require.Equal(t, 1, m.Len())
	})

	t.Run("f64 -0.0 collapses to +0.0", func(t *testing.T) {
		m := NewTypedMap[float64](NewMapType(TypeF64, TypeI32), 0)
		m.Set(math.Copysign(0, -1), BoxI32(1))
		m.Set(0, BoxI32(2))

		got, ok := m.Get(math.Copysign(0, -1))
		require.True(t, ok)
		require.Equal(t, BoxI32(2), got)
		require.Equal(t, 1, m.Len())
	})

	t.Run("f64 NaN is not retrievable", func(t *testing.T) {
		m := NewTypedMap[float64](NewMapType(TypeF64, TypeI32), 0)
		m.Set(math.NaN(), BoxI32(1))
		_, ok := m.Get(math.NaN())
		require.False(t, ok)
	})
}

func TestTypedMap_Delete(t *testing.T) {
	m := NewTypedMap[int32](NewMapType(TypeI32, TypeI32), 0)
	m.Set(1, BoxI32(2))

	old, ok := m.Delete(1)
	require.True(t, ok)
	require.Equal(t, BoxI32(2), old)
	require.Equal(t, 0, m.Len())

	_, ok = m.Delete(1)
	require.False(t, ok)
}

func TestTypedMap_Range(t *testing.T) {
	m := NewTypedMap[int64](NewMapType(TypeI64, TypeI32), 0)
	m.Set(1, BoxI32(2))

	var keys []int64
	m.Range(func(key int64, _ Boxed) {
		keys = append(keys, key)
	})
	require.Equal(t, []int64{1}, keys)
}

func TestTypedMap_Clear(t *testing.T) {
	m := NewTypedMap[int32](NewMapType(TypeI32, TypeI32), 0)
	m.Set(1, BoxI32(2))

	var values []Boxed
	m.Clear(func(value Boxed) {
		values = append(values, value)
	})
	require.Equal(t, []Boxed{BoxI32(2)}, values)
	require.Equal(t, 0, m.Len())
}

func TestTypedMap_String(t *testing.T) {
	t.Run("i32", func(t *testing.T) {
		m := NewTypedMap[int32](NewMapType(TypeI32, TypeI32), 0)
		m.Set(1, BoxI32(2))
		require.Equal(t, "map[i32]i32{1: 2}", m.String())
	})

	t.Run("i64", func(t *testing.T) {
		m := NewTypedMap[int64](NewMapType(TypeI64, TypeI32), 0)
		m.Set(1, BoxI32(2))
		require.Equal(t, "map[i64]i32{1: 2}", m.String())
	})

	t.Run("f32", func(t *testing.T) {
		m := NewTypedMap[float32](NewMapType(TypeF32, TypeI32), 0)
		m.Set(1, BoxI32(2))
		require.Equal(t, "map[f32]i32{1: 2}", m.String())
	})

	t.Run("f64", func(t *testing.T) {
		m := NewTypedMap[float64](NewMapType(TypeF64, TypeI32), 0)
		m.Set(1, BoxI32(2))
		require.Equal(t, "map[f64]i32{1: 2}", m.String())
	})

	t.Run("fallback key", func(t *testing.T) {
		m := NewTypedMap[string](NewMapType(TypeString, TypeI32), 0)
		m.Set("foo", BoxI32(2))
		require.Equal(t, "map[string]i32{foo: 2}", m.String())
	})
}

func TestTypedMap_Refs(t *testing.T) {
	t.Run("inline i64 value", func(t *testing.T) {
		m := NewTypedMap[int32](NewMapType(TypeI32, TypeI64), 0)
		m.Set(1, BoxI64(2))

		var refs []Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = m.Refs()
		})
		require.Empty(t, refs)
		require.Zero(t, allocs)
	})

	t.Run("ref value", func(t *testing.T) {
		m := NewTypedMap[int32](NewMapType(TypeI32, TypeString), 0)
		m.Set(1, BoxRef(2))
		require.Equal(t, []Ref{2}, m.Refs())
	})
}

func TestMapType_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewMapType(TypeI32, TypeI32).Kind())
}

func TestMapType_String(t *testing.T) {
	require.Equal(t, "map[i32]string", NewMapType(TypeI32, TypeString).String())
}

func TestMapType_KeyKind(t *testing.T) {
	require.Equal(t, KindI32, NewMapType(TypeI32, TypeString).KeyKind)
}

func TestMapType_ElemKind(t *testing.T) {
	require.Equal(t, KindRef, NewMapType(TypeI32, TypeString).ElemKind)
}

func TestMapType_TraceKeys(t *testing.T) {
	require.True(t, NewMapType(TypeRef, TypeI32).TraceKeys)
	require.True(t, NewMapType(TypeString, TypeI32).TraceKeys)
}

func TestMapType_TraceValues(t *testing.T) {
	require.True(t, NewMapType(TypeI32, TypeString).TraceValues)
	require.True(t, NewMapType(TypeI32, TypeI64).TraceValues)
	require.False(t, NewMapType(TypeI32, TypeI32).TraceValues)
}

func TestMapKey_String(t *testing.T) {
	tests := []struct {
		key MapKey
		str string
	}{
		{MapKey{Kind: KindI32, Bits: 1}, "1"},
		{MapKey{Kind: KindI64, Bits: 1}, "1"},
		{MapKey{Kind: KindF32, Bits: uint64(math.Float32bits(1))}, "1"},
		{MapKey{Kind: KindF64, Bits: math.Float64bits(1)}, "1"},
		{MapKey{Kind: KindRef, Bits: 1}, "1"},
		{MapKey{Kind: Kind(255)}, "<invalid>"},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.key.String())
		})
	}
}

func TestMapType_Cast(t *testing.T) {
	typ := NewMapType(TypeI32, TypeI32)

	require.True(t, typ.Cast(typ))
	require.True(t, typ.Cast(NewMapType(TypeI32, TypeI32)))
	require.False(t, typ.Cast(NewMapType(TypeI64, TypeI32)))
	require.False(t, typ.Cast(TypeI32))
}

func TestMapType_Equals(t *testing.T) {
	typ := NewMapType(TypeI32, TypeI32)

	require.True(t, typ.Equals(typ))
	require.True(t, typ.Equals(NewMapType(TypeI32, TypeI32)))
	require.False(t, typ.Equals(NewMapType(TypeI32, TypeI64)))
	require.False(t, typ.Equals(TypeI32))
}

func BenchmarkTypedMap_Get(b *testing.B) {
	m := NewTypedMap[int64](NewMapType(TypeI64, TypeI32), 0)
	m.Set(42, BoxI32(7))

	b.ReportAllocs()
	var ok bool
	for n := 0; n < b.N; n++ {
		_, ok = m.Get(42)
	}
	b.StopTimer()
	require.True(b, ok)
}

func BenchmarkMapStringGet_Interned(b *testing.B) {
	m := NewMap(NewMapType(TypeString, TypeI32))
	m.Set(MapKey{Kind: KindRef, Bits: 1}, MapEntry{Key: BoxRef(1), Value: BoxI32(7)})
	key := MapKey{Kind: KindRef, Bits: 1}

	b.ReportAllocs()
	var ok bool
	for n := 0; n < b.N; n++ {
		_, ok = m.Get(key)
	}
	b.StopTimer()
	require.True(b, ok)
}

func BenchmarkMap_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		m := NewMap(NewMapType(TypeI32, TypeI32))
		m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{Key: BoxI32(1), Value: BoxI32(2)})

		var refs []Ref
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			refs = m.Refs()
		}
		b.StopTimer()
		require.Empty(b, refs)
	})

	b.Run("inline i64", func(b *testing.B) {
		m := NewTypedMap[int32](NewMapType(TypeI32, TypeI64), 0)
		m.Set(1, BoxI64(2))

		var refs []Ref
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			refs = m.Refs()
		}
		b.StopTimer()
		require.Empty(b, refs)
	})

	b.Run("child refs", func(b *testing.B) {
		m := NewMap(NewMapType(TypeRef, TypeRef))
		m.Set(MapKey{Kind: KindRef, Bits: 1}, MapEntry{Key: BoxRef(1), Value: BoxRef(2)})

		var refs []Ref
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			refs = m.Refs()
		}
		b.StopTimer()
		require.Len(b, refs, 2)
	})
}
