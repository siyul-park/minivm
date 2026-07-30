package types_test

import (
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewIteratorType(t *testing.T) {
	typ := types.NewIteratorType(types.TypeI32)
	require.Equal(t, types.TypeI32, typ.Elem)
}

func TestIteratorType_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewIteratorType(types.TypeI32).Kind())
}

func TestIteratorType_String(t *testing.T) {
	require.Equal(t, "iterator[i32]", types.NewIteratorType(types.TypeI32).String())
}

func TestIteratorType_Cast(t *testing.T) {
	typ := types.NewIteratorType(types.TypeI32)

	require.True(t, typ.Cast(typ))
	require.True(t, typ.Cast(types.NewIteratorType(types.TypeI32)))
	require.False(t, typ.Cast(types.NewIteratorType(types.TypeI64)))
	require.False(t, typ.Cast(types.TypeRef))
}

func TestIteratorType_Equals(t *testing.T) {
	typ := types.NewIteratorType(types.TypeI32)

	require.True(t, typ.Equals(typ))
	require.True(t, typ.Equals(types.NewIteratorType(types.TypeI32)))
	require.False(t, typ.Equals(types.NewIteratorType(types.TypeI64)))
	require.False(t, typ.Equals(types.TypeRef))
}
