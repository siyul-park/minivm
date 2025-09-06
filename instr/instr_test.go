package instr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	instr := New(NOP)
	require.Len(t, instr, 1)
	require.Equal(t, byte(NOP), instr[0])
}

func TestInstruction_Type(t *testing.T) {
	instr := New(NOP)
	require.Equal(t, types[NOP], instr.Type())
}

func TestInstruction_Opcode(t *testing.T) {
	instr := New(NOP)
	require.Equal(t, NOP, instr.Opcode())
}

func TestInstruction_Operand(t *testing.T) {
	instr := New(NOP)
	require.Empty(t, instr.Operands())
}

func TestInstruction_String(t *testing.T) {
	instr := New(NOP)
	require.Equal(t, "nop", instr.String())
}
