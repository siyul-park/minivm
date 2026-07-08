package program

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestProgram_String(t *testing.T) {
	t.Run("with code", func(t *testing.T) {
		prog := New([]instr.Instruction{instr.New(instr.NOP)})
		require.Equal(t, ".code\n0000:\tnop\n", prog.String())
	})
	t.Run("empty", func(t *testing.T) {
		prog := New(nil)
		require.Equal(t, ".code\n", prog.String())
	})
	t.Run("with handlers", func(t *testing.T) {
		prog := New(nil, WithHandlers(instr.Handler{Start: 0, End: 10, Catch: 20, Depth: 1}))
		require.Contains(t, prog.String(), ".handlers")
		require.Contains(t, prog.String(), "start=0")
		require.Contains(t, prog.String(), "end=10")
		require.Contains(t, prog.String(), "catch=20")
		require.Contains(t, prog.String(), "depth=1")
	})
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

func TestWithLocals(t *testing.T) {
	prog := New(nil, WithLocals(types.TypeI32, types.TypeI64))
	require.Len(t, prog.Locals, 2)
	require.Equal(t, types.TypeI32, prog.Locals[0])
	require.Equal(t, types.TypeI64, prog.Locals[1])
}

func TestWithHandlers(t *testing.T) {
	h := instr.Handler{Start: 0, End: 10, Catch: 20, Depth: 1}
	prog := New(nil, WithHandlers(h))
	require.Len(t, prog.Handlers, 1)
	require.Equal(t, h, prog.Handlers[0])
}
