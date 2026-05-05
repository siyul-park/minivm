package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAlloc(t *testing.T) {
	m, err := Alloc(64)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(m), 64)
	require.NoError(t, m.Free())
}

func TestWrite(t *testing.T) {
	m, err := Alloc(64)
	require.NoError(t, err)
	defer m.Free()

	code := []byte{0x90, 0x90, 0x90}
	err = m.Write(code)
	require.NoError(t, err)
	require.Equal(t, Memory(code), m[:len(code)])
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
