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

func TestMap_Trace(t *testing.T) {
	t.Run("ref key and value", func(t *testing.T) {
		typ := NewMapType(TypeRef, TypeRef)
		m := NewMap(typ)
		m.Set(MapKey{Kind: KindRef, Bits: 1}, MapEntry{
			Key:   BoxRef(1),
			Value: BoxRef(2),
		})
		var refs []Ref
		m.Trace(func(ref Ref) {
			refs = append(refs, ref)
		})
		require.Equal(t, []Ref{1, 2}, refs)
	})

	t.Run("spilled i64 value", func(t *testing.T) {
		typ := NewMapType(TypeI32, TypeI64)
		m := NewMap(typ)
		m.Set(MapKey{Kind: KindI32, Bits: 1}, MapEntry{
			Key:   BoxI32(1),
			Value: BoxRef(2),
		})
		var refs []Ref
		m.Trace(func(ref Ref) {
			refs = append(refs, ref)
		})
		require.Equal(t, []Ref{2}, refs)
	})
}

func TestNewMapForType(t *testing.T) {
	structType := NewStructType(NewStructField(TypeI32))
	tests := []struct {
		name string
		typ  *MapType
		want any
	}{
		{name: "i32", typ: NewMapType(TypeI32, TypeI32), want: (*MapI32)(nil)},
		{name: "i64", typ: NewMapType(TypeI64, TypeI32), want: (*MapI64)(nil)},
		{name: "f32", typ: NewMapType(TypeF32, TypeI32), want: (*MapF32)(nil)},
		{name: "f64", typ: NewMapType(TypeF64, TypeI32), want: (*MapF64)(nil)},
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

func TestMapI32_Get(t *testing.T) {
	m := NewMapI32(NewMapType(TypeI32, TypeI32), 0)
	m.Set(1, BoxI32(2))

	got, ok := m.Get(1)
	require.True(t, ok)
	require.Equal(t, BoxI32(2), got)

	_, ok = m.Get(2)
	require.False(t, ok)
}

func TestMapI32_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewMapI32(NewMapType(TypeI32, TypeI32), 0).Kind())
}

func TestMapI32_Type(t *testing.T) {
	typ := NewMapType(TypeI32, TypeI32)
	require.Equal(t, typ, NewMapI32(typ, 0).Type())
}

func TestMapI32_Len(t *testing.T) {
	m := NewMapI32(NewMapType(TypeI32, TypeI32), 0)
	require.Equal(t, 0, m.Len())
	m.Set(1, BoxI32(2))
	require.Equal(t, 1, m.Len())
}

func TestMapI32_Set(t *testing.T) {
	m := NewMapI32(NewMapType(TypeI32, TypeI32), 0)

	old, ok := m.Set(1, BoxI32(2))
	require.False(t, ok)
	require.Equal(t, Boxed(0), old)

	old, ok = m.Set(1, BoxI32(3))
	require.True(t, ok)
	require.Equal(t, BoxI32(2), old)
}

func TestMapI32_Delete(t *testing.T) {
	m := NewMapI32(NewMapType(TypeI32, TypeI32), 0)
	m.Set(1, BoxI32(2))

	old, ok := m.Delete(1)
	require.True(t, ok)
	require.Equal(t, BoxI32(2), old)
	require.Equal(t, 0, m.Len())
}

func TestMapI32_Range(t *testing.T) {
	m := NewMapI32(NewMapType(TypeI32, TypeI32), 0)
	m.Set(1, BoxI32(2))

	var keys []int32
	m.Range(func(key int32, _ Boxed) {
		keys = append(keys, key)
	})
	require.Equal(t, []int32{1}, keys)
}

func TestMapI32_Clear(t *testing.T) {
	m := NewMapI32(NewMapType(TypeI32, TypeI32), 0)
	m.Set(1, BoxI32(2))

	var values []Boxed
	m.Clear(func(value Boxed) {
		values = append(values, value)
	})
	require.Equal(t, []Boxed{BoxI32(2)}, values)
	require.Equal(t, 0, m.Len())
}

func TestMapI32_String(t *testing.T) {
	m := NewMapI32(NewMapType(TypeI32, TypeI32), 0)
	m.Set(1, BoxI32(2))
	require.Equal(t, "map[i32]i32{1: 2}", m.String())
}

func TestMapI32_Trace(t *testing.T) {
	m := NewMapI32(NewMapType(TypeI32, TypeString), 0)
	m.Set(1, BoxRef(2))
	var refs []Ref
	m.Trace(func(ref Ref) {
		refs = append(refs, ref)
	})
	require.Equal(t, []Ref{2}, refs)
}

func TestMapI64_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewMapI64(NewMapType(TypeI64, TypeI32), 0).Kind())
}

