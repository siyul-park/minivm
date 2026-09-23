package analysis

import (
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/types"
)

// Headers returns the bytecode offset of every loop header in fn: a block
// some back edge targets, in the sense internal/graph.Headers defines it.
func Headers(fn *types.Function) ([]int, error) {
	blocks, err := Blocks(fn)
	if err != nil {
		return nil, err
	}
	g := cfg(blocks)
	headers := graph.Headers(g, graph.NewDominance(g))
	out := make([]int, len(headers))
	for i, h := range headers {
		out[i] = blocks[h].Start
	}
	return out, nil
}

// cfg adapts Blocks' block-indexed successors and predecessors to
// internal/graph's dense integer node space.
type cfg []*BasicBlock

// Len returns the number of blocks.
func (g cfg) Len() int { return len(g) }

// Succ returns a node's successors.
func (g cfg) Succ(node int) []int { return g[node].Succs }

// Pred returns a node's predecessors.
func (g cfg) Pred(node int) []int { return g[node].Preds }
