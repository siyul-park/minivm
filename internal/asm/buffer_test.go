package asm_test

import (
	"testing"

	asm "github.com/siyul-park/minivm/internal/asm"
	"github.com/stretchr/testify/require"
)

func TestNewBuffer(t *testing.T) {
	t.Run("allocates a mapping", func(t *testing.T) {
		buffer, err := asm.NewBuffer(1)
		require.NoError(t, err)
		require.NoError(t, buffer.Free())
	})

	t.Run("rejects a non-positive size", func(t *testing.T) {
		_, err := asm.NewBuffer(0)
		require.ErrorIs(t, err, asm.ErrInvalidSize)
	})
}

func TestBuffer_Free(t *testing.T) {
	buffer, err := asm.NewBuffer(1)
	require.NoError(t, err)
	require.NoError(t, buffer.Free())
	require.NoError(t, buffer.Free())
}
