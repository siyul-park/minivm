package transform

import (
	"errors"
	"fmt"
	"slices"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// Module contains facts translation may assume beyond the bytecode.
type Module struct {
	// Constants is the constant pool.
	Constants []types.Boxed
	// Globals is the declared global-kind table.
	Globals []types.Kind
	// Objects resolves constant references to known cells.
	Objects Objects
	// Types is the declared-type table.
	Types []types.Type
	// Callees maps a dynamic CALL's offset in the translated function to what
	// was observed there. A recorded snapshot, never live state; absent for a
	// site never seen, the zero Callee for a site that saw callees of
	// differing types.
	Callees map[int]Callee
	// Refuted holds the offsets whose guard native code has seen fail. A
	// recorded snapshot: a container op there translates unguarded, so it
	// bridges instead of speculating its shape again.
	Refuted map[int]bool
}

// Callee is what a dynamic CALL site observed.
type Callee struct {
	// Function is the called function's reference when the site saw one
	// callee, zero when it saw several.
	Function int
	// Closure reports that the site calls Function through a closure.
	Closure bool
	// Type is the function type of the callee a site saw, or shared by the
	// several it saw; nil when they differ.
	Type *types.FunctionType
}

// Objects maps constant references to object facts.
type Objects map[int]Object

// Object contains facts known for one referenced cell.
type Object struct {
	// Function is the referenced function, when known.
	Function *types.Function
	// Struct is the referenced struct type, when known.
	Struct *types.StructType
	// Array is the referenced array's own type, when known.
	Array *types.ArrayType
	// I64 is the referenced i64 cell's value, when known.
	I64 *types.I64
}

// ErrEntry reports an entry offset that does not start a basic block.
var ErrEntry = errors.New("entry starts no block")

// Translate converts one bytecode function from entry to SSA.
// It returns ErrEntry for a non-block entry and nil when translation is unsupported.
func Translate(module Module, address int, function *types.Function, entry int) (*ssa.Function, error) {
	if function == nil || len(function.Code) == 0 {
		return nil, nil
	}
	blocks, err := analysis.Blocks(function)
	if err != nil {
		return nil, err
	}
	spans, at := split(function.Code, blocks)
	root, ok := at[entry]
	if !ok {
		return nil, fmt.Errorf("%w: entry %d starts no block", ErrEntry, entry)
	}
	if spans[0].start != 0 {
		return nil, nil
	}
	act := activation{function: function, address: address, slots: function.Declared()}
	states, seen, ok := module.analyze(act, spans, 0, frame{closures: make([]int, len(act.slots))})
	if !ok {
		return nil, nil
	}
	if root != 0 {
		if !seen[root] {
			return nil, nil
		}
		states, seen, ok = module.analyze(act, spans, root, states[root].owning())
		if !ok {
			return nil, nil
		}
	}
	built := module.build(act, spans, states, seen, root)
	if built != nil && len(built.Pred(0)) > 0 {
		built = rotate(built)
	}
	return built, nil
}

// Borrows reports, per parameter, whether function borrows it: a reference
// parameter function never writes, in a function with no RETURN_CALL, which
// may replace every parameter. Its native caller keeps ownership for the
// call; function neither retains nor releases it.
func Borrows(function *types.Function) []bool {
	if function == nil || function.Typ == nil {
		return nil
	}
	written, tail := map[int]bool{}, false
	for _, inst := range instr.Unmarshal(function.Code) {
		if inst.Opcode().Writes(instr.Local) {
			written[int(inst.Operand(0))] = true
		}
		tail = tail || inst.Opcode() == instr.RETURN_CALL
	}
	borrows := make([]bool, len(function.Typ.Params))
	for i, param := range function.Typ.Params {
		borrows[i] = param.Kind() == types.KindRef && !written[i] && !tail
	}
	return borrows
}

// Adopts returns the number of code's topmost popped operands whose
// ownership it takes: every one when it enters a frame, the stored value
// when it overwrites heap contents and yields nothing. The other operands
// stay their owner's to release after code.
func Adopts(code instr.Opcode, pops, results int) int {
	switch {
	case code.Writes(instr.Frame):
		return pops
	case code.Reads(instr.Heap) && code.Writes(instr.Heap) && results == 0:
		return 1
	default:
		return 0
	}
}

func (o Objects) function(reference int) *types.Function {
	return o[reference].Function
}

// rotate prepends an empty block ahead of f's own block 0 when it has a
// predecessor — root is a loop header, reached from both outside f and its
// own back edge — so block 0 has none: every later pass and Lower assume
// this. The new block carries block 0's own params, forwarded unchanged.
func rotate(f *ssa.Function) *ssa.Function {
	r := newRebuilder(f)
	first := r.builder.Block()
	origin := f.Block(0).Params
	args := make([]ssa.Value, len(origin))
	for i, p := range origin {
		args[i] = r.builder.Param(first, f.Type(p))
	}
	for _, block := range graph.Order(f) {
		id := r.open(block)
		b := f.Block(block)
		for _, operation := range b.Operations {
			r.builder.Add(id, r.define(r.operation(operation)))
		}
		r.builder.Term(id, r.terminator(b.Terminator))
	}
	r.builder.Term(first, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: r.block(0), Args: args}}})
	return r.builder.Build()
}

