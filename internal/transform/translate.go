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

// group is the functions one unit translates: the root and every function a
// static tail call from a member joined. Spans of every member share one
// list, which is how a tail call reaches another member.
type group struct {
	Module
	acts  []activation
	spans []span
	// sealed ends joining: the facts analysis settled are final.
	sealed bool
}

// groupMax caps the functions one unit translates.
const groupMax = 4

// ErrEntry reports an entry offset that does not start a basic block.
var ErrEntry = errors.New("entry starts no block")

// Translate converts one bytecode function from entry to SSA, together with
// every function a static tail call from it reaches that can share its
// frame (see group.join).
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
	g := &group{Module: module}
	g.add(function, address, spans)
	states, seen, ok := g.analyze(0, frame{closures: make([]int, len(g.acts[0].slots))})
	if !ok {
		return nil, nil
	}
	if root != 0 {
		if !seen[root] {
			return nil, nil
		}
		states, seen, ok = g.analyze(root, states[root].owning())
		if !ok {
			return nil, nil
		}
	}
	g.sealed = true
	built := g.build(states, seen, root)
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

// join returns the member at address, adding target to the group when it can
// share the frame of the root: a plain function, with the root's slots and
// results, so every slot-typed fact stays valid across the jump. It reports
// false when target cannot be a member.
func (g *group) join(address int, target *types.Function) (int, bool) {
	for a, act := range g.acts {
		if act.address == address {
			return a, act.function == target
		}
	}
	root := g.acts[0].function
	if g.sealed || len(g.acts) == groupMax || len(target.Code) == 0 || len(target.Captures) > 0 || len(target.Handlers) > 0 ||
		len(target.Typ.Params) != len(root.Typ.Params) ||
		!slices.EqualFunc(target.Declared(), g.acts[0].slots, types.Type.Equals) ||
		!slices.EqualFunc(target.Typ.Returns, root.Typ.Returns, types.Type.Equals) {
		return 0, false
	}
	blocks, err := analysis.Blocks(target)
	if err != nil {
		return 0, false
	}
	spans, _ := split(target.Code, blocks)
	if spans[0].start != 0 || slices.ContainsFunc(spans, func(s span) bool { return s.suspend }) {
		return 0, false
	}
	g.add(target, address, spans)
	return len(g.acts) - 1, true
}

// add appends function's spans as a new member.
func (g *group) add(function *types.Function, address int, spans []span) {
	offset := len(g.spans)
	for _, s := range spans {
		s.act = len(g.acts)
		for i := range s.flow {
			s.flow[i] += offset
		}
		for i := range s.succs {
			s.succs[i] += offset
		}
		g.spans = append(g.spans, s)
	}
	g.acts = append(g.acts, activation{function: function, address: address, slots: function.Declared(), first: offset})
}

// analyze returns the facts live at each span's entry and which spans are
// reached from root; nothing flows past a span that ends in an exit. A
// reached span's stack is nil, not just absent, when nothing is live there
// (e.g. an empty operand stack at a loop header): seen is what tells reached
// apart from never visited. A tail call's span continues into the span it
// loops to, found as it is translated.
func (g *group) analyze(root int, in frame) ([]frame, []bool, bool) {
	states := make([]frame, len(g.spans))
	seen := make([]bool, len(g.spans))
	seen[root] = true
	states[root] = in
	work := []int{root}
	for len(work) > 0 {
		id := work[len(work)-1]
		work = work[:len(work)-1]
		w, term, ok := g.transfer(g.spans[id], states[id])
		if !ok {
			return nil, nil, false
		}
		for len(states) < len(g.spans) {
			states, seen = append(states, frame{}), append(seen, false)
		}
		if g.spans[id].tail {
			g.spans[id].flow, g.spans[id].succs = nil, nil
			if w.jump >= 0 {
				g.spans[id].flow, g.spans[id].succs = []int{w.jump}, []int{w.jump}
			}
		}
		if term.Op == ssa.OpExit {
			continue
		}
		exit := frame{stack: make([]fact, len(w.stack)), closures: w.closures}
		for i, o := range w.stack {
			exit.stack[i] = o.fact
		}
		for _, succ := range g.spans[id].flow {
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

// transfer walks s from in and returns the walker, which holds the facts at
// s's end and the span a tail call there looped to, and the terminator of s.
func (g *group) transfer(s span, in frame) (*walker, ssa.Terminator, bool) {
	b := ssa.New("")
	block := b.Block()
	w, ok := newWalker(g, b, block, s, in)
	if !ok {
		return nil, ssa.Terminator{}, false
	}
	term, ok := w.translate(s)
	return w, term, ok
}

// build emits the spans analyze reached, in the order a walk from root over
// their successors visits them.
func (g *group) build(states []frame, seen []bool, root int) *ssa.Function {
	queued := make([]bool, len(g.spans))
	queued[root] = true
	order := []int{root}
	for n := 0; n < len(order); n++ {
		for _, successor := range g.spans[order[n]].succs {
			if seen[successor] && !queued[successor] {
				queued[successor] = true
				order = append(order, successor)
			}
		}
	}
	ids := make([]int, len(g.spans))
	for i := range ids {
		ids[i] = -1
	}
	act := g.acts[0]
	b := ssa.New(fmt.Sprintf("%d:%d", act.address, g.spans[root].start))
	b.Entry(ssa.Frame{Address: act.address, IP: g.spans[root].start, Returns: act.returns()})
	for _, id := range order {
		ids[id] = b.Block()
	}

	for _, id := range order {
		w, ok := newWalker(g, b, ids[id], g.spans[id], states[id])
		if !ok {
			return nil
		}
		term, ok := w.translate(g.spans[id])
		if !ok {
			return nil
		}
		if term.Op != ssa.OpExit {
			if term.Edges, ok = w.edges(g.spans[id], states, ids); !ok {
				return nil
			}
		}
		b.Term(ids[id], term)
	}
	return b.Build()
}
