package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewBuffer(t *testing.T) {
	t.Run("allocates a mapping", func(t *testing.T) {
		buffer, err := NewBuffer(1)
		require.NoError(t, err)
		require.NoError(t, buffer.Free())
	})

	t.Run("rejects a non-positive size", func(t *testing.T) {
		_, err := NewBuffer(0)
		require.ErrorIs(t, err, ErrInvalidSize)
	})
}

func TestBuffer_Free(t *testing.T) {
	buffer, err := NewBuffer(1)
	require.NoError(t, err)
	require.NoError(t, buffer.Free())
	require.NoError(t, buffer.Free())
}
