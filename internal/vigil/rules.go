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
	{id: "CP001", severity: warningSeverity, description: "exported symbols have doc comments", run: files(docs)},
	{id: "CP002", severity: errorSeverity, description: "declarations follow file-order ownership groups", run: files(declarations)},
	{id: "CP004", severity: errorSeverity, description: "public APIs do not expose private types", run: api},
	{id: "CP005", severity: errorSeverity, description: "receiver-owned methods stay in one file", run: cohesion},
	{id: "CP006", severity: errorSeverity, description: "dependents are declared before their dependencies", run: files(dependencyOrder)},
	{id: "CP007", severity: warningSeverity, description: "private single-use forwarding wrappers are inlined", run: privateWrappers},
	{id: "CP008", severity: warningSeverity, description: "high complexity is a review signal", run: complexity},
	{id: "CP009", severity: warningSeverity, description: "dependency hubs are a review signal", run: fanout},
	{id: "CP010", severity: warningSeverity, description: "near-clone symbols are a review signal", run: func(pass *analysis.Pass) { clones(pass, false) }},
	{id: "CP011", severity: warningSeverity, description: "separated similar siblings are a review signal", run: func(pass *analysis.Pass) { clones(pass, true) }},
	{id: "CP012", severity: errorSeverity, description: "public struct private members stay behind their owner", run: boundary},
	{id: "CP013", severity: warningSeverity, description: "wrapped errors preserve dependency identity", run: files(errors)},
	{id: "TP002", severity: errorSeverity, description: "t.Run nesting is at most one case level", run: tests(nesting)},
	{id: "TP003", severity: errorSeverity, description: "readiness polling does not outlive closed resources", run: tests(polling)},
	{id: "TP004", severity: errorSeverity, description: "tests do not reference private target symbols", run: tests(private)},
	{id: "TP007", severity: errorSeverity, description: "test helpers do not hide public target calls", run: testWrappers},
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

func files(check func(*analysis.Pass, *ast.File)) func(*analysis.Pass) {
	return func(pass *analysis.Pass) {
		for _, file := range pass.Files {
			if !ast.IsGenerated(file) {
				check(pass, file)
			}
		}
	}
}

func tests(check func(*analysis.Pass, *ast.File)) func(*analysis.Pass) {
	return func(pass *analysis.Pass) {
		for _, file := range pass.Files {
			if ast.IsGenerated(file) || !strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
				continue
			}
			check(pass, file)
		}
	}
}
