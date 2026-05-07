package types

import (
	"testing"

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
