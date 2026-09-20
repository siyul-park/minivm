package transform

import (
	"maps"
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// PromotePass turns an entry-frame local slot into SSA values: every load of
// it reads the value the last store left, and a merge point takes a block
// parameter for it - textbook mem2reg, using Cytron's iterated dominance
// frontier to place parameters and a dominator-tree walk to rename accesses.
// It is what ForwardPass deliberately cannot do: forwarding drops every
// availability at a multi-predecessor block, which is what keeps it sound
// across a back edge and why a loop-carried local stays a load, a store, and
// the boxing between them on every iteration without this pass. A parameter
// is the answer availability alone cannot spell.
//
// A slot is promotable only when every access agrees on its type; the slot
// holds no reference (promoting a ref slot would move its reference count
// into a value with no ownership mark to carry it - see
// docs/jit-internals.md, Reference Ownership); it belongs to the entry frame
// (Slot.Base is zero - an inlined callee's local has no single deopt-stable
// frame to write back into); and something stores it (a never-stored slot
// already has one definition, itself, so promoting
// it only adds a live range with nothing to save).
//
// A promoted local is no longer where the interpreter looks for it, so every
// OpState's entry frame gains an ssa.Local per promoted slot, holding the
// value that slot must be written back with. Naming the value in the state
// is cheaper than leaving a store in front of every deoptimizing operation,
// which would put one back in front of every guard, store, and call; it is
// what a native backend already does for a carried
// register on the paths that hand control back.
//
// The entry block loads each promoted slot once, since that is what the
// interpreter left there. A function whose entry block is itself a loop
// header gets a fresh entry block in front of it first, so that load runs
// once per entry rather than once per iteration. An entry block that both
// has predecessors and takes parameters is declined outright: a new block in
// front of it would have no arguments to pass those parameters.
type PromotePass struct{}

var _ pass.Pass[*ssa.Function] = (*PromotePass)(nil)

func NewPromotePass() *PromotePass {
	return &PromotePass{}
}

func (p *PromotePass) Run(_ *pass.Manager, fn *ssa.Function) (pass.Preserved, error) {
	slots := promotable(fn)
	if len(slots) == 0 {
		return pass.PreserveAll(), nil
	}
	next, ok := promote(fn, slots)
	if !ok {
		return pass.PreserveAll(), nil
	}
	*fn = *next
	return pass.PreserveNone(), nil
}

// promotable returns the type of every entry-frame local slot fn accesses only
// in ways this pass can replace with values, keyed by the slot's index. It
// returns nothing at all for a function that hands a local opcode to the
// interpreter, or that deoptimizes into a state carrying no entry frame to
// write a promoted local back into.
func promotable(fn *ssa.Function) map[int]ssa.Type {
	typed, stored, blocked := map[int]ssa.Type{}, map[int]bool{}, map[int]bool{}
	for b := range fn.Len() {
		for _, op := range fn.Block(b).Ops {
			var held ssa.Value
			switch op.Op {
			case ssa.OpExec:
				if op.Code.Reads(instr.Local) || op.Code.Writes(instr.Local) {
					return nil
				}
				continue
			case ssa.OpState:
				if entry(op) < 0 {
					return nil
				}
				continue
			case ssa.OpLoad:
				held = op.Results[0]
			case ssa.OpStore:
				held = op.Args[0]
			default:
				continue
			}
			if op.Slot.Space != ssa.SpaceLocal || op.Slot.Base != 0 {
				continue
			}
			t := fn.Type(held)
			if at, seen := typed[op.Slot.Index]; t == ssa.TypeRef || (seen && at != t) {
				blocked[op.Slot.Index] = true
			}
			typed[op.Slot.Index] = t
			stored[op.Slot.Index] = stored[op.Slot.Index] || op.Op == ssa.OpStore
		}
	}
	for index := range typed {
		if blocked[index] || !stored[index] {
			delete(typed, index)
		}
	}
	return typed
}

// promote rewrites fn with every slot in typed held in values, or reports
// false when its entry block can take neither the loads those values start
// from nor a block in front of it holding them.
func promote(fn *ssa.Function, typed map[int]ssa.Type) (*ssa.Function, bool) {
	if len(fn.Pred(0)) > 0 {
		if len(fn.Block(0).Params) > 0 {
			return nil, false
		}
		fn = head(fn)
	}
	indexes := slices.Sorted(maps.Keys(typed))

	dom := graph.NewDominance(fn)
	params := placed(fn, dom, typed, indexes)
	children := domChildren(fn, dom)

	rb := newRebuilder(fn)
	var walk func(block int, reaching map[int]ssa.Value)
	walk = func(block int, reaching map[int]ssa.Value) {
		id := rb.block(block)
		blk := fn.Block(block)
		for _, param := range blk.Params {
			rb.alias(param, rb.b.Param(id, fn.Type(param)))
		}
		for _, index := range params[block] {
			reaching[index] = rb.b.Param(id, typed[index])
		}
		if block == 0 {
			for _, index := range indexes {
				held := rb.b.Value(typed[index])
				rb.b.Add(id, ssa.Operation{
					Op:      ssa.OpLoad,
					Slot:    ssa.Slot{Space: ssa.SpaceLocal, Index: index},
					Results: []ssa.Value{held},
				})
				reaching[index] = held
			}
		}

		for _, op := range blk.Ops {
			op = rb.operation(op)
			switch {
			case op.Op == ssa.OpLoad && promoted(typed, op.Slot):
				rb.alias(op.Results[0], reaching[op.Slot.Index])
				continue
			case op.Op == ssa.OpStore && promoted(typed, op.Slot):
				reaching[op.Slot.Index] = op.Args[0]
				continue
			case op.Op == ssa.OpState:
				at := entry(op)
				op.Frames[at].Locals = pinned(op.Frames[at].Locals, indexes, reaching)
			}
			rb.b.Add(id, rb.define(fn, op))
		}

		term := rb.terminator(blk.Term)
		for i, edge := range blk.Term.Edges {
			for _, index := range params[edge.Block] {
				term.Edges[i].Args = append(term.Edges[i].Args, reaching[index])
			}
		}
		rb.b.Term(id, term)

		for _, child := range children[block] {
			walk(child, maps.Clone(reaching))
		}
	}
	walk(0, map[int]ssa.Value{})

	return rb.b.Build(), true
}

// head returns fn with a fresh entry block in front of its own, jumping
// straight to it. It is the preheader a promoted slot's entry load needs when
// the block that would hold it is itself a loop header: the load belongs where
// control passes once per entry, not once per iteration.
func head(fn *ssa.Function) *ssa.Function {
	rb := newRebuilder(fn)
	first := rb.b.Block()
	for _, block := range order(fn) {
		id := rb.block(block)
		blk := fn.Block(block)
		for _, param := range blk.Params {
			rb.alias(param, rb.b.Param(id, fn.Type(param)))
		}
		for _, op := range blk.Ops {
			rb.b.Add(id, rb.define(fn, rb.operation(op)))
		}
		rb.b.Term(id, rb.terminator(blk.Term))
	}
	rb.b.Term(first, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: rb.block(0)}}})
	return rb.b.Build()
}

