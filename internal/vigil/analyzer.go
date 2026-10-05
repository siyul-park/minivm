package vigil

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

type graph struct {
	functions map[types.Object]*ast.FuncDecl
	calls     map[types.Object]map[types.Object]bool
	callers   map[types.Object]map[types.Object]bool
}

// Analyzer checks minivm coding and testing patterns.
var Analyzer = &analysis.Analyzer{
	Name: "check",
	Doc:  "checks minivm coding-patterns.md and testing.md contracts",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	report := pass.Report
	for _, rule := range rules {
		pass.Report = func(d analysis.Diagnostic) {
			d.Category = string(rule.severity)
			d.Message = "[" + rule.id + "] " + d.Message
			report(d)
		}
		rule.run(pass)
		pass.Report = report
	}
	return nil, nil
}

func collectGraph(pass *analysis.Pass) graph {
	set := graph{
		functions: make(map[types.Object]*ast.FuncDecl),
		calls:     make(map[types.Object]map[types.Object]bool),
		callers:   make(map[types.Object]map[types.Object]bool),
	}
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			obj := pass.TypesInfo.ObjectOf(fn.Name)
			if obj == nil {
				continue
			}
			set.functions[obj] = fn
			for _, target := range targets(pass, fn.Body) {
				if target == obj || target.Pkg() != pass.Pkg {
					continue
				}
				if set.calls[obj] == nil {
					set.calls[obj] = make(map[types.Object]bool)
				}
				set.calls[obj][target] = true
				if set.callers[target] == nil {
					set.callers[target] = make(map[types.Object]bool)
				}
				set.callers[target][obj] = true
			}
		}
	}
	return set
}

func call(body *ast.BlockStmt) *ast.CallExpr {
	if body == nil || len(body.List) != 1 {
		return nil
	}
	switch stmt := body.List[0].(type) {
	case *ast.ExprStmt:
		call, _ := stmt.X.(*ast.CallExpr)
		return call
	case *ast.ReturnStmt:
		if len(stmt.Results) != 1 {
			return nil
		}
		call, _ := stmt.Results[0].(*ast.CallExpr)
		return call
	default:
		return nil
	}
}

func object(pass *analysis.Pass, expr ast.Expr) types.Object {
	switch expr := expr.(type) {
	case *ast.Ident:
		return pass.TypesInfo.Uses[expr]
	case *ast.SelectorExpr:
		if selection := pass.TypesInfo.Selections[expr]; selection != nil {
			return selection.Obj()
		}
		return pass.TypesInfo.Uses[expr.Sel]
	default:
		return nil
	}
}

func unique(objects []types.Object, object types.Object) []types.Object {
	for _, existing := range objects {
		if existing == object {
			return objects
		}
	}
	return append(objects, object)
}

func targets(pass *analysis.Pass, body *ast.BlockStmt) []types.Object {
	var targets []types.Object
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if target := object(pass, call.Fun); target != nil {
			targets = unique(targets, target)
		}
		return true
	})
	return targets
}

func receiverType(fn *types.Func) *types.Named {
	if fn == nil {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return nil
	}
	typ := sig.Recv().Type()
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	named, _ := typ.(*types.Named)
	return named
}

func receiver(field *ast.FieldList) string {
	if field == nil || len(field.List) == 0 {
		return ""
	}
	expr := field.List[0].Type
	for {
		switch t := expr.(type) {
		case *ast.StarExpr:
			expr = t.X
		case *ast.IndexExpr:
			expr = t.X
		case *ast.IndexListExpr:
			expr = t.X
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

func test(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func report(pass *analysis.Pass, pos token.Pos, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	message = strings.TrimSpace(strings.TrimSuffix(message, "."))
	pass.Report(analysis.Diagnostic{
		Pos:     pos,
		Message: message,
	})
}
