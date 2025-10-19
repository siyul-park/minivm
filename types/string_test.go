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
	val := String("")
	require.Equal(t, "", val.String())
}
