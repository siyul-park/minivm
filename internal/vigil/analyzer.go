package vigil

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer checks minivm coding and testing patterns.
var Analyzer = &analysis.Analyzer{
	Name: "check",
	Doc:  "checks minivm coding-patterns.md and testing.md contracts",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	runRules(pass)
	return nil, nil
}

func checkDocs(pass *analysis.Pass, file *ast.File) {
	if strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
		return
	}
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			if isTestFunction(decl.Name.Name) || !decl.Name.IsExported() || decl.Doc != nil {
				continue
			}
			report(pass, decl.Name.Pos(), "exported symbol %s must have a doc comment", decl.Name.Name)
		case *ast.GenDecl:
			switch decl.Tok {
			case token.TYPE:
				for _, spec := range decl.Specs {
					typ := spec.(*ast.TypeSpec)
					if typ.Name.IsExported() && typ.Doc == nil && decl.Doc == nil {
						report(pass, typ.Name.Pos(), "exported symbol %s must have a doc comment", typ.Name.Name)
					}
				}
			case token.CONST, token.VAR:
				for _, spec := range decl.Specs {
					value := spec.(*ast.ValueSpec)
					for _, name := range value.Names {
						if name.IsExported() && value.Doc == nil && decl.Doc == nil {
							report(pass, name.Pos(), "exported symbol %s must have a doc comment", name.Name)
						}
					}
				}
			}
		}
	}
}

func checkDeclarations(pass *analysis.Pass, file *ast.File) {
	last := -1
	var lastDecl ast.Decl
	for _, decl := range file.Decls {
		group := declarationGroup(decl)
		if group < 0 {
			continue
		}
		if group < last {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "init" {
				report(pass, decl.Pos(), "init must appear immediately after package-level declarations")
				continue
			}
			report(pass, decl.Pos(), "declaration %s follows %s in the wrong file-order group", declarationName(decl), declarationName(lastDecl))
		}
		last = group
		lastDecl = decl
	}
}

func declarationGroup(decl ast.Decl) int {
	if fn, ok := decl.(*ast.FuncDecl); ok {
		return functionGroup(fn)
	}
	gen, ok := decl.(*ast.GenDecl)
	if !ok {
		return -1
	}
	switch gen.Tok {
	case token.TYPE:
		if allSpecsUnexportedType(gen.Specs) {
			return 1
		}
		return 0
	case token.CONST:
		if allSpecsUnexported(gen.Specs) {
			return 3
		}
		return 2
	case token.VAR:
		return 4
	default:
		return -1
	}
}

func functionGroup(fn *ast.FuncDecl) int {
	if isTestFunction(fn.Name.Name) {
		return -1
	}
	if fn.Recv != nil {
		if !fn.Name.IsExported() {
			return 11
		}
		if isHook(fn.Name.Name) {
			return 10
		}
		if isConstructor(fn.Name.Name) {
			return 8
		}
		return 9
	}
	if fn.Name.Name == "init" {
		return 6
	}
	if !fn.Name.IsExported() {
		return 11
	}
	if isConstructor(fn.Name.Name) {
		return 8
	}
	return 7
}

func allSpecsUnexportedType(specs []ast.Spec) bool {
	for _, spec := range specs {
		typ, ok := spec.(*ast.TypeSpec)
		if !ok || typ.Name.IsExported() {
			return false
		}
	}
	return true
}

func isConstructor(name string) bool {
	return name == "New" || strings.HasPrefix(name, "New")
}

func isHook(name string) bool {
	switch name {
	case "Cast", "Equals", "Kind", "Type", "String", "Refs", "Marshal", "Unmarshal", "Error", "Unwrap":
		return true
	default:
		return false
	}
}

func allSpecsUnexported(specs []ast.Spec) bool {
	for _, spec := range specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok || len(vs.Names) == 0 || vs.Names[0].IsExported() {
			return false
		}
	}
	return true
}

