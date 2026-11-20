package transform

import (
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// SimplifyPass replaces trivial block params, which every incoming edge feeds
// one value of their type or the param itself, by that value, and merges a
// block that only one jump reaches into the block that jumps to it.
type SimplifyPass struct{}

var _ pass.Pass[*ssa.Function] = (*SimplifyPass)(nil)

// NewSimplifyPass returns the pass.
func NewSimplifyPass() *SimplifyPass {
	return &SimplifyPass{}
}

// Run applies the pass to one SSA function.
func (p *SimplifyPass) Run(_ *pass.Manager, function *ssa.Function) (bool, error) {
	blocks := graph.Order(function)
	incoming := map[int][]ssa.Edge{}
	for _, b := range blocks {
		for _, e := range function.Block(b).Terminator.Edges {
			incoming[e.Block] = append(incoming[e.Block], e)
		}
	}
	same := trivial(function, blocks, incoming)
	next := merges(function, blocks, same, incoming)
	if len(same) == 0 && len(next) == 0 && len(blocks) == function.Len() {
		return true, nil
	}

	joined := map[int]bool{}
	for _, to := range next {
		joined[to] = true
	}

	r := newRebuilder(function)
	for _, head := range blocks {
		if joined[head] {
			continue
		}
		id := r.block(head)
		block := head
		for {
			b := function.Block(block)
			for _, param := range b.Params {
				if v, ok := same[param]; ok {
					r.alias(param, r.value(v))
					continue
				}
				r.alias(param, r.builder.Param(id, function.Type(param)))
			}
			for _, operation := range b.Operations {
				r.builder.Add(id, r.define(r.operation(operation)))
			}
			if to, ok := next[block]; ok {
				block = to
				continue
			}
			r.builder.Term(id, r.prune(b.Terminator, func(param ssa.Value) bool {
				_, ok := same[param]
				return ok
			}))
			break
		}
	}
	*function = *r.builder.Build()
	return false, nil
}

// trivial maps each non-entry block param whose incoming edges, from the given
// blocks, feed one value of its type and possibly itself to that value, which
// is never itself a replaced param. incoming holds each block's edges.
func trivial(function *ssa.Function, blocks []int, incoming map[int][]ssa.Edge) map[ssa.Value]ssa.Value {
	same := map[ssa.Value]ssa.Value{}
	find := func(v ssa.Value) ssa.Value {
		root := v
		for {
			next, ok := same[root]
			if !ok {
				break
			}
			root = next
		}
		for v != root {
			next := same[v]
			same[v] = root
			v = next
		}
		return root
	}
	for changed := true; changed; {
		changed = false
		for _, b := range blocks {
			if b == 0 {
				continue
			}
			for i, param := range function.Block(b).Params {
				if _, ok := same[param]; ok {
					continue
				}
				v := ssa.NoValue
				for _, e := range incoming[b] {
					arg := find(e.Args[i])
					if arg == param || arg == v {
						continue
					}
					if v != ssa.NoValue {
						v = ssa.NoValue
						break
					}
					v = arg
				}
				if v != ssa.NoValue && function.Type(v) == function.Type(param) {
					same[param] = v
					changed = true
				}
			}
		}
	}
	for param := range same {
		same[param] = find(param)
	}
	return same
}

// merges maps each block that jumps to a non-entry block, itself reached by no
// other edge from the given blocks and with every param in same, to that block.
func merges(function *ssa.Function, blocks []int, same map[ssa.Value]ssa.Value, incoming map[int][]ssa.Edge) map[int]int {
	next := map[int]int{}
	for _, b := range blocks {
		term := function.Block(b).Terminator
		if term.Term != ssa.TermJump {
			continue
		}
		to := term.Edges[0].Block
		if to == 0 || to == b || len(incoming[to]) != 1 {
			continue
		}
		merged := true
		for _, param := range function.Block(to).Params {
			if _, ok := same[param]; !ok {
				merged = false
			}
		}
		if merged {
			next[b] = to
		}
	}
	return next
}
