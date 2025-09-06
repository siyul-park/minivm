package program

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestProgram_String(t *testing.T) {
	prog := New([]instr.Instruction{instr.New(instr.NOP)}, nil)
	require.Equal(t, ".text\nmain:\n0000:\tnop\n\n.data\n", prog.String())
}
