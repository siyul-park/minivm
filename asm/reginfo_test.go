package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRegInfo(t *testing.T) {
	ri := NewRegInfo(8, 4, []uint8{0, 1}, []uint8{0})
	require.Equal(t, uint8(8), ri.NumInt)
	require.Equal(t, uint8(4), ri.NumFloat)
}

func TestRegInfo_IsReserved(t *testing.T) {
	ri := NewRegInfo(8, 4, []uint8{0, 1}, []uint8{0})

	require.True(t, ri.IsReserved(NewPReg(0, RegTypeInt, Width64)))
	require.True(t, ri.IsReserved(NewPReg(1, RegTypeInt, Width64)))
	require.False(t, ri.IsReserved(NewPReg(2, RegTypeInt, Width64)))

	require.True(t, ri.IsReserved(NewPReg(0, RegTypeFloat, Width64)))
	require.False(t, ri.IsReserved(NewPReg(1, RegTypeFloat, Width64)))
}

func TestRegInfo_Allocatable(t *testing.T) {
	ri := NewRegInfo(4, 2, []uint8{0}, []uint8{})

	intMask := ri.Allocatable(RegTypeInt)
	// registers 0-3 minus reserved {0} → {1,2,3}
	require.False(t, intMask.Contains(0))
	require.True(t, intMask.Contains(1))
	require.True(t, intMask.Contains(2))
	require.True(t, intMask.Contains(3))
	require.Equal(t, 3, intMask.Count())

	floatMask := ri.Allocatable(RegTypeFloat)
	require.Equal(t, 2, floatMask.Count())
	require.True(t, floatMask.Contains(0))
	require.True(t, floatMask.Contains(1))
}
