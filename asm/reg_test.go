package asm_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
)

func TestNewPReg(t *testing.T) {
	reg := asm.NewPReg(1, asm.RegTypeFloat, asm.Width64)

	require.Equal(t, uint8(1), reg.ID())
	require.Equal(t, asm.RegTypeFloat, reg.Type())
	require.Equal(t, asm.Width64, reg.Width())
}

func TestPReg_String(t *testing.T) {
	tests := []struct {
		reg asm.PReg
		str string
	}{
		{asm.NewPReg(1, asm.RegTypeInt, asm.Width32), "w1"},
		{asm.NewPReg(1, asm.RegTypeInt, asm.Width64), "x1"},
		{asm.NewPReg(1, asm.RegTypeFloat, asm.Width32), "s1"},
		{asm.NewPReg(1, asm.RegTypeFloat, asm.Width64), "d1"},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.reg.String())
		})
	}
}

func TestNewVReg(t *testing.T) {
	reg := asm.NewVReg(1, asm.RegTypeFloat, asm.Width32)

	require.Equal(t, int32(1), reg.ID())
	require.Equal(t, asm.RegTypeFloat, reg.Type())
	require.Equal(t, asm.Width32, reg.Width())
}

func TestVReg_String(t *testing.T) {
	tests := []struct {
		reg asm.VReg
		str string
	}{
		{asm.NewVReg(1, asm.RegTypeInt, asm.Width64), "vr1"},
		{asm.NewVReg(1, asm.RegTypeFloat, asm.Width64), "vf1"},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.reg.String())
		})
	}
}

func TestNewRegMask(t *testing.T) {
	mask := asm.NewRegMask([]uint8{1, 3})

	require.True(t, mask.Contains(1))
	require.True(t, mask.Contains(3))
	require.False(t, mask.Contains(2))
	require.False(t, mask.Contains(64))
}

func TestRegMask_Set(t *testing.T) {
	require.True(t, asm.RegMask(0).Set(1).Contains(1))
	require.False(t, asm.RegMask(0).Set(64).Contains(64))
}

func TestRegMask_Clear(t *testing.T) {
	mask := asm.NewRegMask([]uint8{1})

	require.False(t, mask.Clear(1).Contains(1))
	require.True(t, mask.Clear(64).Contains(1))
}

func TestRegMask_First(t *testing.T) {
	require.Equal(t, uint8(1), asm.NewRegMask([]uint8{3, 1}).First())
	require.Equal(t, uint8(0xFF), asm.RegMask(0).First())
}

func TestNewRegInfo(t *testing.T) {
	info := asm.NewRegInfo(8, 4, []uint8{0, 1}, []uint8{2}, []uint8{7})

	require.Equal(t, uint8(8), info.NumInt)
	require.Equal(t, uint8(4), info.NumFloat)
	require.True(t, info.IntReserved.Contains(0))
	require.True(t, info.FloatReserved.Contains(2))
	require.True(t, info.Scratch.Contains(7))
}

func TestRegInfo_Allocatable(t *testing.T) {
	info := asm.NewRegInfo(4, 3, []uint8{1}, []uint8{2}, nil)

	require.Equal(t, asm.NewRegMask([]uint8{0, 2, 3}), info.Allocatable(asm.RegTypeInt))
	require.Equal(t, asm.NewRegMask([]uint8{0, 1}), info.Allocatable(asm.RegTypeFloat))
}
