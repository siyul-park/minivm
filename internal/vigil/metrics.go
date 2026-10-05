package vigil

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

type functionMetric struct {
	fn         *ast.FuncDecl
	fanIn      int
	fanOut     int
	level      int
	cyclomatic int
	statements int
	nesting    int
}

func checkComplexity(pass *analysis.Pass) {
	for _, metric := range collectMetrics(pass) {
		if isDispatcher(metric.fn.Body) {
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

func checkFanout(pass *analysis.Pass) {
	for _, metric := range collectMetrics(pass) {
		if isDispatcher(metric.fn.Body) {
			continue
		}
		if metric.fanIn >= 8 && metric.fanOut >= 6 && metric.statements >= 20 {
			report(pass, metric.fn.Name.Pos(),
				"function %s is a dependency hub: fan-in=%d fan-out=%d level=%d",
				metric.fn.Name.Name, metric.fanIn, metric.fanOut, metric.level)
			continue
		}
		if metric.fanOut >= 12 && metric.fanIn <= 1 && metric.statements >= 15 {
			report(pass, metric.fn.Name.Pos(),
				"function %s is a high fan-out coordinator: fan-in=%d fan-out=%d level=%d",
				metric.fn.Name.Name, metric.fanIn, metric.fanOut, metric.level)
		}
	}
}

func isDispatcher(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) != 1 {
		return false
	}
	switchStmt, ok := body.List[0].(*ast.SwitchStmt)
	if !ok || switchStmt.Body == nil || len(switchStmt.Body.List) == 0 {
		return false
	}
	for _, stmt := range switchStmt.Body.List {
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

func collectMetrics(pass *analysis.Pass) []*functionMetric {
	set := collectFunctions(pass)
	metrics := make(map[types.Object]*functionMetric, len(set.defs))
	for obj, fn := range set.defs {
		cyclomatic, statements, nesting := measureComplexity(fn.Body)
		metrics[obj] = &functionMetric{
			fn:         fn,
			cyclomatic: cyclomatic,
			statements: statements,
			nesting:    nesting,
		}
	}
	for caller, dependencies := range set.calls {
		metric := metrics[caller]
		metric.fanOut = len(dependencies)
		for dependency := range dependencies {
			if metrics[dependency] != nil {
				metrics[dependency].fanIn++
			}
		}
	}
	for obj, metric := range metrics {
		metric.level = dependencyLevel(set.calls, obj)
	}
	out := make([]*functionMetric, 0, len(metrics))
	for _, metric := range metrics {
		out = append(out, metric)
	}
	return out
}

func measureComplexity(body *ast.BlockStmt) (cyclomatic, statements, nesting int) {
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

func dependencyLevel(graph map[types.Object]map[types.Object]bool, start types.Object) int {
	visiting := make(map[types.Object]bool)
	var visit func(types.Object) int
	visit = func(obj types.Object) int {
		if visiting[obj] {
			return -1
		}
		dependencies := graph[obj]
		if len(dependencies) == 0 {
			return 0
		}
		visiting[obj] = true
		level := 0
		for dependency := range dependencies {
			depth := visit(dependency)
			if depth < 0 {
				delete(visiting, obj)
				return -1
			}
			level = max(level, depth+1)
		}
		delete(visiting, obj)
		return level
	}
	return visit(start)
}
