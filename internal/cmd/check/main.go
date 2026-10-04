package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/siyul-park/minivm/internal/check"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

type result struct {
	pkg        *packages.Package
	diagnostic analysis.Diagnostic
}

type position struct {
	file   string
	line   int
	column int
}

type changedLines map[string]map[int]bool

var diffHunk = regexp.MustCompile(`@@ -[0-9]+(?:,[0-9]+)? \+([0-9]+)(?:,([0-9]+))? @@`)

func main() {
	fix := flag.Bool("fix", false, "apply suggested fixes and recheck")
	diffOnly := flag.Bool("diff", false, "report diagnostics in changes from main")
	jsonOutput := flag.Bool("json", false, "write one diagnostic object per line")
	listRules := flag.Bool("list-rules", false, "list rule IDs and exit")
	strict := flag.Bool("strict", false, "treat warnings as errors")
	flag.Parse()

	if *listRules {
		listRulesOutput()
		return
	}

	pkgs, err := load(flag.Args())
	if err != nil {
		fail(err)
	}

	results := analyze(pkgs)
	if *diffOnly {
		results, err = filterDiff(results, pkgs)
		if err != nil {
			fail(err)
		}
	}
	if *fix {
		if err := applyFixes(results); err != nil {
			fail(err)
		}
		pkgs, err = load(flag.Args())
		if err != nil {
			fail(err)
		}
		results = analyze(pkgs)
		if *diffOnly {
			results, err = filterDiff(results, pkgs)
			if err != nil {
				fail(err)
			}
		}
	}

	if err := writeDiagnostics(results, *jsonOutput); err != nil {
		fail(err)
	}
	errors, warnings := countSeverity(results)
	if errors != 0 || (*strict && warnings != 0) {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}

func filterDiff(results []result, pkgs []*packages.Package) ([]result, error) {
	lines, files, err := changedFiles()
	if err != nil {
		return nil, err
	}
	_ = pkgs
	filtered := results[:0]
	for _, result := range results {
		pos := result.position()
		rel, err := filepath.Rel(mustGetwd(), filepath.Clean(pos.file))
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		rule := ruleOf(result.diagnostic.Message)
		if files[rel] && diffFileRule(rule) || lines[rel][pos.line] {
			filtered = append(filtered, result)
		}
	}
	return filtered, nil
}

func diffFileRule(rule string) bool {
	switch rule {
	case "CP008", "CP009", "CP010", "CP011", "TP006":
		return true
	default:
		return false
	}
}

func changedFiles() (changedLines, map[string]bool, error) {
	cmd := exec.Command("git", "diff", "--unified=0", "main", "--", "*.go")
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return nil, nil, fmt.Errorf("git diff: %s", exit.Stderr)
		}
		return nil, nil, fmt.Errorf("git diff: %w", err)
	}
	lines := make(changedLines)
	files := make(map[string]bool)
	var file string
	var start, count int
	for _, raw := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(raw, "+++ b/"):
			file = filepath.ToSlash(strings.TrimPrefix(raw, "+++ b/"))
			files[file] = true
		case strings.HasPrefix(raw, "@@ "):
			match := diffHunk.FindStringSubmatch(raw)
			if match == nil {
				continue
			}
			start = atoi(match[1])
			count = 1
			if match[2] != "" {
				count = atoi(match[2])
			}
			if file != "" && count > 0 {
				if lines[file] == nil {
					lines[file] = make(map[int]bool)
				}
				for line := start; line < start+count; line++ {
					lines[file][line] = true
				}
			}
		case strings.HasPrefix(raw, "diff --git "):
			file = ""
		}
	}
	return lines, files, nil
}

func atoi(s string) int {
	value := 0
	for _, r := range s {
		value = value*10 + int(r-'0')
	}
	return value
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func load(patterns []string) ([]*packages.Package, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedTypesSizes,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}
	for _, pkg := range pkgs {
		for _, pkgErr := range pkg.Errors {
			return nil, fmt.Errorf("load %s: %s", pkg.PkgPath, pkgErr)
		}
	}
	return pkgs, nil
}

func analyze(pkgs []*packages.Package) []result {
	var results []result
	for _, pkg := range pkgs {
		pass := &analysis.Pass{
			Analyzer:   check.Analyzer,
			Fset:       pkg.Fset,
			Files:      pkg.Syntax,
			Pkg:        pkg.Types,
			TypesInfo:  pkg.TypesInfo,
			TypesSizes: pkg.TypesSizes,
			Report: func(d analysis.Diagnostic) {
				results = append(results, result{pkg: pkg, diagnostic: d})
			},
		}
		if _, err := check.Analyzer.Run(pass); err != nil {
			results = append(results, result{
				pkg: pkg,
				diagnostic: analysis.Diagnostic{
					Pos:     token.Pos(1),
					Message: "[internal] " + err.Error(),
				},
			})
		}
	}
	checkExternalTestPackages(pkgs, &results)
	checkOwnerTests(pkgs, &results)
	sort.Slice(results, func(i, j int) bool {
		a := results[i].position()
		b := results[j].position()
		if a.file != b.file {
			return a.file < b.file
		}
		if a.line != b.line {
			return a.line < b.line
		}
		if a.column != b.column {
			return a.column < b.column
		}
		return results[i].diagnostic.Message < results[j].diagnostic.Message
	})
	unique := results[:0]
	for _, result := range results {
		if len(unique) != 0 {
			last := unique[len(unique)-1]
			a := last.position()
			b := result.position()
			if a.file == b.file && a.line == b.line && a.column == b.column &&
				last.diagnostic.Message == result.diagnostic.Message {
				continue
			}
		}
		unique = append(unique, result)
	}
	return unique
}