func declarationName(decl ast.Decl) string {
	switch decl := decl.(type) {
	case *ast.FuncDecl:
		return decl.Name.Name
	case *ast.GenDecl:
		return decl.Tok.String()
	default:
		return "declaration"
	}
}

func checkDependencyOrder(pass *analysis.Pass, file *ast.File) {
	decls := make(map[types.Object]*ast.FuncDecl)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		obj := pass.TypesInfo.ObjectOf(fn.Name)
		if obj != nil {
			decls[obj] = fn
		}
	}

	deps := make(map[types.Object][]types.Object)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		caller := pass.TypesInfo.ObjectOf(fn.Name)
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
			ident, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			dependency := pass.TypesInfo.Uses[ident]
			dependencyDecl := decls[dependency]
			if dependencyDecl == nil || dependency == caller ||
				declarationGroup(fn) != declarationGroup(dependencyDecl) {
				return true
			}
			deps[caller] = appendUnique(deps[caller], dependency)
			return true
		})
	}

	for caller, dependencies := range deps {
		fn := decls[caller]
		for _, dependency := range dependencies {
			dependencyDecl := decls[dependency]
			if dependencyDecl == nil || dependencyDecl.Pos() >= fn.Pos() {
				continue
			}
			if reaches(deps, dependency, caller) {
				continue
			}
			report(pass, fn.Name.Pos(),
				"dependent %s follows dependency %s; dependents must be declared before their dependencies",
				fn.Name.Name, dependencyDecl.Name.Name)
		}
	}
}

func appendUnique(objects []types.Object, object types.Object) []types.Object {
	for _, existing := range objects {
		if existing == object {
			return objects
		}
	}
	return append(objects, object)
}

func reaches(graph map[types.Object][]types.Object, start, target types.Object) bool {
	seen := make(map[types.Object]bool)
	var visit func(types.Object) bool
	visit = func(object types.Object) bool {
		if object == target {
			return true
		}
		if seen[object] {
			return false
		}
		seen[object] = true
		for _, next := range graph[object] {
			if visit(next) {
				return true
			}
		}
		return false
	}
	return visit(start)
}

func checkHelpers(pass *analysis.Pass) {
	callers := make(map[types.Object]map[types.Object]bool)
	calls := make(map[types.Object]int)
	documented := make(map[types.Object]bool)
	functions := make(map[types.Object]*ast.FuncDecl)
	var current types.Object
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			current = pass.TypesInfo.ObjectOf(fn.Name)
			if current != nil {
				functions[current] = fn
			}
			if current != nil && fn.Doc != nil {
				documented[current] = true
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || current == nil {
					return true
				}
				target := calledObject(pass, call)
				if target == nil || target.Exported() || target == current {
					return true
				}
				calls[target]++
				if callers[target] == nil {
					callers[target] = make(map[types.Object]bool)
				}
				callers[target][current] = true
				return true
			})
		}
	}
	for ident, obj := range pass.TypesInfo.Defs {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Exported() {
			continue
		}
		if documented[obj] {
			continue
		}
		if calls[obj] == 1 {
			fn := functions[obj]
			if fn == nil || len(callers[obj]) != 1 {
				continue
			}
			if inlineableWrapper(pass, callers, fn.Body) {
				report(pass, ident.Pos(),
					"private helper %s is a single-use forwarding wrapper; inline it",
					ident.Name)
			}
		}
	}
}

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

func wrapperCall(body *ast.BlockStmt) *ast.CallExpr {
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

func calledObject(pass *analysis.Pass, call *ast.CallExpr) types.Object {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return pass.TypesInfo.Uses[fun]
	case *ast.SelectorExpr:
		if selection := pass.TypesInfo.Selections[fun]; selection != nil {
			return selection.Obj()
		}
		return pass.TypesInfo.Uses[fun.Sel]
	default:
		return nil
	}
}

