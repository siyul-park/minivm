package vigil

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

func boundary(pass *analysis.Pass) {
	fields := fields(pass)
	owners := make(map[*types.Named]types.Object)
	for field, owner := range fields {
		if _, ok := owners[owner]; !ok {
			owners[owner] = field
		}
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
			current, _ := pass.TypesInfo.ObjectOf(fn.Name).(*types.Func)
			receiver := receiverType(current)
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.SelectorExpr:
					access(pass, fields, owners, current, receiver, object(pass, node), node.Sel.Pos())
				case *ast.KeyValueExpr:
					ident, ok := node.Key.(*ast.Ident)
					if ok {
						access(pass, fields, owners, current, receiver, pass.TypesInfo.Uses[ident], ident.Pos())
					}
				case *ast.CompositeLit:
					if len(node.Elts) == 0 {
						return true
					}
					if _, ok := node.Elts[0].(*ast.KeyValueExpr); ok {
						return true
					}
					named, ok := pass.TypesInfo.TypeOf(node).(*types.Named)
					if ok {
						access(pass, fields, owners, current, receiver, owners[named], node.Pos())
					}
				}
				return true
			})
		}
	}
}

func access(pass *analysis.Pass, fields map[types.Object]*types.Named, owners map[*types.Named]types.Object, current *types.Func, receiver *types.Named, obj types.Object, pos token.Pos) {
	if obj == nil || obj.Exported() || obj.Pkg() != pass.Pkg {
		return
	}
	owner := fields[obj]
	if owner == nil {
		return
	}
	if receiver == nil {
		if !constructor(current.Name()) {
			return
		}
		sig, ok := current.Type().(*types.Signature)
		if !ok || sig.Results() == nil {
			return
		}
		for i := 0; i < sig.Results().Len(); i++ {
			result := sig.Results().At(i).Type()
			if pointer, ok := result.(*types.Pointer); ok {
				result = pointer.Elem()
			}
			named, ok := result.(*types.Named)
			if ok && types.Identical(named, owner) {
				return
			}
		}
		return
	}
	if !current.Exported() || types.Identical(receiver, owner) {
		return
	}
	report(pass, pos, "private member %s of public owner %s must be accessed through its owner", obj.Name(), owner.Obj().Name())
}

func fields(pass *analysis.Pass) map[types.Object]*types.Named {
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
