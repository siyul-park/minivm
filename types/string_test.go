package types_test

import (
	"math/bits"
	"strings"
	"testing"
	"unicode/utf8"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestHashkey(t *testing.T) {
	require.Equal(t, types.Hashkey(), types.Hashkey())
}

func TestNewStringIterator(t *testing.T) {
	it := types.NewStringIterator(3, types.String("a"))
	require.True(t, it.Done())
	require.Equal(t, types.BoxedNull, it.Current())
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

func TestString_Hash(t *testing.T) {
	mix := func(a, b uint64) uint64 {
		hi, lo := bits.Mul64(a, b)
		return hi ^ lo
	}
	le := func(s string, i, n int) uint64 {
		var v uint64
		for j := n - 1; j >= 0; j-- {
			v = v<<8 | uint64(s[i+j])
		}
		return v
	}
	want := func(s string) uint64 {
		const m0, m1 = 0xa0761d6478bd642f, 0xe7037ed1a0b428db
		n := len(s)
		h := types.Hashkey() ^ uint64(n)
		var a, b uint64
		switch {
		case n >= 8:
			for i := 0; i+8 < n; i += 8 {
				h = mix(le(s, i, 8)^m1, h^m0)
			}
			a = le(s, n-8, 8)
		case n >= 4:
			a, b = le(s, 0, 4), le(s, n-4, 4)
		case n > 0:
			a = uint64(s[0])<<16 | uint64(s[n>>1])<<8 | uint64(s[n-1])
		}
		return mix(a^m1, b^h^m0)
	}

	t.Run("bits", func(t *testing.T) {
		for _, n := range []int{0, 1, 2, 3, 4, 7, 8, 9, 15, 16, 17, 31, 64, 100} {
			s := strings.Repeat("abcdefgh", 13)[:n]
			require.Equal(t, want(s), types.String(s).Hash(), n)
		}
	})

	t.Run("equal content", func(t *testing.T) {
		a := types.String(strings.Repeat("x", 20) + "y")
		b := types.String(strings.Clone(string(a)))
		require.Equal(t, a.Hash(), b.Hash())
		require.Equal(t, a.Hash(), a.Hash())
	})

	t.Run("different content", func(t *testing.T) {
		require.NotEqual(t, types.String("a").Hash(), types.String("b").Hash())
		require.NotEqual(t, types.String("").Hash(), types.String("a").Hash())
	})
}

func TestStringIterator_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewStringIterator(3, "a").Kind())
}

func TestStringIterator_Type(t *testing.T) {
	require.Equal(t, types.NewIteratorType(types.TypeI32()), types.NewStringIterator(3, "a").Type())
}

func TestStringIterator_Refs(t *testing.T) {
	it := types.NewStringIterator(3, "a")
	require.Equal(t, []types.Ref{5, 3}, it.Refs([]types.Ref{5}))
}

func TestStringIterator_String(t *testing.T) {
	require.Equal(t, "string.iterator", types.NewStringIterator(3, "a").String())
}

func TestString_Kind(t *testing.T) {
	val := types.String("")
	require.Equal(t, types.KindRef, val.Kind())
}

func TestString_Type(t *testing.T) {
	typ := types.String("").Type()
	require.Equal(t, types.TypeString(), typ)
	require.Equal(t, types.KindRef, typ.Kind())
	require.Equal(t, "string", typ.String())
	require.True(t, typ.Cast(types.TypeString()))
	require.False(t, typ.Cast(types.TypeI32()))
	require.True(t, typ.Equals(types.TypeString()))
	require.False(t, typ.Equals(types.TypeI32()))
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
