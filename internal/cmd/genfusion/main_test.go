package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Run("writes deterministic inline fusion sources", func(t *testing.T) {
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
		require.Contains(t, paths, "docs/fusion.md")

		rules, err := expand(declarations()...)
		require.NoError(t, err)
		arm64 := 0
		for _, rule := range rules {
			if rule.arm64 {
				arm64++
			}
		}
		var docs []byte
		for _, output := range first {
			if output.path == "docs/fusion.md" {
				docs = output.data
				break
			}
		}
		require.Contains(t, string(docs), fmt.Sprintf("| Total | %d | %d |", len(rules), arm64))
		require.NotContains(t, string(docs), "/Users/")
	})

	t.Run("checks generated files", func(t *testing.T) {
		t.Chdir(t.TempDir())

		var stdout bytes.Buffer
		require.NoError(t, run(false, &stdout))
		require.Contains(t, stdout.String(), "interp/fusion_gen.go")
		require.NoError(t, run(true, &stdout))

		path := filepath.Join("interp", "fusion_gen.go")
		require.NoError(t, os.WriteFile(path, []byte("stale"), 0o644))
		require.ErrorContains(t, run(true, &stdout), "is stale")
	})

	t.Run("expands every product combination", func(t *testing.T) {
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

	t.Run("preserves concrete type guards", func(t *testing.T) {
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

	t.Run("orders expanded rules deterministically", func(t *testing.T) {
		declarations := []declaration{
			fuse(op(instr.GLOBAL_GET), op(instr.DROP)),
			fuse(op(instr.LOCAL_GET), op(instr.DROP)),
		}
		forward, err := expand(declarations...)
		require.NoError(t, err)
		reverse, err := expand(declarations[1], declarations[0])
		require.NoError(t, err)
		require.Equal(t, forward, reverse)
	})

	t.Run("renders threaded fusion", func(t *testing.T) {
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
	})

	t.Run("renders declared ARM64 fusion", func(t *testing.T) {
		marked := rule{pattern: fuse(op(instr.REF_NULL), op(instr.DROP)).pattern, arm64: true}
		unmarked := rule{pattern: fuse(op(instr.DUP), op(instr.DROP)).pattern}

		data, err := renderARM64([]rule{marked, unmarked})
		require.NoError(t, err)
		require.Contains(t, string(data), "case uint16(instr.REF_NULL)<<8 | uint16(instr.DROP):")
		require.NotContains(t, string(data), "case uint16(instr.DUP)<<8 | uint16(instr.DROP):")
		require.Contains(t, string(data), "}\n\nfunc (l arm64Lowerer) match")
		require.Contains(t, string(data), "}\n\nfunc (l arm64Lowerer) adjacent")
	})

	t.Run("accepts declared stack effects", func(t *testing.T) {
		rules, err := expand(declarations()...)
		require.NoError(t, err)
		require.NoError(t, validate(rules))
	})

	t.Run("rejects unsupported ARM64 specialization", func(t *testing.T) {
		rules, err := expand(fuse(op(instr.I32_CONST), op(instr.I32_ADD)).withARM64())
		require.NoError(t, err)
		require.ErrorContains(t, validate(rules), "ARM64-marked fusion has no specialization")
	})

	t.Run("rejects duplicate concrete patterns", func(t *testing.T) {
		rules, err := expand(product(
			sources(op(instr.LOCAL_GET), op(instr.LOCAL_GET)),
			consumers(op(instr.DROP)),
		))
		require.NoError(t, err)
		require.Error(t, validate(rules))
	})

	t.Run("rejects variable-width opcodes", func(t *testing.T) {
		rules, err := expand(fuse(op(instr.BR_TABLE), op(instr.DROP)))
		require.NoError(t, err)
		require.Error(t, validate(rules))
	})

	t.Run("accepts disjoint type guards", func(t *testing.T) {
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

	t.Run("rejects shadowed type guards", func(t *testing.T) {
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

	t.Run("rejects runtime-only source guards", func(t *testing.T) {
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

	t.Run("rejects missing threaded renderers", func(t *testing.T) {
		require.ErrorContains(t, validate([]rule{{pattern: fuse(op(instr.LOCAL_GET), op(instr.CALL)).pattern}}), "unsupported threaded fusion")
	})

	t.Run("rejects unsupported trailing operations", func(t *testing.T) {
		fusion := fuse(op(instr.REF_NULL), op(instr.DROP), op(instr.REF_IS_NULL))
		require.ErrorContains(t, validate([]rule{{pattern: fusion.pattern}}), "unsupported trailing operations")
	})

	t.Run("rejects fixed stack kind mismatches", func(t *testing.T) {
		rules, err := expand(fuse(op(instr.I32_CONST), op(instr.I64_ADD)))
		require.NoError(t, err)
		require.ErrorContains(t, validate(rules), "i64.add has stack kind i32, want i64")
	})

	t.Run("rejects fixed stack delta mismatches", func(t *testing.T) {
		pattern := fuse(op(instr.I32_CONST), op(instr.I32_ADD)).pattern
		require.ErrorContains(t, validateStack(pattern, 1), "stack delta 0 (pop 1, push 1), want 1")
	})

	t.Run("accepts representation-compatible stack effects", func(t *testing.T) {
		rules, err := expand(fuse(op(instr.I32_EQZ), op(instr.BR_IF)))
		require.NoError(t, err)
		require.NoError(t, validate(rules))
	})
}
