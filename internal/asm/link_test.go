package asm_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/stretchr/testify/require"
)

func TestLink(t *testing.T) {
	t.Run("nil buffer", func(t *testing.T) {
		_, err := asm.Link(nil, []byte{0})
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	t.Run("empty code", func(t *testing.T) {
		buffer, err := asm.NewBuffer(1)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, buffer.Free()) })

		_, err = asm.Link(buffer, nil)
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	t.Run("publishes executable address", func(t *testing.T) {
		buffer, err := asm.NewBuffer(1)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, buffer.Free()) })

		addr, err := asm.Link(buffer, []byte{0})
		require.NoError(t, err)
		require.NotZero(t, addr)

	})
}
