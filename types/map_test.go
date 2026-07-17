package types

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewTypedMap(t *testing.T) {
	typ := NewMapType(TypeI32, TypeRef)
	m := NewTypedMap[int32](typ, 4)
	require.Same(t, typ, m.Typ)
	require.Equal(t, BoxedNull, m.Zero)
	require.Zero(t, m.Len())
}

func TestNewMap(t *testing.T) {
	typ := NewMapType(TypeRef, TypeI32)
	m := NewMap(typ)
	require.Same(t, typ, m.Typ)
	require.Equal(t, BoxI32(0), m.Zero)
	require.Zero(t, m.Len())
}

func TestNewMapWithCapacity(t *testing.T) {
	typ := NewMapType(TypeI32, TypeRef)
	m := NewMapWithCapacity(typ, 8)
	require.Same(t, typ, m.Typ)
	require.Equal(t, BoxedNull, m.Zero)
	require.Zero(t, m.Len())
}

func TestNewMapForType(t *testing.T) {
	structType := NewStructType(NewStructField(TypeI32))
	tests := []struct {
		typ      *MapType
		wantType any
	}{
		{typ: NewMapType(TypeI32, TypeI32), wantType: (*TypedMap[int32])(nil)},
		{typ: NewMapType(TypeI64, TypeI32), wantType: (*TypedMap[int64])(nil)},
		{typ: NewMapType(TypeF32, TypeI32), wantType: (*TypedMap[float32])(nil)},
		{typ: NewMapType(TypeF64, TypeI32), wantType: (*TypedMap[float64])(nil)},
		{typ: NewMapType(TypeRef, TypeI32), wantType: (*Map)(nil)},
		{typ: NewMapType(TypeString, TypeI32), wantType: (*Map)(nil)},
		{typ: NewMapType(structType, TypeI32), wantType: (*Map)(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.typ.Key.String(), func(t *testing.T) {
			require.IsType(t, tt.wantType, NewMapForType(tt.typ, 0))
		})
	}
}

func TestNewMapIterator(t *testing.T) {
	m := NewTypedMap[int64](NewMapType(TypeI64, TypeI32), 0)
	it := NewMapIterator(7, m)
	require.Equal(t, NewIteratorType(TypeI64), it.Type())
	require.True(t, it.Done())
}

func TestNewMapType(t *testing.T) {
	t.Run("reference key and i64 value", func(t *testing.T) {
		typ := NewMapType(TypeString, TypeI64)
		require.Equal(t, TypeString, typ.Key)
		require.Equal(t, TypeI64, typ.Elem)
		require.Equal(t, KindRef, typ.KeyKind)
		require.Equal(t, KindI64, typ.ElemKind)
		require.True(t, typ.TraceKeys)
		require.True(t, typ.TraceValues)
	})

	t.Run("primitive key and value", func(t *testing.T) {
		typ := NewMapType(TypeI32, TypeI32)
		require.False(t, typ.TraceKeys)
		require.False(t, typ.TraceValues)
	})
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
			refs = m.Refs(nil)
		})
		require.Empty(t, refs)
		require.Equal(t, []Ref{9}, m.Refs([]Ref{9}))
		require.Zero(t, allocs)
	})

	t.Run("ref value", func(t *testing.T) {
		m := NewTypedMap[int32](NewMapType(TypeI32, TypeString), 0)
		m.Set(1, BoxRef(2))
		require.Equal(t, []Ref{9, 2}, m.Refs([]Ref{9}))
	})
}

func TestMap_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewMap(NewMapType(TypeI32, TypeI32)).Kind())
}

func TestMap_Type(t *testing.T) {
	typ := NewMapType(TypeI32, TypeI32)
	require.Equal(t, typ, NewMap(typ).Type())
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

func TestMap_Refs(t *testing.T) {
	t.Run("inline i64 value", func(t *testing.T) {
		m := NewMap(NewMapType(TypeI32, TypeI64))
		m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{Key: BoxI32(1), Value: BoxI64(2)})

		var refs []Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = m.Refs(nil)
		})
		require.Empty(t, refs)
		require.Equal(t, []Ref{9}, m.Refs([]Ref{9}))
		require.Zero(t, allocs)
	})

	t.Run("ref key and value", func(t *testing.T) {
		typ := NewMapType(TypeRef, TypeRef)
		m := NewMap(typ)
		m.Set(MapKey{Kind: KindRef, Bits: 1}, MapEntry{
			Key:   BoxRef(1),
			Value: BoxRef(2),
		})
		require.Equal(t, []Ref{9, 1, 2}, m.Refs([]Ref{9}))
	})

	t.Run("spilled i64 value", func(t *testing.T) {
		typ := NewMapType(TypeI32, TypeI64)
		m := NewMap(typ)
		m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{
			Key:   BoxI32(1),
			Value: BoxRef(2),
		})
		require.Equal(t, []Ref{9, 2}, m.Refs([]Ref{9}))
	})
}

