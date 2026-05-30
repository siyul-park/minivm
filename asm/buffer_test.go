package asm_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
)

func TestBuffer_Write(t *testing.T) {
	t.Run("appends bytes and returns stable pointer", func(t *testing.T) {
		b, err := asm.NewBuffer(64)
		require.NoError(t, err)
		defer b.Free()

		p1, err := b.Write([]byte{0x01, 0x02, 0x03, 0x04})
		require.NoError(t, err)
		require.NotNil(t, p1)

		p2, err := b.Write([]byte{0x05, 0x06})
		require.NoError(t, err)
		require.NotEqual(t, p1, p2)
	})

	t.Run("invalid size", func(t *testing.T) {
		_, err := asm.NewBuffer(0)
		require.ErrorIs(t, err, asm.ErrInvalidSize)
	})
}
