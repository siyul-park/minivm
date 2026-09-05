package transform

import (
	"maps"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// ForwardPass replaces a load with the value an earlier load of the same slot
// already produced, so one storage location read twice is one definition. It
// is the textbook redundant-load elimination LLVM's EarlyCSE performs
// alongside its value numbering, and it is what CSEPass needs in front of it:
// a value here is its own definition, so two reads of one local are two
// definitions and every computation over them is two computations, however
// obviously equal they look in the bytecode they came from. Forwarding the
// second read onto the first is what makes them one, and it is the bulk of
// what the bytecode global value numbering this package replaces eliminated.
//
// A forwarded value is live from the earlier load to the later use, and only
// what happens in between can invalidate it:
//
//   - an OpStore to the same slot, which is the whole point of the slot;
//   - an OpExec or an OpBridge whose opcode instr's effect model says writes
//     the slot's storage, which is where a call comes in - it writes Global
//     and Upval but never the caller's Local, so a call ends a global's
//     availability and leaves a local's alone;
//   - reaching a block by more than one edge, since whatever a dominator left
//     in the slot another predecessor may have overwritten since. That is
//     also what makes a loop safe: a header always has two predecessors, so
//     nothing a preheader read survives into it.
//
// Nothing else can: a guard, an OpState, and every other deoptimizing
// operation resume the interpreter with the slot's committed value, which the
// stores this pass never removes have already written there.
//
// A store does not start an availability of its own. Bytecode already keeps
// that value in the slot the store wrote, so forwarding a later load onto the
// stored value only makes the value live across the store, which the emitter
// then has to home in a fresh local - one more instruction and one more slot
// than reading back the slot the program itself named. See
// docs/pass-system.md.
type ForwardPass struct{}

var _ pass.Pass[*ssa.Function] = (*ForwardPass)(nil)

func NewForwardPass() *ForwardPass {
	return &ForwardPass{}
}

func (p *ForwardPass) Run(_ *pass.Manager, fn *ssa.Function) (pass.Preserved, error) {
	children := domChildren(fn, graph.NewDominance(fn))

	rb := newRebuilder(fn)
	changed := false

	var walk func(block int, held map[ssa.Slot]ssa.Value)
	walk = func(block int, held map[ssa.Slot]ssa.Value) {
		id := rb.block(block)
		blk := fn.Block(block)
		for _, param := range blk.Params {
			rb.alias(param, rb.b.Param(id, fn.Type(param)))
		}
		if len(fn.Pred(block)) != 1 {
			clear(held)
		}

		for _, op := range blk.Ops {
			op = rb.operation(op)
			switch op.Op {
			case ssa.OpLoad:
				if at, ok := held[op.Slot]; ok {
					rb.alias(op.Results[0], at)
					changed = true
					continue
				}
				op = rb.define(fn, op)
				rb.b.Add(id, op)
				held[op.Slot] = op.Results[0]
				continue
			case ssa.OpStore:
				delete(held, op.Slot)
			case ssa.OpExec, ssa.OpBridge:
				invalidate(held, op.Code)
			}
			rb.b.Add(id, rb.define(fn, op))
		}
		rb.b.Term(id, rb.terminator(blk.Term))

		for _, child := range children[block] {
			walk(child, maps.Clone(held))
		}
	}
	walk(0, map[ssa.Slot]ssa.Value{})

	if !changed {
		return pass.PreserveAll(), nil
	}
	*fn = *rb.b.Build()
	return pass.PreserveNone(), nil
}

// invalidate drops every slot code may have written, as instr's effect model
// states it.
func invalidate(held map[ssa.Slot]ssa.Value, code instr.Opcode) {
	for slot := range held {
		if code.Writes(spaces[slot.Space]) {
			delete(held, slot)
		}
	}
}

// spaces is the storage each Space names in instr's effect vocabulary, so what
// an opcode writes decides which slots it invalidates.
var spaces = [...]instr.Effect{
	ssa.SpaceLocal:  instr.Local,
	ssa.SpaceGlobal: instr.Global,
	ssa.SpaceUpval:  instr.Upval,
}
