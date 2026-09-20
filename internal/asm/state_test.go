package asm_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/stretchr/testify/require"
)

func TestNewState(t *testing.T) {
	t.Run("rejects an empty stack", func(t *testing.T) {
		_, err := asm.NewState(0)
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	t.Run("starts with an empty register file", func(t *testing.T) {
		s, err := asm.NewState(4096)
		require.NoError(t, err)
		require.Zero(t, s.Reg(asm.NewPReg(0, asm.RegTypeInt, asm.Width64)))
	})
}

func TestState_Reg(t *testing.T) {
	s, err := asm.NewState(4096)
	require.NoError(t, err)

	intReg := asm.NewPReg(3, asm.RegTypeInt, asm.Width64)
	floatReg := asm.NewPReg(3, asm.RegTypeFloat, asm.Width64)
	s.SetReg(intReg, 42)
	s.SetReg(floatReg, 84)

	require.Equal(t, uint64(42), s.Reg(intReg))
	require.Equal(t, uint64(84), s.Reg(floatReg))
}

func TestState_SetReg(t *testing.T) {
	s, err := asm.NewState(4096)
	require.NoError(t, err)

	reg := asm.NewPReg(3, asm.RegTypeInt, asm.Width64)
	s.SetReg(reg, 42)

	require.Equal(t, uint64(42), s.Reg(reg))
}
