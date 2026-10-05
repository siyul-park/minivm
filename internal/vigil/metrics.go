package vigil

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

type metric struct {
	fn         *ast.FuncDecl
	fanIn      int
	fanOut     int
	cyclomatic int
	statements int
	nesting    int
}

func complexity(pass *analysis.Pass) {
	for _, metric := range metrics(pass) {
		if dispatcher(metric.fn.Body) {
			continue
		}
		if metric.cyclomatic >= 15 && metric.statements >= 30 {
			report(pass, metric.fn.Name.Pos(),
				"function %s has high complexity: cyclomatic=%d statements=%d nesting=%d",
				metric.fn.Name.Name, metric.cyclomatic, metric.statements, metric.nesting)
			continue
		}
		if metric.cyclomatic >= 10 && metric.statements >= 25 && metric.nesting >= 6 {
			report(pass, metric.fn.Name.Pos(),
				"function %s has high structural complexity: cyclomatic=%d statements=%d nesting=%d",
				metric.fn.Name.Name, metric.cyclomatic, metric.statements, metric.nesting)
		}
	}
}

func fanout(pass *analysis.Pass) {
	for _, metric := range metrics(pass) {
		if dispatcher(metric.fn.Body) {
			continue
		}
		if metric.fanIn >= 8 && metric.fanOut >= 6 && metric.statements >= 20 {
			report(pass, metric.fn.Name.Pos(),
				"function %s is a dependency hub: fan-in=%d fan-out=%d",
				metric.fn.Name.Name, metric.fanIn, metric.fanOut)
			continue
		}
		if metric.fanOut >= 12 && metric.fanIn <= 1 && metric.statements >= 15 {
			report(pass, metric.fn.Name.Pos(),
				"function %s is a high fan-out coordinator: fan-in=%d fan-out=%d",
				metric.fn.Name.Name, metric.fanIn, metric.fanOut)
		}
	}
}

func dispatcher(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) != 1 {
		return false
	}
	var list *ast.BlockStmt
	switch stmt := body.List[0].(type) {
	case *ast.SwitchStmt:
		list = stmt.Body
	case *ast.TypeSwitchStmt:
		list = stmt.Body
	default:
		return false
	}
	if list == nil || len(list.List) == 0 {
		return false
	}
	for _, stmt := range list.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok || len(clause.Body) != 1 {
			return false
		}
		if _, ok := clause.Body[0].(*ast.ReturnStmt); !ok {
			return false
		}
	}
	return true
}

func metrics(pass *analysis.Pass) []*metric {
	graph := collectGraph(pass)
	items := make(map[types.Object]*metric, len(graph.functions))
	for obj, fn := range graph.functions {
		cyclomatic, statements, nesting := measure(fn.Body)
		items[obj] = &metric{
			fn:         fn,
			cyclomatic: cyclomatic,
			statements: statements,
			nesting:    nesting,
		}
	}
	for caller, dependencies := range graph.calls {
		metric := items[caller]
		metric.fanOut = len(dependencies)
		for dependency := range dependencies {
			if items[dependency] != nil {
				items[dependency].fanIn++
			}
		}
	}
	out := make([]*metric, 0, len(items))
	for _, metric := range items {
		out = append(out, metric)
	}
	return out
}

func measure(body *ast.BlockStmt) (cyclomatic, statements, nesting int) {
	cyclomatic = 1
	var walkStmt func(ast.Stmt, int)
	var walkBlock func(*ast.BlockStmt, int)

	walkBlock = func(block *ast.BlockStmt, depth int) {
		if block == nil {
			return
		}
		for _, stmt := range block.List {
			statements++
			walkStmt(stmt, depth)
		}
	}

	walkStmt = func(stmt ast.Stmt, depth int) {
		switch node := stmt.(type) {
		case *ast.BlockStmt:
			nesting = max(nesting, depth)
			walkBlock(node, depth)
		case *ast.IfStmt:
			cyclomatic++
			ast.Inspect(node.Cond, func(node ast.Node) bool {
				binary, ok := node.(*ast.BinaryExpr)
				if ok && (binary.Op == token.LAND || binary.Op == token.LOR) {
					cyclomatic++
				}
				return true
			})
			nesting = max(nesting, depth+1)
			walkBlock(node.Body, depth+1)
			if node.Else != nil {
				switch elseNode := node.Else.(type) {
				case *ast.IfStmt:
					walkStmt(elseNode, depth)
				case *ast.BlockStmt:
					walkBlock(elseNode, depth+1)
				}
			}
		case *ast.ForStmt:
			cyclomatic++
			nesting = max(nesting, depth+1)
			walkBlock(node.Body, depth+1)
		case *ast.RangeStmt:
			cyclomatic++
			nesting = max(nesting, depth+1)
			walkBlock(node.Body, depth+1)
		case *ast.SwitchStmt:
			nesting = max(nesting, depth+1)
			for _, stmt := range node.Body.List {
				clause, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				statements++
				if len(clause.List) != 0 {
					cyclomatic++
				}
				for _, nested := range clause.Body {
					statements++
					walkStmt(nested, depth+1)
				}
			}
		case *ast.TypeSwitchStmt:
			nesting = max(nesting, depth+1)
			for _, stmt := range node.Body.List {
				clause, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				statements++
				if len(clause.List) != 0 {
					cyclomatic++
				}
				for _, nested := range clause.Body {
					statements++
					walkStmt(nested, depth+1)
				}
			}
		case *ast.SelectStmt:
			nesting = max(nesting, depth+1)
			for _, stmt := range node.Body.List {
				clause, ok := stmt.(*ast.CommClause)
				if !ok {
					continue
				}
				statements++
				cyclomatic++
				for _, nested := range clause.Body {
					statements++
					walkStmt(nested, depth+1)
				}
			}
		}
	}

	walkBlock(body, 1)
	return cyclomatic, statements, nesting
}
