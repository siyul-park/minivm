package vigil

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

type subtestVisitor struct {
	depth int
	found *bool
}

func (v subtestVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil || *v.found {
		return nil
	}
	call, ok := node.(*ast.CallExpr)
	if !ok || !subtest(call) {
		return v
	}
	if v.depth > 0 {
		*v.found = true
		return nil
	}
	return subtestVisitor{depth: v.depth + 1, found: v.found}
}

func testWrappers(pass *analysis.Pass) {
	callers := make(map[types.Object]map[types.Object]bool)
	functions := make(map[types.Object]*ast.FuncDecl)
	for _, file := range pass.Files {
		if !strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			obj := pass.TypesInfo.ObjectOf(fn.Name)
			if obj == nil {
				continue
			}
			if !test(fn.Name.Name) {
				functions[obj] = fn
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				target := object(pass, call.Fun)
				if target == nil || target.Exported() || target == obj {
					return true
				}
				if callers[target] == nil {
					callers[target] = make(map[types.Object]bool)
				}
				callers[target][obj] = true
				return true
			})
		}
	}
	for obj, fn := range functions {
		if len(callers[obj]) != 1 || !testForwarder(pass, fn) {
			continue
		}
		report(pass, fn.Name.Pos(), "test helper %s is a single-use forwarding wrapper; call the target public symbol directly", fn.Name.Name)
	}
}

func testForwarder(pass *analysis.Pass, fn *ast.FuncDecl) bool {
	call := call(fn.Body)
	if call == nil {
		return false
	}
	target := object(pass, call.Fun)
	if target == nil || !target.Exported() || target.Pkg() == nil {
		return false
	}
	base := strings.TrimSuffix(pass.Pkg.Path(), "_test")
	if target.Pkg().Path() != base {
		return false
	}
	sig, ok := pass.TypesInfo.ObjectOf(fn.Name).Type().(*types.Signature)
	if !ok {
		return false
	}
	args := make([]types.Object, 0, len(call.Args)+1)
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return false
		}
		args = append(args, pass.TypesInfo.Uses[ident])
	}
	for _, arg := range call.Args {
		ident, ok := arg.(*ast.Ident)
		if !ok {
			return false
		}
		args = append(args, pass.TypesInfo.Uses[ident])
	}
	if sig.Params().Len() != len(args) {
		return false
	}
	for i := range args {
		if args[i] != sig.Params().At(i) {
			return false
		}
	}
	return true
}

func nesting(pass *analysis.Pass, file *ast.File) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !test(fn.Name.Name) || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if ok && subtest(call) && nested(call) {
				report(pass, call.Pos(), "t.Run cases must not nest beyond one level")
			}
			return true
		})
	}
}

func polling(pass *analysis.Pass, file *ast.File) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !test(fn.Name.Name) || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if ok && eventually(call) && closes(pass, fn, call) {
				report(pass, call.Pos(), "readiness polling must not outlive a resource it observes")
			}
			return true
		})
	}
}

func private(pass *analysis.Pass, file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok {
			if obj := pass.TypesInfo.Uses[ident]; obj != nil && privateTarget(pass, obj) {
				report(pass, ident.Pos(), "tests must use the public target-package interface; private symbol %s is referenced", obj.Name())
			}
		}
		return true
	})
}

func closes(pass *analysis.Pass, fn *ast.FuncDecl, call *ast.CallExpr) bool {
	var condition *ast.FuncLit
	for _, arg := range call.Args {
		if literal, ok := arg.(*ast.FuncLit); ok {
			condition = literal
			break
		}
	}
	if condition == nil {
		return false
	}
	used := make(map[types.Object]bool)
	ast.Inspect(condition.Body, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if obj := pass.TypesInfo.Uses[ident]; obj != nil {
			used[obj] = true
		}
		return true
	})
	closed := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		nodeCall, ok := node.(*ast.CallExpr)
		if !ok || nodeCall.Pos() <= call.End() {
			return true
		}
		sel, ok := nodeCall.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Close" {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		obj := pass.TypesInfo.Uses[ident]
		if obj != nil && used[obj] {
			closed = true
			return false
		}
		return true
	})
	return closed
}

func subtest(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Run" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "t"
}

func nested(root *ast.CallExpr) bool {
	found := false
	visitor := subtestVisitor{found: &found}
	ast.Walk(visitor, root)
	return found
}

func privateTarget(pass *analysis.Pass, obj types.Object) bool {
	if obj.Pkg() == nil || obj.Pkg() != pass.Pkg || obj.Exported() {
		return false
	}
	file := pass.Fset.File(obj.Pos())
	return file != nil && !strings.HasSuffix(file.Name(), "_test.go")
}

func eventually(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Eventually" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && (id.Name == "require" || id.Name == "assert")
}
