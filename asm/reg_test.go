package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPReg(t *testing.T) {
	r := NewPReg(3, RegTypeInt, Width64)
	require.Equal(t, uint8(3), r.ID())
	require.Equal(t, RegTypeInt, r.Type())
	require.Equal(t, Width64, r.Width())
}

func TestValue(t *testing.T) {
	require.Equal(t, RegTypeInt, I32(7).RegType())
	require.Equal(t, Width32, I32(7).Width())
	require.Equal(t, uint64(7), I32(7).Bits())
	require.Equal(t, "i32", I32(7).String())
	require.Equal(t, RegTypeInt, I64(9).RegType())
	require.Equal(t, Width64, I64(9).Width())
	require.Equal(t, RegTypeFloat, F32(11).RegType())
	require.Equal(t, Width32, F32(11).Width())
	require.Equal(t, RegTypeFloat, F64(13).RegType())
	require.Equal(t, Width64, F64(13).Width())
}

func TestPReg_String(t *testing.T) {
	tests := []struct {
		reg PReg
		str string
	}{
		{NewPReg(0, RegTypeInt, Width64), "x0"},
		{NewPReg(1, RegTypeInt, Width32), "w1"},
		{NewPReg(2, RegTypeFloat, Width64), "d2"},
		{NewPReg(3, RegTypeFloat, Width32), "s3"},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.reg.String())
		})
	}
}

func TestNewVReg(t *testing.T) {
	r := NewVReg(5, RegTypeFloat, Width32)
	require.Equal(t, int32(5), r.ID())
	require.Equal(t, RegTypeFloat, r.Type())
	require.Equal(t, Width32, r.Width())
}

func TestVReg_String(t *testing.T) {
	tests := []struct {
		reg VReg
		str string
	}{
		{NewVReg(0, RegTypeInt, Width64), "vr0"},
		{NewVReg(1, RegTypeFloat, Width32), "vf1"},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.reg.String())
		})
	}
}
