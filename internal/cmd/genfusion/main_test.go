package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Run("writes deterministic threaded sources", func(t *testing.T) {
		first, err := generate()
		require.NoError(t, err)
		second, err := generate()
		require.NoError(t, err)
		require.Equal(t, first, second)

		paths := make([]string, len(first))
		for idx, output := range first {
			paths[idx] = output.path
		}
		require.Equal(t, []string{
			"interp/threaded.go",
			"interp/threaded_test.go",
		}, paths)
	})

	t.Run("checks generated files", func(t *testing.T) {
		t.Chdir(t.TempDir())

		var stdout bytes.Buffer
		require.NoError(t, run(false, &stdout))
		require.Contains(t, stdout.String(), "interp/threaded.go")
		require.NoError(t, run(true, &stdout))

		path := filepath.Join("interp", "threaded.go")
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
		require.NotContains(t, string(data), "func (c *threader) fusion")
		require.NotContains(t, string(data), "candidate0")

		test, err := renderFusionTest([]rule{
			{pattern: fuse(op(instr.I32_CONST), op(instr.I32_ADD)).pattern},
			{pattern: fuse(op(instr.REF_NULL), op(instr.DROP)).pattern},
		})
		require.NoError(t, err)
		require.Contains(t, string(test), "i32.const/i32.add")
		require.Contains(t, string(test), "ref.null/drop")
		require.Contains(t, string(test), ".Run(context.Background())")
		require.Contains(t, string(test), "WithTick(1)")
	})

	t.Run("accepts declared stack effects", func(t *testing.T) {
		rules, err := expand(declarations()...)
		require.NoError(t, err)
		require.NoError(t, validate(rules))
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

	t.Run("accepts representation-compatible stack effects", func(t *testing.T) {
		rules, err := expand(fuse(op(instr.I32_EQZ), op(instr.BR_IF)))
		require.NoError(t, err)
		require.NoError(t, validate(rules))
	})

	t.Run("maps every valid opcode exactly once", func(t *testing.T) {
		for value, lowering := range lowerings {
			op := instr.Opcode(value)
			if instr.Valid(op) {
				require.NotNil(t, lowering, instr.TypeOf(op).Mnemonic)
			} else {
				require.Nil(t, lowering)
			}
		}
	})

	t.Run("generates no interpreter helpers", func(t *testing.T) {
		outputs, err := generate()
		require.NoError(t, err)
		forbidden := map[string]struct{}{
			"arrayGetAt": {}, "branchIf": {}, "callHost": {}, "finish": {},
			"refGet": {}, "releaseArgs": {}, "resume": {}, "ret": {},
			"structGetAt": {}, "suspend": {}, "tail": {},
		}
		for _, output := range outputs {
			if output.path == "interp/threaded_test.go" {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), output.path, output.data, 0)
			require.NoError(t, err)
			ast.Inspect(file, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.FuncDecl:
					if node.Recv != nil {
						for _, field := range node.Recv.List {
							if star, ok := field.Type.(*ast.StarExpr); ok {
								if name, ok := star.X.(*ast.Ident); ok {
									require.NotEqual(t, "Interpreter", name.Name, output.path+":"+node.Name.Name)
								}
							}
						}
					}
				case *ast.CallExpr:
					selector, ok := node.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					receiver, ok := selector.X.(*ast.Ident)
					if ok && receiver.Name == "i" {
						_, found := forbidden[selector.Sel.Name]
						require.False(t, found, output.path+":"+selector.Sel.Name)
					}
				}
				return true
			})
		}
	})

}
