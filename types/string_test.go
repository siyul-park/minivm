package types

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestString_Kind(t *testing.T) {
	val := String("")
	require.Equal(t, KindRef, val.Kind())
}

func TestString_Type(t *testing.T) {
	val := String("")
	require.Equal(t, TypeString, val.Type())
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

func TestStringIterator(t *testing.T) {
	t.Run("ascii and multibyte", func(t *testing.T) {
		iter := NewStringIterator(Ref(3), String("a한"))

		require.True(t, iter.Done())
		require.Equal(t, NewIteratorType(TypeI32), iter.Type())
		require.True(t, iter.Next())
		require.Equal(t, I32('a'), iter.Current())
		require.True(t, iter.Next())
		require.Equal(t, I32('한'), iter.Current())
		require.False(t, iter.Next())
		require.True(t, iter.Done())
		require.Equal(t, BoxedNull, iter.Current())
	})

	t.Run("empty", func(t *testing.T) {
		iter := NewStringIterator(Ref(3), String(""))

		require.False(t, iter.Next())
		require.True(t, iter.Done())
		require.Equal(t, BoxedNull, iter.Current())
	})

	t.Run("invalid utf8", func(t *testing.T) {
		iter := NewStringIterator(Ref(3), String(string([]byte{0xff, 'a'})))

		require.True(t, iter.Next())
		require.Equal(t, I32(utf8.RuneError), iter.Current())
		require.True(t, iter.Next())
		require.Equal(t, I32('a'), iter.Current())
	})

	t.Run("refs", func(t *testing.T) {
		iter := NewStringIterator(Ref(3), String("a"))

		require.Equal(t, []Ref{3}, iter.Refs())
	})
}

func TestStringType_Cast(t *testing.T) {
	require.True(t, TypeString.Cast(TypeString))
	require.False(t, TypeString.Cast(TypeI32))
}

func TestStringType_Equals(t *testing.T) {
	require.True(t, TypeString.Equals(TypeString))
	require.False(t, TypeString.Equals(TypeI32))
}
