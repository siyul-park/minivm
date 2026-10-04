package check

import (
	"go/ast"
	"go/types"
	"strings"

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

func checkMetrics(pass *analysis.Pass) {
	metrics := collectMetrics(pass)
	for _, metric := range metrics {
		if isDispatcher(metric.fn.Body) {
			continue
		}
		if metric.cyclomatic >= 15 && metric.statements >= 30 {
			report(pass, "CP008", metric.fn.Name.Pos(),
				"function %s has high complexity: cyclomatic=%d statements=%d nesting=%d",
				metric.fn.Name.Name, metric.cyclomatic, metric.statements, metric.nesting)
			continue
		}
		if metric.cyclomatic >= 10 && metric.statements >= 25 && metric.nesting >= 6 {
			report(pass, "CP008", metric.fn.Name.Pos(),
				"function %s has high structural complexity: cyclomatic=%d statements=%d nesting=%d",
				metric.fn.Name.Name, metric.cyclomatic, metric.statements, metric.nesting)
		}

		if metric.fanIn >= 8 && metric.fanOut >= 6 && metric.statements >= 20 {
			report(pass, "CP009", metric.fn.Name.Pos(),
				"function %s is a dependency hub: fan-in=%d fan-out=%d level=%d",
				metric.fn.Name.Name, metric.fanIn, metric.fanOut, metric.level)
			continue
		}
		if metric.fanOut >= 12 && metric.fanIn <= 1 && metric.statements >= 15 {
			report(pass, "CP009", metric.fn.Name.Pos(),
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
	defs := make(map[types.Object]*ast.FuncDecl)
	metrics := make(map[types.Object]*functionMetric)

	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			obj := pass.TypesInfo.ObjectOf(fn.Name)
			if obj == nil {
				continue
			}
			defs[obj] = fn
			cyclomatic, statements, nesting := measureComplexity(fn.Body)
			metrics[obj] = &functionMetric{
				fn:         fn,
				cyclomatic: cyclomatic,
				statements: statements,
				nesting:    nesting,
			}
		}
	}

	deps := make(map[types.Object]map[types.Object]bool)
	for obj, metric := range metrics {
		ast.Inspect(metric.fn.Body, func(node ast.Node) bool {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			target := calledObject(pass, call)
			if target == nil || target == obj || defs[target] == nil {
				return true
			}
			if deps[obj] == nil {
				deps[obj] = make(map[types.Object]bool)
			}
			deps[obj][target] = true
			return true
		})
	}

	reverse := make(map[types.Object]int)
	for caller, dependencies := range deps {
		metrics[caller].fanOut = len(dependencies)
		for dependency := range dependencies {
			reverse[dependency]++
		}
	}
	for obj, count := range reverse {
		metrics[obj].fanIn = count
	}
	for obj, metric := range metrics {
		metric.level = dependencyLevel(deps, obj)
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
