package program

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestProgram_String(t *testing.T) {
	prog := New(instr.New(instr.NOP))
	require.Equal(t, "0000: nop\n", prog.String())
}
