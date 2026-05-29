package types

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoxedMap_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewBoxedMap(NewMapType(TypeI32, TypeI32)).Kind())
}

func TestBoxedMap_Type(t *testing.T) {
	typ := NewMapType(TypeI32, TypeI32)
	require.Equal(t, typ, NewBoxedMap(typ).Type())
}

func TestBoxedMap_String(t *testing.T) {
	t.Run("i32 key", func(t *testing.T) {
		typ := NewMapType(TypeI32, TypeI32)
		m := NewBoxedMap(typ)
		m.Set(BoxedMapKey{Kind: KindI32, Bits: 1}, BoxedMapEntry{
			Key:   BoxI32(1),
			Value: BoxI32(2),
		})
		require.Equal(t, "map[i32]i32{1: 2}", m.String())
	})

	t.Run("empty string key", func(t *testing.T) {
		typ := NewMapType(TypeString, TypeI32)
		m := NewBoxedMap(typ)
		m.Set(BoxedMapKey{Kind: KindRef, Bits: 1}, BoxedMapEntry{
			Key:   BoxRef(1),
			Value: BoxI32(2),
		})
		require.Equal(t, "map[string]i32{1: 2}", m.String())
	})

	t.Run("deterministic", func(t *testing.T) {
		typ := NewMapType(TypeI32, TypeI32)
		m := NewBoxedMap(typ)
		m.Set(BoxedMapKey{Kind: KindI32, Bits: 2}, BoxedMapEntry{Key: BoxI32(2), Value: BoxI32(20)})
		m.Set(BoxedMapKey{Kind: KindI32, Bits: 1}, BoxedMapEntry{Key: BoxI32(1), Value: BoxI32(10)})
		require.Equal(t, "map[i32]i32{1: 10, 2: 20}", m.String())
	})
}

func TestBoxedMap_Len(t *testing.T) {
	m := NewBoxedMap(NewMapType(TypeI32, TypeI32))
	require.Equal(t, 0, m.Len())

	m.Set(BoxedMapKey{Kind: KindI32, Bits: 1}, BoxedMapEntry{Key: BoxI32(1), Value: BoxI32(2)})
	require.Equal(t, 1, m.Len())
}

func TestBoxedMap_Get(t *testing.T) {
	m := NewBoxedMap(NewMapType(TypeString, TypeI32))
	m.Set(BoxedMapKey{Kind: KindRef, Bits: 1}, BoxedMapEntry{Key: BoxRef(1), Value: BoxI32(2)})

	entry, ok := m.Get(BoxedMapKey{Kind: KindRef, Bits: 1})
	require.True(t, ok)
	require.Equal(t, BoxI32(2), entry.Value)

	_, ok = m.Get(BoxedMapKey{Kind: KindRef, Bits: 2})
	require.False(t, ok)
}

func TestBoxedMap_Set(t *testing.T) {
	m := NewBoxedMap(NewMapType(TypeI32, TypeI32))

	old, ok := m.Set(BoxedMapKey{Kind: KindI32, Bits: 1}, BoxedMapEntry{Key: BoxI32(1), Value: BoxI32(2)})
	require.False(t, ok)
	require.Equal(t, BoxedMapEntry{}, old)

	old, ok = m.Set(BoxedMapKey{Kind: KindI32, Bits: 1}, BoxedMapEntry{Key: BoxI32(1), Value: BoxI32(3)})
	require.True(t, ok)
	require.Equal(t, BoxI32(2), old.Value)

	entry, ok := m.Get(BoxedMapKey{Kind: KindI32, Bits: 1})
	require.True(t, ok)
	require.Equal(t, BoxI32(3), entry.Value)
}

