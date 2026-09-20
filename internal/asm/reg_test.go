package asm_test

import (
	"testing"

	asm "github.com/siyul-park/minivm/internal/asm"
	"github.com/stretchr/testify/require"
)

func TestNewPReg(t *testing.T) {
	reg := asm.NewPReg(1, asm.RegTypeFloat, asm.Width64)

	require.Equal(t, uint8(1), reg.ID())
	require.Equal(t, asm.RegTypeFloat, reg.Type())
	require.Equal(t, asm.Width64, reg.Width())
}

func TestNewVReg(t *testing.T) {
	reg := asm.NewVReg(1, asm.RegTypeFloat, asm.Width32)

	require.Equal(t, int32(1), reg.ID())
	require.Equal(t, asm.RegTypeFloat, reg.Type())
	require.Equal(t, asm.Width32, reg.Width())
}

func TestPReg_ID(t *testing.T) {
	require.Equal(t, uint8(3), asm.NewPReg(3, asm.RegTypeInt, asm.Width64).ID())
}

func TestPReg_Type(t *testing.T) {
	require.Equal(t, asm.RegTypeFloat, asm.NewPReg(3, asm.RegTypeFloat, asm.Width64).Type())
}

func TestPReg_Width(t *testing.T) {
	require.Equal(t, asm.Width32, asm.NewPReg(3, asm.RegTypeInt, asm.Width32).Width())
}

func TestPReg_String(t *testing.T) {
	tests := []struct {
		reg asm.PReg
		str string
	}{
		{asm.NewPReg(1, asm.RegTypeInt, asm.Width32), "w1"},
		{asm.NewPReg(1, asm.RegTypeInt, asm.Width64), "x1"},
		{asm.NewPReg(1, asm.RegTypeFloat, asm.Width32), "s1"},
		{asm.NewPReg(1, asm.RegTypeFloat, asm.Width64), "d1"}}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.reg.String())
		})
	}
}

func TestVReg_ID(t *testing.T) {
	require.Equal(t, int32(3), asm.NewVReg(3, asm.RegTypeInt, asm.Width64).ID())
}

func TestVReg_Type(t *testing.T) {
	require.Equal(t, asm.RegTypeFloat, asm.NewVReg(3, asm.RegTypeFloat, asm.Width64).Type())
}

func TestVReg_Width(t *testing.T) {
	require.Equal(t, asm.Width32, asm.NewVReg(3, asm.RegTypeInt, asm.Width32).Width())
}

func TestVReg_String(t *testing.T) {
	tests := []struct {
		reg asm.VReg
		str string
	}{
		{asm.NewVReg(1, asm.RegTypeInt, asm.Width64), "vr1"},
		{asm.NewVReg(1, asm.RegTypeFloat, asm.Width64), "vf1"}}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.reg.String())
		})
	}
}
