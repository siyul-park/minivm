package jit_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/stretchr/testify/require"
)

func TestNewContext(t *testing.T) {
	t.Run("rejects an empty stack", func(t *testing.T) {
		_, err := jit.NewContext(0)
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	t.Run("starts with no exit taken", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		require.Zero(t, ctx.Exit())
	})
}

func TestTrap_String(t *testing.T) {
	tests := []struct {
		trap jit.Trap
		want string
	}{
		{jit.TrapReturn, "return"},
		{jit.TrapDeopt, "deopt"},
		{jit.TrapBridge, "bridge"},
		{jit.Trap(99), "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, tt.trap.String())
		})
	}
}
