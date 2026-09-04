package asm

import "github.com/siyul-park/minivm/internal/graph"

// dominance answers "does instruction a dominate instruction b" queries by
// combining block-level dominance — computed once by internal/graph — with
// intra-block position order: two instructions in the same block are
// ordered by index alone, since a block has one entry and one exit.
type dominance struct {
	cfg *cfg
	*graph.Dominance
}

// newDominance computes the immediate dominator of every block reachable
// from block 0 via internal/graph's iterative algorithm (Cooper, Harvey,
// and Kennedy, 2001).
func newDominance(g *cfg) *dominance {
	return &dominance{cfg: g, Dominance: graph.NewDominance(g)}
}

// dominates reports whether instruction a dominates instruction b: every
// control-flow path from the entry to b passes through a.
func (d *dominance) dominates(a, b int) bool {
	ba, bb := d.cfg.of[a], d.cfg.of[b]
	if ba == bb {
		return a <= b
	}
	return d.Dominates(ba, bb)
}
