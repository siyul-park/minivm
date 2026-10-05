package vigil

import (
	"fmt"
	"go/ast"
	"go/types"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
)

type candidate struct {
	fn        *ast.FuncDecl
	owner     string
	signature string
	shape     []string
}

func clones(pass *analysis.Pass, separated bool) {
	candidates := candidates(pass)
	prefixes := prefixes(candidates)
	for i, left := range candidates {
		if len(left.shape) < 20 {
			continue
		}
		for j, right := range candidates[i+1:] {
			if len(right.shape) < 20 || left.owner != right.owner || left.signature != right.signature {
				continue
			}
			similarity := similarity(left.shape, right.shape)
			sameFile := pass.Fset.File(left.fn.Pos()).Name() == pass.Fset.File(right.fn.Pos()).Name()
			gap := j
			related := related(left.fn.Name.Name, right.fn.Name.Name, prefixes)
			if separated {
				if !sameFile || gap < 4 || similarity < 0.72 || !related {
					continue
				}
				report(pass, left.fn.Name.Pos(),
					"similar sibling symbols %s and %s are separated: similarity=%.2f distance=%d",
					left.fn.Name.Name, right.fn.Name.Name, similarity, gap)
				continue
			}
			threshold := 0.90
			if !sameFile {
				threshold = 0.95
			}
			same := related || shared(parts(left.fn.Name.Name), parts(right.fn.Name.Name))
			if (sameFile && gap <= 0) || (sameFile && gap >= 4 && similarity >= 0.72 && related) || similarity < threshold || !same {
				continue
			}
			report(pass, left.fn.Name.Pos(),
				"symbols %s and %s are near-clones: similarity=%.2f distance=%d",
				left.fn.Name.Name, right.fn.Name.Name, similarity, gap)
		}
	}
}

func candidates(pass *analysis.Pass) []*candidate {
	var funcs []*candidate
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
			sig, ok := obj.Type().(*types.Signature)
			if !ok {
				continue
			}
			funcs = append(funcs, &candidate{
				fn:        fn,
				owner:     receiver(fn.Recv),
				signature: signature(sig),
				shape:     shape(fn.Body),
			})
		}
	}
	sort.Slice(funcs, func(i, j int) bool {
		left := pass.Fset.Position(funcs[i].fn.Pos())
		right := pass.Fset.Position(funcs[j].fn.Pos())
		if left.Filename != right.Filename {
			return left.Filename < right.Filename
		}
		return left.Offset < right.Offset
	})
	return funcs
}

func signature(sig *types.Signature) string {
	params := sig.Params()
	results := sig.Results()
	return fmt.Sprintf("%d/%d/%t", params.Len(), results.Len(), sig.Variadic())
}

func shape(root ast.Node) []string {
	var shape []string
	ast.Inspect(root, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		if node != root {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		switch node := node.(type) {
		case *ast.BinaryExpr:
			shape = append(shape, "bin:"+node.Op.String())
		case *ast.UnaryExpr:
			shape = append(shape, "un:"+node.Op.String())
		case *ast.AssignStmt:
			shape = append(shape, "assign:"+node.Tok.String())
		case *ast.IncDecStmt:
			shape = append(shape, "inc:"+node.Tok.String())
		case *ast.BasicLit:
			shape = append(shape, "lit")
		case *ast.Ident:
			shape = append(shape, "id")
		default:
			shape = append(shape, reflect.TypeOf(node).String())
		}
		return true
	})
	return shape
}

func similarity(left, right []string) float64 {
	if len(left) < 5 || len(right) < 5 {
		return 0
	}
	a := shingles(left)
	b := shingles(right)
	intersection := 0
	for token := range a {
		if b[token] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func shingles(tokens []string) map[string]bool {
	const width = 5
	out := make(map[string]bool, len(tokens))
	for i := 0; i+width <= len(tokens); i++ {
		out[strings.Join(tokens[i:i+width], "|")] = true
	}
	return out
}

func prefixes(funcs []*candidate) map[string]bool {
	counts := make(map[string]int)
	signatures := make(map[string]map[string]bool)
	for _, fn := range funcs {
		parts := parts(fn.fn.Name.Name)
		if len(parts) <= 1 {
			continue
		}
		prefix := strings.ToLower(parts[0])
		counts[prefix]++
		if signatures[prefix] == nil {
			signatures[prefix] = make(map[string]bool)
		}
		signatures[prefix][fn.signature] = true
	}
	out := make(map[string]bool)
	for prefix, count := range counts {
		if count >= 4 && len(signatures[prefix]) >= 3 {
			out[prefix] = true
		}
	}
	return out
}

func related(left, right string, prefixes map[string]bool) bool {
	leftParts := parts(left)
	rightParts := parts(right)
	if prefix(leftParts, rightParts) || prefix(rightParts, leftParts) {
		return true
	}
	if len(leftParts) >= 2 && len(rightParts) >= 2 &&
		strings.EqualFold(leftParts[0], rightParts[0]) &&
		!generic(leftParts[0]) &&
		!prefixes[strings.ToLower(leftParts[0])] {
		return true
	}
	families := map[string]bool{
		"Add": true, "Sub": true, "Mul": true, "Div": true,
		"Get": true, "Set": true, "Load": true, "Store": true,
		"Encode": true, "Decode": true, "Marshal": true, "Unmarshal": true,
		"Box": true, "Unbox": true, "Enter": true, "Exit": true,
		"Before": true, "After": true, "First": true, "Last": true,
	}
	return families[left] && families[right]
}

func prefix(left, right []string) bool {
	if len(left) == 0 || len(left) > len(right) {
		return false
	}
	for i := range left {
		if !strings.EqualFold(left[i], right[i]) {
			return false
		}
	}
	return true
}

func shared(left, right []string) bool {
	for _, a := range left {
		if len(a) < 4 || generic(a) {
			continue
		}
		for _, b := range right {
			if strings.EqualFold(a, b) {
				return true
			}
		}
	}
	return false
}

func generic(name string) bool {
	switch strings.ToLower(name) {
	case "build", "check", "decode", "encode", "parse", "run":
		return true
	default:
		return false
	}
}

func parts(name string) []string {
	var parts []string
	start := 0
	for i := 1; i < len(name); i++ {
		if name[i] >= 'A' && name[i] <= 'Z' {
			parts = append(parts, name[start:i])
			start = i
		}
	}
	parts = append(parts, name[start:])
	return parts
}