func checkExternalTestPackages(pkgs []*packages.Package, results *[]result) {
	paths := make(map[string]bool)
	for _, pkg := range pkgs {
		if pkg.ID == pkg.PkgPath {
			paths[pkg.PkgPath] = true
		}
	}
	for _, pkg := range pkgs {
		if !paths[pkg.PkgPath] || pkg.ID == pkg.PkgPath {
			continue
		}
		for _, file := range pkg.Syntax {
			name := pkg.Fset.File(file.Pos()).Name()
			if !strings.HasSuffix(name, "_test.go") || strings.HasSuffix(file.Name.Name, "_test") {
				continue
			}
			*results = append(*results, result{
				pkg: pkg,
				diagnostic: analysis.Diagnostic{
					Pos:     file.Name.Pos(),
					Message: fmt.Sprintf("[TP001] feature tests must use an external package named %s_test", pkg.Name),
				},
			})
		}
	}
}

func checkOwnerTests(pkgs []*packages.Package, results *[]result) {
	for _, pkg := range pkgs {
		if pkg.ID != pkg.PkgPath {
			continue
		}
		testPkg := findTestPackage(pkgs, pkg.PkgPath)
		if testPkg == nil {
			continue
		}
		tests := make(map[string]int)
		for _, file := range testPkg.Syntax {
			name := testPkg.Fset.File(file.Pos()).Name()
			if !strings.HasSuffix(name, "_test.go") {
				continue
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
					tests[fn.Name.Name]++
				}
			}
		}
		for ident, obj := range pkg.TypesInfo.Defs {
			switch obj := obj.(type) {
			case *types.Func:
				if !obj.Exported() {
					continue
				}
				name := "Test" + obj.Name()
				if recv := obj.Type().(*types.Signature).Recv(); recv != nil {
					named, ok := derefNamed(recv.Type())
					if !ok || !named.Obj().Exported() {
						continue
					}
					if _, ok := named.Underlying().(*types.Interface); ok {
						continue
					}
					name = "Test" + named.Obj().Name() + "_" + obj.Name()
				}
				exact, extra := ownerTests(tests, name)
				reportOwnerTest(pkg, ident.Pos(), name, exact, extra, results)
			case *types.TypeName:
				if _, ok := obj.Type().(*types.TypeParam); ok {
					continue
				}
				if obj.Exported() {
					name := "Test" + obj.Name()
					exact, extra := ownerTests(tests, name, methodNames(obj.Type())...)
					reportOwnerTest(pkg, ident.Pos(), name, exact, extra, results)
				}
			}
		}
	}
}

func ownerTests(tests map[string]int, owner string, methods ...string) (exact, extra int) {
	exact = tests[owner]
	allowed := make(map[string]bool, len(methods))
	for _, method := range methods {
		allowed[owner+"_"+method] = true
	}
	prefix := owner + "_"
	for name, value := range tests {
		if strings.HasPrefix(name, prefix) && !allowed[name] {
			extra += value
		}
	}
	return exact, extra
}

