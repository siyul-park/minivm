package program

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestProgram_String(t *testing.T) {
	prog := New([]instr.Instruction{instr.New(instr.NOP)}, nil)
	require.Equal(t, "0000:\tnop\n", prog.String())
}
