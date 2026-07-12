package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/dave/jennifer/jen"
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

	t.Run("preserves standalone i64 consumption", func(t *testing.T) {
		outputs, err := generate()
		require.NoError(t, err)
		file, err := parser.ParseFile(token.NewFileSet(), outputs[0].path, outputs[0].data, 0)
		require.NoError(t, err)

		var handler ast.Expr
		ast.Inspect(file, func(node ast.Node) bool {
			specification, ok := node.(*ast.ValueSpec)
			if !ok || len(specification.Names) != 1 || specification.Names[0].Name != "threaded" || len(specification.Values) != 1 {
				return true
			}
			literal, ok := specification.Values[0].(*ast.CompositeLit)
			if !ok {
				return false
			}
			for _, element := range literal.Elts {
				entry, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				opcode, ok := entry.Key.(*ast.SelectorExpr)
				if ok && opcode.Sel.Name == "I64_ADD" {
					handler = entry.Value
					return false
				}
			}
			return false
		})
		require.NotNil(t, handler)

		unbox := 0
		borrow := 0
		ast.Inspect(handler, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "unboxI64":
				unbox++
			case "borrowI64":
				borrow++
			}
			return true
		})
		require.Equal(t, 2, unbox)
		require.Zero(t, borrow)
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

	t.Run("uses role based lowering names", func(t *testing.T) {
		types := make(map[string]struct{})
		values := make(map[string]struct{})
		functions := make(map[string]*ast.FuncDecl)
		for _, path := range []string{"pattern.go", "lower.go", "validate.go"} {
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			require.NoError(t, err)
			ast.Inspect(file, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.TypeSpec:
					types[node.Name.Name] = struct{}{}
				case *ast.ValueSpec:
					for _, name := range node.Names {
						values[name.Name] = struct{}{}
					}
				case *ast.FuncDecl:
					functions[node.Name.Name] = node
				}
				return true
			})
		}

		for _, name := range []string{"match", "step", "value", "state", "target", "loader", "lowerer"} {
			require.Contains(t, types, name)
		}
		require.Contains(t, values, "lowerers")
		for _, name := range []string{
			"compose", "lower", "resolve", "lowerSource", "lowerRef", "lowerIndex",
			"lowerCall", "lowerNumeric", "lowerBranch", "load", "materialize",
			"dynamicCall", "branch", "lookup", "numeric", "checked", "validateStack",
		} {
			require.Contains(t, functions, name)
		}
		for _, name := range []string{
			"fact", "lowering", "loweringState", "callTarget", "sourceContext",
		} {
			require.NotContains(t, types, name)
		}
		for _, name := range []string{
			"standalone", "prepare", "sourceLower", "refSource", "refLower", "indexLower",
			"callLower", "numericLower", "branchLower", "sourceAccess", "sourcePush",
			"dynamicCallCode", "branchBody", "indexCode", "numericCode",
			"trappedNumericCode", "validateEffect",
		} {
			require.NotContains(t, functions, name)
		}
		for _, name := range []string{"compose", "lower"} {
			fn := functions[name]
			require.NotNil(t, fn)
			uses := false
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				index, ok := node.(*ast.IndexExpr)
				if !ok {
					return true
				}
				identifier, ok := index.X.(*ast.Ident)
				if ok && identifier.Name == "lowerers" {
					uses = true
				}
				return true
			})
			require.True(t, uses, name)
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
