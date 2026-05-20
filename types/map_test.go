package types

import (
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
		m.Entries[MapKey{Kind: KindI32, Bits: 1}] = MapEntry{
			Key:   BoxI32(1),
			Value: BoxI32(2),
		}
		require.Equal(t, "map[i32]i32{1: 2}", m.String())
	})

	t.Run("empty string key", func(t *testing.T) {
		typ := NewMapType(TypeString, TypeI32)
		m := NewMap(typ)
		m.Entries[MapKey{Kind: KindRef, Text: ""}] = MapEntry{
			Key:   BoxedNull,
			Value: BoxI32(2),
		}
		require.Equal(t, "map[string]i32{\"\": 2}", m.String())
	})
}

func TestMap_Refs(t *testing.T) {
	typ := NewMapType(TypeRef, TypeRef)
	m := NewMap(typ)
	m.Entries[MapKey{Kind: KindRef, Bits: 1}] = MapEntry{
		Key:   BoxRef(1),
		Value: BoxRef(2),
	}
	require.Equal(t, []Ref{1, 2}, m.Refs())
}

func TestMapType_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewMapType(TypeI32, TypeI32).Kind())
}

func TestMapType_String(t *testing.T) {
	require.Equal(t, "map[i32]string", NewMapType(TypeI32, TypeString).String())
}

func TestMapType_Cast(t *testing.T) {
	require.True(t, NewMapType(TypeI32, TypeI32).Cast(NewMapType(TypeI32, TypeI32)))
	require.False(t, NewMapType(TypeI32, TypeI32).Cast(NewMapType(TypeI64, TypeI32)))
}

func TestMapType_Equals(t *testing.T) {
	require.True(t, NewMapType(TypeI32, TypeI32).Equals(NewMapType(TypeI32, TypeI32)))
	require.False(t, NewMapType(TypeI32, TypeI32).Equals(NewMapType(TypeI32, TypeI64)))
}