func checkContext(pass *analysis.Pass, file *ast.File) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type.Params == nil || len(fn.Type.Params.List) < 2 {
			continue
		}
		for _, field := range fn.Type.Params.List[1:] {
			if isContext(pass, field.Type) {
				report(pass, field.Pos(), "context.Context must be the first parameter of %s", fn.Name.Name)
				break
			}
		}
	}
}

func isContext(pass *analysis.Pass, expr ast.Expr) bool {
	typ := pass.TypesInfo.TypeOf(expr)
	if typ == nil {
		return false
	}
	named, ok := typ.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "context" && obj.Name() == "Context"
}

func checkErrors(pass *analysis.Pass, file *ast.File) {
	if strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
		return
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			target, ok := calledObject(pass, call).(*types.Func)
			if !ok || target.Pkg() == nil || target.Pkg().Path() != "fmt" || target.Name() != "Errorf" || len(call.Args) < 2 {
				return true
			}
			value := pass.TypesInfo.Types[call.Args[0]].Value
			if value == nil || value.Kind() != constant.String {
				return true
			}
			format := constant.StringVal(value)
			if !hasVerb(format, 'v') || hasVerb(format, 'w') {
				return true
			}
			errorType := types.Universe.Lookup("error").Type()
			for _, arg := range call.Args[1:] {
				typ := pass.TypesInfo.TypeOf(arg)
				if typ != nil && (types.AssignableTo(typ, errorType) || types.Implements(typ, errorType.Underlying().(*types.Interface))) {
					report(pass, call.Pos(), "fmt.Errorf formats an error with %%v; use %%w when preserving dependency identity")
					break
				}
			}
			return true
		})
	}
}

func hasVerb(format string, want byte) bool {
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 >= len(format) {
			continue
		}
		i++
		if format[i] == '%' {
			continue
		}
		for i < len(format) && !((format[i] >= 'a' && format[i] <= 'z') || (format[i] >= 'A' && format[i] <= 'Z')) {
			i++
		}
		if i < len(format) && format[i] == want {
			return true
		}
	}
	return false
}

func checkPrivateBoundary(pass *analysis.Pass) {
	fields := privateFields(pass)
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			current, _ := pass.TypesInfo.ObjectOf(fn.Name).(*types.Func)
			currentOwner := receiverNamed(current)
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.SelectorExpr:
					checkPrivateAccess(pass, fields, current, currentOwner, selectorObject(pass, node), node.Sel.Pos())
				case *ast.KeyValueExpr:
					ident, ok := node.Key.(*ast.Ident)
					if ok {
						checkPrivateAccess(pass, fields, current, currentOwner, pass.TypesInfo.Uses[ident], ident.Pos())
					}
				}
				return true
			})
		}
	}
}

func checkPrivateAccess(pass *analysis.Pass, fields map[types.Object]*types.Named, current *types.Func, currentOwner *types.Named, obj types.Object, pos token.Pos) {
	if obj == nil || obj.Exported() || obj.Pkg() != pass.Pkg {
		return
	}
	owner := fields[obj]
	if owner == nil {
		if fnObj, ok := obj.(*types.Func); ok {
			owner = receiverNamed(fnObj)
		}
	}
	if owner == nil || !owner.Obj().Exported() {
		return
	}
	if currentOwner != nil && types.Identical(currentOwner, owner) {
		return
	}
	if currentOwner != nil && !currentOwner.Obj().Exported() && current != nil && !current.Exported() {
		return
	}
	if currentOwner == nil && current != nil && strings.HasPrefix(current.Name(), "New") {
		sig, ok := current.Type().(*types.Signature)
		if ok && sig.Results() != nil {
			for index := 0; index < sig.Results().Len(); index++ {
				result := sig.Results().At(index).Type()
				if ptr, ok := result.(*types.Pointer); ok {
					result = ptr.Elem()
				}
				named, ok := result.(*types.Named)
				if ok && types.Identical(named, owner) {
					return
				}
			}
		}
	}
	report(pass, pos, "private member %s of public owner %s must be accessed through its owner", obj.Name(), owner.Obj().Name())
}

