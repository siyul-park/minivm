package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIteratorType_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewIteratorType(TypeI32).Kind())
}

func TestIteratorType_String(t *testing.T) {
	require.Equal(t, "iterator[i32]", NewIteratorType(TypeI32).String())
}

func TestIteratorType_Cast(t *testing.T) {
	typ := NewIteratorType(TypeI32)

	require.True(t, typ.Cast(typ))
	require.True(t, typ.Cast(NewIteratorType(TypeI32)))
	require.False(t, typ.Cast(NewIteratorType(TypeI64)))
	require.False(t, typ.Cast(TypeRef))
}

func TestIteratorType_Equals(t *testing.T) {
	typ := NewIteratorType(TypeI32)

	require.True(t, typ.Equals(typ))
	require.True(t, typ.Equals(NewIteratorType(TypeI32)))
	require.False(t, typ.Equals(NewIteratorType(TypeI64)))
	require.False(t, typ.Equals(TypeRef))
}
