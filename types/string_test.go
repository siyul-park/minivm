package types_test

import (
	"testing"
	"unicode/utf8"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewStringIterator(t *testing.T) {
	it := types.NewStringIterator(3, types.String("a"))
	require.True(t, it.Done())
	require.Equal(t, types.BoxedNull, it.Current())
}

func TestString_Kind(t *testing.T) {
	val := types.String("")
	require.Equal(t, types.KindRef, val.Kind())
}

func TestString_Type(t *testing.T) {
	typ := types.String("").Type()
	require.Equal(t, types.TypeString, typ)
	require.Equal(t, types.KindRef, typ.Kind())
	require.Equal(t, "string", typ.String())
	require.True(t, typ.Cast(types.TypeString))
	require.False(t, typ.Cast(types.TypeI32))
	require.True(t, typ.Equals(types.TypeString))
	require.False(t, typ.Equals(types.TypeI32))
}

func TestString_String(t *testing.T) {
	tests := []struct {
		val types.String
		str string
	}{
		{val: types.String(""), str: `""`},
		{val: types.String("hello"), str: `"hello"`},
	}
	for _, tt := range tests {
		t.Run(string(tt.val), func(t *testing.T) {
			require.Equal(t, tt.str, tt.val.String())
		})
	}
}

func TestStringIterator_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewStringIterator(3, "a").Kind())
}

func TestStringIterator_Type(t *testing.T) {
	require.Equal(t, types.NewIteratorType(types.TypeI32), types.NewStringIterator(3, "a").Type())
}

func TestStringIterator_String(t *testing.T) {
	require.Equal(t, "string.iterator", types.NewStringIterator(3, "a").String())
}

func TestStringIterator_Next(t *testing.T) {
	t.Run("ascii and multibyte", func(t *testing.T) {
		it := types.NewStringIterator(3, types.String("a한"))
		require.True(t, it.Next())
		require.Equal(t, types.I32('a'), it.Current())
		require.True(t, it.Next())
		require.Equal(t, types.I32('한'), it.Current())
		require.False(t, it.Next())
	})

	t.Run("empty", func(t *testing.T) {
		it := types.NewStringIterator(3, "")
		require.False(t, it.Next())
		require.Equal(t, types.BoxedNull, it.Current())
	})

	t.Run("invalid utf8", func(t *testing.T) {
		it := types.NewStringIterator(3, types.String(string([]byte{0xff, 'a'})))
		require.True(t, it.Next())
		require.Equal(t, types.I32(utf8.RuneError), it.Current())
		require.True(t, it.Next())
		require.Equal(t, types.I32('a'), it.Current())
	})
}

func TestStringIterator_Current(t *testing.T) {
	it := types.NewStringIterator(3, "a")
	require.Equal(t, types.BoxedNull, it.Current())
	require.True(t, it.Next())
	require.Equal(t, types.I32('a'), it.Current())
}

func TestStringIterator_Done(t *testing.T) {
	it := types.NewStringIterator(3, "a")
	require.True(t, it.Done())
	require.True(t, it.Next())
	require.False(t, it.Done())
	require.False(t, it.Next())
	require.True(t, it.Done())
}

func TestStringIterator_Refs(t *testing.T) {
	it := types.NewStringIterator(3, "a")
	require.Equal(t, []types.Ref{5, 3}, it.Refs([]types.Ref{5}))
}
