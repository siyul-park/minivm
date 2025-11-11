package jit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssembler_Basic(t *testing.T) {
	asm := NewAssembler()

	require.Equal(t, 0, asm.Len())

	asm.MovImm32ToReg(RAX, 42)
	require.NotEqual(t, 0, asm.Len())

	bytes := asm.Bytes()
	require.Equal(t, asm.Len(), len(bytes))
}

func TestAssembler_Reset(t *testing.T) {
	asm := NewAssembler()
	asm.MovImm32ToReg(RAX, 42)

	initialLen := asm.Len()
	require.NotEqual(t, 0, initialLen)

	asm.Reset()
	require.Equal(t, 0, asm.Len())
}

func TestAssembler_MovImm32ToReg(t *testing.T) {
	tests := []struct {
		name string
		reg  Register
		imm  int32
	}{
		{"RAX", RAX, 42},
		{"RCX", RCX, -1},
		{"R8", R8, 100},
		{"R15", R15, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asm := NewAssembler()
			asm.MovImm32ToReg(tt.reg, tt.imm)

			require.NotEqual(t, 0, asm.Len())
		})
	}
}

func TestAssembler_Arithmetic(t *testing.T) {
	asm := NewAssembler()

	asm.MovImm32ToReg(RAX, 10)
	asm.MovImm32ToReg(RCX, 5)
	asm.AddRegToReg32(RAX, RCX)
	asm.SubRegFromReg32(RAX, RCX)
	asm.ImulRegReg32(RAX, RCX)

	require.NotEqual(t, 0, asm.Len())
}

func TestAssembler_StackOps(t *testing.T) {
	asm := NewAssembler()

	asm.PushReg(RAX)
	asm.PushReg(RBX)
	asm.PopReg(RCX)
	asm.PopReg(RDX)

	require.NotEqual(t, 0, asm.Len())
}

func TestAssembler_CompleteFunction(t *testing.T) {
	asm := NewAssembler()

	asm.PushReg(RBP)
	asm.MovRegToReg32(RBP, RSP)
	asm.MovImm32ToReg(RAX, 42)
	asm.PopReg(RBP)
	asm.Ret()

	bytes := asm.Bytes()
	require.NotEmpty(t, bytes)
	require.Equal(t, byte(0xC3), bytes[len(bytes)-1])
}
