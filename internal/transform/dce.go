package transform

import (
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// DCEPass removes unreachable blocks, dead pure operations and block params
// nothing reads.
type DCEPass struct{}

type site struct {
	block int
	index int
}

var _ pass.Pass[*ssa.Function] = (*DCEPass)(nil)

// NewDCEPass returns the pass.
func NewDCEPass() *DCEPass {
	return &DCEPass{}
}

// Run applies the pass to one SSA function.
func (p *DCEPass) Run(_ *pass.Manager, function *ssa.Function) (bool, error) {
	blocks := graph.Order(function)
	live, params := liveness(function, blocks)

	r := newRebuilder(function)
	changed := len(blocks) != function.Len()
	for _, block := range blocks {
		id := r.block(block)
		for _, param := range function.Block(block).Params {
			if !params[param] {
				changed = true
				continue
			}
			r.alias(param, r.builder.Param(id, function.Type(param)))
		}
		b := function.Block(block)
		for i, operation := range b.Operations {
			if !live[site{block, i}] {
				changed = true
				continue
			}
			r.builder.Add(id, r.define(r.operation(operation)))
		}
		term := r.terminator(b.Terminator)
		for i, e := range b.Terminator.Edges {
			var kept []ssa.Value
			for j, param := range function.Block(e.Block).Params {
				if params[param] {
					kept = append(kept, term.Edges[i].Args[j])
				}
			}
			term.Edges[i].Args = kept
		}
		r.builder.Term(id, term)
	}

	if !changed {
		return true, nil
	}
	*function = *r.builder.Build()
	return false, nil
}

// liveness returns the live operations and the live block params. An entry
// param is always live; any other is live when an operation, a terminator or a
// live param's incoming edge reads it.
func liveness(function *ssa.Function, blocks []int) (map[site]bool, map[ssa.Value]bool) {
	defs := map[ssa.Value]site{}
	for _, b := range blocks {
		for i, operation := range function.Block(b).Operations {
			for _, r := range operation.Results {
				defs[r] = site{b, i}
			}
		}
	}

	where, incoming := map[ssa.Value][2]int{}, map[int][]ssa.Edge{}
	params := map[ssa.Value]bool{}
	for _, b := range blocks {
		for i, param := range function.Block(b).Params {
			if b == 0 {
				params[param] = true
			} else {
				where[param] = [2]int{b, i}
			}
		}
		for _, e := range function.Block(b).Terminator.Edges {
			incoming[e.Block] = append(incoming[e.Block], e)
		}
	}
	live := map[site]bool{}
	var queue []ssa.Value
	push := func(v ssa.Value) {
		if v != ssa.NoValue {
			queue = append(queue, v)
		}
	}
	mark := func(s site, operation ssa.Operation) {
		if live[s] {
			return
		}
		live[s] = true
		for _, a := range operation.Args {
			push(a)
		}
		for _, frame := range operation.Frames {
			for _, o := range frame.Stack {
				push(o.Value)
			}
			for _, l := range frame.Locals {
				push(l.Value)
			}
		}
		push(operation.State)
	}

	for _, b := range blocks {
		currentBlock := function.Block(b)
		for i, operation := range currentBlock.Operations {
			effectful := false
			switch operation.Op {
			case ssa.OpStore, ssa.OpGuardKind, ssa.OpGuardShape, ssa.OpGuardBounds, ssa.OpGuardValue,
				ssa.OpRetain, ssa.OpRelease:
				effectful = true
			case ssa.OpExec:
				effectful = !operation.Code.IsPure() || operation.Code.Traps()
			}
			if effectful {
				mark(site{b, i}, operation)
			}
		}
		for _, a := range currentBlock.Terminator.Args {
			push(a)
		}
		push(currentBlock.Terminator.State)
	}
	for i := range function.Block(0).Params {
		for _, e := range incoming[0] {
			push(e.Args[i])
		}
	}

	for len(queue) > 0 {
		v := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if at, ok := where[v]; ok {
			if !params[v] {
				params[v] = true
				for _, e := range incoming[at[0]] {
					push(e.Args[at[1]])
				}
			}
			continue
		}
		s, ok := defs[v]
		if !ok {
			continue
		}
		mark(s, function.Block(s.block).Operations[s.index])
	}
	return live, params
}
