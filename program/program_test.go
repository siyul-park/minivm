package program

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestProgram_String(t *testing.T) {
	prog := New([]instr.Instruction{instr.New(instr.NOP)})
	require.Equal(t, "0000:\tnop\n", prog.String())
}

func TestWithConstants(t *testing.T) {
	prog := New(nil, WithConstants(types.I32(1), types.I64(2)))
	require.Len(t, prog.Constants, 2)
	require.Equal(t, types.I32(1), prog.Constants[0])
	require.Equal(t, types.I64(2), prog.Constants[1])
}

func TestWithTypes(t *testing.T) {
	prog := New(nil, WithTypes(types.TypeI32, types.TypeI64))
	require.Len(t, prog.Types, 2)
	require.Equal(t, types.TypeI32, prog.Types[0])
	require.Equal(t, types.TypeI64, prog.Types[1])
}
