package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/jennifer/jen"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestGenerate(t *testing.T) {
	t.Run("preserves standalone i64 consumption", func(t *testing.T) {
		file := jen.NewFile("review")
		file.Var().Id("handler").Op("=").Add(lower(instr.I64_ADD))
		source := file.GoString()
		require.Equal(t, 2, strings.Count(source, ".unboxI64("))
		require.NotContains(t, source, ".borrowI64(")
	})
	t.Run("checks generated files", func(t *testing.T) {
		t.Chdir(t.TempDir())

		generated := output{path: filepath.Join("interp", "threaded.go"), data: []byte("generated")}
		var stdout bytes.Buffer
		require.NoError(t, generated.sync(false, &stdout))
		require.Equal(t, "interp/threaded.go\n", stdout.String())
		require.NoError(t, generated.sync(true, &stdout))

		require.NoError(t, os.WriteFile(generated.path, []byte("stale"), 0o644))
		require.ErrorContains(t, generated.sync(true, &stdout), "is stale")
	})
	t.Run("crosses every pattern", func(t *testing.T) {
		got := cross(
			[]pattern{op(instr.LOCAL_GET), op(instr.GLOBAL_GET)},
			op(instr.DROP),
			seq(op(instr.REF_IS_NULL), op(instr.BR_IF)),
		)
		want := []pattern{
			seq(op(instr.LOCAL_GET), op(instr.DROP)),
			seq(op(instr.LOCAL_GET), op(instr.REF_IS_NULL), op(instr.BR_IF)),
			seq(op(instr.GLOBAL_GET), op(instr.DROP)),
			seq(op(instr.GLOBAL_GET), op(instr.REF_IS_NULL), op(instr.BR_IF)),
		}
		require.ElementsMatch(t, want, got)
	})

	t.Run("preserves type guards", func(t *testing.T) {
		got := cross(
			[]pattern{constant[types.String](), except[types.String]()},
			op(instr.DROP),
		)
		want := []pattern{
			seq(constant[types.String](), op(instr.DROP)),
			seq(except[types.String](), op(instr.DROP)),
		}
		require.ElementsMatch(t, want, got)
	})

	t.Run("orders the catalog deterministically", func(t *testing.T) {
		require.Equal(t, catalog(), catalog())
	})

	t.Run("renders compose tables", func(t *testing.T) {
		patterns := []pattern{
			seq(op(instr.REF_NULL), op(instr.DROP)),
			seq(op(instr.DUP), op(instr.DROP)),
		}
		data, err := render(patterns)
		require.NoError(t, err)
		require.Contains(t, string(data), "goto l0")
		require.Contains(t, string(data), "l0:")
		require.NotContains(t, string(data), "func (c *threader) compose")
		require.NotContains(t, string(data), "candidate0")

	})

	t.Run("advances fusion by the first opcode width", func(t *testing.T) {
		pattern := seq(op(instr.LOCAL_GET), op(instr.REF_IS_NULL), op(instr.BR_IF))
		body, err := compose(pattern, pattern.width(), "")
		require.NoError(t, err)
		file := jen.NewFile("review")
		file.Func().Id("render").Params().Block(body...)
		require.Contains(t, file.GoString(), "c.ip += 2")
	})

	t.Run("uses ref representation for drop fusion", func(t *testing.T) {
		pattern := seq(op(instr.LOCAL_GET), op(instr.DROP))
		body, err := compose(pattern, pattern.width(), "")
		require.NoError(t, err)
		file := jen.NewFile("review")
		file.Func().Id("render").Params().Block(body...)
		source := file.GoString()
		require.Contains(t, source, "types.KindRef")
		require.NotContains(t, source, "types.KindI32")
	})

	t.Run("accepts the catalog", func(t *testing.T) {
		require.NoError(t, validate(catalog()))
	})

	t.Run("rejects duplicate patterns", func(t *testing.T) {
		duplicate := seq(op(instr.LOCAL_GET), op(instr.DROP))
		require.ErrorContains(t, validate([]pattern{duplicate, duplicate}), "duplicate")
	})

	t.Run("rejects variable-width opcodes", func(t *testing.T) {
		require.ErrorContains(t, validate([]pattern{seq(op(instr.BR_TABLE), op(instr.DROP))}), "variable-width")
	})

	t.Run("accepts disjoint guards", func(t *testing.T) {
		require.NoError(t, validate([]pattern{
			seq(constant[types.String](), op(instr.DROP)),
			seq(except[types.String](), op(instr.DROP)),
		}))
	})

	t.Run("rejects shadowed guards", func(t *testing.T) {
		require.ErrorContains(t, validate([]pattern{
			seq(op(instr.CONST_GET), op(instr.DROP)),
			seq(constant[types.I32](), op(instr.DROP)),
		}), "ambiguous")
	})

	t.Run("rejects overlapping exclusions", func(t *testing.T) {
		require.ErrorContains(t, validate([]pattern{
			seq(except[types.String](), op(instr.DROP)),
			seq(except[types.I32](), op(instr.DROP)),
		}), "ambiguous")
	})

	t.Run("rejects runtime guards", func(t *testing.T) {
		guarded := constant[types.String]()
		guarded[0].op = instr.LOCAL_GET
		require.ErrorContains(t, validate([]pattern{seq(guarded, op(instr.DROP))}), "cannot resolve")
	})

	t.Run("rejects unknown opcodes", func(t *testing.T) {
		require.ErrorContains(t, validate([]pattern{seq(op(instr.Opcode(0xff)), op(instr.DROP))}), "unsupported opcode")
	})

	t.Run("rejects missing lowerers", func(t *testing.T) {
		require.ErrorContains(t, validate([]pattern{seq(op(instr.LOCAL_GET), op(instr.CALL))}), "unsupported fusion")
	})

	t.Run("rejects unresolved trailing operations", func(t *testing.T) {
		require.ErrorContains(t, validate([]pattern{
			seq(op(instr.REF_NULL), op(instr.DROP), op(instr.REF_IS_NULL)),
		}), "unsupported fusion")
	})

	t.Run("rejects stack kind mismatches", func(t *testing.T) {
		require.ErrorContains(t, validate([]pattern{
			seq(op(instr.I32_CONST), op(instr.I64_ADD)),
		}), "i64.add has stack kind i32, want i64")
	})

	t.Run("accepts compatible stack representations", func(t *testing.T) {
		require.NoError(t, validate([]pattern{
			seq(op(instr.I32_EQZ), op(instr.BR_IF)),
		}))
	})

	t.Run("rejects stack mismatches after dynamic effects", func(t *testing.T) {
		require.ErrorContains(t, validate([]pattern{seq(
			op(instr.I32_CONST),
			op(instr.CALL),
			op(instr.I64_ADD),
		)}), "i64.add has stack kind i32, want i64")
	})

	t.Run("maps every opcode once", func(t *testing.T) {
		for code, emit := range lowerers {
			op := instr.Opcode(code)
			if instr.Valid(op) {
				require.NotNil(t, emit, instr.TypeOf(op).Mnemonic)
			} else {
				require.Nil(t, emit)
			}
		}
	})

	t.Run("generates no interpreter helpers", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "interp", "threaded.go"))
		require.NoError(t, err)
		forbidden := map[string]struct{}{
			"arrayGetAt": {}, "branchIf": {}, "callHost": {}, "finish": {},
			"refGet": {}, "releaseArgs": {}, "resume": {}, "ret": {},
			"structGetAt": {}, "suspend": {}, "tail": {},
		}
		file, err := parser.ParseFile(token.NewFileSet(), "interp/threaded.go", data, 0)
		require.NoError(t, err)
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.FuncDecl:
				if node.Recv != nil {
					for _, field := range node.Recv.List {
						if star, ok := field.Type.(*ast.StarExpr); ok {
							if name, ok := star.X.(*ast.Ident); ok {
								require.NotEqual(t, "Interpreter", name.Name, "interp/threaded.go:"+node.Name.Name)
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
					require.False(t, found, "interp/threaded.go:"+selector.Sel.Name)
				}
			}
			return true
		})
	})
}
