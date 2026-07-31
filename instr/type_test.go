package instr_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestTypeOf(t *testing.T) {
	t.Run("defined opcode", func(t *testing.T) {
		require.Equal(t, instr.Type{
			Mnemonic: "i32.const",
			Widths:   []int{4},
			Push:     []instr.Kind{instr.KindI32},
		}, instr.TypeOf(instr.I32_CONST))
	})

	t.Run("undefined opcode", func(t *testing.T) {
		require.Zero(t, instr.TypeOf(instr.Opcode(0xff)))
	})
}

func TestValid(t *testing.T) {
	mnemonics := make(map[string]instr.Opcode)
	for op := instr.NOP; op <= instr.STRING_ITER; op++ {
		require.True(t, instr.Valid(op), "opcode %d has no metadata", op)
		typ := instr.TypeOf(op)
		require.NotEmpty(t, typ.Mnemonic, "opcode %d has no mnemonic", op)
		previous, exists := mnemonics[typ.Mnemonic]
		require.False(t, exists, "opcodes %d and %d share mnemonic %q", previous, op, typ.Mnemonic)
		mnemonics[typ.Mnemonic] = op
		for _, width := range typ.Widths {
			require.Contains(t, []int{-8, -4, -2, -1, 1, 2, 4, 8}, width, "%s has invalid operand width", typ.Mnemonic)
		}
	}

	require.Equal(t, instr.I32_CONST, mnemonics["i32.const"])
	for code := int(instr.STRING_ITER) + 1; code < 256; code++ {
		require.False(t, instr.Valid(instr.Opcode(code)), "opcode %d is registered past STRING_ITER", code)
	}
}
