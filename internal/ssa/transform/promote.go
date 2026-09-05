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
// parameter for it. It is textbook mem2reg - Cytron's iterated dominance
// frontier decides where a parameter goes, and a dominator-tree walk renames
// every access to the definition that reaches it - and it is what ForwardPass
// deliberately cannot do. Forwarding drops every availability at a
// multi-predecessor block, which is exactly what keeps it sound across a back
// edge and exactly why a loop-carried local stays a load, a store, and the
// boxing between them on every iteration. A parameter is the answer that
// availability alone has no way to spell.
//
// # What qualifies
//
// The IR has no way to take a local's address, so aliasing is not the question
// here that it is in a language with one: SpaceLocal storage is named by
// nothing but OpLoad and OpStore, and instr's effect model gives LOCAL_GET,
// LOCAL_SET, and LOCAL_TEE as the only opcodes that read or write it - a call
// writes Global, Upval, Heap, and Frame, never the caller's own locals. A slot
// is promotable unless one of four things says otherwise:
//
//   - It is not the entry frame's (Slot.Base is not zero). A local of a frame
//     a frontend inlined is left alone: its writeback would name a frame the
//     deopt state only sometimes carries, and a trace may enter two different
//     callees at one depth, so one Base names one floor and not one variable.
//     carry takes the same position, on root-frame locals only.
//   - Its accesses disagree on a type. One value has one type, so a slot read
//     back as something other than what was stored is a slot this pass has no
//     single value to give.
//   - It holds a reference. Promotion moves the slot's content into a value,
//     and a ref slot's content includes the reference count the slot holds:
//     removing the store removes the release of what it replaced and the
//     retain of what it stored, and ssa.Local has no ownership mark to say
//     where the count went (see docs/jit-internals.md, Reference Ownership).
//     carry takes scalars only for the same reason; a ref local stays in its
//     slot and keeps paying for its loads.
//   - The function bridges a local opcode to the interpreter. An OpBridge runs
//     its opcode in the interpreter, which reads the frame the promotion just
//     emptied. No frontend emits one today - LOCAL_* is lowered everywhere -
//     so this is the one "reads the frame as a whole" case there is, and the
//     whole function declines rather than one slot.
//   - Nothing stores it. A slot only ever read already has one definition
//     everywhere, and it is the slot itself: a parameter, or a local the frame
//     setup zeroed. Promoting it buys no definition that was not already
//     there and costs the value a live range across every block that reads it,
//     which a bytecode emitter has to home in a local and a register allocator
//     has to keep. Reading it where it is read is cheaper, so this pass leaves
//     a read-only slot in the frame and ForwardPass unifies its repeated reads
//     within a dominator scope as it always did.
//
// # What a deopt sees
//
// A promoted local is no longer where the interpreter looks for it, so the
// state a deopt resumes into has to name it: every OpState's entry frame gains
// an ssa.Local per promoted slot, holding the value that slot must be written
// back with. The alternative - leaving a store in front of every deoptimizing
// operation - would put one back in front of every guard, every other store,
// and every call, which is more stores than the loads this pass removes, on
// the hot path rather than the cold one. Naming the value in the state instead
// is what internal/jit/arm64's commitCarried already does for a carried
// register: write the register to its VM slot on the paths that hand control
// back, and nowhere else. A function with no OpState at all - which is every
// function the ahead-of-time optimizer sees, since bytecode does not
// deoptimize - simply has nothing to name.
//
// An OpState records the values current at its own position, which is where
// its frames' operand stacks are already accurate as recorded, so promotion
// adds no rule about where a state may be used that its stack did not already
// carry.
//
// # Entry
//
// The value a promoted slot holds on entry is whatever the interpreter left
// there, so the entry block loads each promoted slot once and every reaching
// definition starts from that load. A function whose entry block is itself a
// loop header - which is what a native loop entry compiles to - gets a fresh
// entry block in front of it first, so that load runs once per entry rather
// than once per iteration. That is the one place this pass grows the block
// graph, and it grows it at the entry rather than by splitting an edge, so no
// predecessor is rewired. An entry block that both has predecessors and takes
// parameters is declined outright: its parameters are the operands a native
// entry is handed, and a new block in front of it has none to pass on.
//
// # Ordering
//
// It runs before CSEPass for the reason ForwardPass does - a computation over
// a slot read twice is two computations until both reads are one value - and
// before ForwardPass itself, which then has only the globals and upvalues left
// to forward. DCEPass afterwards is what removes an entry load nothing reads,
// which is every promoted slot a function stores before it ever loads.
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
			case ssa.OpExec, ssa.OpBridge:
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
