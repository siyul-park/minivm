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

	"github.com/siyul-park/minivm/internal/vigil"
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

type packageRule struct {
	id          string
	description string
	severity    string
	run         func([]*packages.Package, *[]result)
}

var diffHunk = regexp.MustCompile(`@@ -[0-9]+(?:,[0-9]+)? \+([0-9]+)(?:,([0-9]+))? @@`)

var packageRules = []packageRule{
	{id: "TP001", description: "feature tests use an external test package", severity: "error", run: checkExternalTestPackages},
	{id: "TP005", description: "one semantic owner test per public symbol", severity: "mixed", run: checkOwnerTests},
}

func main() {
	fix := flag.Bool("fix", false, "apply suggested fixes and recheck")
	diffOnly := flag.Bool("diff", false, "report diagnostics in changes from main")
	jsonOutput := flag.Bool("json", false, "write one diagnostic object per line")
	listRules := flag.Bool("list-rules", false, "list rule IDs and exit")
	ruleList := flag.String("rules", "", "comma-separated rule IDs to report")
	strict := flag.Bool("strict", false, "treat warnings as errors")
	flag.Parse()

	if *listRules {
		listRulesOutput()
		return
	}

	rules, err := parseRules(*ruleList)
	if err != nil {
		fail(err)
	}

	pkgs, err := load(flag.Args())
	if err != nil {
		fail(err)
	}

	results := analyze(pkgs)
	if *diffOnly {
		results, err = filterDiff(results)
		if err != nil {
			fail(err)
		}
	}
	results = filterRules(results, rules)
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
			results, err = filterDiff(results)
			if err != nil {
				fail(err)
			}
		}
		results = filterRules(results, rules)
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

func parseRules(value string) (map[string]bool, error) {
	if value == "" {
		return nil, nil
	}
	known := make(map[string]bool)
	for _, text := range vigil.Rules() {
		known[strings.Fields(text)[0]] = true
	}
	for _, rule := range packageRules {
		known[rule.id] = true
	}
	selected := make(map[string]bool)
	for _, id := range strings.Split(value, ",") {
		id = strings.TrimSpace(id)
		if id == "" || !known[id] {
			return nil, fmt.Errorf("unknown rule %q", id)
		}
		selected[id] = true
	}
	return selected, nil
}

func filterRules(results []result, selected map[string]bool) []result {
	if len(selected) == 0 {
		return results
	}
	out := results[:0]
	for _, result := range results {
		if selected[ruleOf(result.diagnostic.Message)] {
			out = append(out, result)
		}
	}
	return out
}

func filterDiff(results []result) ([]result, error) {
	lines, files, err := changedFiles()
	if err != nil {
		return nil, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	filtered := results[:0]
	for _, result := range results {
		pos := result.position()
		rel, err := filepath.Rel(wd, filepath.Clean(pos.file))
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
	case "CP008", "CP009", "CP010", "CP011":
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
			Analyzer:   vigil.Analyzer,
			Fset:       pkg.Fset,
			Files:      pkg.Syntax,
			Pkg:        pkg.Types,
			TypesInfo:  pkg.TypesInfo,
			TypesSizes: pkg.TypesSizes,
			Report: func(d analysis.Diagnostic) {
				results = append(results, result{pkg: pkg, diagnostic: d})
			},
		}
		if _, err := vigil.Analyzer.Run(pass); err != nil {
			results = append(results, result{
				pkg: pkg,
				diagnostic: analysis.Diagnostic{
					Pos:     token.Pos(1),
					Message: "[internal] " + err.Error(),
				},
			})
		}
	}
	runPackageRules(pkgs, &results)
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

func runPackageRules(pkgs []*packages.Package, results *[]result) {
	for _, rule := range packageRules {
		start := len(*results)
		rule.run(pkgs, results)
		for i := start; i < len(*results); i++ {
			diagnostic := &(*results)[i].diagnostic
			diagnostic.Message = "[" + rule.id + "] " + diagnostic.Message
			if diagnostic.Category == "" {
				diagnostic.Category = rule.severity
			}
		}
	}
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
					Message: fmt.Sprintf("feature tests must use an external package named %s_test", pkg.Name),
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
		for ident, obj := range pkg.TypesInfo.Defs {
			switch obj := obj.(type) {
			case *types.Func:
				if obj.Exported() {
					reportOwnerTest(pkg, ident.Pos(), obj, semanticOwnerTests(testPkg, obj), results)
				}
			case *types.TypeName:
				if _, ok := obj.Type().(*types.TypeParam); ok {
					continue
				}
				if obj.Exported() {
					reportOwnerTest(pkg, ident.Pos(), obj, semanticOwnerTests(testPkg, obj), results)
				}
			}
		}
	}
}

func semanticOwnerTests(testPkg *packages.Package, target types.Object) []string {
	if testPkg == nil {
		return nil
	}
	expected := ownerTestName(target)
	if expected == "" {
		return nil
	}
	prefix := expected + "_"
	allowPrefix := false
	if fn, ok := target.(*types.Func); ok {
		sig, ok := fn.Type().(*types.Signature)
		allowPrefix = ok && sig.Recv() == nil
	}
	var owners []string
	for _, file := range testPkg.Syntax {
		if !strings.HasSuffix(testPkg.Fset.File(file.Pos()).Name(), "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			if fn.Name.Name != expected && (!allowPrefix || !strings.HasPrefix(fn.Name.Name, prefix)) {
				continue
			}
			found := false
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if found {
					return false
				}
				if ident, ok := node.(*ast.Ident); ok && testPkg.TypesInfo.Uses[ident] == target {
					found = true
					return false
				}
				sel, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if selection := testPkg.TypesInfo.Selections[sel]; selection != nil && selection.Obj() == target {
					found = true
				}
				return !found
			})
			if found {
				owners = append(owners, fn.Name.Name)
			}
		}
	}
	sort.Strings(owners)
	return owners
}

func ownerTestName(obj types.Object) string {
	switch obj := obj.(type) {
	case *types.Func:
		sig, ok := obj.Type().(*types.Signature)
		if !ok {
			return ""
		}
		if recv := sig.Recv(); recv != nil {
			named, ok := derefNamed(recv.Type())
			if !ok || !named.Obj().Exported() {
				return ""
			}
			return "Test" + named.Obj().Name() + "_" + obj.Name()
		}
		return "Test" + obj.Name()
	case *types.TypeName, *types.Const, *types.Var:
		return "Test" + obj.Name()
	default:
		return ""
	}
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

func reportOwnerTest(pkg *packages.Package, pos token.Pos, obj types.Object, owners []string, results *[]result) {
	if len(owners) == 1 {
		return
	}
	category := "warning"
	message := fmt.Sprintf("public symbol %s has no semantic owner test", obj.Name())
	if len(owners) > 1 {
		category = "error"
		message = fmt.Sprintf("public symbol %s is exercised by multiple top-level tests: %s", obj.Name(), strings.Join(owners, ", "))
	}
	*results = append(*results, result{pkg: pkg, diagnostic: analysis.Diagnostic{
		Pos:      pos,
		Category: category,
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
	for _, text := range vigil.Rules() {
		fmt.Fprintln(os.Stdout, text)
	}
	for _, rule := range packageRules {
		fmt.Fprintln(os.Stdout, formatRule(rule))
	}
}

func formatRule(rule packageRule) string {
	suffix := ""
	switch rule.severity {
	case "warning":
		suffix = " [warning]"
	case "mixed":
		suffix = " [warning if missing, error if split]"
	}
	return rule.id + " " + rule.description + suffix
}
