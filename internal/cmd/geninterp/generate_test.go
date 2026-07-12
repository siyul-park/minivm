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

func TestGenerate(t *testing.T) {
	t.Run("writes deterministic sources", func(t *testing.T) {
		first, err := generate()
		require.NoError(t, err)
		second, err := generate()
		require.NoError(t, err)
		require.Equal(t, first, second)

		paths := make([]string, len(first))
		for index, output := range first {
			paths[index] = output.path
		}
		require.Equal(t, []string{"interp/threaded.go"}, paths)

		file, err := parser.ParseFile(token.NewFileSet(), first[0].path, first[0].data, 0)
		require.NoError(t, err)
		var declarations []string
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.GenDecl:
				for _, specification := range declaration.Specs {
					switch specification := specification.(type) {
					case *ast.TypeSpec:
						declarations = append(declarations, specification.Name.Name)
					case *ast.ValueSpec:
						for _, name := range specification.Names {
							declarations = append(declarations, name.Name)
						}
					}
				}
			case *ast.FuncDecl:
				declarations = append(declarations, declaration.Name.Name)
			}
		}
		require.Equal(t, []string{"threader", "threaded", "fusions", "invalid", "init", "Compile"}, declarations)
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
		require.ErrorContains(t, validate([]pattern{seq(op(instr.LOCAL_GET), op(instr.CALL))}), "unsupported fusion")
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

	t.Run("uses unified lowering architecture", func(t *testing.T) {
		file, err := parser.ParseFile(token.NewFileSet(), "lower.go", nil, 0)
		require.NoError(t, err)

		types := make(map[string]struct{})
		values := make(map[string]struct{})
		functions := make(map[string]struct{})
		mappings := make(map[string]string)
		calls := make(map[string]int)
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.TypeSpec:
				types[node.Name.Name] = struct{}{}
			case *ast.ValueSpec:
				for index, name := range node.Names {
					values[name.Name] = struct{}{}
					if name.Name != "lowerers" || index >= len(node.Values) {
						continue
					}
					literal, ok := node.Values[index].(*ast.CompositeLit)
					if !ok {
						continue
					}
					for _, element := range literal.Elts {
						entry, ok := element.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						op, ok := entry.Key.(*ast.SelectorExpr)
						if !ok {
							continue
						}
						switch value := entry.Value.(type) {
						case *ast.Ident:
							mappings[op.Sel.Name] = value.Name
						case *ast.CallExpr:
							fn, ok := value.Fun.(*ast.Ident)
							if !ok || len(value.Args) != 1 {
								continue
							}
							arg, ok := value.Args[0].(*ast.Ident)
							if ok {
								mappings[op.Sel.Name] = fn.Name + "(" + arg.Name + ")"
							}
						}
					}
				}
			case *ast.FuncDecl:
				functions[node.Name.Name] = struct{}{}
			case *ast.CallExpr:
				if fn, ok := node.Fun.(*ast.Ident); ok {
					calls[fn.Name]++
				}
			}
			return true
		})

		require.Contains(t, types, "lowering")
		require.Contains(t, values, "lowerers")
		require.Contains(t, functions, "compose")
		require.Contains(t, functions, "prepare")
		require.Equal(t, 1, calls["sourceAccess"])

		expected := map[string]string{
			"BR_IF":       "branchLower",
			"CALL":        "callLower",
			"CLOSURE_NEW": "callLower",
			"CONST_GET":   "sourceLower",
			"DROP":        "refLower",
			"DUP":         "refSource",
			"F32_CONST":   "sourceLower",
			"F64_CONST":   "sourceLower",
			"GLOBAL_GET":  "sourceLower",
			"I32_CONST":   "sourceLower",
			"I64_CONST":   "sourceLower",
			"LOCAL_GET":   "sourceLower",
			"REF_IS_NULL": "refLower",
			"REF_NULL":    "refSource",
			"RETURN_CALL": "callLower",
			"STRUCT_GET":  "indexLower",
			"ARRAY_GET":   "indexLower",
			"UPVAL_GET":   "sourceLower",
		}
		for _, family := range families {
			for _, op := range append(family.binary, family.compare...) {
				expected[symbol(op)] = "numericLower"
			}
		}
		expected["I32_EQZ"] = "numericLower"
		expected["I64_EQZ"] = "numericLower"
		for op, lower := range expected {
			require.Equal(t, lower, mappings[op], op)
		}

		for _, name := range []string{
			"fusion", "call", "reference", "index", "arithmetic", "integer", "operand",
			"callSequence", "composeCall", "composeDirectBranch", "composeIndex",
			"composeNumeric", "composeRef", "indexSequence", "refSequence",
		} {
			require.NotContains(t, functions, name)
		}
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