// analyze returns the facts live at each span's entry and which spans are
// reached from root; nothing flows past a span that ends in an exit. A
// reached span's stack is nil, not just absent, when nothing is live there
// (e.g. an empty operand stack at a loop header): seen is what tells reached
// apart from never visited.
func (m Module) analyze(act activation, spans []span, root int, in frame) ([]frame, []bool, bool) {
	states := make([]frame, len(spans))
	seen := make([]bool, len(spans))
	seen[root] = true
	states[root] = in
	work := []int{root}
	for len(work) > 0 {
		id := work[len(work)-1]
		work = work[:len(work)-1]
		exit, leaves, ok := m.transfer(act, spans[id], states[id])
		if !ok {
			return nil, nil, false
		}
		if leaves {
			continue
		}
		for _, succ := range spans[id].flow {
			if !seen[succ] {
				seen[succ] = true
				states[succ] = frame{stack: slices.Clone(exit.stack), closures: slices.Clone(exit.closures)}
				work = append(work, succ)
				continue
			}
			changed, ok := states[succ].merge(exit)
			if !ok {
				return nil, nil, false
			}
			if changed {
				work = append(work, succ)
			}
		}
	}
	return states, seen, true
}

// transfer returns the facts at s's end and whether s leaves native code
// there, which no span follows.
func (m Module) transfer(act activation, s span, in frame) (frame, bool, bool) {
	b := ssa.New("")
	block := b.Block()
	w, ok := newWalker(m, b, block, act, in)
	if !ok {
		return frame{}, false, false
	}
	term, ok := w.translate(s)
	if !ok {
		return frame{}, false, false
	}
	out := make([]fact, len(w.stack))
	for i, o := range w.stack {
		out[i] = o.fact
	}
	return frame{stack: out, closures: w.closures}, term.Op == ssa.OpExit, true
}

// build emits the spans analyze reached, in the order a walk from root over
// their successors visits them.
func (m Module) build(act activation, spans []span, states []frame, seen []bool, root int) *ssa.Function {
	queued := make([]bool, len(spans))
	queued[root] = true
	order := []int{root}
	for n := 0; n < len(order); n++ {
		for _, successor := range spans[order[n]].succs {
			if seen[successor] && !queued[successor] {
				queued[successor] = true
				order = append(order, successor)
			}
		}
	}
	ids := make([]int, len(spans))
	for i := range ids {
		ids[i] = -1
	}
	b := ssa.New(fmt.Sprintf("%d:%d", act.address, spans[root].start))
	b.Entry(ssa.Frame{Address: act.address, IP: spans[root].start, Returns: act.returns()})
	for _, id := range order {
		ids[id] = b.Block()
	}

	for _, id := range order {
		w, ok := newWalker(m, b, ids[id], act, states[id])
		if !ok {
			return nil
		}
		term, ok := w.translate(spans[id])
		if !ok {
			return nil
		}
		if term.Op != ssa.OpExit {
			if term.Edges, ok = w.edges(spans[id], states, ids); !ok {
				return nil
			}
		}
		b.Term(ids[id], term)
	}
	return b.Build()
}
