package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewCodeBuffer(t *testing.T) {
	b, err := NewBuffer(64)
	require.NoError(t, err)
	require.NotNil(t, b)
	require.NoError(t, b.Free())
}

func TestBuffer_Append(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		b, err := NewBuffer(64)
		require.NoError(t, err)
		defer b.Free()

		chunk, err := b.Append([]byte{0x90, 0x90, 0x90})
		require.NoError(t, err)
		require.NotNil(t, chunk)
	})
	t.Run("when sealed", func(t *testing.T) {
		b, err := NewBuffer(64)
		require.NoError(t, err)
		defer b.Free()

		require.NoError(t, b.Seal())
		_, err = b.Append([]byte{0x90})
		require.ErrorIs(t, err, ErrBufferSealed)
	})
}

func TestBuffer_Seal(t *testing.T) {
	b, err := NewBuffer(64)
	require.NoError(t, err)
	defer b.Free()

	err = b.Seal()
	require.NoError(t, err)
}

func TestBuffer_Unseal(t *testing.T) {
	b, err := NewBuffer(64)
	require.NoError(t, err)
	defer b.Free()

	err = b.Seal()
	require.NoError(t, err)

	err = b.Unseal()
	require.NoError(t, err)
}

func TestBuffer_Sealed(t *testing.T) {
	b, err := NewBuffer(64)
	require.NoError(t, err)
	defer b.Free()

	require.False(t, b.Sealed())

	require.NoError(t, b.Seal())
	require.True(t, b.Sealed())

	require.NoError(t, b.Unseal())
	require.False(t, b.Sealed())
}

func TestChunk_Ptr(t *testing.T) {
	b, err := NewBuffer(64)
	require.NoError(t, err)
	defer b.Free()

	chunk, err := b.Append([]byte{0x90, 0x91})
	require.NoError(t, err)
	require.NotNil(t, chunk.Ptr())
}

func TestChunk_At(t *testing.T) {
	b, err := NewBuffer(64)
	require.NoError(t, err)
	defer b.Free()

	chunk, err := b.Append([]byte{0x90, 0x91, 0x92})
	require.NoError(t, err)

	t.Run("subchunk", func(t *testing.T) {
		sub, err := chunk.Slice(1)
		require.NoError(t, err)
		require.Equal(t, 2, sub.Size())
		require.NotEqual(t, chunk.Ptr(), sub.Ptr())
	})

	t.Run("invalid offset", func(t *testing.T) {
		_, err := chunk.Slice(4)
		require.ErrorIs(t, err, ErrInvalidArgs)
	})
}
