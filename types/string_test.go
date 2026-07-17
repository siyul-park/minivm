package types

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestNewStringIterator(t *testing.T) {
	it := NewStringIterator(3, String("a"))
	require.True(t, it.Done())
	require.Equal(t, BoxedNull, it.Current())
}

func TestString_Kind(t *testing.T) {
	val := String("")
	require.Equal(t, KindRef, val.Kind())
}

func TestString_Type(t *testing.T) {
	typ := String("").Type()
	require.Equal(t, TypeString, typ)
	require.Equal(t, KindRef, typ.Kind())
	require.Equal(t, "string", typ.String())
	require.True(t, typ.Cast(TypeString))
	require.False(t, typ.Cast(TypeI32))
	require.True(t, typ.Equals(TypeString))
	require.False(t, typ.Equals(TypeI32))
}

func TestString_String(t *testing.T) {
	tests := []struct {
		val String
		str string
	}{
		{val: String(""), str: `""`},
		{val: String("hello"), str: `"hello"`},
	}
	for _, tt := range tests {
		t.Run(string(tt.val), func(t *testing.T) {
			require.Equal(t, tt.str, tt.val.String())
		})
	}
}

func TestStringIterator_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewStringIterator(3, "a").Kind())
}

func TestStringIterator_Type(t *testing.T) {
	require.Equal(t, NewIteratorType(TypeI32), NewStringIterator(3, "a").Type())
}

func TestStringIterator_String(t *testing.T) {
	require.Equal(t, "string.iterator", NewStringIterator(3, "a").String())
}

func TestStringIterator_Next(t *testing.T) {
	t.Run("ascii and multibyte", func(t *testing.T) {
		it := NewStringIterator(3, String("a한"))
		require.True(t, it.Next())
		require.Equal(t, I32('a'), it.Current())
		require.True(t, it.Next())
		require.Equal(t, I32('한'), it.Current())
		require.False(t, it.Next())
	})

	t.Run("empty", func(t *testing.T) {
		it := NewStringIterator(3, "")
		require.False(t, it.Next())
		require.Equal(t, BoxedNull, it.Current())
	})

	t.Run("invalid utf8", func(t *testing.T) {
		it := NewStringIterator(3, String(string([]byte{0xff, 'a'})))
		require.True(t, it.Next())
		require.Equal(t, I32(utf8.RuneError), it.Current())
		require.True(t, it.Next())
		require.Equal(t, I32('a'), it.Current())
	})
}

func TestStringIterator_Current(t *testing.T) {
	it := NewStringIterator(3, "a")
	require.Equal(t, BoxedNull, it.Current())
	require.True(t, it.Next())
	require.Equal(t, I32('a'), it.Current())
}

func TestStringIterator_Done(t *testing.T) {
	it := NewStringIterator(3, "a")
	require.True(t, it.Done())
	require.True(t, it.Next())
	require.False(t, it.Done())
	require.False(t, it.Next())
	require.True(t, it.Done())
}

func TestStringIterator_Refs(t *testing.T) {
	it := NewStringIterator(3, "a")
	require.Equal(t, []Ref{5, 3}, it.Refs([]Ref{5}))
}
