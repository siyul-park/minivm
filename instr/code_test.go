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

func TestFormat(t *testing.T) {
	t.Run("no branches", func(t *testing.T) {
		insts := []instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD)}
		assembly := instr.Format(instr.Marshal(insts))
		require.Equal(t, "0000:\ti32.const 0x00000001\n0005:\ti32.const 0x00000002\n0010:\ti32.add\n", assembly)
	})

	t.Run("forward branch gets a label", func(t *testing.T) {
		b := instr.NewBuilder()
		end := b.Label()
		b.Br(end).Emit(instr.NOP).Bind(end).Emit(instr.RETURN)
		insts, err := b.Assemble()
		require.NoError(t, err)

		assembly := instr.Format(instr.Marshal(insts))
		require.Equal(t, "0000:\tbr L0004\n0003:\tnop\nL0004:\n0004:\treturn\n", assembly)
	})

	t.Run("backward branch gets a label", func(t *testing.T) {
		b := instr.NewBuilder()
		loop := b.Label()
		b.Bind(loop).Emit(instr.NOP).Br(loop)
		insts, err := b.Assemble()
		require.NoError(t, err)

		assembly := instr.Format(instr.Marshal(insts))
		require.Equal(t, "L0000:\n0000:\tnop\n0001:\tbr L0000\n", assembly)
	})

	t.Run("br_table renders count first with case labels", func(t *testing.T) {
		b := instr.NewBuilder()
		zero, one, def := b.Label(), b.Label(), b.Label()
		b.BrTable(def, zero, one).
			Bind(zero).Emit(instr.NOP).
			Bind(one).Emit(instr.NOP).
			Bind(def).Emit(instr.RETURN)
		insts, err := b.Assemble()
		require.NoError(t, err)

		assembly := instr.Format(instr.Marshal(insts))
		require.Equal(t, "0000:\tbr_table 0x02 L0008 L0009 L0010\nL0008:\n0008:\tnop\nL0009:\n0009:\tnop\nL0010:\n0010:\treturn\n", assembly)
	})

	t.Run("end of code is a label site", func(t *testing.T) {
		// A loop exit branches one past the last instruction to leave the code,
		// and program/verify.go accepts that offset, so it earns a label like
		// any other boundary.
		b := instr.NewBuilder()
		done := b.Label()
		b.Emit(instr.I32_CONST, 1).BrIf(done).Emit(instr.NOP).Bind(done)
		insts, err := b.Assemble()
		require.NoError(t, err)

		assembly := instr.Format(instr.Marshal(insts))
		require.Equal(t, "0000:\ti32.const 0x00000001\n0005:\tbr_if L0009\n0008:\tnop\nL0009:\n", assembly)
	})

	t.Run("target past the end falls back to numeric", func(t *testing.T) {
		// next is 8 and the code is 8 bytes, so 0x0001 lands at 9 -- past the
		// end, on no boundary at all -- while 0x0000 lands exactly at the end.
		insts := []instr.Instruction{instr.New(instr.BR_TABLE, 2, 0, 1, 0)}
		assembly := instr.Format(instr.Marshal(insts))
		require.Equal(t, "0000:\tbr_table 0x02 L0008 0x0001 L0008\nL0008:\n", assembly)
	})
}

func TestUnmarshal(t *testing.T) {
	insts := []instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD)}
	actual := instr.Unmarshal(instr.Marshal(insts))
	require.Equal(t, insts, actual)
}
