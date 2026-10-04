package check

import (
	"fmt"
	"go/ast"
	"go/types"
	"reflect"
	"strings"

	"golang.org/x/tools/go/analysis"
)

type cloneMetric struct {
	fn        *ast.FuncDecl
	index     int
	owner     string
	signature string
	shape     []string
}

func checkClones(pass *analysis.Pass) {
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
			continue
		}
		var funcs []*cloneMetric
		for index, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			obj := pass.TypesInfo.ObjectOf(fn.Name)
			if obj == nil {
				continue
			}
			signature, ok := obj.Type().(*types.Signature)
			if !ok {
				continue
			}
			funcs = append(funcs, &cloneMetric{
				fn:        fn,
				index:     index,
				owner:     receiverName(fn.Recv),
				signature: signatureKey(signature),
				shape:     normalizedShape(fn.Body),
			})
		}

		genericPrefixes := genericPrefixes(funcs)
		reportedClone := make(map[[2]int]bool)
		reportedPair := make(map[[2]int]bool)
		for i := 0; i < len(funcs); i++ {
			left := funcs[i]
			if len(left.shape) < 20 {
				continue
			}
			for j := i + 1; j < len(funcs); j++ {
				right := funcs[j]
				if len(right.shape) < 20 || left.owner != right.owner || left.signature != right.signature {
					continue
				}
				similarity := shapeSimilarity(left.shape, right.shape)
				gap := right.index - left.index - 1
				key := [2]int{left.index, right.index}

				symmetric := symmetricNames(left.fn.Name.Name, right.fn.Name.Name, genericPrefixes)
				if gap >= 4 && similarity >= 0.72 && symmetric && !reportedPair[key] {
					report(pass, "CP011", left.fn.Name.Pos(),
						"similar sibling symbols %s and %s are separated: similarity=%.2f distance=%d",
						left.fn.Name.Name, right.fn.Name.Name, similarity, gap)
					reportedPair[key] = true
					continue
				}
				leftPrefix := nameParts(left.fn.Name.Name)
				rightPrefix := nameParts(right.fn.Name.Name)
				sharedGeneric := len(leftPrefix) > 0 && len(rightPrefix) > 0 &&
					strings.EqualFold(leftPrefix[0], rightPrefix[0]) &&
					genericPrefixes[strings.ToLower(leftPrefix[0])]
				if gap > 0 && similarity >= 0.90 && !symmetric && !sharedGeneric && !reportedClone[key] {
					report(pass, "CP010", left.fn.Name.Pos(),
						"symbols %s and %s are near-clones: similarity=%.2f distance=%d",
						left.fn.Name.Name, right.fn.Name.Name, similarity, gap)
					reportedClone[key] = true
				}
			}
		}
	}
}

func signatureKey(signature *types.Signature) string {
	params := signature.Params()
	results := signature.Results()
	return fmt.Sprintf("%d/%d/%t", params.Len(), results.Len(), signature.Variadic())
}

func normalizedShape(root ast.Node) []string {
	var shape []string
	ast.Inspect(root, func(node ast.Node) bool {
		if node == nil {
			return true
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

func shapeSimilarity(left, right []string) float64 {
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

func genericPrefixes(funcs []*cloneMetric) map[string]bool {
	counts := make(map[string]int)
	signatures := make(map[string]map[string]bool)
	for _, fn := range funcs {
		parts := nameParts(fn.fn.Name.Name)
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

func symmetricNames(left, right string, genericPrefixes map[string]bool) bool {
	leftParts := nameParts(left)
	rightParts := nameParts(right)
	if namePrefix(leftParts, rightParts) || namePrefix(rightParts, leftParts) {
		return true
	}
	if len(leftParts) >= 2 && len(rightParts) >= 2 &&
		strings.EqualFold(leftParts[0], rightParts[0]) &&
		!genericPrefixes[strings.ToLower(leftParts[0])] {
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

func namePrefix(left, right []string) bool {
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

func nameParts(name string) []string {
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
