package transform

import (
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
)

func deduplicate(function *ssa.Function, key func(*ssa.Function, ssa.Operation) (string, bool)) (*ssa.Function, bool) {
	dominance := graph.NewDominance(function)
	children := dominatorChildren(function, dominance)

	rebuilder := newRebuilder(function)
	table := map[string][]ssa.Value{}
	changed := false

	var walk func(block int)
	walk = func(block int) {
		id := rebuilder.block(block)
		currentBlock := function.Block(block)
		for _, p := range currentBlock.Params {
			rebuilder.alias(p, rebuilder.builder.Param(id, function.Type(p)))
		}

		var pushed []string
		for _, operation := range currentBlock.Operations {
			operation = rebuilder.operation(operation)
			if k, ok := key(function, operation); ok {
				if rep, seen := table[k]; seen {
					for i, old := range operation.Results {
						rebuilder.alias(old, rep[i])
					}
					changed = true
					continue
				}
				operation = rebuilder.define(function, operation)
				rebuilder.builder.Add(id, operation)
				table[k] = operation.Results
				pushed = append(pushed, k)
				continue
			}
			rebuilder.builder.Add(id, rebuilder.define(function, operation))
		}
		rebuilder.builder.Term(id, rebuilder.terminator(currentBlock.Terminator))

		for _, c := range children[block] {
			walk(c)
		}
		for _, k := range pushed {
			delete(table, k)
		}
	}
	walk(0)

	return rebuilder.builder.Build(), changed
}
