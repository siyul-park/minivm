package transform

import (
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
)

func dominatorChildren(function *ssa.Function, dominance *graph.Dominance) [][]int {
	children := make([][]int, function.Len())
	for b := 1; b < function.Len(); b++ {
		p := dominance.IDom(b)
		if p < 0 {
			continue
		}
		children[p] = append(children[p], b)
	}
	return children
}
