package transform

import (
	"maps"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// ForwardPass replaces a load of a slot by the value an earlier load read from
// it, or an earlier store wrote to it, when the slot is unchanged and the types
// agree. A stored reference is not forwarded, because the slot owns it, nor is
// a stored i64, because its load is guarded.
type ForwardPass struct{}

var _ pass.Pass[*ssa.Function] = (*ForwardPass)(nil)

var effects = [...]instr.Effect{
	ssa.SpaceLocal:  instr.Local,
	ssa.SpaceGlobal: instr.Global,
	ssa.SpaceUpval:  instr.Upval,
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
			case ssa.OpExec:
				// Drop the held values the code may overwrite.
				for slot := range held {
					// A local is reloaded after CALL: a load is cheaper than a
					// value kept live, and so spilled, across the call.
					if operation.Code.Writes(effects[slot.Space]) || (slot.Space == ssa.SpaceLocal && operation.Code == instr.CALL) {
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
