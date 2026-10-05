package vigil

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

func checkTestHelpers(pass *analysis.Pass) {
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
			if !isTestFunction(fn.Name.Name) {
				functions[obj] = fn
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				target := calledObject(pass, call)
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
		if len(callers[obj]) != 1 || !isTestForwarder(pass, fn) {
			continue
		}
		report(pass, fn.Name.Pos(), "test helper %s is a single-use forwarding wrapper; call the target public symbol directly", fn.Name.Name)
	}
}

func isTestForwarder(pass *analysis.Pass, fn *ast.FuncDecl) bool {
	call := wrapperCall(fn.Body)
	if call == nil {
		return false
	}
	target := calledObject(pass, call)
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

func inlineableWrapper(pass *analysis.Pass, callers map[types.Object]map[types.Object]bool, body *ast.BlockStmt) bool {
	call := wrapperCall(body)
	if call == nil {
		return false
	}
	target := calledObject(pass, call)
	return target != nil && !target.Exported() && len(callers[target]) == 1
}

func checkTestNesting(pass *analysis.Pass, file *ast.File) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !isTestFunction(fn.Name.Name) || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if ok && isTestRun(call) && nestedRun(call) {
				report(pass, call.Pos(), "t.Run cases must not nest beyond one level")
			}
			return true
		})
	}
}

func checkTestPolling(pass *analysis.Pass, file *ast.File) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !isTestFunction(fn.Name.Name) || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if ok && isEventually(call) && eventuallyClosesResource(pass, fn, call) {
				report(pass, call.Pos(), "Eventually must not poll a resource that the case closes; use the package poll helper on the test goroutine")
			}
			return true
		})
	}
}

func checkTestPrivate(pass *analysis.Pass, file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok {
			if obj := pass.TypesInfo.Uses[ident]; obj != nil && isPrivateTarget(pass, obj) {
				report(pass, ident.Pos(), "tests must use the public target-package interface; private symbol %s is referenced", obj.Name())
			}
		}
		return true
	})
}

func eventuallyClosesResource(pass *analysis.Pass, fn *ast.FuncDecl, call *ast.CallExpr) bool {
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

func isPrivateTarget(pass *analysis.Pass, obj types.Object) bool {
	if obj.Pkg() == nil || obj.Pkg() != pass.Pkg || obj.Exported() {
		return false
	}
	file := pass.Fset.File(obj.Pos())
	return file != nil && !strings.HasSuffix(file.Name(), "_test.go")
}

func isTestRun(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Run" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "t"
}

func nestedRun(root *ast.CallExpr) bool {
	depth := 0
	found := false
	ast.Inspect(root, func(node ast.Node) bool {
		if found {
			return false
		}
		if call, ok := node.(*ast.CallExpr); ok && isTestRun(call) {
			depth++
			if depth > 1 {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func isEventually(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Eventually" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && (id.Name == "require" || id.Name == "assert")
}
