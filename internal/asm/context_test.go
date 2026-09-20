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

	t.Run("starts at the aligned top of its stack", func(t *testing.T) {
		ctx, err := asm.NewContext(4096)
		require.NoError(t, err)
		require.NotZero(t, ctx.NSP)
		require.Zero(t, ctx.NSP%16)
		require.Equal(t, asm.TrapReturn, ctx.Trap)
	})
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
