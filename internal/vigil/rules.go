package vigil

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

type rule struct {
	id          string
	description string
	severity    string
	run         func(*analysis.Pass)
}

const (
	errorSeverity   = "error"
	warningSeverity = "warning"
)

var rules = []rule{
	{id: "CP001", severity: warningSeverity, description: "exported symbols have doc comments", run: fileRule(checkDocs)},
	{id: "CP002", severity: errorSeverity, description: "declarations follow file-order ownership groups", run: fileRule(checkDeclarations)},
	{id: "CP003", severity: errorSeverity, description: "context.Context is the first parameter", run: fileRule(checkContext)},
	{id: "CP004", severity: errorSeverity, description: "constructors return concrete types", run: checkConstructors},
	{id: "CP005", severity: errorSeverity, description: "receiver-owned methods stay in one file", run: checkCohesion},
	{id: "CP006", severity: errorSeverity, description: "dependents are declared before their dependencies", run: fileRule(checkDependencyOrder)},
	{id: "CP007", severity: warningSeverity, description: "private single-use forwarding wrappers are inlined", run: checkHelpers},
	{id: "CP008", severity: warningSeverity, description: "high complexity is a review signal", run: checkComplexity},
	{id: "CP009", severity: warningSeverity, description: "dependency hubs are a review signal", run: checkFanout},
	{id: "CP010", severity: warningSeverity, description: "near-clone symbols are a review signal", run: func(pass *analysis.Pass) { checkClones(pass, false) }},
	{id: "CP011", severity: warningSeverity, description: "separated similar siblings are a review signal", run: func(pass *analysis.Pass) { checkClones(pass, true) }},
	{id: "CP012", severity: errorSeverity, description: "public struct private members stay behind their owner", run: checkPrivateBoundary},
	{id: "CP013", severity: warningSeverity, description: "wrapped errors preserve dependency identity", run: fileRule(checkErrors)},
	{id: "TP002", severity: errorSeverity, description: "t.Run nesting is at most one case level", run: testFileRule(checkTestNesting)},
	{id: "TP003", severity: errorSeverity, description: "resource polling uses the package poll helper", run: testFileRule(checkTestPolling)},
	{id: "TP004", severity: errorSeverity, description: "tests do not reference private target symbols", run: testFileRule(checkTestPrivate)},
	{id: "TP007", severity: errorSeverity, description: "test helpers do not hide public target calls", run: checkTestHelpers},
}

// Rules returns the registered analyzer rule metadata.
func Rules() []string {
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		suffix := ""
		if rule.severity == warningSeverity {
			suffix = " [warning]"
		}
		out = append(out, rule.id+" "+rule.description+suffix)
	}
	return out
}

func fileRule(check func(*analysis.Pass, *ast.File)) func(*analysis.Pass) {
	return func(pass *analysis.Pass) {
		for _, file := range pass.Files {
			if !ast.IsGenerated(file) {
				check(pass, file)
			}
		}
	}
}

func testFileRule(check func(*analysis.Pass, *ast.File)) func(*analysis.Pass) {
	return func(pass *analysis.Pass) {
		for _, file := range pass.Files {
			if ast.IsGenerated(file) || !strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
				continue
			}
			check(pass, file)
		}
	}
}
