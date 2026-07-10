package main

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestExpand(t *testing.T) {
	t.Run("product expands every source and consumer combination", func(t *testing.T) {
		got, err := expand(Product(
			Sources(Op(instr.LOCAL_GET), Op(instr.GLOBAL_GET)),
			Consumers(Op(instr.DROP), Seq(Op(instr.REF_IS_NULL), Op(instr.BR_IF))),
		))
		require.NoError(t, err)

		want := []any{
			Fuse(Op(instr.LOCAL_GET), Op(instr.DROP)),
			Fuse(Op(instr.LOCAL_GET), Op(instr.REF_IS_NULL), Op(instr.BR_IF)),
			Fuse(Op(instr.GLOBAL_GET), Op(instr.DROP)),
			Fuse(Op(instr.GLOBAL_GET), Op(instr.REF_IS_NULL), Op(instr.BR_IF)),
		}
		require.ElementsMatch(t, want, got)
	})

	t.Run("preserves exact concrete type guards and negation", func(t *testing.T) {
		got, err := expand(Product(
			Sources(
				Op(instr.CONST_GET, TypeFor[types.String]()),
				Op(instr.CONST_GET, Not(TypeFor[types.String]())),
			),
			Consumers(Op(instr.DROP)),
		))
		require.NoError(t, err)

		want := []any{
			Fuse(Op(instr.CONST_GET, TypeFor[types.String]()), Op(instr.DROP)),
			Fuse(Op(instr.CONST_GET, Not(TypeFor[types.String]())), Op(instr.DROP)),
		}
		require.ElementsMatch(t, want, got)
	})
}

func TestValidate(t *testing.T) {
	t.Run("rejects duplicate concrete patterns after product expansion", func(t *testing.T) {
		rules, err := expand(Product(
			Sources(Op(instr.LOCAL_GET), Op(instr.LOCAL_GET)),
			Consumers(Op(instr.DROP)),
		))
		require.NoError(t, err)
		require.Error(t, validate(rules))
	})

	t.Run("rejects variable width opcodes", func(t *testing.T) {
		rules, err := expand(Fuse(Op(instr.BR_TABLE), Op(instr.DROP)))
		require.NoError(t, err)
		require.Error(t, validate(rules))
	})

	t.Run("accepts distinct exact and negated type guards", func(t *testing.T) {
		rules, err := expand(Product(
			Sources(
				Op(instr.CONST_GET, TypeFor[types.String]()),
				Op(instr.CONST_GET, Not(TypeFor[types.String]())),
			),
			Consumers(Op(instr.DROP)),
		))
		require.NoError(t, err)
		require.NoError(t, validate(rules))
	})
}

func TestGenerate(t *testing.T) {
	first, err := generate()
	require.NoError(t, err)
	second, err := generate()
	require.NoError(t, err)
	require.Equal(t, first, second)

	paths := make([]string, len(first))
	for idx, output := range first {
		paths[idx] = output.path
	}
	require.IsIncreasing(t, paths)
}
