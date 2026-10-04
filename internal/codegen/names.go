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
	parent   *nameScope
	used     map[string]struct{}
	bindings map[string][]*nameBinding
	next     int
}

type nameBinding struct {
	scope *nameScope
	name  string
	pos   token.Pos
	refs  []*ast.Ident
	blank bool
}

func normalizeLocalNames(src []byte) ([]byte, error) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "generated.go", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	root := &nameScope{used: map[string]struct{}{}, bindings: map[string][]*nameBinding{}}
	scopes := map[ast.Node]*nameScope{file: root}
	nodes := map[ast.Node]*nameScope{}
	ast.PreorderStack(file, nil, func(node ast.Node, stack []ast.Node) bool {
		parent := root
		for i := len(stack) - 1; i >= 0; i-- {
			if scope := scopes[stack[i]]; scope != nil {
				parent = scope
				break
			}
		}
		if scopeNode(node, stack) {
			scope := &nameScope{
				parent:   parent,
				used:     map[string]struct{}{},
				bindings: map[string][]*nameBinding{},
			}
			scopes[node] = scope
			parent = scope
		}
		nodes[node] = parent
		return true
	})

	order := []*nameBinding{}
	declarations := map[*ast.Ident]*nameBinding{}
	renamed := map[*ast.Ident]*nameBinding{}
	ast.PreorderStack(file, nil, func(node ast.Node, stack []ast.Node) bool {
		scope := nodes[node]
		switch node := node.(type) {
		case *ast.FuncDecl:
			reserveFields(scope, node.Recv)
			reserveFields(scope, node.Type.Params)
			reserveFields(scope, node.Type.Results)
		case *ast.FuncLit:
			reserveFields(scope, node.Type.Params)
			reserveFields(scope, node.Type.Results)
		case *ast.GenDecl:
			if node.Tok != token.VAR {
				for _, spec := range node.Specs {
					reserveDeclaration(scope, spec)
				}
			}
		case *ast.ValueSpec:
			for _, name := range node.Names {
				binding := declare(scope, name.Name, node.End())
				binding.blank = true
				declarations[name] = binding
				order = append(order, binding)
			}
		case *ast.AssignStmt:
			if node.Tok == token.DEFINE {
				newVars := 0
				for _, left := range node.Lhs {
					name, ok := left.(*ast.Ident)
					if ok && name.Name != "_" && !hasBinding(scope, name.Name) {
						newVars++
					}
				}
				for _, left := range node.Lhs {
					name, ok := left.(*ast.Ident)
					if !ok || name.Name == "_" || hasBinding(scope, name.Name) {
						continue
					}
					binding := declare(scope, name.Name, node.End())
					binding.blank = newVars > 1
					declarations[name] = binding
					order = append(order, binding)
				}
			}
		case *ast.RangeStmt:
			if node.Tok == token.DEFINE {
				newVars := 0
				for _, expr := range []ast.Expr{node.Key, node.Value} {
					name, ok := expr.(*ast.Ident)
					if ok && name.Name != "_" {
						newVars++
					}
				}
				for _, expr := range []ast.Expr{node.Key, node.Value} {
					name, ok := expr.(*ast.Ident)
					if !ok || name.Name == "_" {
						continue
					}
					binding := declare(scope, name.Name, node.Body.Pos())
					binding.blank = newVars > 1
					declarations[name] = binding
					order = append(order, binding)
				}
			}
		}

		if ident, ok := node.(*ast.Ident); ok && ident.Name != "_" {
			_, declaration := declarations[ident]
			if declaration || !referenceIdent(ident, stack) {
				return true
			}

			if binding := lookup(scope, ident.Name, ident.Pos()); binding != nil {
				binding.refs = append(binding.refs, ident)
			}
		}
		return true
	})

	rename := map[*nameBinding]string{}
	for _, binding := range order {
		if binding.scope == root {
			continue
		}
		if len(binding.refs) == 0 && binding.blank {
			rename[binding] = "_"
			continue
		}
		name := nextName(binding.scope)
		rename[binding] = name
		binding.scope.used[name] = struct{}{}
		for _, ref := range binding.refs {
			for current := nodes[ref]; current != nil; current = current.parent {
				current.used[name] = struct{}{}
			}
		}
	}
	for binding := range rename {
		for _, ref := range binding.refs {
			renamed[ref] = binding
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if binding := declarations[ident]; binding != nil {
			if name, ok := rename[binding]; ok {
				ident.Name = name
			}
			return true
		}
		if binding := renamed[ident]; binding != nil {
			ident.Name = rename[binding]
		}
		return true
	})

	var out bytes.Buffer
	if err := format.Node(&out, set, file); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func scopeNode(node ast.Node, stack []ast.Node) bool {
	switch node.(type) {
	case *ast.FuncDecl, *ast.FuncLit, *ast.ForStmt, *ast.RangeStmt, *ast.IfStmt,
		*ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.CaseClause, *ast.CommClause:
		return true
	case *ast.BlockStmt:
		if len(stack) == 0 {
			return true
		}
		_, function := stack[len(stack)-1].(*ast.FuncDecl)
		if function {
			return false
		}
		if _, function := stack[len(stack)-1].(*ast.FuncLit); function {
			return false
		}
		return true
	default:
		return false
	}
}

func reserveFields(scope *nameScope, fields *ast.FieldList) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			scope.used[name.Name] = struct{}{}
		}
	}
}

func reserveDeclaration(scope *nameScope, spec ast.Spec) {
	switch spec := spec.(type) {
	case *ast.TypeSpec:
		scope.used[spec.Name.Name] = struct{}{}
	case *ast.ValueSpec:
		for _, name := range spec.Names {
			scope.used[name.Name] = struct{}{}
		}
	}
}

func declare(scope *nameScope, name string, pos token.Pos) *nameBinding {
	binding := &nameBinding{scope: scope, name: name, pos: pos}
	scope.bindings[name] = append(scope.bindings[name], binding)
	return binding
}

func hasBinding(scope *nameScope, name string) bool {
	return len(scope.bindings[name]) > 0
}

func lookup(scope *nameScope, name string, pos token.Pos) *nameBinding {
	for current := scope; current != nil; current = current.parent {
		bindings := current.bindings[name]
		for i := len(bindings) - 1; i >= 0; i-- {
			if bindings[i].pos <= pos {
				return bindings[i]
			}
		}
	}
	return nil
}

func referenceIdent(ident *ast.Ident, stack []ast.Node) bool {
	if len(stack) == 0 {
		return true
	}
	parent := stack[len(stack)-1]
	switch parent := parent.(type) {
	case *ast.SelectorExpr:
		return parent.Sel != ident
	case *ast.KeyValueExpr:
		return parent.Key != ident
	case *ast.ImportSpec:
		return false
	case *ast.TypeSpec:
		return parent.Name != ident
	case *ast.FuncDecl:
		return parent.Name != ident
	case *ast.LabeledStmt:
		return parent.Label != ident
	case *ast.BranchStmt:
		return parent.Label != ident
	case *ast.Field:
		for _, name := range parent.Names {
			if name == ident {
				return false
			}
		}
	}
	return true
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
