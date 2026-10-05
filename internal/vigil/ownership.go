package vigil

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

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
