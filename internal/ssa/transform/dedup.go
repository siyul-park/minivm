package transform

import (
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
)

// dedup collapses every operation a dominating operation with the same key
// already performed, walking fn's dominator tree in preorder so a key
// established while entering a block stays visible to every block it
// dominates and is forgotten again once that whole subtree is done - the
// scoped hash-consing LLVM's EarlyCSE uses, over the dominator tree rather
// than the flat block order, since a fact from one arm of a branch must never
// leak into a sibling arm that never ran it.
//
// key sees each operation with its arguments already translated through the
// rebuild in progress, so two operations that only became equal because an
// earlier dedup or renumbering unified their inputs are still recognized; its
// bool return is whether key considers the operation dedup-eligible at all,
// and false leaves it untouched.
//
// This is the one mechanism cse.go and guard.go share: CSE keys a pure
// computation by its opcode and arguments, redundant guard elimination keys a
// guard by what it admits, and both are otherwise this same textbook
// dominator-tree-scoped value numbering. Neither ever needs to consult
// op.State or an OpState's Frames directly - dedup only ever elides a whole
// operation in favor of an equal, already-dominating one, and translates
// every value every surviving operation (including every OpState) reads
// through rebuilder.operation exactly as fold.go's simpler in-place rewrite
// does not have to, since fold.go never removes a value a frame could name.
func dedup(fn *ssa.Function, key func(*ssa.Function, ssa.Operation) (string, bool)) (*ssa.Function, bool) {
	dom := graph.NewDominance(fn)
	children := domChildren(fn, dom)

	rb := newRebuilder(fn)
	table := map[string][]ssa.Value{}
	changed := false

	var walk func(block int)
	walk = func(block int) {
		id := rb.block(block)
		blk := fn.Block(block)
		for _, p := range blk.Params {
			rb.alias(p, rb.b.Param(id, fn.Type(p)))
		}

		var pushed []string
		for _, op := range blk.Ops {
			op = rb.operation(op)
			if k, ok := key(fn, op); ok {
				if rep, seen := table[k]; seen {
					for i, old := range op.Results {
						rb.alias(old, rep[i])
					}
					changed = true
					continue
				}
				op = rb.define(fn, op)
				rb.b.Add(id, op)
				table[k] = op.Results
				pushed = append(pushed, k)
				continue
			}
			rb.b.Add(id, rb.define(fn, op))
		}
		rb.b.Term(id, rb.terminator(blk.Term))

		for _, c := range children[block] {
			walk(c)
		}
		for _, k := range pushed {
			delete(table, k)
		}
	}
	walk(0)

	return rb.b.Build(), changed
}

// domChildren returns fn's dominator tree as adjacency lists: children[b] is
// every block whose immediate dominator is b, in block-id order.
func domChildren(fn *ssa.Function, dom *graph.Dominance) [][]int {
	children := make([][]int, fn.Len())
	for b := 1; b < fn.Len(); b++ {
		p := dom.IDom(b)
		if p < 0 {
			continue
		}
		children[p] = append(children[p], b)
	}
	return children
}
