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
func (p *PromotePass) Run(_ *pass.Manager, function *ssa.Function) (bool, error) {
	slots := promotable(function)
	if len(slots) == 0 {
		return true, nil
	}
	next, ok := promote(function, slots)
	if !ok {
		return true, nil
	}
	*function = *next
	return false, nil
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
				if entry(operation) < 0 {
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
	indexes := slices.Sorted(maps.Keys(localTypes))
	wide := slices.ContainsFunc(indexes, func(index int) bool { return localTypes[index] == ssa.TypeI64 })

	dominance := graph.NewDominance(function)
	params := placements(function, dominance, localTypes, indexes)
	children := dominance.Children()

	rebuilder := newRebuilder(function)
	// raw holds the reaching values of promoted i64 locals: already raw
	// ints, so a guard.kind on one aliases away.
	raw := map[ssa.Value]bool{}
	var walk func(block int, reaching map[int]ssa.Value)
	walk = func(block int, reaching map[int]ssa.Value) {
		id := rebuilder.block(block)
		currentBlock := function.Block(block)
		for _, param := range currentBlock.Params {
			rebuilder.alias(param, rebuilder.builder.Param(id, function.Type(param)))
		}
		for _, index := range params[block] {
			v := rebuilder.builder.Param(id, localTypes[index])
			reaching[index] = v
			if localTypes[index] == ssa.TypeI64 {
				raw[v] = true
			}
		}
		if block == 0 {
			state := ssa.NoValue
			if wide {
				state = enter(rebuilder, function, id)
			}
			for _, index := range indexes {
				held := rebuilder.builder.Value(localTypes[index])
				rebuilder.builder.Add(id, ssa.Operation{
					Op:      ssa.OpLoad,
					Slot:    ssa.Slot{Space: ssa.SpaceLocal, Index: index},
					Results: []ssa.Value{held},
				})
				v := held
				if localTypes[index] == ssa.TypeI64 {
					guarded := rebuilder.builder.Value(ssa.TypeI64)
					rebuilder.builder.Add(id, ssa.Operation{
						Op: ssa.OpGuardKind, Args: []ssa.Value{held}, State: state, Results: []ssa.Value{guarded},
					})
					v = guarded
					raw[v] = true
				}
				reaching[index] = v
			}
		}

		for _, operation := range currentBlock.Operations {
			operation = rebuilder.operation(operation)
			switch {
			case operation.Op == ssa.OpLoad && promoted(localTypes, operation.Slot):
				rebuilder.alias(operation.Results[0], reaching[operation.Slot.Index])
				continue
			case operation.Op == ssa.OpStore && promoted(localTypes, operation.Slot):
				reaching[operation.Slot.Index] = operation.Args[0]
				if localTypes[operation.Slot.Index] == ssa.TypeI64 {
					raw[operation.Args[0]] = true
				}
				continue
			case operation.Op == ssa.OpGuardKind && raw[operation.Args[0]]:
				rebuilder.alias(operation.Results[0], operation.Args[0])
				continue
			case operation.Op == ssa.OpState:
				at := entry(operation)
				for _, index := range indexes {
					operation.Frames[at].Locals = append(operation.Frames[at].Locals, ssa.Local{Index: index, Value: reaching[index]})
				}
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

// enter emits the unit's entry state in block id: function's Entry frame
// with block 0's params as its stack, refs owned, and no locals, since
// every promoted slot still holds its entry value there.
func enter(rebuilder *rebuilder, function *ssa.Function, id int) ssa.Value {
	block := function.Block(0)
	var stack []ssa.Operand
	if len(block.Params) > 0 {
		stack = make([]ssa.Operand, len(block.Params))
		for i, param := range block.Params {
			stack[i] = ssa.Operand{Value: rebuilder.value(param), Owned: function.Type(param) == ssa.TypeRef}
		}
	}
	frame := function.Entry()
	frame.Stack = stack
	state := rebuilder.builder.Value(ssa.TypeState)
	rebuilder.builder.Add(id, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{frame}, Results: []ssa.Value{state}})
	return state
}

func placements(function *ssa.Function, dominance *graph.Dominance, localTypes map[int]ssa.Type, indexes []int) [][]int {
	stored := map[int][]int{}
	for block := range function.Len() {
		for _, operation := range function.Block(block).Operations {
			if operation.Op != ssa.OpStore || !promoted(localTypes, operation.Slot) {
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

func promoted(localTypes map[int]ssa.Type, s ssa.Slot) bool {
	if s.Space != ssa.SpaceLocal || s.Base != 0 {
		return false
	}
	_, ok := localTypes[s.Index]
	return ok
}

func entry(operation ssa.Operation) int {
	for i, frame := range operation.Frames {
		if frame.Base == 0 {
			return i
		}
	}
	return -1
}
