package transform

import (
	"maps"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// ForwardPass replaces repeated loads when the slot is unchanged.
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

	rebuilder := newRebuilder(function)
	changed := false

	var walk func(block int, held map[ssa.Slot]ssa.Value)
	walk = func(block int, held map[ssa.Slot]ssa.Value) {
		id := rebuilder.block(block)
		currentBlock := function.Block(block)
		for _, param := range currentBlock.Params {
			rebuilder.alias(param, rebuilder.builder.Param(id, function.Type(param)))
		}
		if len(function.Predecessors(block)) != 1 {
			clear(held)
		}

		for _, operation := range currentBlock.Operations {
			operation = rebuilder.operation(operation)
			switch operation.Op {
			case ssa.OpLoad:
				if at, ok := held[operation.Slot]; ok {
					rebuilder.alias(operation.Results[0], at)
					changed = true
					continue
				}
				operation = rebuilder.define(function, operation)
				rebuilder.builder.Add(id, operation)
				held[operation.Slot] = operation.Results[0]
				continue
			case ssa.OpStore:
				delete(held, operation.Slot)
			case ssa.OpExec:
				invalidate(held, operation.Code)
			}
			rebuilder.builder.Add(id, rebuilder.define(function, operation))
		}
		rebuilder.builder.Term(id, rebuilder.terminator(currentBlock.Terminator))

		for _, child := range children[block] {
			walk(child, maps.Clone(held))
		}
	}
	walk(0, map[ssa.Slot]ssa.Value{})

	if !changed {
		return true, nil
	}
	*function = *rebuilder.builder.Build()
	return false, nil
}

func invalidate(held map[ssa.Slot]ssa.Value, code instr.Opcode) {
	for slot := range held {
		if code.Writes(effects[slot.Space]) {
			delete(held, slot)
		}
	}
}