// placed returns the slots each block takes a parameter for, in index order:
// the iterated dominance frontier of the blocks that store the slot, plus the
// entry, which is where its own load defines it. That is Cytron's placement,
// and it is what makes the renaming below find exactly one reaching definition
// at every use.
func placed(fn *ssa.Function, dom *graph.Dominance, typed map[int]ssa.Type, indexes []int) [][]int {
	stored := map[int][]int{}
	for block := range fn.Len() {
		for _, op := range fn.Block(block).Ops {
			if op.Op != ssa.OpStore || !promoted(typed, op.Slot) {
				continue
			}
			at := stored[op.Slot.Index]
			if len(at) == 0 || at[len(at)-1] != block {
				stored[op.Slot.Index] = append(at, block)
			}
		}
	}

	frontier := graph.Frontier(fn, dom)
	params := make([][]int, fn.Len())
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

// promoted reports whether s names one of the entry-frame local slots typed
// holds a value for.
func promoted(typed map[int]ssa.Type, s ssa.Slot) bool {
	if s.Space != ssa.SpaceLocal || s.Base != 0 {
		return false
	}
	_, ok := typed[s.Index]
	return ok
}

// pinned returns the promoted locals one frame is written back with: whatever
// an earlier promotion already pinned there, plus the value each slot promoted
// now holds, in slot order.
func pinned(already []ssa.Local, indexes []int, reaching map[int]ssa.Value) []ssa.Local {
	locals := make([]ssa.Local, 0, len(already)+len(indexes))
	locals = append(locals, already...)
	for _, index := range indexes {
		locals = append(locals, ssa.Local{Index: index, Value: reaching[index]})
	}
	slices.SortFunc(locals, func(a, b ssa.Local) int { return a.Index - b.Index })
	return locals
}

// entry returns the position of the entry frame in op's frame chain - the one
// counting from the floor a promoted slot's index is relative to - or -1 when
// op carries none.
func entry(op ssa.Operation) int {
	for i, frame := range op.Frames {
		if frame.Base == 0 {
			return i
		}
	}
	return -1
}
