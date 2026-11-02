package program_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	program "github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewBuilder(t *testing.T) {
	prog, err := program.NewBuilder().Build()
	require.NoError(t, err)
	require.Empty(t, prog.Code)
}

func TestBuilder_Label(t *testing.T) {
	b := program.NewBuilder()
	require.NotEqual(t, b.Label(), b.Label())
}

func TestBuilder_Bind(t *testing.T) {
	b := program.NewBuilder()
	end := b.Label()
	b.Br(end)
	_, err := b.Build()
	require.ErrorIs(t, err, instr.ErrUnboundLabel)

	require.Same(t, b, b.Bind(end))
	prog, err := b.Build()
	require.NoError(t, err)
	require.NoError(t, program.Verify(prog))
}

func TestBuilder_Br(t *testing.T) {
	b := program.NewBuilder()
	end := b.Label()
	require.Same(t, b, b.Br(end))
	prog, err := b.Emit(instr.NOP).Bind(end).Build()
	require.NoError(t, err)
	require.NoError(t, program.Verify(prog))
}

func TestBuilder_BrIf(t *testing.T) {
	b := program.NewBuilder()
	end := b.Label()
	b.Emit(instr.I32_CONST, 1)
	require.Same(t, b, b.BrIf(end))
	prog, err := b.Emit(instr.NOP).Bind(end).Build()
	require.NoError(t, err)
	require.NoError(t, program.Verify(prog))
}

func TestBuilder_BrTable(t *testing.T) {
	b := program.NewBuilder()
	first, def := b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0)
	require.Same(t, b, b.BrTable(def, first))
	prog, err := b.Bind(first).Emit(instr.NOP).Bind(def).Build()
	require.NoError(t, err)
	require.NoError(t, program.Verify(prog))
}

func TestBuilder_Try(t *testing.T) {
	b := program.NewBuilder()
	start, end, catch := b.Label(), b.Label(), b.Label()
	require.Same(t, b, b.Bind(start).Emit(instr.NOP).Bind(end).Emit(instr.RETURN).Bind(catch).Try(start, end, catch, 2))
	prog, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, []instr.Handler{{Start: 0, End: 1, Catch: 2, Depth: 2}}, prog.Handlers)
}

func TestBuilder_ConstGet(t *testing.T) {
	t.Run("interns constant", func(t *testing.T) {
		b := program.NewBuilder()
		b.ConstGet(types.String("x")).ConstGet(types.String("x"))

		prog, err := b.Build()
		require.NoError(t, err)
		require.Equal(t, []types.Value{types.String("x")}, prog.Constants)

		instrs := instr.Unmarshal(prog.Code)
		require.Equal(t, instr.CONST_GET, instrs[0].Opcode())
		require.Equal(t, uint64(0), instrs[0].Operand(0))
		require.Equal(t, uint64(0), instrs[1].Operand(0))
	})

	t.Run("nil value", func(t *testing.T) {
		prog, err := program.NewBuilder().ConstGet(nil).Build()
		require.ErrorIs(t, err, program.ErrNilValue)
		require.Nil(t, prog)
	})
}

func TestBuilder_Emit(t *testing.T) {
	t.Run("encodes instructions", func(t *testing.T) {
		b := program.NewBuilder()
		require.Same(t, b, b.Emit(instr.I32_CONST, 42).Emit(instr.DROP))
		prog, err := b.Build()
		require.NoError(t, err)
		require.Equal(t, []instr.Instruction{instr.MustNew(instr.I32_CONST, 42), instr.MustNew(instr.DROP)}, instr.Unmarshal(prog.Code))
	})

	t.Run("invalid operands", func(t *testing.T) {
		prog, err := program.NewBuilder().Emit(instr.I32_CONST).Build()
		require.ErrorIs(t, err, instr.ErrOperandCount)
		require.Nil(t, prog)
	})
}

