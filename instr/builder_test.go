package instr_test

import (
	"math"
	"testing"

	instr "github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestNewBuilder(t *testing.T) {
	b := instr.NewBuilder()
	require.NotNil(t, b)

	instrs, err := b.Assemble()
	require.NoError(t, err)
	require.Empty(t, instrs)
	require.Nil(t, b.Handlers())
}

func TestBuilder_Label(t *testing.T) {
	b := instr.NewBuilder()
	first := b.Label()
	second := b.Label()
	require.NotEqual(t, first, second)
}

func TestBuilder_Bind(t *testing.T) {
	b := instr.NewBuilder()
	start, end := b.Label(), b.Label()
	require.Same(t, b, b.Bind(start).Emit(instr.NOP).Emit(instr.NOP).Bind(end))

	b.Try(start, end, end, 0)

	_, err := b.Assemble()
	require.NoError(t, err)
	require.Equal(t, []instr.Handler{{Start: 0, End: 2, Catch: 2, Depth: 0}}, b.Handlers())
}

func TestBuilder_Emit(t *testing.T) {
	b := instr.NewBuilder()
	b.Emit(instr.I32_CONST, 42)

	instrs, err := b.Assemble()
	require.NoError(t, err)
	require.Equal(t, instr.I32_CONST, instrs[0].Opcode())
	require.Equal(t, uint64(42), instrs[0].Operand(0))
}

func TestBuilder_Append(t *testing.T) {
	b := instr.NewBuilder()
	b.Append(instr.New(instr.NOP), instr.New(instr.RETURN))

	instrs, err := b.Assemble()
	require.NoError(t, err)
	require.Equal(t, instr.NOP, instrs[0].Opcode())
	require.Equal(t, instr.RETURN, instrs[1].Opcode())
}

func TestBuilder_Br(t *testing.T) {
	t.Run("backward", func(t *testing.T) {
		b := instr.NewBuilder()
		loop := b.Label()
		b.Bind(loop).Emit(instr.NOP).Emit(instr.NOP).Br(loop)

		instrs, err := b.Assemble()
		require.NoError(t, err)
		require.Equal(t, instr.BR, instrs[2].Opcode())
		require.Equal(t, -5, instr.ParseI16(instrs[2], 1))
	})

	t.Run("forward", func(t *testing.T) {
		b := instr.NewBuilder()
		end := b.Label()
		b.Br(end).Emit(instr.NOP).Bind(end).Emit(instr.RETURN)

		instrs, err := b.Assemble()
		require.NoError(t, err)
		require.Equal(t, 1, instr.ParseI16(instrs[0], 1))
	})
}

func TestBuilder_BrIf(t *testing.T) {
	b := instr.NewBuilder()
	end := b.Label()
	b.BrIf(end).Emit(instr.NOP).Bind(end).Emit(instr.RETURN)

	instrs, err := b.Assemble()
	require.NoError(t, err)
	require.Equal(t, instr.BR_IF, instrs[0].Opcode())
	require.Equal(t, 1, instr.ParseI16(instrs[0], 1))
}

func TestBuilder_BrTable(t *testing.T) {
	b := instr.NewBuilder()
	zero := b.Label()
	one := b.Label()
	def := b.Label()
	b.BrTable(def, zero, one).
		Bind(zero).Emit(instr.NOP).
		Bind(one).Emit(instr.NOP).
		Bind(def).Emit(instr.RETURN)

	instrs, err := b.Assemble()
	require.NoError(t, err)
	require.Equal(t, instr.BR_TABLE, instrs[0].Opcode())
	require.Equal(t, []uint64{2, 0, 1, 2}, instrs[0].Operands())
}

func TestBuilder_Try(t *testing.T) {
	b := instr.NewBuilder()
	start, end, catch := b.Label(), b.Label(), b.Label()
	require.Same(t, b, b.Try(start, end, catch, 2))

	_, err := b.Assemble()
	require.ErrorIs(t, err, instr.ErrUnboundLabel)
}

func TestBuilder_Assemble(t *testing.T) {
	t.Run("unbound label", func(t *testing.T) {
		b := instr.NewBuilder()
		b.Br(b.Label())

		_, err := b.Assemble()
		require.ErrorIs(t, err, instr.ErrUnboundLabel)
	})

	t.Run("offset out of range", func(t *testing.T) {
		b := instr.NewBuilder()
		end := b.Label()
		b.Br(end)
		for i := 0; i < math.MaxInt16+1; i++ {
			b.Emit(instr.NOP)
		}
		b.Bind(end).Emit(instr.RETURN)

		_, err := b.Assemble()
		require.ErrorIs(t, err, instr.ErrOffsetRange)
	})
}

func TestBuilder_Handlers(t *testing.T) {
	b := instr.NewBuilder()
	start, end, catch := b.Label(), b.Label(), b.Label()
	b.Bind(start).Emit(instr.NOP).Bind(end).Emit(instr.RETURN).Bind(catch).Emit(instr.DROP)
	b.Try(start, end, catch, 2)

	_, err := b.Assemble()
	require.NoError(t, err)
	require.Equal(t, []instr.Handler{{Start: 0, End: 1, Catch: 2, Depth: 2}}, b.Handlers())

	handlers := b.Handlers()
	handlers[0].Depth = 9
	require.Equal(t, 2, b.Handlers()[0].Depth)
}
