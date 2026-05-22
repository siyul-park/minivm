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
		m.Set(MapKey{Kind: KindRef, Text: ""}, MapEntry{
			Key:   BoxedNull,
			Value: BoxI32(2),
		})
		require.Equal(t, "map[string]i32{\"\": 2}", m.String())
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
	m.Set(MapKey{Kind: KindRef, Text: "a"}, MapEntry{Key: BoxedNull, Value: BoxI32(2)})

	entry, ok := m.Get(MapKey{Kind: KindRef, Text: "a"})
	require.True(t, ok)
	require.Equal(t, BoxI32(2), entry.Value)

	_, ok = m.Get(MapKey{Kind: KindRef, Text: "b"})
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

func TestMap_Refs(t *testing.T) {
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

func TestMapType_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewMapType(TypeI32, TypeI32).Kind())
}

func TestMapType_String(t *testing.T) {
	require.Equal(t, "map[i32]string", NewMapType(TypeI32, TypeString).String())
}

func TestMapType_BoxKeyKind(t *testing.T) {
	require.Equal(t, KindI32, NewMapType(TypeI32, TypeString).KeyKind)
}

func TestMapType_ElemKind(t *testing.T) {
	require.Equal(t, KindRef, NewMapType(TypeI32, TypeString).ElemKind)
}

func TestMapType_TraceKeys(t *testing.T) {
	require.True(t, NewMapType(TypeRef, TypeI32).TraceKeys)
	require.False(t, NewMapType(TypeString, TypeI32).TraceKeys)
}

func TestMapType_TraceValues(t *testing.T) {
	require.True(t, NewMapType(TypeI32, TypeString).TraceValues)
	require.True(t, NewMapType(TypeI32, TypeI64).TraceValues)
	require.False(t, NewMapType(TypeI32, TypeI32).TraceValues)
}

func TestMapType_StringKeys(t *testing.T) {
	require.True(t, NewMapType(TypeString, TypeI32).StringKeys)
	require.False(t, NewMapType(TypeRef, TypeI32).StringKeys)
}

func TestMapType_HasRefs(t *testing.T) {
	require.True(t, NewMapType(TypeRef, TypeI32).HasRefs())
	require.True(t, NewMapType(TypeI32, TypeString).HasRefs())
	require.True(t, NewMapType(TypeI32, TypeI64).HasRefs())
	require.False(t, NewMapType(TypeI32, TypeI32).HasRefs())
}

func TestMapType_KeyRef(t *testing.T) {
	ref, ok := NewMapType(TypeString, TypeI32).KeyRef(BoxRef(3))
	require.True(t, ok)
	require.Equal(t, 3, ref)

	ref, ok = NewMapType(TypeRef, TypeI32).KeyRef(BoxRef(4))
	require.True(t, ok)
	require.Equal(t, 4, ref)

	_, ok = NewMapType(TypeI32, TypeI32).KeyRef(BoxRef(5))
	require.False(t, ok)

	_, ok = NewMapType(TypeString, TypeI32).KeyRef(BoxI32(1))
	require.False(t, ok)
}

func TestMapType_BoxKey(t *testing.T) {
	t.Run("i32", func(t *testing.T) {
		key, entry, ok := NewMapType(TypeI32, TypeI32).BoxKey(BoxI32(1))
		require.True(t, ok)
		require.Equal(t, MapKey{Kind: KindI32, Bits: 1}, key)
		require.Equal(t, BoxI32(1), entry)
	})

	t.Run("f32 negative zero", func(t *testing.T) {
		key, entry, ok := NewMapType(TypeF32, TypeI32).BoxKey(BoxF32(float32(math.Copysign(0, -1))))
		require.True(t, ok)
		require.Equal(t, MapKey{Kind: KindF32, Bits: 0}, key)
		require.Equal(t, BoxF32(0), entry)
	})

	t.Run("f64 negative zero", func(t *testing.T) {
		key, entry, ok := NewMapType(TypeF64, TypeI32).BoxKey(BoxF64(math.Copysign(0, -1)))
		require.True(t, ok)
		require.Equal(t, MapKey{Kind: KindF64, Bits: 0}, key)
		require.Equal(t, BoxF64(0), entry)
	})

	t.Run("ref", func(t *testing.T) {
		key, entry, ok := NewMapType(TypeRef, TypeI32).BoxKey(BoxRef(7))
		require.True(t, ok)
		require.Equal(t, MapKey{Kind: KindRef, Bits: 7}, key)
		require.Equal(t, BoxRef(7), entry)
	})

	t.Run("ref mismatch", func(t *testing.T) {
		_, _, ok := NewMapType(TypeRef, TypeI32).BoxKey(BoxI32(7))
		require.False(t, ok)
	})
}

func TestMapType_I64Key(t *testing.T) {
	key, entry := NewMapType(TypeI64, TypeI32).I64Key(1 << 50)
	require.Equal(t, MapKey{Kind: KindI64, Bits: uint64(1 << 50)}, key)
	require.Equal(t, Boxed(0), entry)
}

func TestMapType_StringKey(t *testing.T) {
	key, entry := NewMapType(TypeString, TypeI32).StringKey("a")
	require.Equal(t, MapKey{Kind: KindRef, Text: "a"}, key)
	require.Equal(t, BoxedNull, entry)
}

func TestMapType_Zero(t *testing.T) {
	require.Equal(t, BoxI32(0), NewMapType(TypeI32, TypeI32).Zero())
	require.Equal(t, BoxI64(0), NewMapType(TypeI32, TypeI64).Zero())
	require.Equal(t, BoxF32(0), NewMapType(TypeI32, TypeF32).Zero())
	require.Equal(t, BoxF64(0), NewMapType(TypeI32, TypeF64).Zero())
	require.Equal(t, BoxedNull, NewMapType(TypeI32, TypeString).Zero())
}

func TestMapType_Cast(t *testing.T) {
	require.True(t, NewMapType(TypeI32, TypeI32).Cast(NewMapType(TypeI32, TypeI32)))
	require.False(t, NewMapType(TypeI32, TypeI32).Cast(NewMapType(TypeI64, TypeI32)))
}

func TestMapType_Equals(t *testing.T) {
	require.True(t, NewMapType(TypeI32, TypeI32).Equals(NewMapType(TypeI32, TypeI32)))
	require.False(t, NewMapType(TypeI32, TypeI32).Equals(NewMapType(TypeI32, TypeI64)))
}
