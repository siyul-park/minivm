package program

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestProgram_Instruction(t *testing.T) {
	prog := New(instr.New(instr.NOP))
	inst := prog.Instruction(0)
	require.NotNil(t, inst)
	require.Equal(t, instr.New(instr.NOP), inst)
}

func TestProgram_Instructions(t *testing.T) {
	prog := New(instr.New(instr.NOP))
	instrs := prog.Instructions()
	require.Len(t, instrs, 1)
	require.Equal(t, instr.New(instr.NOP), instrs[0])
}

func TestProgram_String(t *testing.T) {
	prog := New(instr.New(instr.NOP))
	require.Equal(t, "0000: nop\n", prog.String())
}
