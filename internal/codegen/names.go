package codegen

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
)

type nameScope struct {
	parent *nameScope
	used   map[string]struct{}
	next   int
}

func normalizeLocalNames(src []byte) ([]byte, error) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "generated.go", src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	root := &nameScope{used: map[string]struct{}{}}
	scope := root
	stack := []bool{}
	objects := map[*ast.Object]*nameScope{}
	order := []*ast.Object{}
	uses := map[*ast.Object][]*nameScope{}
	functionBodies := map[*ast.BlockStmt]bool{}

	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncDecl:
			if node.Body != nil {
				functionBodies[node.Body] = true
			}
		case *ast.FuncLit:
			functionBodies[node.Body] = true
		}
		return true
	})

	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			if stack[len(stack)-1] {
				scope = scope.parent
			}
			stack = stack[:len(stack)-1]
			return true
		}

		push := false
		switch node := node.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			push = true
		case *ast.BlockStmt:
			push = !functionBodies[node]
		case *ast.ForStmt,
			*ast.IfStmt,
			*ast.RangeStmt,
			*ast.SwitchStmt,
			*ast.TypeSwitchStmt,
			*ast.CaseClause,
			*ast.CommClause:
			push = true
		}
		if push {
			scope = &nameScope{parent: scope, used: map[string]struct{}{}}
		}
		stack = append(stack, push)

		ident, ok := node.(*ast.Ident)
		if !ok || ident.Obj == nil || ident.Obj.Kind != ast.Var {
			return true
		}
		if _, exists := objects[ident.Obj]; !exists {
			objects[ident.Obj] = scope
			order = append(order, ident.Obj)
		}
		uses[ident.Obj] = append(uses[ident.Obj], scope)
		return true
	})

	for _, obj := range order {
		defined := objects[obj]
		if defined != root && isGeneratedName(obj.Name) {
			defined.used[obj.Name] = struct{}{}
		}
	}

	rename := map[*ast.Object]string{}
	for _, obj := range order {
		defined := objects[obj]
		if obj.Kind != ast.Var || defined == root {
			continue
		}
		if file.Scope.Objects[obj.Name] == obj {
			continue
		}
		if _, field := obj.Decl.(*ast.Field); field {
			continue
		}
		if len(uses[obj]) == 1 {
			rename[obj] = "_"
			continue
		}

		name := nextName(defined)
		rename[obj] = name
		for _, use := range uses[obj] {
			for current := use; current != nil; current = current.parent {
				current.used[name] = struct{}{}
			}
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok {
			if name, exists := rename[ident.Obj]; exists {
				ident.Name = name
			}
		}
		return true
	})

	var out bytes.Buffer
	if err := format.Node(&out, set, file); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func nextName(scope *nameScope) string {
	for {
		name := "v" + strconv.Itoa(scope.next)
		scope.next++
		if _, used := scope.used[name]; !used {
			return name
		}
	}
}

func isGeneratedName(name string) bool {
	if len(name) < 2 || name[0] != 'v' {
		return false
	}
	_, err := strconv.Atoi(name[1:])
	return err == nil
}
