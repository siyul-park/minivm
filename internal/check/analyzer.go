package check

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
)

type severity string

const (
	errorSeverity   severity = "error"
	warningSeverity severity = "warning"
)

// Analyzer checks minivm coding and testing patterns.
var Analyzer = &analysis.Analyzer{
	Name: "check",
	Doc:  "checks minivm coding-patterns.md and testing.md contracts",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			continue
		}
		checkDocs(pass, file)
		checkDeclarations(pass, file)
		checkDependencyOrder(pass, file)
		checkContext(pass, file)
		if strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
			checkTestFile(pass, file)
		}
	}
	checkCohesion(pass)
	checkConstructors(pass)
	checkHelpers(pass)
	checkMetrics(pass)
	checkClones(pass)
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
			report(pass, "CP001", decl.Name.Pos(), "exported symbol %s must have a doc comment", decl.Name.Name)
		case *ast.GenDecl:
			switch decl.Tok {
			case token.TYPE:
				for _, spec := range decl.Specs {
					typ := spec.(*ast.TypeSpec)
					if typ.Name.IsExported() && typ.Doc == nil && decl.Doc == nil {
						report(pass, "CP001", typ.Name.Pos(), "exported symbol %s must have a doc comment", typ.Name.Name)
					}
				}
			case token.CONST, token.VAR:
				for _, spec := range decl.Specs {
					value := spec.(*ast.ValueSpec)
					for _, name := range value.Names {
						if name.IsExported() && value.Doc == nil && decl.Doc == nil {
							report(pass, "CP001", name.Pos(), "exported symbol %s must have a doc comment", name.Name)
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
			report(pass, "CP002", decl.Pos(), "declaration %s follows %s in the wrong file-order group", declarationName(decl), declarationName(lastDecl))
		}
		last = group
		lastDecl = decl
	}
}

func declarationGroup(decl ast.Decl) int {
	switch decl := decl.(type) {
	case *ast.FuncDecl:
		if isTestFunction(decl.Name.Name) {
			return -1
		}
		if decl.Recv != nil {
			if !decl.Name.IsExported() {
				return 11
			}
			switch {
			case isHook(decl.Name.Name):
				return 10
			case isConstructor(decl.Name.Name):
				return 8
			default:
				return 9
			}
		}
		if decl.Name.Name == "init" {
			return 6
		}
		if decl.Name.IsExported() {
			if isConstructor(decl.Name.Name) {
				return 8
			}
			return 7
		}
		return 11
	case *ast.GenDecl:
		switch decl.Tok {
		case token.TYPE:
			if allSpecsUnexportedType(decl.Specs) {
				return 1
			}
			return 0
		case token.CONST:
			if allSpecsUnexported(decl.Specs) {
				return 3
			}
			return 2
		case token.VAR:
			return 4
		}
	}
	return -1
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
			report(pass, "CP006", fn.Name.Pos(),
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
	documented := make(map[types.Object]bool)
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
		if len(callers[obj]) == 1 {
			report(pass, "CP007", ident.Pos(),
				"private helper %s has one caller; inline it unless it names a real policy or mechanic",
				ident.Name)
		}
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
				report(pass, "CP003", field.Pos(), "context.Context must be the first parameter of %s", fn.Name.Name)
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

func checkConstructors(pass *analysis.Pass) {
	for ident, obj := range pass.TypesInfo.Defs {
		fn, ok := obj.(*types.Func)
		if !ok || !ident.IsExported() || !strings.HasPrefix(fn.Name(), "New") {
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
				report(pass, "CP004", ident.Pos(), "constructor %s returns an interface; constructors must return concrete types", fn.Name())
				break
			}
		}
	}
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
			report(pass, "CP005", pos, "receiver %s has methods in multiple files; cohesive owner methods must share one file (%s)", recv, strings.Join(others, ", "))
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

func checkTestFile(pass *analysis.Pass, file *ast.File) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !isTestFunction(fn.Name.Name) {
			continue
		}
		checkTestStyle(pass, fn)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			if isTestRun(n) && nestedRun(n) {
				report(pass, "TP002", n.Pos(), "t.Run cases must not nest beyond one level")
			}
			if isEventually(n) {
				report(pass, "TP003", n.Pos(), "require.Eventually/assert.Eventually must not be used for resource polling; use the package poll helper on the test goroutine")
			}
		case *ast.Ident:
			if obj := pass.TypesInfo.Uses[n]; obj != nil && isPrivateTarget(pass, obj) {
				report(pass, "TP004", n.Pos(), "tests must use the public target-package interface; private symbol %s is referenced", obj.Name())
			}
		}
		return true
	})
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

func checkTestStyle(pass *analysis.Pass, fn *ast.FuncDecl) {
	hasRun, hasDirectAssertion := false, false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isTestRun(call) {
			hasRun = true
			return false
		}
		if isTestAssertion(call) {
			hasDirectAssertion = true
		}
		return true
	})
	if hasRun && hasDirectAssertion {
		report(pass, "TP006", fn.Name.Pos(),
			"test %s mixes direct assertions with t.Run cases; keep cases at one consistent level",
			fn.Name.Name)
	}
}

func isTestAssertion(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	if id.Name == "assert" || id.Name == "require" {
		return true
	}
	if id.Name != "t" {
		return false
	}
	switch sel.Sel.Name {
	case "Error", "Errorf", "Fail", "FailNow", "Fatal", "Fatalf":
		return true
	default:
		return false
	}
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

func report(pass *analysis.Pass, rule string, pos token.Pos, format string, args ...any) {
	message := format
	if len(args) != 0 {
		message = fmt.Sprintf(format, args...)
	}
	message = strings.TrimSpace(strings.TrimSuffix(message, "."))
	pass.Report(analysis.Diagnostic{
		Pos:      pos,
		Category: string(ruleSeverity(rule)),
		Message:  "[" + rule + "] " + message,
	})
}

func ruleSeverity(rule string) severity {
	switch rule {
	case "CP001", "CP007", "CP008", "CP009", "CP010", "CP011", "CP012", "TP006":
		return warningSeverity
	default:
		return errorSeverity
	}
}
