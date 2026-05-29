package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRegInfo(t *testing.T) {
	ri := NewRegInfo(8, 4, []uint8{6, 7}, []uint8{3})
	require.Equal(t, uint8(8), ri.NumInt)
	require.Equal(t, uint8(4), ri.NumFloat)
}

func TestRegInfo_Allocatable(t *testing.T) {
	ri := NewRegInfo(4, 2, []uint8{3}, []uint8{1})

	intMask := ri.Allocatable(RegTypeInt)
	require.True(t, intMask.Contains(0))
	require.True(t, intMask.Contains(1))
	require.True(t, intMask.Contains(2))
	require.False(t, intMask.Contains(3))

	floatMask := ri.Allocatable(RegTypeFloat)
	require.True(t, floatMask.Contains(0))
	require.False(t, floatMask.Contains(1))
}
