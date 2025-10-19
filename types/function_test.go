package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFunction_Kind(t *testing.T) {
	fn := NewFunction(NewFunctionSignature())
	require.Equal(t, KindRef, fn.Kind())
}

func TestFunction_Type(t *testing.T) {
	fn := NewFunction(NewFunctionSignature())
	require.Equal(t, &FunctionType{}, fn.Type())
}

func TestFunction_String(t *testing.T) {
	fn := NewFunction(NewFunctionSignature())
	require.Equal(t, "func() ()\n", fn.String())
}