func privateFields(pass *analysis.Pass) map[types.Object]*types.Named {
	out := make(map[types.Object]*types.Named)
	for _, obj := range pass.TypesInfo.Defs {
		typeName, ok := obj.(*types.TypeName)
		if !ok || !typeName.Exported() {
			continue
		}
		named, ok := typeName.Type().(*types.Named)
		if !ok {
			continue
		}
		structType, ok := named.Underlying().(*types.Struct)
		if !ok {
			continue
		}
		for i := 0; i < structType.NumFields(); i++ {
			field := structType.Field(i)
			if !field.Exported() {
				out[field] = named
			}
		}
	}
	return out
}

func selectorObject(pass *analysis.Pass, sel *ast.SelectorExpr) types.Object {
	if selection := pass.TypesInfo.Selections[sel]; selection != nil {
		return selection.Obj()
	}
	return pass.TypesInfo.Uses[sel.Sel]
}

func receiverNamed(fn *types.Func) *types.Named {
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

func checkConstructors(pass *analysis.Pass) {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			ident, ok := decl.(*ast.FuncDecl)
			if !ok || !ident.Name.IsExported() || !strings.HasPrefix(ident.Name.Name, "New") {
				continue
			}
			fn, ok := pass.TypesInfo.ObjectOf(ident.Name).(*types.Func)
			if !ok {
				continue
			}
			sig, ok := fn.Type().(*types.Signature)
			if !ok || sig.Results() == nil {
				continue
			}
			for i := 0; i < sig.Results().Len(); i++ {
				typ := sig.Results().At(i).Type()
				if isErrorType(typ) {
					continue
				}
				if _, ok := typ.Underlying().(*types.Interface); ok {
					if isFactory(pass, ident) {
						break
					}
					report(pass, ident.Name.Pos(), "constructor %s returns an interface; constructors must return concrete types", fn.Name())
					break
				}
			}
		}
	}
}

func isFactory(pass *analysis.Pass, fn *ast.FuncDecl) bool {
	if fn.Body == nil {
		return false
	}
	var returned []types.Type
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		ret, ok := node.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, result := range ret.Results {
			typ := pass.TypesInfo.TypeOf(result)
			if typ == nil || isErrorType(typ) {
				continue
			}
			if _, ok := typ.Underlying().(*types.Interface); ok {
				continue
			}
			seen := false
			for _, existing := range returned {
				if types.Identical(existing, typ) {
					seen = true
					break
				}
			}
			if seen {
				continue
			}
			returned = append(returned, typ)
			if len(returned) > 1 {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func isErrorType(typ types.Type) bool {
	err := types.Universe.Lookup("error")
	return err != nil && types.Identical(typ, err.Type())
}

func checkCohesion(pass *analysis.Pass) {
	files := make(map[string]map[string]token.Pos)
	for _, file := range pass.Files {
		name := pass.Fset.File(file.Pos()).Name()
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil {
				continue
			}
			recv := receiverName(fn.Recv)
			if recv == "" {
				continue
			}
			if files[recv] == nil {
				files[recv] = make(map[string]token.Pos)
			}
			files[recv][name] = fn.Name.Pos()
		}
	}
	for recv, byFile := range files {
		if len(byFile) < 2 {
			continue
		}
		for file, pos := range byFile {
			others := make([]string, 0, len(byFile)-1)
			for other := range byFile {
				if other != file {
					others = append(others, other)
				}
			}
			sort.Strings(others)
			report(pass, pos, "receiver %s has methods in multiple files; cohesive owner methods must share one file (%s)", recv, strings.Join(others, ", "))
		}
	}
}

func receiverName(field *ast.FieldList) string {
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

func isTestFunction(name string) bool {
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
