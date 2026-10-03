package transform

import (
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
)

// deduplicate replaces each keyed operation with the results of an earlier
// operation with the same key. The table holds the dominating definitions
// visible at the current block, so the walk goes down the dominator tree:
// NewDominance's children place every definition before the blocks it
// dominates, and a block's entries are retracted when the walk leaves its
// subtree, keeping a cheaply-visible replacement value correct only where it
// dominates the use.
func deduplicate(function *ssa.Function, key func(*ssa.Function, ssa.Operation) (string, bool)) (*ssa.Function, bool) {
	children := graph.NewDominance(function).Children()

	r := newRebuilder(function)
	table := map[string][]ssa.Value{}
	changed := false

	var walk func(block int)
	walk = func(block int) {
		id := r.open(block)
		b := function.Block(block)

		var pushed []string
		for _, operation := range b.Operations {
			operation = r.operation(operation)
			k, keyed := key(function, operation)
			if rep, seen := table[k]; keyed && seen {
				for i, old := range operation.Results {
					r.alias(old, rep[i])
				}
				changed = true
				continue
			}
			operation = r.define(operation)
			r.builder.Add(id, operation)
			if keyed {
				table[k] = operation.Results
				pushed = append(pushed, k)
			}
		}
		r.builder.Term(id, r.terminator(b.Terminator))

		for _, c := range children[block] {
			walk(c)
		}
		for _, k := range pushed {
			delete(table, k)
		}
	}
	walk(0)

	return r.builder.Build(), changed
}
