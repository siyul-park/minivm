package program

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewBuilder(t *testing.T) {
	prog, err := NewBuilder().Build()
	require.NoError(t, err)
	require.Empty(t, prog.Code)
}

func TestBuilder_Emit(t *testing.T) {
	b := NewBuilder()
	require.Same(t, b, b.Emit(instr.I32_CONST, 42).Emit(instr.DROP))
	prog, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, []instr.Instruction{instr.New(instr.I32_CONST, 42), instr.New(instr.DROP)}, instr.Unmarshal(prog.Code))
}

func TestBuilder_Label(t *testing.T) {
	b := NewBuilder()
	require.NotEqual(t, b.Label(), b.Label())
}

func TestBuilder_Bind(t *testing.T) {
	b := NewBuilder()
	end := b.Label()
	b.Br(end)
	_, err := b.Build()
	require.ErrorIs(t, err, instr.ErrUnboundLabel)

	require.Same(t, b, b.Bind(end))
	prog, err := b.Build()
	require.NoError(t, err)
	require.NoError(t, Verify(prog))
}

func TestBuilder_Br(t *testing.T) {
	b := NewBuilder()
	end := b.Label()
	require.Same(t, b, b.Br(end))
	prog, err := b.Emit(instr.NOP).Bind(end).Build()
	require.NoError(t, err)
	require.NoError(t, Verify(prog))
}

func TestBuilder_BrIf(t *testing.T) {
	b := NewBuilder()
	end := b.Label()
	b.Emit(instr.I32_CONST, 1)
	require.Same(t, b, b.BrIf(end))
	prog, err := b.Emit(instr.NOP).Bind(end).Build()
	require.NoError(t, err)
	require.NoError(t, Verify(prog))
}

func TestBuilder_BrTable(t *testing.T) {
	b := NewBuilder()
	first, def := b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0)
	require.Same(t, b, b.BrTable(def, first))
	prog, err := b.Bind(first).Emit(instr.NOP).Bind(def).Build()
	require.NoError(t, err)
	require.NoError(t, Verify(prog))
}

func TestBuilder_Try(t *testing.T) {
	b := NewBuilder()
	start, end, catch := b.Label(), b.Label(), b.Label()
	require.Same(t, b, b.Bind(start).Emit(instr.NOP).Bind(end).Emit(instr.RETURN).Bind(catch).Try(start, end, catch, 2))
	prog, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, []instr.Handler{{Start: 0, End: 1, Catch: 2, Depth: 2}}, prog.Handlers)
}

func TestBuilder_ConstGet(t *testing.T) {
	b := NewBuilder()
	b.ConstGet(types.String("x")).ConstGet(types.String("x"))

	prog, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, []types.Value{types.String("x")}, prog.Constants)

	instrs := instr.Unmarshal(prog.Code)
	require.Equal(t, instr.CONST_GET, instrs[0].Opcode())
	require.Equal(t, uint64(0), instrs[0].Operand(0))
	require.Equal(t, uint64(0), instrs[1].Operand(0))
}

func TestBuilder_Const(t *testing.T) {
	t.Run("reuses comparable values", func(t *testing.T) {
		b := NewBuilder()

		require.Equal(t, 0, b.Const(types.String("a")))
		require.Equal(t, 1, b.Const(types.String("b")))
		require.Equal(t, 0, b.Const(types.String("a")))
	})

	t.Run("rejects nil", func(t *testing.T) {
		require.Equal(t, -1, NewBuilder().Const(nil))
	})

	t.Run("uses pointer identity", func(t *testing.T) {
		b := NewBuilder()
		first := &types.Function{}
		second := &types.Function{}

		require.Equal(t, 0, b.Const(first))
		require.Equal(t, 0, b.Const(first))
		require.Equal(t, 1, b.Const(second))
	})
}

func TestBuilder_Type(t *testing.T) {
	b := NewBuilder()

	require.Equal(t, -1, b.Type(nil))
	require.Equal(t, 0, b.Type(types.TypeI32))
	require.Equal(t, 1, b.Type(types.NewArrayType(types.TypeI32)))
	require.Equal(t, 0, b.Type(types.TypeI32))
}

func TestBuilder_Locals(t *testing.T) {
	b := NewBuilder()
	b.Locals(types.TypeI32, types.TypeI64).Emit(instr.LOCAL_GET, 0).Emit(instr.DROP)

	prog, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, []types.Type{types.TypeI32, types.TypeI64}, prog.Locals)
}

func TestBuilder_Globals(t *testing.T) {
	b := NewBuilder()
	b.Globals(types.TypeI32, types.NewArrayType(types.TypeF64)).Emit(instr.GLOBAL_GET, 0).Emit(instr.DROP)

	prog, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, []types.Type{types.TypeI32, types.NewArrayType(types.TypeF64)}, prog.Globals)
}

func TestBuilder_Build(t *testing.T) {
	t.Run("assembles code and pools", func(t *testing.T) {
		b := NewBuilder()
		skip := b.Label()
		b.Emit(instr.I32_CONST, 1).
			BrIf(skip).
			ConstGet(types.String("x")).
			Emit(instr.DROP).
			Bind(skip).
			Emit(instr.NOP)

		prog, err := b.Build()
		require.NoError(t, err)
		require.Equal(t, []types.Value{types.String("x")}, prog.Constants)
		require.NoError(t, Verify(prog))
	})

	t.Run("unbound label", func(t *testing.T) {
		b := NewBuilder()
		b.Br(b.Label())

		_, err := b.Build()
		require.ErrorIs(t, err, instr.ErrUnboundLabel)
	})
}
