package instr_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instruction := instr.New(instr.NOP)
		require.Equal(t, []byte{byte(instr.NOP)}, []byte(instruction))
		require.Equal(t, instr.NOP, instruction.Opcode())
		require.Empty(t, instruction.Operands())
		require.Equal(t, 1, instruction.Width())
	})
	t.Run("dynamic", func(t *testing.T) {
		instruction := instr.New(instr.BR_TABLE, 2, 0, 1, 0)
		require.Equal(t, []byte{byte(instr.BR_TABLE), 2, 0, 0, 1, 0, 0, 0}, []byte(instruction))
		require.Equal(t, instr.BR_TABLE, instruction.Opcode())
		require.Equal(t, []uint64{2, 0, 1, 0}, instruction.Operands())
		require.Equal(t, 8, instruction.Width())
	})
}

func TestInstruction_SetOperand(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instruction := instr.New(instr.I32_CONST, 1)
		instruction.SetOperand(0, 42)
		require.Equal(t, []uint64{42}, instruction.Operands())
	})
	t.Run("narrow", func(t *testing.T) {
		instruction := instr.New(instr.LOCAL_GET, 1)
		instruction.SetOperand(0, 7)
		require.Equal(t, []uint64{7}, instruction.Operands())
	})
	t.Run("wide", func(t *testing.T) {
		instruction := instr.New(instr.I64_CONST, 1)
		instruction.SetOperand(0, 0x0102030405060708)
		require.Equal(t, []uint64{0x0102030405060708}, instruction.Operands())
	})
	t.Run("dynamic", func(t *testing.T) {
		instruction := instr.New(instr.BR_TABLE, 2, 0, 1, 0)
		instruction.SetOperand(1, 5)
		require.Equal(t, []uint64{2, 5, 1, 0}, instruction.Operands())
	})
	t.Run("dynamic count", func(t *testing.T) {
		instruction := instr.New(instr.BR_TABLE, 2, 0, 1, 0)
		instruction.SetOperand(0, 1)
		require.Equal(t, []uint64{1, 0, 1}, instruction.Operands())
	})
	t.Run("out of range", func(t *testing.T) {
		instruction := instr.New(instr.BR, 5)
		instruction.SetOperand(1, 9)
		require.Equal(t, []uint64{5}, instruction.Operands())
	})
}

func TestInstruction_Operand(t *testing.T) {
	t.Run("in range", func(t *testing.T) {
		instruction := instr.New(instr.BR, 5)
		require.Equal(t, uint64(5), instruction.Operand(0))
	})
	t.Run("out of range", func(t *testing.T) {
		instruction := instr.New(instr.BR, 5)
		require.Zero(t, instruction.Operand(1))
	})
}

func TestInstruction_Width(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instruction := instr.New(instr.NOP)
		require.Equal(t, 1, instruction.Width())
	})
	t.Run("dynamic", func(t *testing.T) {
		instruction := instr.New(instr.BR_TABLE, 2, 0, 1, 0)
		require.Equal(t, 8, instruction.Width())
	})
}

func TestInstruction_String(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instruction := instr.New(instr.NOP)
		require.Equal(t, "nop", instruction.String())
	})
	t.Run("dynamic", func(t *testing.T) {
		instruction := instr.New(instr.BR_TABLE, 2, 0, 1, 0)
		require.Equal(t, "br_table 0x02 0x0000 0x0001 0x0000", instruction.String())
	})
}

func TestInstruction_Operands(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instruction := instr.New(instr.NOP)
		require.Empty(t, instruction.Operands())
	})
	t.Run("dynamic", func(t *testing.T) {
		instruction := instr.New(instr.BR_TABLE, 2, 0, 1, 0)
		require.Equal(t, []uint64{2, 0, 1, 0}, instruction.Operands())
	})
}

func TestInstruction_Type(t *testing.T) {
	instruction := instr.New(instr.I32_CONST, 42)
	require.Equal(t, instr.Type{
		Mnemonic: "i32.const",
		Widths:   []int{4},
		Push:     []instr.Kind{instr.KindI32},
	}, instruction.Type())
}

func TestInstruction_Opcode(t *testing.T) {
	instruction := instr.New(instr.NOP)
	require.Equal(t, instr.NOP, instruction.Opcode())
}