func TestBoxedMap_Delete(t *testing.T) {
	m := NewBoxedMap(NewMapType(TypeI32, TypeI32))
	m.Set(BoxedMapKey{Kind: KindI32, Bits: 1}, BoxedMapEntry{Key: BoxI32(1), Value: BoxI32(2)})

	old, ok := m.Delete(BoxedMapKey{Kind: KindI32, Bits: 1})
	require.True(t, ok)
	require.Equal(t, BoxI32(2), old.Value)
	require.Equal(t, 0, m.Len())

	_, ok = m.Delete(BoxedMapKey{Kind: KindI32, Bits: 1})
	require.False(t, ok)
}

func TestBoxedMap_Range(t *testing.T) {
	m := NewBoxedMap(NewMapType(TypeI32, TypeI32))
	m.Set(BoxedMapKey{Kind: KindI32, Bits: 1}, BoxedMapEntry{Key: BoxI32(1), Value: BoxI32(2)})

	var keys []BoxedMapKey
	m.Range(func(key BoxedMapKey, _ BoxedMapEntry) {
		keys = append(keys, key)
	})
	require.Equal(t, []BoxedMapKey{{Kind: KindI32, Bits: 1}}, keys)
}

func TestBoxedMap_Clear(t *testing.T) {
	m := NewBoxedMap(NewMapType(TypeI32, TypeI32))
	m.Set(BoxedMapKey{Kind: KindI32, Bits: 1}, BoxedMapEntry{Key: BoxI32(1), Value: BoxI32(2)})

	var entries []BoxedMapEntry
	m.Clear(func(entry BoxedMapEntry) {
		entries = append(entries, entry)
	})
	require.Equal(t, []BoxedMapEntry{{Key: BoxI32(1), Value: BoxI32(2)}}, entries)
	require.Equal(t, 0, m.Len())
}

func TestBoxedMap_Refs(t *testing.T) {
	t.Run("inline i64 value", func(t *testing.T) {
		m := NewBoxedMap(NewMapType(TypeI32, TypeI64))
		m.Set(BoxedMapKey{Kind: KindI32, Bits: 1}, BoxedMapEntry{Key: BoxI32(1), Value: BoxI64(2)})

		var refs []Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = m.Refs()
		})
		require.Empty(t, refs)
		require.Zero(t, allocs)
	})

	t.Run("ref key and value", func(t *testing.T) {
		typ := NewMapType(TypeRef, TypeRef)
		m := NewBoxedMap(typ)
		m.Set(BoxedMapKey{Kind: KindRef, Bits: 1}, BoxedMapEntry{
			Key:   BoxRef(1),
			Value: BoxRef(2),
		})
		require.Equal(t, []Ref{1, 2}, m.Refs())
	})

	t.Run("spilled i64 value", func(t *testing.T) {
		typ := NewMapType(TypeI32, TypeI64)
		m := NewBoxedMap(typ)
		m.Set(BoxedMapKey{Kind: KindI32, Bits: 1}, BoxedMapEntry{
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
		{name: "i32", typ: NewMapType(TypeI32, TypeI32), want: (*Map[int32])(nil)},
		{name: "i64", typ: NewMapType(TypeI64, TypeI32), want: (*Map[int64])(nil)},
		{name: "f32", typ: NewMapType(TypeF32, TypeI32), want: (*Map[float32])(nil)},
		{name: "f64", typ: NewMapType(TypeF64, TypeI32), want: (*Map[float64])(nil)},
		{name: "ref", typ: NewMapType(TypeRef, TypeI32), want: (*BoxedMap)(nil)},
		{name: "string", typ: NewMapType(TypeString, TypeI32), want: (*BoxedMap)(nil)},
		{name: "struct", typ: NewMapType(structType, TypeI32), want: (*BoxedMap)(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.IsType(t, tt.want, NewMapForType(tt.typ, 0))
		})
	}
}

func TestMap_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewMap[int32](NewMapType(TypeI32, TypeI32), 0).Kind())
}

func TestMap_Type(t *testing.T) {
	typ := NewMapType(TypeI32, TypeI32)
	require.Equal(t, typ, NewMap[int32](typ, 0).Type())
}

