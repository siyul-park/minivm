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

func docs(pass *analysis.Pass, file *ast.File) {
	if strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
		return
	}
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			if test(decl.Name.Name) || !decl.Name.IsExported() || decl.Doc != nil {
				continue
			}
			if decl.Recv != nil && !ast.IsExported(receiver(decl.Recv)) {
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

func declarations(pass *analysis.Pass, file *ast.File) {
	last := -1
	var lastDecl ast.Decl
	for _, decl := range file.Decls {
		group := declarationGroup(pass, decl)
		if group < 0 {
			continue
		}
		if group < last {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "init" {
				report(pass, decl.Pos(), "init must appear immediately after package-level declarations")
			} else {
				report(pass, decl.Pos(), "declaration %s follows %s in the wrong file-order group", name(decl), name(lastDecl))
			}
			continue
		}
		last = group
		lastDecl = decl
	}
}

func declarationGroup(pass *analysis.Pass, decl ast.Decl) int {
	if fn, ok := decl.(*ast.FuncDecl); ok {
		return functionGroup(pass, fn)
	}
	gen, ok := decl.(*ast.GenDecl)
	if !ok {
		return -1
	}
	switch gen.Tok {
	case token.TYPE:
		if unexportedTypes(gen.Specs) {
			return 1
		}
		return 0
	case token.CONST:
		if unexported(gen.Specs) {
			return 3
		}
		return 2
	case token.VAR:
		return 4
	default:
		return -1
	}
}

func functionGroup(pass *analysis.Pass, fn *ast.FuncDecl) int {
	if test(fn.Name.Name) {
		return -1
	}
	if fn.Recv != nil {
		if hook(fn.Name.Name) || interfaceHook(pass, fn) {
			return 10
		}
		if !ast.IsExported(receiver(fn.Recv)) || !fn.Name.IsExported() {
			return 11
		}
		if constructor(fn.Name.Name) {
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
	if constructor(fn.Name.Name) {
		return 8
	}
	return 7
}

func unexportedTypes(specs []ast.Spec) bool {
	for _, spec := range specs {
		typ, ok := spec.(*ast.TypeSpec)
		if !ok || typ.Name.IsExported() {
			return false
		}
	}
	return true
}

func constructor(name string) bool {
	return name == "New" || strings.HasPrefix(name, "New")
}

func hook(name string) bool {
	switch name {
	case "Cast", "Equals", "Kind", "Type", "String", "Refs", "Marshal", "Unmarshal", "Error", "Unwrap":
		return true
	default:
		return false
	}
}

func interfaceHook(pass *analysis.Pass, fn *ast.FuncDecl) bool {
	obj, ok := pass.TypesInfo.ObjectOf(fn.Name).(*types.Func)
	if !ok {
		return false
	}
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	for _, iface := range interfaces(pass) {
		if !implements(sig.Recv().Type(), iface) {
			continue
		}
		for i := 0; i < iface.NumMethods(); i++ {
			if iface.Method(i).Name() == obj.Name() {
				return true
			}
		}
	}
	return false
}

func interfaces(pass *analysis.Pass) []*types.Interface {
	var out []*types.Interface
	add := func(scope *types.Scope) {
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			typeName, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := typeName.Type().(*types.Named)
			if !ok {
				continue
			}
			iface, ok := named.Underlying().(*types.Interface)
			if ok {
				out = append(out, iface)
			}
		}
	}
	add(pass.Pkg.Scope())
	for _, pkg := range pass.Pkg.Imports() {
		add(pkg.Scope())
	}
	return out
}

func implements(typ types.Type, iface *types.Interface) bool {
	return types.Implements(typ, iface)
}

func unexported(specs []ast.Spec) bool {
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

func name(decl ast.Decl) string {
	switch decl := decl.(type) {
	case *ast.FuncDecl:
		return decl.Name.Name
	case *ast.GenDecl:
		return decl.Tok.String()
	default:
		return "declaration"
	}
}

func dependencyOrder(pass *analysis.Pass, file *ast.File) {
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
				dependency = object(pass, node)
			default:
				return true
			}
			dependencyDecl := decls[dependency]
			if dependencyDecl == nil || dependency == caller ||
				declarationGroup(pass, fn) != declarationGroup(pass, dependencyDecl) {
				return true
			}
			deps[caller] = unique(deps[caller], dependency)
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

func privateWrappers(pass *analysis.Pass) {
	set := collectGraph(pass)
	for ident, obj := range pass.TypesInfo.Defs {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Exported() {
			continue
		}
		decl := set.functions[obj]
		if decl == nil || decl.Doc != nil || len(set.callers[obj]) != 1 {
			continue
		}
		if privateForwarder(pass, set, obj, decl.Body) {
			report(pass, ident.Pos(),
				"private helper %s is a single-use forwarding wrapper; inline it",
				ident.Name)
		}
	}
}
func privateForwarder(pass *analysis.Pass, graph graph, current types.Object, body *ast.BlockStmt) bool {
	call := call(body)
	if call == nil {
		return false
	}
	target := object(pass, call.Fun)
	if target == nil || target.Exported() || len(graph.callers[target]) != 1 {
		return false
	}
	currentFunc, ok := current.(*types.Func)
	if !ok {
		return false
	}
	targetFunc, ok := target.(*types.Func)
	if !ok || !forwarded(pass, currentFunc, targetFunc, call) {
		return false
	}
	seen := make(map[types.Object]bool)
	var visit func(types.Object) bool
	visit = func(obj types.Object) bool {
		if obj == current {
			return true
		}
		if seen[obj] {
			return false
		}
		seen[obj] = true
		for dependency := range graph.calls[obj] {
			if visit(dependency) {
				return true
			}
		}
		return false
	}
	return !visit(target)
}

func forwarded(pass *analysis.Pass, current, target *types.Func, call *ast.CallExpr) bool {
	currentSig, ok := current.Type().(*types.Signature)
	if !ok {
		return false
	}
	targetSig, ok := target.Type().(*types.Signature)
	if !ok || targetSig.Params().Len() != currentSig.Params().Len() || len(call.Args) != currentSig.Params().Len() {
		return false
	}
	if currentSig.Recv() != nil {
		selection, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || pass.TypesInfo.Selections[selection] == nil || targetSig.Recv() == nil {
			return false
		}
		receiver, ok := selection.X.(*ast.Ident)
		if !ok || pass.TypesInfo.Uses[receiver] != currentSig.Recv() {
			return false
		}
	} else if targetSig.Recv() != nil {
		return false
	}
	for i, arg := range call.Args {
		ident, ok := arg.(*ast.Ident)
		if !ok || pass.TypesInfo.Uses[ident] != currentSig.Params().At(i) {
			return false
		}
	}
	return true
}

func errors(pass *analysis.Pass, file *ast.File) {
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
			target, ok := object(pass, call.Fun).(*types.Func)
			if !ok || target.Pkg() == nil || target.Pkg().Path() != "fmt" || target.Name() != "Errorf" || len(call.Args) < 2 {
				return true
			}
			value := pass.TypesInfo.Types[call.Args[0]].Value
			if value == nil || value.Kind() != constant.String {
				return true
			}
			format := constant.StringVal(value)
			if !verb(format, 'v') || verb(format, 'w') {
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

func verb(format string, want byte) bool {
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

func api(pass *analysis.Pass) {
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			declaration(pass, decl)
		}
	}
}

func declaration(pass *analysis.Pass, decl ast.Decl) {
	switch decl := decl.(type) {
	case *ast.FuncDecl:
		fn, ok := pass.TypesInfo.ObjectOf(decl.Name).(*types.Func)
		if ok && fn.Exported() {
			function(pass, fn, decl)
		}
	case *ast.GenDecl:
		for _, spec := range decl.Specs {
			switch spec := spec.(type) {
			case *ast.TypeSpec:
				specification(pass, spec)
			case *ast.ValueSpec:
				value(pass, spec)
			}
		}
	}
}

func specification(pass *analysis.Pass, spec ast.Spec) {
	typ, ok := spec.(*ast.TypeSpec)
	if !ok || !typ.Name.IsExported() {
		return
	}
	obj := pass.TypesInfo.ObjectOf(typ.Name)
	if obj == nil {
		return
	}
	switch obj.Type().Underlying().(type) {
	case *types.Struct, *types.Interface:
		if private := privateType(pass, obj.Type().Underlying()); private != nil {
			report(pass, typ.Name.Pos(), "public symbol %s exposes private type %s", obj.Name(), private.Obj().Name())
		}
	case *types.Signature:
		// Public named function types intentionally hide their private option state.
	}
}

func value(pass *analysis.Pass, spec *ast.ValueSpec) {
	for _, name := range spec.Names {
		if !name.IsExported() {
			continue
		}
		obj := pass.TypesInfo.ObjectOf(name)
		if obj == nil {
			continue
		}
		if private := privateType(pass, obj.Type()); private != nil {
			report(pass, name.Pos(), "public symbol %s exposes private type %s", obj.Name(), private.Obj().Name())
		}
	}
}

func function(pass *analysis.Pass, fn *types.Func, decl *ast.FuncDecl) {
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return
	}
	for i := 0; i < sig.Params().Len(); i++ {
		if private := privateType(pass, sig.Params().At(i).Type()); private != nil {
			report(pass, decl.Name.Pos(), "public symbol %s exposes private parameter type %s", fn.Name(), private.Obj().Name())
			return
		}
	}
	for i := 0; i < sig.Results().Len(); i++ {
		if private := privateType(pass, sig.Results().At(i).Type()); private != nil {
			report(pass, decl.Name.Pos(), "public symbol %s exposes private result type %s", fn.Name(), private.Obj().Name())
			return
		}
	}
}

func privateType(pass *analysis.Pass, typ types.Type) *types.Named {
	seen := make(map[types.Type]bool)
	var visit func(types.Type) *types.Named
	visit = func(typ types.Type) *types.Named {
		typ = types.Unalias(typ)
		if seen[typ] {
			return nil
		}
		seen[typ] = true
		switch typ := typ.(type) {
		case *types.Named:
			obj := typ.Obj()
			if obj.Pkg() == pass.Pkg && !obj.Exported() {
				return typ
			}
			if obj.Exported() {
				return nil
			}
			return visit(typ.Underlying())
		case *types.Pointer:
			return visit(typ.Elem())
		case *types.Slice:
			return visit(typ.Elem())
		case *types.Array:
			return visit(typ.Elem())
		case *types.Map:
			if key := visit(typ.Key()); key != nil {
				return key
			}
			return visit(typ.Elem())
		case *types.Chan:
			return visit(typ.Elem())
		case *types.Signature:
			if found := tuple(visit, typ.Params()); found != nil {
				return found
			}
			return tuple(visit, typ.Results())
		case *types.Struct:
			for i := 0; i < typ.NumFields(); i++ {
				field := typ.Field(i)
				if field.Exported() {
					if found := visit(field.Type()); found != nil {
						return found
					}
				}
			}
		case *types.Interface:
			for i := 0; i < typ.NumMethods(); i++ {
				method := typ.Method(i)
				if method.Exported() {
					if found := visit(method.Type()); found != nil {
						return found
					}
				}
			}
		}
		return nil
	}
	return visit(typ)
}

func tuple(visit func(types.Type) *types.Named, tuple *types.Tuple) *types.Named {
	for i := 0; i < tuple.Len(); i++ {
		if found := visit(tuple.At(i).Type()); found != nil {
			return found
		}
	}
	return nil
}

func cohesion(pass *analysis.Pass) {
	files := make(map[string]map[string]token.Pos)
	for _, file := range pass.Files {
		name := pass.Fset.File(file.Pos()).Name()
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil {
				continue
			}
			recv := receiver(fn.Recv)
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
