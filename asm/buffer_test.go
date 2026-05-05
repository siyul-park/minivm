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
	b, err := NewBuffer(64)
	require.NoError(t, err)
	defer b.Free()

	chunk, err := b.Append([]byte{0x90, 0x90, 0x90})
	require.NoError(t, err)
	require.NotNil(t, chunk)
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
