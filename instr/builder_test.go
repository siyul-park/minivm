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
	t.Run("fixes label", func(t *testing.T) {
		b := instr.NewBuilder()
		start, end := b.Label(), b.Label()
		require.Same(t, b, b.Bind(start).Emit(instr.NOP).Emit(instr.NOP).Bind(end))

		b.Try(start, end, end, 0)

		_, err := b.Assemble()
		require.NoError(t, err)
		require.Equal(t, []instr.Handler{{Start: 0, End: 2, Catch: 2, Depth: 0}}, b.Handlers())
	})

	t.Run("zero label", func(t *testing.T) {
		_, err := instr.NewBuilder().Bind(instr.Label{}).Assemble()
		require.ErrorIs(t, err, instr.ErrForeignLabel)
	})

	t.Run("foreign label", func(t *testing.T) {
		_, err := instr.NewBuilder().Bind(instr.NewBuilder().Label()).Assemble()
		require.ErrorIs(t, err, instr.ErrForeignLabel)
	})

	t.Run("duplicate bind", func(t *testing.T) {
		b := instr.NewBuilder()
		l := b.Label()
		_, err := b.Bind(l).Emit(instr.NOP).Bind(l).Assemble()
		require.ErrorIs(t, err, instr.ErrBoundLabel)
	})
}

func TestBuilder_Emit(t *testing.T) {
	t.Run("encodes instruction", func(t *testing.T) {
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 42)

		instrs, err := b.Assemble()
		require.NoError(t, err)
		require.Equal(t, instr.I32_CONST, instrs[0].Opcode())
		require.Equal(t, uint64(42), instrs[0].Operand(0))
	})

	t.Run("invalid operands", func(t *testing.T) {
		instrs, err := instr.NewBuilder().Emit(instr.LOCAL_GET).Assemble()
		require.ErrorIs(t, err, instr.ErrOperandCount)
		require.Nil(t, instrs)
	})

	t.Run("first error is sticky", func(t *testing.T) {
		b := instr.NewBuilder()
		b.Emit(instr.LOCAL_GET).Emit(instr.NOP).Emit(instr.Opcode(0xFF)).Bind(instr.Label{}).Append(nil)

		instrs, err := b.Assemble()
		require.ErrorIs(t, err, instr.ErrOperandCount)
		require.NotErrorIs(t, err, instr.ErrUnknownOpcode)
		require.Nil(t, instrs)
	})
}

func TestBuilder_Append(t *testing.T) {
	t.Run("appends instructions", func(t *testing.T) {
		b := instr.NewBuilder()
		b.Append(instr.MustNew(instr.NOP), instr.MustNew(instr.RETURN))

		instrs, err := b.Assemble()
		require.NoError(t, err)
		require.Equal(t, instr.NOP, instrs[0].Opcode())
		require.Equal(t, instr.RETURN, instrs[1].Opcode())
	})

	t.Run("nil instruction", func(t *testing.T) {
		_, err := instr.NewBuilder().Append(instr.MustNew(instr.NOP), nil).Assemble()
		require.ErrorIs(t, err, instr.ErrInvalidInstruction)
	})

	t.Run("empty instruction", func(t *testing.T) {
		_, err := instr.NewBuilder().Append(instr.Instruction{}).Assemble()
		require.ErrorIs(t, err, instr.ErrInvalidInstruction)
	})

	t.Run("unknown opcode", func(t *testing.T) {
		_, err := instr.NewBuilder().Append(instr.Instruction{0xFF}).Assemble()
		require.ErrorIs(t, err, instr.ErrInvalidInstruction)
	})

	t.Run("truncated operand", func(t *testing.T) {
		_, err := instr.NewBuilder().Append(instr.Instruction{byte(instr.BR), 0}).Assemble()
		require.ErrorIs(t, err, instr.ErrInvalidInstruction)
	})

	t.Run("truncated table", func(t *testing.T) {
		_, err := instr.NewBuilder().Append(instr.Instruction{byte(instr.BR_TABLE), 2, 0, 0}).Assemble()
		require.ErrorIs(t, err, instr.ErrInvalidInstruction)
	})

	t.Run("missing table count", func(t *testing.T) {
		_, err := instr.NewBuilder().Append(instr.Instruction{byte(instr.BR_TABLE)}).Assemble()
		require.ErrorIs(t, err, instr.ErrInvalidInstruction)
	})

	t.Run("trailing bytes", func(t *testing.T) {
		_, err := instr.NewBuilder().Append(instr.Instruction{byte(instr.NOP), 0}).Assemble()
		require.ErrorIs(t, err, instr.ErrInvalidInstruction)
	})
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

	t.Run("zero label", func(t *testing.T) {
		_, err := instr.NewBuilder().Br(instr.Label{}).Assemble()
		require.ErrorIs(t, err, instr.ErrForeignLabel)
	})

	t.Run("foreign label", func(t *testing.T) {
		_, err := instr.NewBuilder().Br(instr.NewBuilder().Label()).Assemble()
		require.ErrorIs(t, err, instr.ErrForeignLabel)
	})
}

