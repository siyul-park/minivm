package instr_test

import (
	"testing"

	instr "github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestMarshal(t *testing.T) {
	insts := []instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD)}
	code := instr.Marshal(insts)
	require.Len(t, code, 11)
}

func TestUnmarshal(t *testing.T) {
	insts := []instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD)}
	actual := instr.Unmarshal(instr.Marshal(insts))
	require.Equal(t, insts, actual)
}

func TestFormat(t *testing.T) {
	insts := []instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD)}
	assembly := instr.Format(instr.Marshal(insts))
	require.Equal(t, "0000:\ti32.const 0x00000001\n0005:\ti32.const 0x00000002\n0010:\ti32.add\n", assembly)
}

func TestTargets(t *testing.T) {
	t.Run("branch", func(t *testing.T) {
		b := instr.NewBuilder()
		end := b.Label()
		b.Br(end).Emit(instr.NOP).Bind(end).Emit(instr.RETURN)
		instrs, err := b.Assemble()
		require.NoError(t, err)

		require.Equal(t, []int{4}, instr.Targets(instr.Marshal(instrs), 0))
	})

	t.Run("conditional branch", func(t *testing.T) {
		b := instr.NewBuilder()
		end := b.Label()
		b.BrIf(end).Emit(instr.NOP).Bind(end).Emit(instr.RETURN)
		instrs, err := b.Assemble()
		require.NoError(t, err)

		require.Equal(t, []int{4}, instr.Targets(instr.Marshal(instrs), 0))
	})

	t.Run("branch table", func(t *testing.T) {
		b := instr.NewBuilder()
		first, second, def := b.Label(), b.Label(), b.Label()
		b.BrTable(def, first, second).
			Bind(first).Emit(instr.NOP).
			Bind(second).Emit(instr.NOP).
			Bind(def).Emit(instr.RETURN)
		instrs, err := b.Assemble()
		require.NoError(t, err)

		require.Equal(t, []int{8, 9, 10}, instr.Targets(instr.Marshal(instrs), 0))
	})

	t.Run("non branch", func(t *testing.T) {
		require.Nil(t, instr.Targets(instr.Marshal([]instr.Instruction{instr.New(instr.NOP)}), 0))
	})
}