func TestBuilder_Append(t *testing.T) {
	t.Run("appends instructions", func(t *testing.T) {
		b := program.NewBuilder()
		require.Same(t, b, b.Append(instr.MustNew(instr.I32_CONST, 42), instr.MustNew(instr.DROP)))
		prog, err := b.Build()
		require.NoError(t, err)
		require.Equal(t, []instr.Instruction{instr.MustNew(instr.I32_CONST, 42), instr.MustNew(instr.DROP)}, instr.Unmarshal(prog.Code))
	})

	t.Run("nil instruction", func(t *testing.T) {
		_, err := program.NewBuilder().Append(nil).Build()
		require.ErrorIs(t, err, instr.ErrInvalidInstruction)
	})
}

func TestBuilder_Const(t *testing.T) {
	t.Run("reuses comparable values", func(t *testing.T) {
		b := program.NewBuilder()

		require.Equal(t, 0, b.Const(types.String("a")))
		require.Equal(t, 1, b.Const(types.String("b")))
		require.Equal(t, 0, b.Const(types.String("a")))
	})

	t.Run("rejects nil", func(t *testing.T) {
		b := program.NewBuilder()
		require.Equal(t, -1, b.Const(nil))

		_, err := b.Build()
		require.ErrorIs(t, err, program.ErrNilValue)
	})

	t.Run("uses pointer identity", func(t *testing.T) {
		b := program.NewBuilder()
		first := &types.Function{}
		second := &types.Function{}

		require.Equal(t, 0, b.Const(first))
		require.Equal(t, 0, b.Const(first))
		require.Equal(t, 1, b.Const(second))
	})
}

func TestBuilder_Locals(t *testing.T) {
	b := program.NewBuilder()
	b.Locals(types.TypeI32(), types.TypeI64()).Emit(instr.LOCAL_GET, 0).Emit(instr.DROP)

	prog, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, []types.Type{types.TypeI32(), types.TypeI64()}, prog.Locals)
}

func TestBuilder_Globals(t *testing.T) {
	b := program.NewBuilder()
	b.Globals(types.TypeI32(), types.NewArrayType(types.TypeF64())).Emit(instr.GLOBAL_GET, 0).Emit(instr.DROP)

	prog, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, []types.Type{types.TypeI32(), types.NewArrayType(types.TypeF64())}, prog.Globals)
}

func TestBuilder_MustBuild(t *testing.T) {
	t.Run("valid program", func(t *testing.T) {
		prog := program.NewBuilder().Emit(instr.NOP).MustBuild()
		require.Equal(t, []instr.Instruction{instr.MustNew(instr.NOP)}, instr.Unmarshal(prog.Code))
	})

	t.Run("invalid program", func(t *testing.T) {
		require.Panics(t, func() { program.NewBuilder().Emit(instr.I32_CONST).MustBuild() })
	})
}

func TestBuilder_Build(t *testing.T) {
	t.Run("assembles code and pools", func(t *testing.T) {
		b := program.NewBuilder()
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
		require.NoError(t, program.Verify(prog))
	})

	t.Run("unbound label", func(t *testing.T) {
		b := program.NewBuilder()
		b.Br(b.Label())

		_, err := b.Build()
		require.ErrorIs(t, err, instr.ErrUnboundLabel)
	})

	t.Run("foreign label", func(t *testing.T) {
		_, err := program.NewBuilder().Br(program.NewBuilder().Label()).Build()
		require.ErrorIs(t, err, instr.ErrForeignLabel)
	})
}

func TestBuilder_Type(t *testing.T) {
	t.Run("interns type", func(t *testing.T) {
		b := program.NewBuilder()

		require.Equal(t, 0, b.Type(types.TypeI32()))
		require.Equal(t, 1, b.Type(types.NewArrayType(types.TypeI32())))
		require.Equal(t, 0, b.Type(types.TypeI32()))
	})

	t.Run("rejects nil", func(t *testing.T) {
		b := program.NewBuilder()
		require.Equal(t, -1, b.Type(nil))

		_, err := b.Build()
		require.ErrorIs(t, err, program.ErrNilValue)
	})
}
