package main

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestExpand(t *testing.T) {
	t.Run("product expands every source and consumer combination", func(t *testing.T) {
		got, err := expand(product(
			sources(op(instr.LOCAL_GET), op(instr.GLOBAL_GET)),
			consumers(op(instr.DROP), seq(op(instr.REF_IS_NULL), op(instr.BR_IF))),
		))
		require.NoError(t, err)

		want := []rule{
			{pattern: fuse(op(instr.LOCAL_GET), op(instr.DROP)).pattern},
			{pattern: fuse(op(instr.LOCAL_GET), op(instr.REF_IS_NULL), op(instr.BR_IF)).pattern},
			{pattern: fuse(op(instr.GLOBAL_GET), op(instr.DROP)).pattern},
			{pattern: fuse(op(instr.GLOBAL_GET), op(instr.REF_IS_NULL), op(instr.BR_IF)).pattern},
		}
		require.ElementsMatch(t, want, got)
	})

	t.Run("preserves exact concrete type guards and negation", func(t *testing.T) {
		got, err := expand(product(
			sources(
				op(instr.CONST_GET, typeFor[types.String]()),
				op(instr.CONST_GET, not(typeFor[types.String]())),
			),
			consumers(op(instr.DROP)),
		))
		require.NoError(t, err)

		want := []rule{
			{pattern: fuse(op(instr.CONST_GET, typeFor[types.String]()), op(instr.DROP)).pattern},
			{pattern: fuse(op(instr.CONST_GET, not(typeFor[types.String]())), op(instr.DROP)).pattern},
		}
		require.ElementsMatch(t, want, got)
	})
}

func TestValidate(t *testing.T) {
	t.Run("rejects ARM64 marker without specialization", func(t *testing.T) {
		rules, err := expand(fuse(op(instr.I32_CONST), op(instr.I32_ADD)).withARM64())
		require.NoError(t, err)
		require.ErrorContains(t, validate(rules), "ARM64-marked fusion has no specialization")
	})

	t.Run("rejects duplicate concrete patterns after product expansion", func(t *testing.T) {
		rules, err := expand(product(
			sources(op(instr.LOCAL_GET), op(instr.LOCAL_GET)),
			consumers(op(instr.DROP)),
		))
		require.NoError(t, err)
		require.Error(t, validate(rules))
	})

	t.Run("rejects variable width opcodes", func(t *testing.T) {
		rules, err := expand(fuse(op(instr.BR_TABLE), op(instr.DROP)))
		require.NoError(t, err)
		require.Error(t, validate(rules))
	})

	t.Run("accepts distinct exact and negated type guards", func(t *testing.T) {
		rules, err := expand(product(
			sources(
				op(instr.CONST_GET, typeFor[types.String]()),
				op(instr.CONST_GET, not(typeFor[types.String]())),
			),
			consumers(op(instr.DROP)),
		))
		require.NoError(t, err)
		require.NoError(t, validate(rules))
	})

	t.Run("rejects an unguarded pattern that shadows a guarded pattern", func(t *testing.T) {
		rules := []rule{
			{pattern: fuse(op(instr.CONST_GET), op(instr.DROP)).pattern},
			{pattern: fuse(op(instr.CONST_GET, typeFor[types.I32]()), op(instr.DROP)).pattern},
		}
		require.ErrorContains(t, validate(rules), "ambiguous")
	})

	t.Run("rejects overlapping negated guards", func(t *testing.T) {
		rules := []rule{
			{pattern: fuse(op(instr.CONST_GET, not(typeFor[types.String]())), op(instr.DROP)).pattern},
			{pattern: fuse(op(instr.CONST_GET, not(typeFor[types.I32]())), op(instr.DROP)).pattern},
		}
		require.ErrorContains(t, validate(rules), "ambiguous")
	})

	t.Run("rejects nested negation", func(t *testing.T) {
		rules, err := expand(fuse(op(instr.CONST_GET, not(not(typeFor[types.String]()))), op(instr.DROP)))
		require.NoError(t, err)
		require.Error(t, validate(rules))
	})

	t.Run("rejects guards on runtime-only sources", func(t *testing.T) {
		rules, err := expand(fuse(op(instr.LOCAL_GET, typeFor[types.String]()), op(instr.DROP)))
		require.NoError(t, err)
		require.Error(t, validate(rules))
	})

	t.Run("rejects multiple guards", func(t *testing.T) {
		rules, err := expand(fuse(op(instr.CONST_GET, typeFor[types.String](), typeFor[types.I32]()), op(instr.DROP)))
		require.NoError(t, err)
		require.Error(t, validate(rules))
	})

	t.Run("rejects unknown opcodes", func(t *testing.T) {
		require.ErrorContains(t, validate([]rule{{pattern: fuse(op(instr.Opcode(0xff)), op(instr.DROP)).pattern}}), "unsupported opcode")
	})

	t.Run("rejects patterns without a threaded renderer", func(t *testing.T) {
		require.ErrorContains(t, validate([]rule{{pattern: fuse(op(instr.LOCAL_GET), op(instr.CALL)).pattern}}), "unsupported threaded fusion")
	})

	t.Run("rejects ownership unsafe trailing operations", func(t *testing.T) {
		fusion := fuse(op(instr.REF_NULL), op(instr.DROP), op(instr.DROP))
		require.ErrorContains(t, validate([]rule{{pattern: fusion.pattern}}), "unsupported trailing operations")
	})

	t.Run("rejects fixed stack kind mismatches", func(t *testing.T) {
		rules, err := expand(fuse(op(instr.I32_CONST), op(instr.I64_ADD)))
		require.NoError(t, err)
		require.ErrorContains(t, validate(rules), "stack")
	})
}

