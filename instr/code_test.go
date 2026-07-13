package instr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshal(t *testing.T) {
	insts := []Instruction{
		New(I32_CONST, 1),
		New(I32_CONST, 2),
		New(I32_ADD),
	}
	code := Marshal(insts)
	require.Len(t, code, 11)
}

func TestUnmarshal(t *testing.T) {
	insts := []Instruction{
		New(I32_CONST, 1),
		New(I32_CONST, 2),
		New(I32_ADD),
	}
	actual := Unmarshal(Marshal(insts))
	require.Equal(t, insts, actual)
}

func TestFormat(t *testing.T) {
	insts := []Instruction{
		New(I32_CONST, 1),
		New(I32_CONST, 2),
		New(I32_ADD),
	}
	assembly := Format(Marshal(insts))
	require.Equal(t, "0000:\ti32.const 0x00000001\n0005:\ti32.const 0x00000002\n0010:\ti32.add\n", assembly)
}

func TestTargets(t *testing.T) {
	t.Run("branch", func(t *testing.T) {
		b := NewBuilder()
		end := b.Label()
		b.Br(end).Emit(NOP).Bind(end).Emit(RETURN)
		instrs, err := b.Assemble()
		require.NoError(t, err)

		require.Equal(t, []int{4}, Targets(Marshal(instrs), 0))
	})

	t.Run("conditional branch", func(t *testing.T) {
		b := NewBuilder()
		end := b.Label()
		b.BrIf(end).Emit(NOP).Bind(end).Emit(RETURN)
		instrs, err := b.Assemble()
		require.NoError(t, err)

		require.Equal(t, []int{4}, Targets(Marshal(instrs), 0))
	})

	t.Run("branch table", func(t *testing.T) {
		b := NewBuilder()
		first, second, def := b.Label(), b.Label(), b.Label()
		b.BrTable(def, first, second).
			Bind(first).Emit(NOP).
			Bind(second).Emit(NOP).
			Bind(def).Emit(RETURN)
		instrs, err := b.Assemble()
		require.NoError(t, err)

		require.Equal(t, []int{8, 9, 10}, Targets(Marshal(instrs), 0))
	})

	t.Run("non branch", func(t *testing.T) {
		require.Nil(t, Targets(Marshal([]Instruction{New(NOP)}), 0))
	})
}