func TestBuilder_BrIf(t *testing.T) {
	t.Run("forward", func(t *testing.T) {
		b := instr.NewBuilder()
		end := b.Label()
		b.BrIf(end).Emit(instr.NOP).Bind(end).Emit(instr.RETURN)

		instrs, err := b.Assemble()
		require.NoError(t, err)
		require.Equal(t, instr.BR_IF, instrs[0].Opcode())
		require.Equal(t, 1, instr.ParseI16(instrs[0], 1))
	})

	t.Run("zero label", func(t *testing.T) {
		_, err := instr.NewBuilder().BrIf(instr.Label{}).Assemble()
		require.ErrorIs(t, err, instr.ErrForeignLabel)
	})

	t.Run("foreign label", func(t *testing.T) {
		_, err := instr.NewBuilder().BrIf(instr.NewBuilder().Label()).Assemble()
		require.ErrorIs(t, err, instr.ErrForeignLabel)
	})
}

func TestBuilder_BrTable(t *testing.T) {
	t.Run("targets and default", func(t *testing.T) {
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
	})

	t.Run("zero default", func(t *testing.T) {
		b := instr.NewBuilder()
		_, err := b.BrTable(instr.Label{}, b.Label()).Assemble()
		require.ErrorIs(t, err, instr.ErrForeignLabel)
	})

	t.Run("foreign target", func(t *testing.T) {
		b := instr.NewBuilder()
		_, err := b.BrTable(b.Label(), instr.NewBuilder().Label()).Assemble()
		require.ErrorIs(t, err, instr.ErrForeignLabel)
	})

	t.Run("too many targets", func(t *testing.T) {
		b := instr.NewBuilder()
		targets := make([]instr.Label, 256)
		for i := range targets {
			targets[i] = b.Label()
		}
		_, err := b.BrTable(b.Label(), targets...).Assemble()
		require.ErrorIs(t, err, instr.ErrOperandRange)
	})
}

func TestBuilder_Try(t *testing.T) {
	t.Run("unbound labels", func(t *testing.T) {
		b := instr.NewBuilder()
		start, end, catch := b.Label(), b.Label(), b.Label()
		require.Same(t, b, b.Try(start, end, catch, 2))

		_, err := b.Assemble()
		require.ErrorIs(t, err, instr.ErrUnboundLabel)
	})

	t.Run("zero label", func(t *testing.T) {
		b := instr.NewBuilder()
		l := b.Label()
		_, err := b.Try(l, l, instr.Label{}, 0).Assemble()
		require.ErrorIs(t, err, instr.ErrForeignLabel)
	})

	t.Run("foreign label", func(t *testing.T) {
		b := instr.NewBuilder()
		l := b.Label()
		_, err := b.Try(l, instr.NewBuilder().Label(), l, 0).Assemble()
		require.ErrorIs(t, err, instr.ErrForeignLabel)
	})
}

func TestBuilder_Assemble(t *testing.T) {
	tests := []struct {
		name    string
		offset  int
		unbound bool
		wantErr error
	}{
		{name: "unbound label", unbound: true, wantErr: instr.ErrUnboundLabel},
		{name: "below minimum offset", offset: math.MinInt16 - 1, wantErr: instr.ErrOffsetRange},
		{name: "minimum offset", offset: math.MinInt16},
		{name: "maximum offset", offset: math.MaxInt16},
		{name: "above maximum offset", offset: math.MaxInt16 + 1, wantErr: instr.ErrOffsetRange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := instr.NewBuilder()
			target := b.Label()
			if tt.offset < 0 {
				b.Bind(target)
				for range -tt.offset - 3 {
					b.Emit(instr.NOP)
				}
				b.Br(target)
			} else {
				b.Br(target)
				for range tt.offset {
					b.Emit(instr.NOP)
				}
				if !tt.unbound {
					b.Bind(target)
				}
			}
			instructions, err := b.Assemble()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, instructions)
				return
			}
			require.NoError(t, err)
			branch := instructions[0]
			if tt.offset < 0 {
				branch = instructions[len(instructions)-1]
			}
			require.Equal(t, tt.offset, instr.ParseI16(branch, 1))
		})
	}
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
