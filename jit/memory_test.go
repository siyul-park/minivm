package jit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllocateExecutable(t *testing.T) {
	mem, err := AllocateExecutable(1024)
	require.NoError(t, err)
	defer mem.Free()

	require.NotNil(t, mem.Data)
	require.Len(t, mem.Data, 1024)
}

func TestAllocateExecutable_InvalidSize(t *testing.T) {
	_, err := AllocateExecutable(0)
	require.Error(t, err)

	_, err = AllocateExecutable(-1)
	require.Error(t, err)
}

func TestExecutableMemory_Execute_Simple(t *testing.T) {
	asm := NewAssembler()
	asm.MovImm32ToReg(RAX, 42)
	asm.Ret()

	mem, err := AllocateExecutable(len(asm.Bytes()))
	require.NoError(t, err)
	defer mem.Free()

	copy(mem.Data, asm.Bytes())

	result, err := mem.Execute()
	require.NoError(t, err)
	require.Equal(t, uint64(42), result)
}

func TestExecutableMemory_Execute_Addition(t *testing.T) {
	asm := NewAssembler()
	asm.MovImm32ToReg(RAX, 10)
	asm.MovImm32ToReg(RCX, 32)
	asm.AddRegToReg32(RAX, RCX)
	asm.Ret()

	mem, err := AllocateExecutable(len(asm.Bytes()))
	require.NoError(t, err)
	defer mem.Free()

	copy(mem.Data, asm.Bytes())

	result, err := mem.Execute()
	require.NoError(t, err)
	require.Equal(t, int32(42), int32(result))
}

func TestExecutableMemory_Execute_Multiplication(t *testing.T) {
	asm := NewAssembler()
	asm.MovImm32ToReg(RAX, 6)
	asm.MovImm32ToReg(RCX, 7)
	asm.ImulRegReg32(RAX, RCX)
	asm.Ret()

	mem, err := AllocateExecutable(len(asm.Bytes()))
	require.NoError(t, err)
	defer mem.Free()

	copy(mem.Data, asm.Bytes())

	result, err := mem.Execute()
	require.NoError(t, err)
	require.Equal(t, int32(42), int32(result))
}

func TestExecutableMemory_Free(t *testing.T) {
	mem, err := AllocateExecutable(1024)
	require.NoError(t, err)

	err = mem.Free()
	require.NoError(t, err)
	require.Nil(t, mem.Data)

	err = mem.Free()
	require.NoError(t, err)
}
