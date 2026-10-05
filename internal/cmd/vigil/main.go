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

type finding struct {
	pkg        *packages.Package
	diagnostic analysis.Diagnostic
}

type position struct {
	file   string
	line   int
	column int
}

type lines map[string]map[int]bool

type rule struct {
	id          string
	description string
	severity    string
	run         func([]*packages.Package, *[]finding)
}

var diffHunk = regexp.MustCompile(`@@ -[0-9]+(?:,[0-9]+)? \+([0-9]+)(?:,([0-9]+))? @@`)

var checks = []rule{
	{id: "TP001", description: "feature tests use an external test package", severity: "error", run: external},
	{id: "TP005", description: "one semantic owner test per public symbol", severity: "mixed", run: ownership},
}

func main() {
	applyFlag := flag.Bool("fix", false, "apply suggested fixes and recheck")
	diffOnly := flag.Bool("diff", false, "report diagnostics in changes from main")
	jsonOutput := flag.Bool("json", false, "write one diagnostic object per line")
	listRules := flag.Bool("list-rules", false, "list rule IDs and exit")
	ruleList := flag.String("rules", "", "comma-separated rule IDs to report")
	strict := flag.Bool("strict", false, "treat warnings as errors")
	flag.Parse()

	if *listRules {
		list()
		return
	}

	rules, err := selectRules(*ruleList)
	if err != nil {
		fail(err)
	}

	apply := *applyFlag
	var findings []finding
	for {
		pkgs, err := load(flag.Args())
		if err != nil {
			fail(err)
		}

		findings = analyze(pkgs)
		if *diffOnly {
			findings, err = diff(findings)
			if err != nil {
				fail(err)
			}
		}
		findings = filter(findings, rules)
		if !apply {
			break
		}
		if err := fix(findings); err != nil {
			fail(err)
		}
		apply = false
	}

	if err := write(findings, *jsonOutput); err != nil {
		fail(err)
	}
	errors, warnings := count(findings)
	if errors != 0 || (*strict && warnings != 0) {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}

func selectRules(value string) (map[string]bool, error) {
	if value == "" {
		return nil, nil
	}
	known := make(map[string]bool)
	for _, text := range vigil.Rules() {
		known[strings.Fields(text)[0]] = true
	}
	for _, check := range checks {
		known[check.id] = true
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

func filter(findings []finding, selected map[string]bool) []finding {
	if len(selected) == 0 {
		return findings
	}
	out := findings[:0]
	for _, finding := range findings {
		if selected[id(finding.diagnostic.Message)] {
			out = append(out, finding)
		}
	}
	return out
}

func diff(findings []finding) ([]finding, error) {
	lines, err := changed()
	if err != nil {
		return nil, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	filtered := findings[:0]
	for _, finding := range findings {
		pos := finding.position()
		rel, err := filepath.Rel(wd, filepath.Clean(pos.file))
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		check := id(finding.diagnostic.Message)
		_, changedFile := lines[rel]
		if changedFile && file(check) || lines[rel][pos.line] {
			filtered = append(filtered, finding)
		}
	}
	return filtered, nil
}

func file(rule string) bool {
	switch rule {
	case "CP008", "CP009", "CP010", "CP011":
		return true
	default:
		return false
	}
}

func changed() (lines, error) {
	cmd := exec.Command("git", "diff", "--unified=0", "main", "--", "*.go")
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git diff: %s", exit.Stderr)
		}
		return nil, fmt.Errorf("git diff: %w", err)
	}
	lines := make(lines)
	var file string
	var start, count int
	for _, raw := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(raw, "+++ b/"):
			file = filepath.ToSlash(strings.TrimPrefix(raw, "+++ b/"))
			if lines[file] == nil {
				lines[file] = make(map[int]bool)
			}
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
	return lines, nil
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

func analyze(pkgs []*packages.Package) []finding {
	var findings []finding
	for _, pkg := range pkgs {
		pass := &analysis.Pass{
			Analyzer:   vigil.Analyzer,
			Fset:       pkg.Fset,
			Files:      pkg.Syntax,
			Pkg:        pkg.Types,
			TypesInfo:  pkg.TypesInfo,
			TypesSizes: pkg.TypesSizes,
			Report: func(d analysis.Diagnostic) {
				findings = append(findings, finding{pkg: pkg, diagnostic: d})
			},
		}
		if _, err := vigil.Analyzer.Run(pass); err != nil {
			findings = append(findings, finding{
				pkg: pkg,
				diagnostic: analysis.Diagnostic{
					Pos:     token.Pos(1),
					Message: "[internal] " + err.Error(),
				},
			})
		}
	}
	rules(pkgs, &findings)
	sort.Slice(findings, func(i, j int) bool {
		a := findings[i].position()
		b := findings[j].position()
		if a.file != b.file {
			return a.file < b.file
		}
		if a.line != b.line {
			return a.line < b.line
		}
		if a.column != b.column {
			return a.column < b.column
		}
		return findings[i].diagnostic.Message < findings[j].diagnostic.Message
	})
	unique := findings[:0]
	for _, finding := range findings {
		if len(unique) != 0 {
			last := unique[len(unique)-1]
			a := last.position()
			b := finding.position()
			if a.file == b.file && a.line == b.line && a.column == b.column &&
				last.diagnostic.Message == finding.diagnostic.Message {
				continue
			}
		}
		unique = append(unique, finding)
	}
	return unique
}

func rules(pkgs []*packages.Package, findings *[]finding) {
	for _, check := range checks {
		start := len(*findings)
		check.run(pkgs, findings)
		for i := start; i < len(*findings); i++ {
			diagnostic := &(*findings)[i].diagnostic
			diagnostic.Message = "[" + check.id + "] " + diagnostic.Message
			if diagnostic.Category == "" {
				diagnostic.Category = check.severity
			}
		}
	}
}

func external(pkgs []*packages.Package, findings *[]finding) {
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
			*findings = append(*findings, finding{
				pkg: pkg,
				diagnostic: analysis.Diagnostic{
					Pos:     file.Name.Pos(),
					Message: fmt.Sprintf("feature tests must use an external package named %s_test", pkg.Name),
				},
			})
		}
	}
}

func ownership(pkgs []*packages.Package, findings *[]finding) {
	for _, pkg := range pkgs {
		if pkg.ID != pkg.PkgPath {
			continue
		}
		testPkg := tests(pkgs, pkg.PkgPath)
		for ident, obj := range pkg.TypesInfo.Defs {
			switch obj := obj.(type) {
			case *types.Func:
				if obj.Exported() {
					report(pkg, ident.Pos(), obj, owners(testPkg, obj), findings)
				}
			case *types.TypeName:
				if _, ok := obj.Type().(*types.TypeParam); ok {
					continue
				}
				if obj.Exported() {
					report(pkg, ident.Pos(), obj, owners(testPkg, obj), findings)
				}
			}
		}
	}
}

func owners(testPkg *packages.Package, target types.Object) []string {
	if testPkg == nil {
		return nil
	}
	expected := expected(target)
	allowPrefix := false
	if _, ok := target.(*types.Func); ok {
		allowPrefix = true
	}
	var named, used []string
	for _, file := range testPkg.Syntax {
		if !strings.HasSuffix(testPkg.Fset.File(file.Pos()).Name(), "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			matches := expected != "" && (fn.Name.Name == expected || allowPrefix && strings.HasPrefix(fn.Name.Name, expected+"_"))
			found := false
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if found {
					return false
				}
				switch node := node.(type) {
				case *ast.Ident:
					found = testPkg.TypesInfo.Uses[node] == target
				case *ast.SelectorExpr:
					if selection := testPkg.TypesInfo.Selections[node]; selection != nil {
						found = selection.Obj() == target
					} else {
						found = testPkg.TypesInfo.Uses[node.Sel] == target
					}
				}
				return !found
			})
			if found {
				used = append(used, fn.Name.Name)
				if matches {
					named = append(named, fn.Name.Name)
				}
			}
		}
	}
	if len(named) != 0 {
		sort.Strings(named)
		return named
	}
	if len(used) == 1 {
		return used
	}
	return nil
}

