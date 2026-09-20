package transform

import (
	"maps"
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// PromotePass converts eligible entry-frame locals to SSA values.
type PromotePass struct{}

var _ pass.Pass[*ssa.Function] = (*PromotePass)(nil)

// NewPromotePass returns the pass.
func NewPromotePass() *PromotePass {
	return &PromotePass{}
}

// Run applies the pass to one SSA function.
func (p *PromotePass) Run(_ *pass.Manager, function *ssa.Function) (pass.Preserved, error) {
	slots := promotable(function)
	if len(slots) == 0 {
		return pass.PreserveAll(), nil
	}
	next, ok := promote(function, slots)
	if !ok {
		return pass.PreserveAll(), nil
	}
	*function = *next
	return pass.PreserveNone(), nil
}

func promotable(function *ssa.Function) map[int]ssa.Type {
	localTypes, stored, blocked := map[int]ssa.Type{}, map[int]bool{}, map[int]bool{}
	for b := range function.Len() {
		for _, operation := range function.Block(b).Operations {
			var held ssa.Value
			switch operation.Op {
			case ssa.OpExec:
				if operation.Code.Reads(instr.Local) || operation.Code.Writes(instr.Local) {
					return nil
				}
				continue
			case ssa.OpState:
				if frameEntry(operation) < 0 {
					return nil
				}
				continue
			case ssa.OpLoad:
				held = operation.Results[0]
			case ssa.OpStore:
				held = operation.Args[0]
			default:
				continue
			}
			if operation.Slot.Space != ssa.SpaceLocal || operation.Slot.Base != 0 {
				continue
			}
			t := function.Type(held)
			if at, seen := localTypes[operation.Slot.Index]; t == ssa.TypeRef || (seen && at != t) {
				blocked[operation.Slot.Index] = true
			}
			localTypes[operation.Slot.Index] = t
			stored[operation.Slot.Index] = stored[operation.Slot.Index] || operation.Op == ssa.OpStore
		}
	}
	for index := range localTypes {
		if blocked[index] || !stored[index] {
			delete(localTypes, index)
		}
	}
	return localTypes
}

func promote(function *ssa.Function, localTypes map[int]ssa.Type) (*ssa.Function, bool) {
	if len(function.Predecessors(0)) > 0 {
		if len(function.Block(0).Params) > 0 {
			return nil, false
		}
		function = prependEntry(function)
	}
	indexes := slices.Sorted(maps.Keys(localTypes))

	dominance := graph.NewDominance(function)
	params := placements(function, dominance, localTypes, indexes)
	children := dominance.Children()

	rebuilder := newRebuilder(function)
	var walk func(block int, reaching map[int]ssa.Value)
	walk = func(block int, reaching map[int]ssa.Value) {
		id := rebuilder.block(block)
		currentBlock := function.Block(block)
		for _, param := range currentBlock.Params {
			rebuilder.alias(param, rebuilder.builder.Param(id, function.Type(param)))
		}
		for _, index := range params[block] {
			reaching[index] = rebuilder.builder.Param(id, localTypes[index])
		}
		if block == 0 {
			for _, index := range indexes {
				held := rebuilder.builder.Value(localTypes[index])
				rebuilder.builder.Add(id, ssa.Operation{
					Op:      ssa.OpLoad,
					Slot:    ssa.Slot{Space: ssa.SpaceLocal, Index: index},
					Results: []ssa.Value{held},
				})
				reaching[index] = held
			}
		}

		for _, operation := range currentBlock.Operations {
			operation = rebuilder.operation(operation)
			switch {
			case operation.Op == ssa.OpLoad && isPromoted(localTypes, operation.Slot):
				rebuilder.alias(operation.Results[0], reaching[operation.Slot.Index])
				continue
			case operation.Op == ssa.OpStore && isPromoted(localTypes, operation.Slot):
				reaching[operation.Slot.Index] = operation.Args[0]
				continue
			case operation.Op == ssa.OpState:
				at := frameEntry(operation)
				operation.Frames[at].Locals = pinned(operation.Frames[at].Locals, indexes, reaching)
			}
			rebuilder.builder.Add(id, rebuilder.define(function, operation))
		}

		term := rebuilder.terminator(currentBlock.Terminator)
		for i, edge := range currentBlock.Terminator.Edges {
			for _, index := range params[edge.Block] {
				term.Edges[i].Args = append(term.Edges[i].Args, reaching[index])
			}
		}
		rebuilder.builder.Term(id, term)

		for _, child := range children[block] {
			walk(child, maps.Clone(reaching))
		}
	}
	walk(0, map[int]ssa.Value{})

	return rebuilder.builder.Build(), true
}

func prependEntry(function *ssa.Function) *ssa.Function {
	rebuilder := newRebuilder(function)
	first := rebuilder.builder.AddBlock()
	for _, block := range graph.ReversePostorder(function) {
		id := rebuilder.block(block)
		currentBlock := function.Block(block)
		for _, param := range currentBlock.Params {
			rebuilder.alias(param, rebuilder.builder.Param(id, function.Type(param)))
		}
		for _, operation := range currentBlock.Operations {
			rebuilder.builder.Add(id, rebuilder.define(function, rebuilder.operation(operation)))
		}
		rebuilder.builder.Term(id, rebuilder.terminator(currentBlock.Terminator))
	}
	rebuilder.builder.Term(first, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: rebuilder.block(0)}}})
	return rebuilder.builder.Build()
}

func placements(function *ssa.Function, dominance *graph.Dominance, localTypes map[int]ssa.Type, indexes []int) [][]int {
	stored := map[int][]int{}
	for block := range function.Len() {
		for _, operation := range function.Block(block).Operations {
			if operation.Op != ssa.OpStore || !isPromoted(localTypes, operation.Slot) {
				continue
			}
			at := stored[operation.Slot.Index]
			if len(at) == 0 || at[len(at)-1] != block {
				stored[operation.Slot.Index] = append(at, block)
			}
		}
	}

	frontier := graph.Frontier(function, dominance)
	params := make([][]int, function.Len())
	for _, index := range indexes {
		work := append([]int{0}, stored[index]...)
		joined := map[int]bool{}
		for len(work) > 0 {
			block := work[len(work)-1]
			work = work[:len(work)-1]
			for _, join := range frontier[block] {
				if joined[join] {
					continue
				}
				joined[join] = true
				params[join] = append(params[join], index)
				work = append(work, join)
			}
		}
	}
	return params
}

func isPromoted(localTypes map[int]ssa.Type, s ssa.Slot) bool {
	if s.Space != ssa.SpaceLocal || s.Base != 0 {
		return false
	}
	_, ok := localTypes[s.Index]
	return ok
}

func pinned(already []ssa.Local, indexes []int, reaching map[int]ssa.Value) []ssa.Local {
	locals := make([]ssa.Local, 0, len(already)+len(indexes))
	locals = append(locals, already...)
	for _, index := range indexes {
		locals = append(locals, ssa.Local{Index: index, Value: reaching[index]})
	}
	slices.SortFunc(locals, func(a, b ssa.Local) int { return a.Index - b.Index })
	return locals
}

func frameEntry(operation ssa.Operation) int {
	for i, frame := range operation.Frames {
		if frame.Base == 0 {
			return i
		}
	}
	return -1
}
