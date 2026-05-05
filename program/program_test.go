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

func TestProgram_WithConstants(t *testing.T) {
	c0 := types.I32(42)
	c1 := types.F64(3.14)
	prog := New([]instr.Instruction{instr.New(instr.NOP)}, WithConstants(c0, c1))
	require.Equal(t, []types.Value{c0, c1}, prog.Constants)
}

func TestProgram_WithTypes(t *testing.T) {
	ft := &types.FunctionType{
		Params:  []types.Type{types.TypeI32},
		Returns: []types.Type{types.TypeI64},
	}
	prog := New([]instr.Instruction{instr.New(instr.NOP)}, WithTypes(ft))
	require.Equal(t, []types.Type{ft}, prog.Types)
}

func TestProgram_String_WithConstants(t *testing.T) {
	prog := New(
		[]instr.Instruction{instr.New(instr.NOP)},
		WithConstants(types.I32(1)),
	)
	s := prog.String()
	require.Contains(t, s, "0000:\t1")
}

func TestProgram_String_WithTypes(t *testing.T) {
	ft := &types.FunctionType{}
	prog := New(
		[]instr.Instruction{instr.New(instr.NOP)},
		WithTypes(ft),
	)
	s := prog.String()
	require.Contains(t, s, "func()")
}
