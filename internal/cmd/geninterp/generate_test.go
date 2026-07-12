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

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestGenerate(t *testing.T) {
	t.Run("writes deterministic sources", func(t *testing.T) {
		first, err := generate()
		require.NoError(t, err)
		second, err := generate()
		require.NoError(t, err)
		require.Equal(t, first, second)

		paths := make([]string, len(first))
		for indexSequence, output := range first {
			paths[indexSequence] = output.path
		}
		require.Equal(t, []string{"interp/threaded.go"}, paths)
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
		data, err := source(patterns)
		require.NoError(t, err)
		require.Contains(t, string(data), "goto l0")
		require.Contains(t, string(data), "l0:")
		require.NotContains(t, string(data), "func (c *threader) compose")
		require.NotContains(t, string(data), "candidate0")

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
		require.ErrorContains(t, validate([]pattern{seq(op(instr.LOCAL_GET), op(instr.CALL))}), "unsupported compose")
	})

	t.Run("rejects unsupported trailing operations", func(t *testing.T) {
		require.ErrorContains(t, validate([]pattern{
			seq(op(instr.REF_NULL), op(instr.DROP), op(instr.REF_IS_NULL)),
		}), "unsupported trailing operations")
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

	t.Run("uses one opcode lowering path", func(t *testing.T) {
		file, err := parser.ParseFile(token.NewFileSet(), "lower.go", nil, 0)
		require.NoError(t, err)

		types := make(map[string]struct{})
		values := make(map[string]struct{})
		functions := make(map[string]struct{})
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.GenDecl:
				for _, specification := range declaration.Specs {
					switch specification := specification.(type) {
					case *ast.TypeSpec:
						types[specification.Name.Name] = struct{}{}
					case *ast.ValueSpec:
						for _, name := range specification.Names {
							values[name.Name] = struct{}{}
						}
					}
				}
			case *ast.FuncDecl:
				functions[declaration.Name.Name] = struct{}{}
			}
		}

		require.Contains(t, types, "lowering")
		require.Contains(t, values, "lowerers")
		require.Contains(t, functions, "compose")
		for _, name := range []string{"fusion", "call", "reference", "index", "arithmetic", "integer", "operand"} {
			require.NotContains(t, functions, name)
		}
	})

	t.Run("shares numeric lowerings", func(t *testing.T) {
		data, err := os.ReadFile("lower.go")
		require.NoError(t, err)
		source := string(data)
		for _, mapping := range []string{
			"instr.I32_ADD:             numericLower",
			"instr.I64_EQZ:             numericLower",
			"instr.F64_GE:              numericLower",
		} {
			require.Contains(t, source, mapping)
		}
		for _, function := range []string{"func i32Add(", "func i64Eqz(", "func f64Ge("} {
			require.NotContains(t, source, function)
		}
	})

	t.Run("shares source lowerings", func(t *testing.T) {
		data, err := os.ReadFile("lower.go")
		require.NoError(t, err)
		source := string(data)
		for _, opcode := range []string{
			"CONST_GET", "F32_CONST", "F64_CONST", "GLOBAL_GET",
			"I32_CONST", "I64_CONST", "LOCAL_GET", "UPVAL_GET",
		} {
			require.Contains(t, source, "instr."+opcode+":")
		}
		for _, function := range []string{
			"func constGet(", "func f32Const(", "func f64Const(",
			"func globalGet(", "func i32Const(", "func i64Const(",
			"func localGet(", "func upvalGet(",
		} {
			require.NotContains(t, source, function)
		}
		require.GreaterOrEqual(t, strings.Count(source, "sourceLower,"), 8)
	})

	t.Run("composes numeric sources through lowerers", func(t *testing.T) {
		data, err := os.ReadFile("lower.go")
		require.NoError(t, err)
		require.Equal(t, 2, strings.Count(string(data), "sourceAccess("))
	})

	t.Run("maps every opcode once", func(t *testing.T) {
		for value, lowering := range lowerers {
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