func TestMapIterator_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewMapIterator(1, NewMap(NewMapType(TypeRef, TypeI32))).Kind())
}

func TestMapIterator_Type(t *testing.T) {
	m := NewTypedMap[int64](NewMapType(TypeI64, TypeI32), 0)
	require.Equal(t, NewIteratorType(TypeI64), NewMapIterator(1, m).Type())
}

func TestMapIterator_String(t *testing.T) {
	require.Equal(t, "map.iterator", NewMapIterator(1, NewMap(NewMapType(TypeRef, TypeI32))).String())
}

func TestMapIterator_Next(t *testing.T) {
	t.Run("typed key", func(t *testing.T) {
		m := NewTypedMap[int64](NewMapType(TypeI64, TypeI32), 0)
		m.Set(1<<50, BoxI32(2))
		it := NewMapIterator(7, m)
		require.True(t, it.Next())
		require.Equal(t, I64(1<<50), it.Current())
		require.False(t, it.Next())
	})

	t.Run("generic ref key", func(t *testing.T) {
		m := NewMap(NewMapType(TypeString, TypeI32))
		m.Set(MapKey{Kind: KindRef, Bits: 9}, MapEntry{Key: BoxRef(9), Value: BoxI32(2)})
		it := NewMapIterator(7, m)
		require.True(t, it.Next())
		require.Equal(t, BoxRef(9), it.Current())
	})
}

func TestMapIterator_Current(t *testing.T) {
	m := NewTypedMap[int32](NewMapType(TypeI32, TypeI32), 0)
	m.Set(3, BoxI32(4))
	it := NewMapIterator(1, m)
	require.Equal(t, BoxedNull, it.Current())
	require.True(t, it.Next())
	require.Equal(t, I32(3), it.Current())
}

func TestMapIterator_Done(t *testing.T) {
	m := NewTypedMap[int32](NewMapType(TypeI32, TypeI32), 0)
	m.Set(3, BoxI32(4))
	it := NewMapIterator(1, m)
	require.True(t, it.Done())
	require.True(t, it.Next())
	require.False(t, it.Done())
	require.False(t, it.Next())
	require.True(t, it.Done())
}

func TestMapIterator_Refs(t *testing.T) {
	m := NewMap(NewMapType(TypeString, TypeI32))
	m.Set(MapKey{Kind: KindRef, Bits: 9}, MapEntry{Key: BoxRef(9), Value: BoxI32(2)})
	it := NewMapIterator(7, m)
	require.True(t, it.Next())
	require.Equal(t, []Ref{5, 7, 9}, it.Refs([]Ref{5}))
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
		t.Run(tt.key.Kind.String(), func(t *testing.T) {
			require.Equal(t, tt.str, tt.key.String())
		})
	}
}

func TestMapType_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewMapType(TypeI32, TypeI32).Kind())
}

func TestMapType_String(t *testing.T) {
	require.Equal(t, "map[i32]string", NewMapType(TypeI32, TypeString).String())
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

func BenchmarkTypedMap_Refs(b *testing.B) {
	m := NewTypedMap[int32](NewMapType(TypeI32, TypeI64), 0)
	m.Set(1, BoxI64(2))
	require.Empty(b, m.Refs(nil))

	var refs []Ref
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
		m := NewMap(NewMapType(TypeI32, TypeI32))
		m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{Key: BoxI32(1), Value: BoxI32(2)})
		require.Empty(b, m.Refs(nil))

		var refs []Ref
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = m.Refs(nil)
		}
		b.StopTimer()
		require.Empty(b, refs)
	})

	b.Run("child refs", func(b *testing.B) {
		m := NewMap(NewMapType(TypeRef, TypeRef))
		m.Set(MapKey{Kind: KindRef, Bits: 1}, MapEntry{Key: BoxRef(1), Value: BoxRef(2)})
		require.Equal(b, []Ref{1, 2}, m.Refs(nil))

		refs := make([]Ref, 0, 2)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = m.Refs(refs[:0])
		}
		b.StopTimer()
		require.Equal(b, []Ref{1, 2}, refs)
	})
}
