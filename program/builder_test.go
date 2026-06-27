package program

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestBuilder_Const(t *testing.T) {
	b := NewBuilder()

	require.Equal(t, 0, b.Const(types.String("a")))
	require.Equal(t, 1, b.Const(types.String("b")))
	require.Equal(t, 0, b.Const(types.String("a")))
}

func TestBuilder_Type(t *testing.T) {
	b := NewBuilder()

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

func TestBuilder_Ext(t *testing.T) {
	b := NewBuilder()
	b.Ext(1, 2)
	b.Ext(3, 4, 7, 9)

	prog, err := b.Build()
	require.NoError(t, err)

	instrs := instr.Unmarshal(prog.Code)

	require.Equal(t, instr.EXT, instrs[0].Opcode())
	require.Equal(t, uint64(1<<8|2), instrs[0].Operand(0))
	require.Equal(t, uint64(0), instrs[0].Operand(1))
	require.Len(t, instrs[0], 4)

	require.Equal(t, instr.EXT, instrs[1].Opcode())
	require.Equal(t, uint64(3<<8|4), instrs[1].Operand(0))
	require.Equal(t, uint64(2), instrs[1].Operand(1))
	require.Equal(t, uint64(7), instrs[1].Operand(2))
	require.Equal(t, uint64(9), instrs[1].Operand(3))
	require.Len(t, instrs[1], 20)
}

func TestBuilder_Build(t *testing.T) {
	t.Run("resolves branch to label", func(t *testing.T) {
		b := NewBuilder()
		skip := b.Label()
		b.Emit(instr.I32_CONST, 1).
			BrIf(skip).
			ConstGet(types.String("x")).
			Bind(skip).
			Emit(instr.RETURN)

		got, err := b.Build()
		require.NoError(t, err)

		want := New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_IF, 3),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.RETURN),
			},
			WithConstants(types.String("x")),
		)
		require.Equal(t, want.String(), got.String())
	})

	t.Run("unbound label", func(t *testing.T) {
		b := NewBuilder()
		b.Br(b.Label())

		_, err := b.Build()
		require.ErrorIs(t, err, instr.ErrUnboundLabel)
	})
}
