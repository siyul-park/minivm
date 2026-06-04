package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRegMask(t *testing.T) {
	require.True(t, NewRegMask([]uint8{1, 3}).Contains(1))
	require.True(t, NewRegMask([]uint8{1, 3}).Contains(3))
}

func TestRegMask_Set(t *testing.T) {
	require.True(t, RegMask(0).Set(1).Contains(1))
	require.False(t, RegMask(0).Set(64).Contains(64))
}

func TestRegMask_Clear(t *testing.T) {
	require.False(t, NewRegMask([]uint8{1}).Clear(1).Contains(1))
	require.True(t, NewRegMask([]uint8{1}).Clear(64).Contains(1))
}

func TestRegMask_Contains(t *testing.T) {
	mask := NewRegMask([]uint8{1})

	require.True(t, mask.Contains(1))
	require.False(t, mask.Contains(2))
	require.False(t, mask.Contains(64))
}

func TestRegMask_First(t *testing.T) {
	require.Equal(t, uint8(1), NewRegMask([]uint8{3, 1}).First())
	require.Equal(t, uint8(0xFF), RegMask(0).First())
}

func TestRegMask_PopFirst(t *testing.T) {
	first, rest := NewRegMask([]uint8{1, 3}).PopFirst()

	require.Equal(t, uint8(1), first)
	require.False(t, rest.Contains(1))
	require.True(t, rest.Contains(3))

	first, rest = RegMask(0).PopFirst()
	require.Equal(t, uint8(0xFF), first)
	require.Zero(t, rest)
}

func TestRegMask_Count(t *testing.T) {
	require.Equal(t, 2, NewRegMask([]uint8{1, 3}).Count())
}
