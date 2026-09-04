package instr_test

import (
	"strings"
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

func TestOpcode_Reads(t *testing.T) {
	t.Run("names the storage an opcode reads", func(t *testing.T) {
		require.True(t, instr.LOCAL_GET.Reads(instr.Local))
		require.True(t, instr.GLOBAL_GET.Reads(instr.Global))
		require.True(t, instr.UPVAL_GET.Reads(instr.Upval))
		require.True(t, instr.ARRAY_GET.Reads(instr.Heap))
		require.True(t, instr.ARRAY_SET.Reads(instr.Heap))
		require.False(t, instr.ARRAY_NEW_DEFAULT.Reads(instr.Heap))
		require.False(t, instr.I32_ADD.Reads(instr.Heap))
	})

	t.Run("holds every container opcode to its namespace", func(t *testing.T) {
		spaces := map[string]instr.Effect{
			"local": instr.Local, "global": instr.Global, "upval": instr.Upval,
			"array": instr.Heap, "map": instr.Heap, "struct": instr.Heap,
			"string": instr.Heap, "error": instr.Heap, "coro": instr.Heap,
		}
		for op := instr.NOP; op <= instr.STRING_ITER; op++ {
			mnemonic := instr.TypeOf(op).Mnemonic
			space, _, ok := strings.Cut(mnemonic, ".")
			if effect, named := spaces[space]; ok && named {
				require.True(t, op.Reads(effect) || op.Writes(effect),
					"%s touches no %s storage", mnemonic, space)
			}
		}
	})
}

func TestOpcode_Writes(t *testing.T) {
	t.Run("names the storage an opcode writes", func(t *testing.T) {
		require.True(t, instr.LOCAL_SET.Writes(instr.Local))
		require.True(t, instr.LOCAL_TEE.Writes(instr.Local))
		require.False(t, instr.LOCAL_GET.Writes(instr.Local))
		require.True(t, instr.ARRAY_SET.Writes(instr.Heap))
		require.True(t, instr.ARRAY_NEW_DEFAULT.Writes(instr.Heap))
		require.False(t, instr.ARRAY_GET.Writes(instr.Heap))
	})

	t.Run("enters a frame only where a call does", func(t *testing.T) {
		for op := instr.NOP; op <= instr.STRING_ITER; op++ {
			want := op == instr.CALL || op == instr.RETURN_CALL
			require.Equal(t, want, op.Writes(instr.Frame),
				"%s disagrees about entering a frame", instr.TypeOf(op).Mnemonic)
		}
	})

	t.Run("moves the instruction pointer only where control leaves", func(t *testing.T) {
		leaves := map[instr.Opcode]bool{
			instr.BR: true, instr.BR_IF: true, instr.BR_TABLE: true,
			instr.CALL: true, instr.RETURN: true, instr.RETURN_CALL: true,
			instr.THROW: true, instr.UNREACHABLE: true,
			instr.YIELD: true, instr.RESUME: true,
		}
		for op := instr.NOP; op <= instr.STRING_ITER; op++ {
			require.Equal(t, leaves[op], op.Writes(instr.Branch),
				"%s disagrees about moving the instruction pointer", instr.TypeOf(op).Mnemonic)
		}
	})
}

func TestOpcode_IsPure(t *testing.T) {
	t.Run("computes from its operands alone", func(t *testing.T) {
		require.True(t, instr.I32_ADD.IsPure())
		require.True(t, instr.F64_LT.IsPure())
		require.True(t, instr.REF_EQ.IsPure())
		require.True(t, instr.REF_IS_NULL.IsPure())
		require.True(t, instr.SELECT.IsPure())
	})

	t.Run("rejects an opcode that touches anything else", func(t *testing.T) {
		require.False(t, instr.ARRAY_LEN.IsPure())
		require.False(t, instr.STRING_EQ.IsPure())
		require.False(t, instr.LOCAL_GET.IsPure())
		require.False(t, instr.CALL.IsPure())
		require.False(t, instr.REF_NEW.IsPure())
	})

	t.Run("rejects an opcode that yields nothing", func(t *testing.T) {
		require.False(t, instr.NOP.IsPure())
		require.False(t, instr.DROP.IsPure())
	})
}