func TestMapI64_Type(t *testing.T) {
	typ := NewMapType(TypeI64, TypeI32)
	require.Equal(t, typ, NewMapI64(typ, 0).Type())
}

func TestMapI64_Len(t *testing.T) {
	m := NewMapI64(NewMapType(TypeI64, TypeI32), 0)
	require.Equal(t, 0, m.Len())
	m.Set(1, BoxI32(2))
	require.Equal(t, 1, m.Len())
}

func TestMapI64_Get(t *testing.T) {
	m := NewMapI64(NewMapType(TypeI64, TypeI32), 0)
	m.Set(1<<50, BoxI32(2))

	got, ok := m.Get(1 << 50)
	require.True(t, ok)
	require.Equal(t, BoxI32(2), got)

	_, ok = m.Get(2)
	require.False(t, ok)
}

func TestMapI64_Set(t *testing.T) {
	m := NewMapI64(NewMapType(TypeI64, TypeI32), 0)

	old, ok := m.Set(1<<50, BoxI32(2))
	require.False(t, ok)
	require.Equal(t, Boxed(0), old)

	old, ok = m.Set(1<<50, BoxI32(3))
	require.True(t, ok)
	require.Equal(t, BoxI32(2), old)
}

func TestMapI64_Delete(t *testing.T) {
	m := NewMapI64(NewMapType(TypeI64, TypeI32), 0)
	m.Set(1, BoxI32(2))

	old, ok := m.Delete(1)
	require.True(t, ok)
	require.Equal(t, BoxI32(2), old)
	require.Equal(t, 0, m.Len())
}

func TestMapI64_Range(t *testing.T) {
	m := NewMapI64(NewMapType(TypeI64, TypeI32), 0)
	m.Set(1, BoxI32(2))

	var keys []int64
	m.Range(func(key int64, _ Boxed) {
		keys = append(keys, key)
	})
	require.Equal(t, []int64{1}, keys)
}

func TestMapI64_Clear(t *testing.T) {
	m := NewMapI64(NewMapType(TypeI64, TypeI32), 0)
	m.Set(1, BoxI32(2))

	var values []Boxed
	m.Clear(func(value Boxed) {
		values = append(values, value)
	})
	require.Equal(t, []Boxed{BoxI32(2)}, values)
	require.Equal(t, 0, m.Len())
}

func TestMapI64_String(t *testing.T) {
	m := NewMapI64(NewMapType(TypeI64, TypeI32), 0)
	m.Set(1, BoxI32(2))
	require.Equal(t, "map[i64]i32{1: 2}", m.String())
}

func TestMapI64_Trace(t *testing.T) {
	m := NewMapI64(NewMapType(TypeI64, TypeString), 0)
	m.Set(1, BoxRef(2))
	var refs []Ref
	m.Trace(func(ref Ref) {
		refs = append(refs, ref)
	})
	require.Equal(t, []Ref{2}, refs)
}

func TestMapF32_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewMapF32(NewMapType(TypeF32, TypeI32), 0).Kind())
}

func TestMapF32_Type(t *testing.T) {
	typ := NewMapType(TypeF32, TypeI32)
	require.Equal(t, typ, NewMapF32(typ, 0).Type())
}

func TestMapF32_Len(t *testing.T) {
	m := NewMapF32(NewMapType(TypeF32, TypeI32), 0)
	require.Equal(t, 0, m.Len())
	m.Set(1, BoxI32(2))
	require.Equal(t, 1, m.Len())
}

func TestMapF32_Get(t *testing.T) {
	m := NewMapF32(NewMapType(TypeF32, TypeI32), 0)
	m.Set(1, BoxI32(2))

	got, ok := m.Get(1)
	require.True(t, ok)
	require.Equal(t, BoxI32(2), got)

	_, ok = m.Get(2)
	require.False(t, ok)
}

func TestMapF32_Set(t *testing.T) {
	m := NewMapF32(NewMapType(TypeF32, TypeI32), 0)

	m.Set(float32(math.Copysign(0, -1)), BoxI32(1))
	m.Set(0, BoxI32(2))

	got, ok := m.Get(float32(math.Copysign(0, -1)))
	require.True(t, ok)
	require.Equal(t, BoxI32(2), got)
	require.Equal(t, 1, m.Len())
}

func TestMapF32_Delete(t *testing.T) {
	m := NewMapF32(NewMapType(TypeF32, TypeI32), 0)
	m.Set(1, BoxI32(2))

	old, ok := m.Delete(1)
	require.True(t, ok)
	require.Equal(t, BoxI32(2), old)
	require.Equal(t, 0, m.Len())
}

func TestMapF32_Range(t *testing.T) {
	m := NewMapF32(NewMapType(TypeF32, TypeI32), 0)
	m.Set(1, BoxI32(2))

	var keys []float32
	m.Range(func(key float32, _ Boxed) {
		keys = append(keys, key)
	})
	require.Equal(t, []float32{1}, keys)
}