func TestExpandAll(t *testing.T) {
	declarations := []declaration{
		fuse(op(instr.GLOBAL_GET), op(instr.DROP)),
		fuse(op(instr.LOCAL_GET), op(instr.DROP)),
	}
	forward, err := expandAll(declarations)
	require.NoError(t, err)
	reverse, err := expandAll([]declaration{declarations[1], declarations[0]})
	require.NoError(t, err)
	require.Equal(t, forward, reverse)
}

func TestRenderThreaded(t *testing.T) {
	data, err := renderThreaded([]rule{
		{pattern: fuse(op(instr.REF_NULL), op(instr.DROP)).pattern},
		{pattern: fuse(op(instr.DUP), op(instr.DROP)).pattern},
	})
	require.NoError(t, err)
	require.Contains(t, string(data), "goto l0")
	require.Contains(t, string(data), "l0:")
	require.NotContains(t, string(data), "candidate0")

	test, err := renderThreadedTest([]rule{
		{pattern: fuse(op(instr.I32_CONST), op(instr.I32_ADD)).pattern},
		{pattern: fuse(op(instr.REF_NULL), op(instr.DROP)).pattern},
	})
	require.NoError(t, err)
	require.Contains(t, string(test), "i32.const/i32.add")
	require.Contains(t, string(test), "ref.null/drop")
	require.Contains(t, string(test), ".Run(context.Background())")
	require.Contains(t, string(test), "WithTick(1)")
}

func TestRenderARM64(t *testing.T) {
	marked := rule{pattern: fuse(op(instr.REF_NULL), op(instr.DROP)).pattern, arm64: true}
	unmarked := rule{pattern: fuse(op(instr.DUP), op(instr.DROP)).pattern}

	data, err := renderARM64([]rule{marked, unmarked})
	require.NoError(t, err)
	require.Contains(t, string(data), "case uint16(instr.REF_NULL)<<8 | uint16(instr.DROP):")
	require.NotContains(t, string(data), "case uint16(instr.DUP)<<8 | uint16(instr.DROP):")
	require.Contains(t, string(data), "}\n\nfunc (l arm64Lowerer) match")
	require.Contains(t, string(data), "}\n\nfunc (l arm64Lowerer) adjacent")
}
