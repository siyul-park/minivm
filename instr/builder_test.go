package instr

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewBuilder(t *testing.T) {
	b := NewBuilder()
	require.NotNil(t, b)

	instrs, err := b.Assemble()
	require.NoError(t, err)
	require.Empty(t, instrs)
	require.Nil(t, b.Handlers())
}

func TestBuilder_Label(t *testing.T) {
	b := NewBuilder()
	first := b.Label()
	second := b.Label()
	require.NotEqual(t, first, second)
}

func TestBuilder_Bind(t *testing.T) {
	b := NewBuilder()
	end := b.Label()
	require.Same(t, b, b.Br(end).Emit(NOP).Bind(end).Emit(RETURN))

	instrs, err := b.Assemble()
	require.NoError(t, err)
	require.Equal(t, 1, ParseI16(instrs[0], 1))
}

func TestBuilder_Emit(t *testing.T) {
	b := NewBuilder()
	b.Emit(I32_CONST, 42)

	instrs, err := b.Assemble()
	require.NoError(t, err)
	require.Equal(t, I32_CONST, instrs[0].Opcode())
	require.Equal(t, uint64(42), instrs[0].Operand(0))
}

func TestBuilder_Append(t *testing.T) {
	b := NewBuilder()
	b.Append(New(NOP), New(RETURN))

	instrs, err := b.Assemble()
	require.NoError(t, err)
	require.Equal(t, NOP, instrs[0].Opcode())
	require.Equal(t, RETURN, instrs[1].Opcode())
}

func TestBuilder_Br(t *testing.T) {
	t.Run("backward", func(t *testing.T) {
		b := NewBuilder()
		loop := b.Label()
		b.Bind(loop).Emit(NOP).Emit(NOP).Br(loop)

		instrs, err := b.Assemble()
		require.NoError(t, err)
		require.Equal(t, BR, instrs[2].Opcode())
		require.Equal(t, -5, ParseI16(instrs[2], 1))
	})

	t.Run("forward", func(t *testing.T) {
		b := NewBuilder()
		end := b.Label()
		b.Br(end).Emit(NOP).Bind(end).Emit(RETURN)

		instrs, err := b.Assemble()
		require.NoError(t, err)
		require.Equal(t, 1, ParseI16(instrs[0], 1))
	})
}

func TestBuilder_BrIf(t *testing.T) {
	b := NewBuilder()
	end := b.Label()
	b.BrIf(end).Emit(NOP).Bind(end).Emit(RETURN)

	instrs, err := b.Assemble()
	require.NoError(t, err)
	require.Equal(t, BR_IF, instrs[0].Opcode())
	require.Equal(t, 1, ParseI16(instrs[0], 1))
}

func TestBuilder_BrTable(t *testing.T) {
	b := NewBuilder()
	zero := b.Label()
	one := b.Label()
	def := b.Label()
	b.BrTable(def, zero, one).
		Bind(zero).Emit(NOP).
		Bind(one).Emit(NOP).
		Bind(def).Emit(RETURN)

	instrs, err := b.Assemble()
	require.NoError(t, err)
	require.Equal(t, BR_TABLE, instrs[0].Opcode())
	require.Equal(t, []uint64{2, 0, 1, 2}, instrs[0].Operands())
}

func TestBuilder_Try(t *testing.T) {
	b := NewBuilder()
	start, end, catch := b.Label(), b.Label(), b.Label()
	require.Same(t, b, b.Try(start, end, catch, 2))

	_, err := b.Assemble()
	require.ErrorIs(t, err, ErrUnboundLabel)
}

func TestBuilder_Assemble(t *testing.T) {
	t.Run("unbound label", func(t *testing.T) {
		b := NewBuilder()
		b.Br(b.Label())

		_, err := b.Assemble()
		require.ErrorIs(t, err, ErrUnboundLabel)
	})

	t.Run("offset out of range", func(t *testing.T) {
		b := NewBuilder()
		end := b.Label()
		b.Br(end)
		for i := 0; i < math.MaxInt16+1; i++ {
			b.Emit(NOP)
		}
		b.Bind(end).Emit(RETURN)

		_, err := b.Assemble()
		require.ErrorIs(t, err, ErrOffsetRange)
	})
}

func TestBuilder_Handlers(t *testing.T) {
	b := NewBuilder()
	start, end, catch := b.Label(), b.Label(), b.Label()
	b.Bind(start).Emit(NOP).Bind(end).Emit(RETURN).Bind(catch).Emit(DROP)
	b.Try(start, end, catch, 2)

	_, err := b.Assemble()
	require.NoError(t, err)
	require.Equal(t, []Handler{{Start: 0, End: 1, Catch: 2, Depth: 2}}, b.Handlers())

	handlers := b.Handlers()
	handlers[0].Depth = 9
	require.Equal(t, 2, b.Handlers()[0].Depth)
}
