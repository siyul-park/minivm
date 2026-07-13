package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPReg(t *testing.T) {
	reg := NewPReg(1, RegTypeFloat, Width64)

	require.Equal(t, uint8(1), reg.ID())
	require.Equal(t, RegTypeFloat, reg.Type())
	require.Equal(t, Width64, reg.Width())
}

func TestPReg_ID(t *testing.T) {
	require.Equal(t, uint8(3), NewPReg(3, RegTypeInt, Width64).ID())
}

func TestPReg_Type(t *testing.T) {
	require.Equal(t, RegTypeFloat, NewPReg(3, RegTypeFloat, Width64).Type())
}

func TestPReg_Width(t *testing.T) {
	require.Equal(t, Width32, NewPReg(3, RegTypeInt, Width32).Width())
}

func TestPReg_String(t *testing.T) {
	tests := []struct {
		reg PReg
		str string
	}{
		{NewPReg(1, RegTypeInt, Width32), "w1"},
		{NewPReg(1, RegTypeInt, Width64), "x1"},
		{NewPReg(1, RegTypeFloat, Width32), "s1"},
		{NewPReg(1, RegTypeFloat, Width64), "d1"},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.reg.String())
		})
	}
}

func TestNewVReg(t *testing.T) {
	reg := NewVReg(1, RegTypeFloat, Width64)

	require.Equal(t, int32(1), reg.ID())
	require.Equal(t, RegTypeFloat, reg.Type())
	require.Equal(t, Width64, reg.Width())
}

func TestVReg_ID(t *testing.T) {
	require.Equal(t, int32(3), NewVReg(3, RegTypeInt, Width64).ID())
}

func TestVReg_Type(t *testing.T) {
	require.Equal(t, RegTypeFloat, NewVReg(3, RegTypeFloat, Width64).Type())
}

func TestVReg_Width(t *testing.T) {
	require.Equal(t, Width32, NewVReg(3, RegTypeInt, Width32).Width())
}

func TestVReg_String(t *testing.T) {
	tests := []struct {
		reg VReg
		str string
	}{
		{NewVReg(1, RegTypeInt, Width64), "vr1"},
		{NewVReg(1, RegTypeFloat, Width64), "vf1"},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.reg.String())
		})
	}
}

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

func TestNewRegInfo(t *testing.T) {
	info := NewRegInfo(8, 4, []uint8{0, 1}, []uint8{2}, []uint8{7})
	require.Equal(t, uint8(8), info.NumInt)
	require.Equal(t, uint8(4), info.NumFloat)
	require.True(t, info.IntReserved.Contains(0))
	require.True(t, info.FltReserved.Contains(2))
	require.True(t, info.Scratch.Contains(7))
}

func TestRegInfo_Allocatable(t *testing.T) {
	info := NewRegInfo(4, 3, []uint8{1}, []uint8{2}, nil)
	require.Equal(t, NewRegMask([]uint8{0, 2, 3}), info.Allocatable(RegTypeInt))
	require.Equal(t, NewRegMask([]uint8{0, 1}), info.Allocatable(RegTypeFloat))
}
