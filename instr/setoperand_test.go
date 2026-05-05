package instr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstruction_SetOperand(t *testing.T) {
	t.Run("static_1byte", func(t *testing.T) {
		i := New(I32_CONST, 0)
		i.SetOperand(0, 99)
		require.Equal(t, uint64(99), i.Operand(0))
	})

	t.Run("static_2byte", func(t *testing.T) {
		i := New(BR, 0)
		i.SetOperand(0, 500)
		require.Equal(t, uint64(500), i.Operand(0))
	})

	t.Run("dynamic_count_field", func(t *testing.T) {
		// BR_TABLE: count(1byte) + count*2 targets(2byte each)
		i := New(BR_TABLE, 2, 0, 10, 0)
		// operand 0 is the count
		// operands 1..n are the jump targets
		i.SetOperand(1, 20)
		require.Equal(t, uint64(20), i.Operand(1))
	})
}

func TestInstruction_Operand(t *testing.T) {
	t.Run("in_range", func(t *testing.T) {
		i := New(I32_CONST, 77)
		require.Equal(t, uint64(77), i.Operand(0))
	})

	t.Run("out_of_range", func(t *testing.T) {
		i := New(NOP)
		require.Equal(t, uint64(0), i.Operand(5))
	})

	t.Run("negative_index", func(t *testing.T) {
		i := New(NOP)
		require.Equal(t, uint64(0), i.Operand(-1))
	})
}
