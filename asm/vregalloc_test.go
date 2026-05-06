package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewVRegAlloc(t *testing.T) {
	a := NewVRegAlloc()
	require.NotNil(t, a)
}

func TestVRegAlloc_Alloc(t *testing.T) {
	a := NewVRegAlloc()

	r0 := a.Alloc(RegTypeInt, Width64)
	require.Equal(t, int32(0), r0.ID())
	require.Equal(t, RegTypeInt, r0.Type())
	require.Equal(t, Width64, r0.Width())

	r1 := a.Alloc(RegTypeFloat, Width32)
	require.Equal(t, int32(1), r1.ID())
	require.Equal(t, RegTypeFloat, r1.Type())
	require.Equal(t, Width32, r1.Width())
}

func TestVRegAlloc_Reset(t *testing.T) {
	a := NewVRegAlloc()
	a.Alloc(RegTypeInt, Width64)
	a.Alloc(RegTypeInt, Width64)
	a.Reset()

	r := a.Alloc(RegTypeInt, Width64)
	require.Equal(t, int32(0), r.ID())
}