func TestMap_Len(t *testing.T) {
	m := NewMap[int32](NewMapType(TypeI32, TypeI32), 0)
	require.Equal(t, 0, m.Len())
	m.Set(1, BoxI32(2))
	require.Equal(t, 1, m.Len())
}

func TestMap_Get(t *testing.T) {
	t.Run("i32", func(t *testing.T) {
		m := NewMap[int32](NewMapType(TypeI32, TypeI32), 0)
		m.Set(1, BoxI32(2))
		got, ok := m.Get(1)
		require.True(t, ok)
		require.Equal(t, BoxI32(2), got)
		_, ok = m.Get(2)
		require.False(t, ok)
	})

	t.Run("i64 wide key", func(t *testing.T) {
		m := NewMap[int64](NewMapType(TypeI64, TypeI32), 0)
		m.Set(1<<50, BoxI32(2))
		got, ok := m.Get(1 << 50)
		require.True(t, ok)
		require.Equal(t, BoxI32(2), got)
		_, ok = m.Get(2)
		require.False(t, ok)
	})

	t.Run("f64", func(t *testing.T) {
		m := NewMap[float64](NewMapType(TypeF64, TypeI32), 0)
		m.Set(1.5, BoxI32(2))
		got, ok := m.Get(1.5)
		require.True(t, ok)
		require.Equal(t, BoxI32(2), got)
	})
}

func TestMap_Set(t *testing.T) {
	t.Run("overwrite returns old", func(t *testing.T) {
		m := NewMap[int32](NewMapType(TypeI32, TypeI32), 0)
		old, ok := m.Set(1, BoxI32(2))
		require.False(t, ok)
		require.Equal(t, Boxed(0), old)

		old, ok = m.Set(1, BoxI32(3))
		require.True(t, ok)
		require.Equal(t, BoxI32(2), old)
	})

	t.Run("f32 -0.0 collapses to +0.0", func(t *testing.T) {
		m := NewMap[float32](NewMapType(TypeF32, TypeI32), 0)
		m.Set(float32(math.Copysign(0, -1)), BoxI32(1))
		m.Set(0, BoxI32(2))

		got, ok := m.Get(float32(math.Copysign(0, -1)))
		require.True(t, ok)
		require.Equal(t, BoxI32(2), got)
		require.Equal(t, 1, m.Len())
	})

	t.Run("f64 -0.0 collapses to +0.0", func(t *testing.T) {
		m := NewMap[float64](NewMapType(TypeF64, TypeI32), 0)
		m.Set(math.Copysign(0, -1), BoxI32(1))
		m.Set(0, BoxI32(2))

		got, ok := m.Get(math.Copysign(0, -1))
		require.True(t, ok)
		require.Equal(t, BoxI32(2), got)
		require.Equal(t, 1, m.Len())
	})

	t.Run("f64 NaN is not retrievable", func(t *testing.T) {
		m := NewMap[float64](NewMapType(TypeF64, TypeI32), 0)
		m.Set(math.NaN(), BoxI32(1))
		_, ok := m.Get(math.NaN())
		require.False(t, ok)
	})
}

func TestMap_Delete(t *testing.T) {
	m := NewMap[int32](NewMapType(TypeI32, TypeI32), 0)
	m.Set(1, BoxI32(2))

	old, ok := m.Delete(1)
	require.True(t, ok)
	require.Equal(t, BoxI32(2), old)
	require.Equal(t, 0, m.Len())

	_, ok = m.Delete(1)
	require.False(t, ok)
}

func TestMap_Range(t *testing.T) {
	m := NewMap[int64](NewMapType(TypeI64, TypeI32), 0)
	m.Set(1, BoxI32(2))

	var keys []int64
	m.Range(func(key int64, _ Boxed) {
		keys = append(keys, key)
	})
	require.Equal(t, []int64{1}, keys)
}

