package asm_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/stretchr/testify/require"
)

func TestNewContext(t *testing.T) {
	t.Run("rejects an empty stack", func(t *testing.T) {
		_, err := asm.NewContext(0)
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	t.Run("initializes an execution state", func(t *testing.T) {
		ctx, err := asm.NewContext(4096)
		require.NoError(t, err)
		require.Zero(t, ctx.Exit())
	})
}

func TestContext_Reg(t *testing.T) {
	ctx, err := asm.NewContext(4096)
	require.NoError(t, err)

	intReg := asm.NewPReg(3, asm.RegTypeInt, asm.Width64)
	floatReg := asm.NewPReg(4, asm.RegTypeFloat, asm.Width64)
	ctx.SetReg(intReg, 42)
	ctx.SetReg(floatReg, 84)

	require.Equal(t, uint64(42), ctx.Reg(intReg))
	require.Equal(t, uint64(84), ctx.Reg(floatReg))
}

func TestContext_SetReg(t *testing.T) {
	ctx, err := asm.NewContext(4096)
	require.NoError(t, err)

	reg := asm.NewPReg(3, asm.RegTypeInt, asm.Width64)
	ctx.SetReg(reg, 42)

	require.Equal(t, uint64(42), ctx.Reg(reg))
}

func TestTrap_String(t *testing.T) {
	tests := []struct {
		trap asm.Trap
		want string
	}{
		{asm.TrapReturn, "return"},
		{asm.TrapDeopt, "deopt"},
		{asm.TrapBridge, "bridge"},
		{asm.Trap(99), "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, tt.trap.String())
		})
	}
}
