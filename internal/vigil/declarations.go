package vigil

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
)

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
		if !ok || len(vs.Names) == 0 {
			return false
		}
		for _, name := range vs.Names {
			if name.IsExported() {
				return false
			}
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
			var dependency types.Object
			switch node := node.(type) {
			case *ast.Ident:
				dependency = pass.TypesInfo.Uses[node]
			case *ast.SelectorExpr:
				dependency = selectorObject(pass, node)
			default:
				return true
			}
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
			if dependencyDecl == nil || dependencyDecl.Pos() >= fn.Pos() || reaches(deps, dependency, caller) {
				continue
			}
			report(pass, fn.Name.Pos(),
				"dependent %s follows dependency %s; dependents must be declared before their dependencies",
				fn.Name.Name, dependencyDecl.Name.Name)
		}
	}
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
	set := collectFunctions(pass)
	for ident, obj := range pass.TypesInfo.Defs {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Exported() {
			continue
		}
		decl := set.defs[obj]
		if decl == nil || decl.Doc != nil || len(set.callers[obj]) != 1 {
			continue
		}
		if inlineableWrapper(pass, set.callers, decl.Body) {
			report(pass, ident.Pos(),
				"private helper %s is a single-use forwarding wrapper; inline it",
				ident.Name)
		}
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

func checkConstructors(pass *analysis.Pass) {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() || !strings.HasPrefix(fn.Name.Name, "New") {
				continue
			}
			object, ok := pass.TypesInfo.ObjectOf(fn.Name).(*types.Func)
			if !ok {
				continue
			}
			sig, ok := object.Type().(*types.Signature)
			if !ok || sig.Results() == nil || returnsMultipleConcreteTypes(pass, fn) {
				continue
			}
			for i := 0; i < sig.Results().Len(); i++ {
				typ := sig.Results().At(i).Type()
				if isErrorType(typ) {
					continue
				}
				if _, ok := typ.Underlying().(*types.Interface); ok {
					report(pass, fn.Name.Pos(),
						"constructor %s returns an interface; constructors must return concrete types",
						object.Name())
					break
				}
			}
		}
	}
}

func returnsMultipleConcreteTypes(pass *analysis.Pass, fn *ast.FuncDecl) bool {
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
			for _, existing := range returned {
				if types.Identical(existing, typ) {
					continue
				}
				found = true
				return false
			}
			returned = append(returned, typ)
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