func tests(pkgs []*packages.Package, path string) *packages.Package {
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

func report(pkg *packages.Package, pos token.Pos, obj types.Object, owners []string, findings *[]finding) {
	if expected(obj) == "" || len(owners) == 1 {
		return
	}
	category := "warning"
	message := fmt.Sprintf("public symbol %s has no semantic owner test", obj.Name())
	if len(owners) > 1 {
		category = "error"
		message = fmt.Sprintf("public symbol %s is exercised by multiple top-level tests: %s", obj.Name(), strings.Join(owners, ", "))
	}
	*findings = append(*findings, finding{pkg: pkg, diagnostic: analysis.Diagnostic{
		Pos:      pos,
		Category: category,
		Message:  message,
	}})
}

func expected(obj types.Object) string {
	switch obj := obj.(type) {
	case *types.Func:
		sig, ok := obj.Type().(*types.Signature)
		if !ok {
			return ""
		}
		if recv := sig.Recv(); recv != nil {
			typ := recv.Type()
			if ptr, ok := typ.(*types.Pointer); ok {
				typ = ptr.Elem()
			}
			named, ok := typ.(*types.Named)
			if !ok || !named.Obj().Exported() {
				return ""
			}
			if _, ok := named.Underlying().(*types.Interface); ok {
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

func fix(findings []finding) error {
	type edit struct {
		start int
		end   int
		text  []byte
	}
	edits := make(map[string][]edit)
	for _, finding := range findings {
		if len(finding.diagnostic.SuggestedFixes) == 0 {
			continue
		}
		for _, change := range finding.diagnostic.SuggestedFixes[0].TextEdits {
			pos := finding.pkg.Fset.PositionFor(change.Pos, false)
			end := finding.pkg.Fset.PositionFor(change.End, false)
			if pos.Filename == "" || end.Filename == "" || pos.Filename != end.Filename {
				return fmt.Errorf("invalid suggested fix for %s", finding.diagnostic.Message)
			}
			file := finding.pkg.Fset.File(change.Pos)
			if file == nil {
				return fmt.Errorf("invalid suggested fix position for %s", finding.diagnostic.Message)
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

func write(findings []finding, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		for _, finding := range findings {
			pos := finding.position()
			value := map[string]any{
				"rule":     id(finding.diagnostic.Message),
				"severity": severity(finding.diagnostic),
				"message":  finding.diagnostic.Message,
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
	for _, finding := range findings {
		pos := finding.position()
		fmt.Fprintf(os.Stderr, "%s:%d:%d: %s: %s\n",
			pos.file, pos.line, pos.column, severity(finding.diagnostic), finding.diagnostic.Message)
	}
	return nil
}

func (r finding) position() position {
	pos := r.pkg.Fset.Position(r.diagnostic.Pos)
	return position{file: pos.Filename, line: pos.Line, column: pos.Column}
}

func count(findings []finding) (errors, warnings int) {
	for _, finding := range findings {
		if severity(finding.diagnostic) == "warning" {
			warnings++
		} else {
			errors++
		}
	}
	return errors, warnings
}

func severity(diagnostic analysis.Diagnostic) string {
	if diagnostic.Category == "warning" {
		return "warning"
	}
	return "error"
}

func id(message string) string {
	if len(message) > 1 && message[0] == '[' {
		if end := strings.IndexByte(message, ']'); end > 1 {
			return message[1:end]
		}
	}
	return "internal"
}

func list() {
	for _, text := range vigil.Rules() {
		fmt.Fprintln(os.Stdout, text)
	}
	for _, check := range checks {
		fmt.Fprintln(os.Stdout, format(check))
	}
}

func format(check rule) string {
	suffix := ""
	switch check.severity {
	case "warning":
		suffix = " [warning]"
	case "mixed":
		suffix = " [warning if missing, error if split]"
	}
	return check.id + " " + check.description + suffix
}
