package transform

import (
	"maps"

	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// ForwardPass replaces a load of a slot by the value an earlier load read from
// it, or an earlier store wrote to it, when the slot is unchanged and the types
// agree. A stored reference is not forwarded, because the slot owns it, nor is
// a stored i64, because its load is guarded. A held local is dropped at a CALL.
type ForwardPass struct{}

var _ pass.Pass[*ssa.Function] = (*ForwardPass)(nil)

var effects = [...]ssa.Effect{
	ssa.SpaceLocal:  ssa.EffectLocal,
	ssa.SpaceGlobal: ssa.EffectGlobal,
	ssa.SpaceUpval:  ssa.EffectUpval,
}

// NewForwardPass returns the pass.
func NewForwardPass() *ForwardPass {
	return &ForwardPass{}
}

// Run applies the pass to one SSA function.
func (p *ForwardPass) Run(_ *pass.Manager, function *ssa.Function) (bool, error) {
	children := graph.NewDominance(function).Children()

	r := newRebuilder(function)
	changed := false

	var walk func(block int, held map[ssa.Slot]ssa.Value)
	walk = func(block int, held map[ssa.Slot]ssa.Value) {
		id := r.open(block)
		b := function.Block(block)
		if len(function.Pred(block)) != 1 {
			clear(held)
		}

		for _, operation := range b.Operations {
			operation = r.operation(operation)
			switch operation.Op {
			case ssa.OpLoad:
				if at, ok := held[operation.Slot]; ok && r.builder.Type(at) == function.Type(operation.Results[0]) {
					r.alias(operation.Results[0], at)
					changed = true
					continue
				}
			case ssa.OpStore:
				delete(held, operation.Slot)
				// An i64 load carries a kind guard that the stored raw value
				// cannot satisfy, so only a scalar of another type is held.
				if at := operation.Args[0]; r.builder.Type(at) != ssa.TypeRef && r.builder.Type(at) != ssa.TypeI64 {
					held[operation.Slot] = at
				}
			default:
				// Drop the held values the operation may overwrite.
				for slot := range held {
					// A local is reloaded after a call: a load is cheaper than a
					// value kept live, and so spilled, across the call.
					if operation.Writes()&effects[slot.Space] != 0 || (slot.Space == ssa.SpaceLocal && operation.Op == ssa.OpCall) {
						delete(held, slot)
					}
				}
			}
			operation = r.define(operation)
			if operation.Op == ssa.OpLoad {
				held[operation.Slot] = operation.Results[0]
			}
			r.builder.Add(id, operation)
		}
		r.builder.Term(id, r.terminator(b.Terminator))

		for _, child := range children[block] {
			walk(child, maps.Clone(held))
		}
	}
	walk(0, map[ssa.Slot]ssa.Value{})

	if !changed {
		return true, nil
	}
	*function = *r.builder.Build()
	return false, nil
}
