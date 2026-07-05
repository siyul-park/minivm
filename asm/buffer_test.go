package asm

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestBuffer_Write(t *testing.T) {
	t.Run("appends bytes and returns stable pointer", func(t *testing.T) {
		b, err := NewBuffer(64)
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
		_, err := NewBuffer(0)
		require.ErrorIs(t, err, ErrInvalidSize)
	})

	t.Run("resets offset after grow", func(t *testing.T) {
		b, err := NewBuffer(1)
		require.NoError(t, err)
		defer b.Free()

		_, err = b.Write(make([]byte, len(b.mem)))
		require.NoError(t, err)
		require.Equal(t, len(b.mem), b.offset)

		_, err = b.Write([]byte{0x01, 0x02, 0x03})
		require.NoError(t, err)
		require.Len(t, b.old, 1)
		require.Equal(t, 3, b.offset)
	})

	t.Run("patches batch writes without leaving the buffer writable", func(t *testing.T) {
		b, err := NewBuffer(64)
		require.NoError(t, err)
		defer b.Free()

		bases, err := b.writeBatch([][]byte{{0x01}, {0x02, 0x03}}, func(bases []unsafe.Pointer) error {
			_, err := b.patch(bases[0], []byte{0x09}, true)
			require.NoError(t, err)
			require.False(t, b.sealed)
			return nil
		})
		require.NoError(t, err)
		require.True(t, b.sealed)
		require.Len(t, bases, 2)
		require.Equal(t, byte(0x09), (*[1]byte)(bases[0])[0])
	})

	t.Run("seals after batch patch error", func(t *testing.T) {
		b, err := NewBuffer(64)
		require.NoError(t, err)
		defer b.Free()

		_, err = b.writeBatch([][]byte{{0x01}, {0x02, 0x03}}, func([]unsafe.Pointer) error {
			require.False(t, b.sealed)
			return ErrInvalidArgs
		})
		require.ErrorIs(t, err, ErrInvalidArgs)
		require.True(t, b.sealed)
	})
}
