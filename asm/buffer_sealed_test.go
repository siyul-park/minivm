package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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

func TestBuffer_Append_WhenSealed(t *testing.T) {
	b, err := NewBuffer(64)
	require.NoError(t, err)
	defer b.Free()

	require.NoError(t, b.Seal())
	_, err = b.Append([]byte{0x90})
	require.ErrorIs(t, err, ErrBufferSealed)
}

func TestBuffer_Grow(t *testing.T) {
	b, err := NewBuffer(4)
	require.NoError(t, err)
	defer b.Free()

	// Write more than initial size to trigger grow
	data := make([]byte, 8)
	_, err = b.Append(data)
	require.NoError(t, err)
}

func TestChunk_Ptr(t *testing.T) {
	b, err := NewBuffer(64)
	require.NoError(t, err)
	defer b.Free()

	chunk, err := b.Append([]byte{0x01, 0x02})
	require.NoError(t, err)
	require.NotNil(t, chunk.Ptr())
}

func TestMemory_Ptr(t *testing.T) {
	m, err := Alloc(64)
	require.NoError(t, err)
	defer m.Free()

	ptr := m.Ptr()
	require.NotNil(t, ptr)
}
