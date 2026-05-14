package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAlloc(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		m, err := Alloc(64)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(m), 64)
		require.NoError(t, m.Free())
	})

	t.Run("invalid size", func(t *testing.T) {
		_, err := Alloc(0)
		require.ErrorIs(t, err, ErrInvalidSize)

		_, err = Alloc(-1)
		require.ErrorIs(t, err, ErrInvalidSize)
	})
}

func TestWrite(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		m, err := Alloc(64)
		require.NoError(t, err)
		defer m.Free()

		code := []byte{0x90, 0x90, 0x90}
		err = m.Write(code)
		require.NoError(t, err)
		require.Equal(t, Memory(code), m[:len(code)])
	})

	t.Run("too large", func(t *testing.T) {
		m, err := Alloc(4)
		require.NoError(t, err)
		defer m.Free()

		err = m.Write(make([]byte, len(m)+1))
		require.ErrorIs(t, err, ErrCodeTooLarge)
	})
}

func TestExecutable(t *testing.T) {
	m, err := Alloc(64)
	require.NoError(t, err)
	defer m.Free()

	err = m.Executable()
	require.NoError(t, err)
}

func TestFree(t *testing.T) {
	m, err := Alloc(64)
	require.NoError(t, err)

	err = m.Free()
	require.NoError(t, err)
}

func TestMemory_Writable(t *testing.T) {
	m, err := Alloc(64)
	require.NoError(t, err)
	defer m.Free()

	require.NoError(t, m.Executable())
	require.NoError(t, m.Writable())

	code := []byte{0x90}
	require.NoError(t, m.Write(code))
}

func TestMemory_Ptr(t *testing.T) {
	m, err := Alloc(64)
	require.NoError(t, err)
	defer m.Free()

	require.NotNil(t, m.Ptr())
}
