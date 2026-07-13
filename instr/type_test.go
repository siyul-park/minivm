package instr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypeOf(t *testing.T) {
	t.Run("defined opcode", func(t *testing.T) {
		require.Equal(t, Type{
			Mnemonic: "i32.const",
			Widths:   []int{4},
			Push:     []Kind{KindI32},
		}, TypeOf(I32_CONST))
	})

	t.Run("undefined opcode", func(t *testing.T) {
		require.Zero(t, TypeOf(Opcode(0xff)))
	})
}

func TestValid(t *testing.T) {
	mnemonics := make(map[string]Opcode)
	for op := Opcode(0); op < opcodeCount; op++ {
		typ := TypeOf(op)
		require.True(t, Valid(op), "opcode %d has no metadata", op)
		require.NotEmpty(t, typ.Mnemonic, "opcode %d has no mnemonic", op)
		previous, exists := mnemonics[typ.Mnemonic]
		require.False(t, exists, "opcodes %d and %d share mnemonic %q", previous, op, typ.Mnemonic)
		mnemonics[typ.Mnemonic] = op
		for _, width := range typ.Widths {
			require.Contains(t, []int{-8, -4, -2, -1, 1, 2, 4, 8}, width, "%s has invalid operand width", typ.Mnemonic)
		}
	}

	require.LessOrEqual(t, int(opcodeCount), 256)
	for code := int(opcodeCount); code < 256; code++ {
		require.False(t, Valid(Opcode(code)), "opcode %d is registered past opcodeCount", code)
	}
}