func TestMap_Clear(t *testing.T) {
	m := NewMap[int32](NewMapType(TypeI32, TypeI32), 0)
	m.Set(1, BoxI32(2))

	var values []Boxed
	m.Clear(func(value Boxed) {
		values = append(values, value)
	})
	require.Equal(t, []Boxed{BoxI32(2)}, values)
	require.Equal(t, 0, m.Len())
}

func TestMap_String(t *testing.T) {
	t.Run("i32", func(t *testing.T) {
		m := NewMap[int32](NewMapType(TypeI32, TypeI32), 0)
		m.Set(1, BoxI32(2))
		require.Equal(t, "map[i32]i32{1: 2}", m.String())
	})

	t.Run("i64", func(t *testing.T) {
		m := NewMap[int64](NewMapType(TypeI64, TypeI32), 0)
		m.Set(1, BoxI32(2))
		require.Equal(t, "map[i64]i32{1: 2}", m.String())
	})

	t.Run("f32", func(t *testing.T) {
		m := NewMap[float32](NewMapType(TypeF32, TypeI32), 0)
		m.Set(1, BoxI32(2))
		require.Equal(t, "map[f32]i32{1: 2}", m.String())
	})

	t.Run("f64", func(t *testing.T) {
		m := NewMap[float64](NewMapType(TypeF64, TypeI32), 0)
		m.Set(1, BoxI32(2))
		require.Equal(t, "map[f64]i32{1: 2}", m.String())
	})
}

func TestMap_Refs(t *testing.T) {
	t.Run("inline i64 value", func(t *testing.T) {
		m := NewMap[int32](NewMapType(TypeI32, TypeI64), 0)
		m.Set(1, BoxI64(2))

		var refs []Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = m.Refs()
		})
		require.Empty(t, refs)
		require.Zero(t, allocs)
	})

	t.Run("ref value", func(t *testing.T) {
		m := NewMap[int32](NewMapType(TypeI32, TypeString), 0)
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

func TestMapType_Cast(t *testing.T) {
	require.True(t, NewMapType(TypeI32, TypeI32).Cast(NewMapType(TypeI32, TypeI32)))
	require.False(t, NewMapType(TypeI32, TypeI32).Cast(NewMapType(TypeI64, TypeI32)))
}

func TestMapType_Equals(t *testing.T) {
	require.True(t, NewMapType(TypeI32, TypeI32).Equals(NewMapType(TypeI32, TypeI32)))
	require.False(t, NewMapType(TypeI32, TypeI32).Equals(NewMapType(TypeI32, TypeI64)))
}

func BenchmarkMap_Get(b *testing.B) {
	m := NewMap[int64](NewMapType(TypeI64, TypeI32), 0)
	m.Set(42, BoxI32(7))

	b.ReportAllocs()
	var ok bool
	for n := 0; n < b.N; n++ {
		_, ok = m.Get(42)
	}
	b.StopTimer()
	require.True(b, ok)
}

func BenchmarkBoxedMapStringGet_Interned(b *testing.B) {
	m := NewBoxedMap(NewMapType(TypeString, TypeI32))
	m.Set(BoxedMapKey{Kind: KindRef, Bits: 1}, BoxedMapEntry{Key: BoxRef(1), Value: BoxI32(7)})
	key := BoxedMapKey{Kind: KindRef, Bits: 1}

	b.ReportAllocs()
	var ok bool
	for n := 0; n < b.N; n++ {
		_, ok = m.Get(key)
	}
	b.StopTimer()
	require.True(b, ok)
}

func BenchmarkBoxedMap_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		m := NewBoxedMap(NewMapType(TypeI32, TypeI32))
		m.Set(BoxedMapKey{Kind: KindI32, Bits: 1}, BoxedMapEntry{Key: BoxI32(1), Value: BoxI32(2)})

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
		m := NewMap[int32](NewMapType(TypeI32, TypeI64), 0)
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
		m := NewBoxedMap(NewMapType(TypeRef, TypeRef))
		m.Set(BoxedMapKey{Kind: KindRef, Bits: 1}, BoxedMapEntry{Key: BoxRef(1), Value: BoxRef(2)})

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
