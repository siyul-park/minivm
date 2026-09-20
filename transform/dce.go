package transform

import (
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// DCEPass removes unreachable blocks and dead pure operations.
type DCEPass struct{}

type operationSite struct {
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
	blocks := graph.ReversePostorder(function)
	live := liveness(function, blocks)

	rebuilder := newRebuilder(function)
	changed := len(blocks) != function.Len()
	for _, block := range blocks {
		id := rebuilder.block(block)
		currentBlock := function.Block(block)
		for _, param := range currentBlock.Params {
			rebuilder.alias(param, rebuilder.builder.Param(id, function.Type(param)))
		}
		for i, operation := range currentBlock.Operations {
			if !live[operationSite{block, i}] {
				changed = true
				continue
			}
			rebuilder.builder.Add(id, rebuilder.define(function, rebuilder.operation(operation)))
		}
		rebuilder.builder.Term(id, rebuilder.terminator(currentBlock.Terminator))
	}

	if !changed {
		return true, nil
	}
	next := rebuilder.builder.Build()
	*function = *next
	return false, nil
}

func liveness(function *ssa.Function, blocks []int) map[operationSite]bool {
	defs := map[ssa.Value]operationSite{}
	for _, b := range blocks {
		for i, operation := range function.Block(b).Operations {
			for _, r := range operation.Results {
				defs[r] = operationSite{b, i}
			}
		}
	}

	live := map[operationSite]bool{}
	var queue []ssa.Value
	push := func(v ssa.Value) {
		if v != ssa.NoValue {
			queue = append(queue, v)
		}
	}
	mark := func(s operationSite, operation ssa.Operation) {
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
				effectful = !operation.Code.IsPure()
			}
			if effectful {
				mark(operationSite{b, i}, operation)
			}
		}
		for _, a := range currentBlock.Terminator.Args {
			push(a)
		}
		for _, e := range currentBlock.Terminator.Edges {
			for _, a := range e.Args {
				push(a)
			}
		}
		push(currentBlock.Terminator.State)
	}

	for len(queue) > 0 {
		v := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		s, ok := defs[v]
		if !ok {
			continue
		}
		mark(s, function.Block(s.block).Operations[s.index])
	}
	return live
}