func TestMapF32_Clear(t *testing.T) {
	m := NewMapF32(NewMapType(TypeF32, TypeI32), 0)
	m.Set(1, BoxI32(2))

	var values []Boxed
	m.Clear(func(value Boxed) {
		values = append(values, value)
	})
	require.Equal(t, []Boxed{BoxI32(2)}, values)
	require.Equal(t, 0, m.Len())
}

func TestMapF32_String(t *testing.T) {
	m := NewMapF32(NewMapType(TypeF32, TypeI32), 0)
	m.Set(1, BoxI32(2))
	require.Equal(t, "map[f32]i32{1: 2}", m.String())
}

func TestMapF32_Trace(t *testing.T) {
	m := NewMapF32(NewMapType(TypeF32, TypeString), 0)
	m.Set(1, BoxRef(2))
	var refs []Ref
	m.Trace(func(ref Ref) {
		refs = append(refs, ref)
	})
	require.Equal(t, []Ref{2}, refs)
}

func TestMapF64_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewMapF64(NewMapType(TypeF64, TypeI32), 0).Kind())
}

func TestMapF64_Type(t *testing.T) {
	typ := NewMapType(TypeF64, TypeI32)
	require.Equal(t, typ, NewMapF64(typ, 0).Type())
}

func TestMapF64_Len(t *testing.T) {
	m := NewMapF64(NewMapType(TypeF64, TypeI32), 0)
	require.Equal(t, 0, m.Len())
	m.Set(1, BoxI32(2))
	require.Equal(t, 1, m.Len())
}

func TestMapF64_Get(t *testing.T) {
	m := NewMapF64(NewMapType(TypeF64, TypeI32), 0)
	m.Set(1, BoxI32(2))

	got, ok := m.Get(1)
	require.True(t, ok)
	require.Equal(t, BoxI32(2), got)

	_, ok = m.Get(2)
	require.False(t, ok)
}

func TestMapF64_Set(t *testing.T) {
	m := NewMapF64(NewMapType(TypeF64, TypeI32), 0)

	m.Set(math.Copysign(0, -1), BoxI32(1))
	m.Set(0, BoxI32(2))

	got, ok := m.Get(math.Copysign(0, -1))
	require.True(t, ok)
	require.Equal(t, BoxI32(2), got)
	require.Equal(t, 1, m.Len())
}

func TestMapF64_Delete(t *testing.T) {
	m := NewMapF64(NewMapType(TypeF64, TypeI32), 0)
	m.Set(1, BoxI32(2))

	old, ok := m.Delete(1)
	require.True(t, ok)
	require.Equal(t, BoxI32(2), old)
	require.Equal(t, 0, m.Len())
}

func TestMapF64_Range(t *testing.T) {
	m := NewMapF64(NewMapType(TypeF64, TypeI32), 0)
	m.Set(1, BoxI32(2))

	var keys []float64
	m.Range(func(key float64, _ Boxed) {
		keys = append(keys, key)
	})
	require.Equal(t, []float64{1}, keys)
}

func TestMapF64_Clear(t *testing.T) {
	m := NewMapF64(NewMapType(TypeF64, TypeI32), 0)
	m.Set(1, BoxI32(2))

	var values []Boxed
	m.Clear(func(value Boxed) {
		values = append(values, value)
	})
	require.Equal(t, []Boxed{BoxI32(2)}, values)
	require.Equal(t, 0, m.Len())
}

func TestMapF64_String(t *testing.T) {
	m := NewMapF64(NewMapType(TypeF64, TypeI32), 0)
	m.Set(1, BoxI32(2))
	require.Equal(t, "map[f64]i32{1: 2}", m.String())
}

func TestMapF64_Trace(t *testing.T) {
	m := NewMapF64(NewMapType(TypeF64, TypeString), 0)
	m.Set(1, BoxRef(2))
	var refs []Ref
	m.Trace(func(ref Ref) {
		refs = append(refs, ref)
	})
	require.Equal(t, []Ref{2}, refs)
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

func BenchmarkMapI64_Get(b *testing.B) {
	m := NewMapI64(NewMapType(TypeI64, TypeI32), 0)
	m.Set(42, BoxI32(7))

	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		if _, ok := m.Get(42); !ok {
			b.Fatal("missing key")
		}
	}
}

func BenchmarkMapStringGet_Interned(b *testing.B) {
	m := NewMap(NewMapType(TypeString, TypeI32))
	m.Set(MapKey{Kind: KindRef, Bits: 1}, MapEntry{Key: BoxRef(1), Value: BoxI32(7)})
	key := MapKey{Kind: KindRef, Bits: 1}

	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		if _, ok := m.Get(key); !ok {
			b.Fatal("missing key")
		}
	}
}