func methodNames(typ types.Type) []string {
	named, ok := typ.(*types.Named)
	if !ok {
		return nil
	}
	seen := make(map[string]bool)
	var names []string
	for _, setType := range []types.Type{named, types.NewPointer(named)} {
		for i := 0; i < types.NewMethodSet(setType).Len(); i++ {
			name := types.NewMethodSet(setType).At(i).Obj().Name()
			if !types.NewMethodSet(setType).At(i).Obj().Exported() || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

func findTestPackage(pkgs []*packages.Package, path string) *packages.Package {
	for _, pkg := range pkgs {
		if pkg.PkgPath != path+"_test" && !(pkg.PkgPath == path && pkg.ID != path) {
			continue
		}
		for _, file := range pkg.Syntax {
			if strings.HasSuffix(pkg.Fset.File(file.Pos()).Name(), "_test.go") {
				return pkg
			}
		}
	}
	return nil
}

func derefNamed(typ types.Type) (*types.Named, bool) {
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	named, ok := typ.(*types.Named)
	return named, ok && named.Obj() != nil
}

func reportOwnerTest(pkg *packages.Package, pos token.Pos, name string, exact, extra int, results *[]result) {
	if exact == 1 && extra == 0 {
		return
	}
	severity := "warning"
	message := fmt.Sprintf("[TP005] public symbol has no top-level owner test %s", name)
	if exact > 0 && extra > 0 {
		severity = "error"
		count := exact + extra
		message = fmt.Sprintf("[TP005] public symbol is split across %d top-level tests; use one owner test %s", count, name)
	}
	*results = append(*results, result{pkg: pkg, diagnostic: analysis.Diagnostic{
		Pos:      pos,
		Category: severity,
		Message:  message,
	}})
}

func applyFixes(results []result) error {
	type edit struct {
		start int
		end   int
		text  []byte
	}
	edits := make(map[string][]edit)
	for _, result := range results {
		if len(result.diagnostic.SuggestedFixes) == 0 {
			continue
		}
		for _, change := range result.diagnostic.SuggestedFixes[0].TextEdits {
			pos := result.pkg.Fset.PositionFor(change.Pos, false)
			end := result.pkg.Fset.PositionFor(change.End, false)
			if pos.Filename == "" || end.Filename == "" || pos.Filename != end.Filename {
				return fmt.Errorf("invalid suggested fix for %s", result.diagnostic.Message)
			}
			file := result.pkg.Fset.File(change.Pos)
			if file == nil {
				return fmt.Errorf("invalid suggested fix position for %s", result.diagnostic.Message)
			}
			edits[pos.Filename] = append(edits[pos.Filename], edit{
				start: file.Offset(change.Pos),
				end:   file.Offset(change.End),
				text:  change.NewText,
			})
		}
	}

	for path, fileEdits := range edits {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s for fix: %w", path, err)
		}
		sort.Slice(fileEdits, func(i, j int) bool {
			return fileEdits[i].start > fileEdits[j].start
		})
		lastStart := len(data) + 1
		for _, edit := range fileEdits {
			if edit.end > lastStart || edit.start < 0 || edit.end < edit.start || edit.end > len(data) {
				return fmt.Errorf("overlapping suggested fixes in %s", path)
			}
			data = append(append(append([]byte{}, data[:edit.start]...), edit.text...), data[edit.end:]...)
			lastStart = edit.start
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return fmt.Errorf("write %s for fix: %w", path, err)
		}
	}
	return nil
}

func writeDiagnostics(results []result, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		for _, result := range results {
			pos := result.position()
			value := map[string]any{
				"rule":     ruleOf(result.diagnostic.Message),
				"severity": severityOf(result.diagnostic),
				"message":  result.diagnostic.Message,
				"file":     pos.file,
				"line":     pos.line,
				"column":   pos.column,
			}
			if err := encoder.Encode(value); err != nil {
				return err
			}
		}
		return nil
	}
	for _, result := range results {
		pos := result.position()
		fmt.Fprintf(os.Stderr, "%s:%d:%d: %s: %s\n",
			pos.file, pos.line, pos.column, severityOf(result.diagnostic), result.diagnostic.Message)
	}
	return nil
}

func (r result) position() position {
	pos := r.pkg.Fset.Position(r.diagnostic.Pos)
	return position{file: pos.Filename, line: pos.Line, column: pos.Column}
}

func countSeverity(results []result) (errors, warnings int) {
	for _, result := range results {
		if severityOf(result.diagnostic) == "warning" {
			warnings++
		} else {
			errors++
		}
	}
	return errors, warnings
}

func severityOf(diagnostic analysis.Diagnostic) string {
	if diagnostic.Category == "warning" {
		return "warning"
	}
	return "error"
}

func ruleOf(message string) string {
	if len(message) > 1 && message[0] == '[' {
		if end := strings.IndexByte(message, ']'); end > 1 {
			return message[1:end]
		}
	}
	return "internal"
}

func listRulesOutput() {
	for _, rule := range []string{
		"CP001 exported symbols have doc comments",
		"CP002 declarations follow file-order ownership groups",
		"CP003 context.Context is the first parameter",
		"CP004 constructors return concrete types",
		"CP005 receiver-owned methods stay in one file",
		"TP001 feature tests use an external test package",
		"TP002 t.Run nesting is at most one case level",
		"TP003 use the package poll helper instead of Eventually",
		"TP004 tests do not reference private target symbols",
		"CP006 dependents are declared before their dependencies",
		"CP007 private helpers have at least two callers [warning]",
		"CP008 high complexity is a review signal [warning]",
		"CP009 extreme fan-in/fan-out is a review signal [warning]",
		"CP010 near-clone symbols are a review signal [warning]",
		"CP011 separated similar siblings are a review signal [warning]",
		"TP005 one top-level owner test per public symbol [warning if missing, error if split]",
		"TP006 tests do not mix direct assertions with t.Run cases [warning]",
	} {
		fmt.Fprintln(os.Stdout, rule)
	}
}
