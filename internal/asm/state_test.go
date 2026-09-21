package asm_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/asm/arm64"
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

func TestState_Slot(t *testing.T) {
	s, err := asm.NewState(4096)
	require.NoError(t, err)
	a := asm.New(arm64.New())
	a.Emit(
		arm64.MOVI(arm64.X0, 42),
		arm64.STR(arm64.X0, arm64.SP, 0),
		arm64.LDR(arm64.X16, arm64.Ctx, int16(asm.OffsetStub)),
		arm64.BLR(arm64.X16),
	)
	code, err := a.Build()
	require.NoError(t, err)
	buffer, err := asm.NewBuffer(len(code))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, buffer.Free()) })
	address, err := asm.Link(buffer, code)
	require.NoError(t, err)
	s.SetReg(arm64.X0, 42)

	require.True(t, asm.Enter(address, &s))
	require.Equal(t, uint64(42), s.Slot(0))
}

func TestState_SetReg(t *testing.T) {
	s, err := asm.NewState(4096)
	require.NoError(t, err)

	reg := asm.NewPReg(3, asm.RegTypeInt, asm.Width64)
	s.SetReg(reg, 42)

	require.Equal(t, uint64(42), s.Reg(reg))
}
