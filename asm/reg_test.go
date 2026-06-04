package asm

import (
	"fmt"
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

func TestI32(t *testing.T) {
	v := I32(1)

	require.Equal(t, RegTypeInt, v.RegType())
	require.Equal(t, Width32, v.Width())
	require.Equal(t, uint64(1), v.Bits())
}

func TestI64(t *testing.T) {
	v := I64(1)

	require.Equal(t, RegTypeInt, v.RegType())
	require.Equal(t, Width64, v.Width())
	require.Equal(t, uint64(1), v.Bits())
}

func TestF32(t *testing.T) {
	v := F32(1)

	require.Equal(t, RegTypeFloat, v.RegType())
	require.Equal(t, Width32, v.Width())
	require.Equal(t, uint64(1), v.Bits())
}

func TestF64(t *testing.T) {
	v := F64(1)

	require.Equal(t, RegTypeFloat, v.RegType())
	require.Equal(t, Width64, v.Width())
	require.Equal(t, uint64(1), v.Bits())
}

func TestValue_Valid(t *testing.T) {
	require.True(t, I64(1).Valid())
	require.False(t, Value{}.Valid())
}

func TestValue_String(t *testing.T) {
	tests := []struct {
		val Value
		str string
	}{
		{I32(1), "i32"},
		{I64(1), "i64"},
		{F32(1), "f32"},
		{F64(1), "f64"},
		{Value{}, "<invalid>"},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.val.String())
		})
	}
}

func TestValue_GoString(t *testing.T) {
	tests := []struct {
		val Value
		str string
	}{
		{I32(1), "I32(1)"},
		{I64(1), "I64(1)"},
		{F32(1), "F32(00000001)"},
		{F64(1), "F64(0000000000000001)"},
		{Value{}, "<invalid>"},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, fmt.Sprintf("%#v", tt.val))
		})
	}
}
