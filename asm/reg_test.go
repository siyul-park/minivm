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

func TestNewVReg(t *testing.T) {
	reg := NewVReg(1, RegTypeFloat, Width64)

	require.Equal(t, int32(1), reg.ID())
	require.Equal(t, RegTypeFloat, reg.Type())
	require.Equal(t, Width64, reg.Width())
}

func TestCompatible(t *testing.T) {
	require.True(t, Compatible(NewVReg(0, RegTypeInt, Width64), NewPReg(0, RegTypeInt, Width64)))
	require.False(t, Compatible(NewVReg(0, RegTypeInt, Width64), NewPReg(0, RegTypeFloat, Width64)))
	require.False(t, Compatible(NewVReg(0, RegTypeInt, Width64), NewPReg(0, RegTypeInt, Width32)))
}

func TestCompatibles(t *testing.T) {
	require.True(t, Compatibles(
		[]VReg{NewVReg(0, RegTypeInt, Width64), NewVReg(1, RegTypeFloat, Width32)},
		[]PReg{NewPReg(0, RegTypeInt, Width64), NewPReg(1, RegTypeFloat, Width32)},
	))
	require.False(t, Compatibles([]VReg{NewVReg(0, RegTypeInt, Width64)}, []PReg(nil)))
	require.False(t, Compatibles([]VReg{NewVReg(0, RegTypeInt, Width64)}, []PReg{NewPReg(0, RegTypeInt, Width32)}))
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
