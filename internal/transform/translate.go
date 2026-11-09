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

// Module contains the program facts translation may assume beyond the
// bytecode. It is fixed for the program's lifetime: nothing recorded at run
// time belongs here (see Profile).
type Module struct {
	// Constants is the constant pool.
	Constants []types.Boxed
	// Globals is the declared global-kind table.
	Globals []types.Kind
	// Objects resolves constant references to known cells.
	Objects Objects
	// Types is the declared-type table.
	Types []types.Type
}

// Profile is the runtime feedback recorded for one function, keyed by
// bytecode offsets in that function. A value is never mutated once
// published: a change publishes a new one with the next Generation.
type Profile struct {
	// Callees maps a dynamic CALL's offset to what was observed there;
	// absent for a site never seen, the zero Callee for a site that saw
	// callees of differing types.
	Callees map[int]Callee
	// Refuted holds the offsets whose guard native code has seen fail: a
	// container op there translates unguarded, so it bridges instead of
	// speculating its shape again.
	Refuted map[int]bool
	// Generation counts the changes recorded so far; compiled code is stale
	// once the generation of any function it read moved.
	Generation uint64
}

// Profiles resolves a function address to its current Profile. A Profiles
// is a snapshot taken for one compile.
type Profiles []*Profile

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

// translation is what one Translate call compiles: a root function and the
// members a static tail call proves it can share its frame with, their spans
// in one list, which is how a tail call reaches another member. Its members
// are fixed before analysis, and it is immutable after.
type translation struct {
	module Module
	acts   members
	spans  []span
}

// members are a translation's functions, root first.
type members []activation

// reach is what analysis learned: the facts live at each span's entry, which
// spans were reached, the tail call targets it proved, and, per tail span
// whose call loops, the member span it jumps to.
type reach struct {
	states []facts
	seen   []bool
	proofs []proof
	jumps  map[int]int
}

// proof is a tail call target the walker resolved with its own facts.
type proof struct {
	address  int
	function *types.Function
}

// memberMax caps a translation's functions, the root included.
const memberMax = 4

// ErrEntry reports an entry offset that does not start a basic block.
var ErrEntry = errors.New("entry starts no block")

// Translate converts one bytecode function from entry to SSA, together with
// every function a static tail call from it proves can share its frame (see
// newTranslation), and returns the addresses of those functions, root
// first: the code depends on each one's profile.
// It returns ErrEntry for a non-block entry and nil when translation is unsupported.
func Translate(module Module, profiles Profiles, address int, function *types.Function, entry int) (*ssa.Function, []int, error) {
	if function == nil || len(function.Code) == 0 {
		return nil, nil, nil
	}
	blocks, err := analysis.Blocks(function)
	if err != nil {
		return nil, nil, err
	}
	spans, at := split(function.Code, blocks)
	root, ok := at[entry]
	if !ok {
		return nil, nil, fmt.Errorf("%w: entry %d starts no block", ErrEntry, entry)
	}
	if spans[0].start != 0 {
		return nil, nil, nil
	}
	t, ok := newTranslation(module, activation{function: function, address: address, slots: function.Declared(), profile: profiles.Of(address)}, spans, profiles)
	if !ok {
		return nil, nil, nil
	}
	reached, ok := t.analyze(0, facts{closures: make([]int, len(t.acts[0].slots))})
	if !ok {
		return nil, nil, nil
	}
	if root != 0 {
		if !reached.seen[root] {
			return nil, nil, nil
		}
		reached, ok = t.analyze(root, reached.states[root].owning())
		if !ok {
			return nil, nil, nil
		}
	}
	built := t.build(reached, root)
	if built != nil && len(built.Pred(0)) > 0 {
		built = rotate(built)
	}
	addresses := make([]int, len(t.acts))
	for i, act := range t.acts {
		addresses[i] = act.address
	}
	return built, addresses, nil
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

// Of returns the profile of the function at address: the zero Profile when
// none was recorded.
func (p Profiles) Of(address int) Profile {
	if address < 0 || address >= len(p) || p[address] == nil {
		return Profile{}
	}
	return *p[address]
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

// newTranslation fixes the translation's members before any joint
// analysis. It analyzes the root alone, with every tail call to another
// function an exit, and collects the targets the walker's facts prove;
// then, breadth first up to memberMax functions, it admits each target that
// can share the root's frame and translates alone, and collects the targets
// that member proves in turn. A target proven only by facts that flow across
// members is missed, which leaves its call an exit. It fails when the root
// itself does not translate.
func newTranslation(m Module, root activation, spans []span, profiles Profiles) (*translation, bool) {
	t := &translation{module: m}
	t.add(root, spans)
	reached, ok := t.analyze(0, facts{closures: make([]int, len(root.slots))})
	if !ok {
		return nil, false
	}
	queue := reached.proofs
	for q := 0; q < len(queue) && len(t.acts) < memberMax; q++ {
		target := queue[q]
		if _, known := t.acts.find(target.address); known || !t.admits(target.function) {
			continue
		}
		blocks, err := analysis.Blocks(target.function)
		if err != nil {
			continue
		}
		spans, _ := split(target.function.Code, blocks)
		if spans[0].start != 0 || slices.ContainsFunc(spans, func(s span) bool { return s.suspend }) {
			continue
		}
		act := activation{function: target.function, address: target.address, slots: target.function.Declared(), profile: profiles.Of(target.address)}
		alone := &translation{module: m}
		alone.add(act, spans)
		reached, ok := alone.analyze(0, facts{closures: make([]int, len(act.slots))})
		if !ok {
			continue
		}
		t.add(act, spans)
		queue = append(queue, reached.proofs...)
	}
	return t, true
}

// admits reports whether target can share the root's frame: a plain function
// (no captures, so no upvals, and no handlers) with code, and the root's
// parameters, slots and results, so every slot-typed fact stays valid across
// the jump. Coroutine bodies never get here: they hold YIELD or RESUME,
// which newTranslation checks on the spans.
func (t *translation) admits(target *types.Function) bool {
	root := t.acts[0].function
	return target.Typ != nil && len(target.Code) > 0 && len(target.Captures) == 0 && len(target.Handlers) == 0 &&
		len(target.Typ.Params) == len(root.Typ.Params) &&
		slices.EqualFunc(target.Declared(), t.acts[0].slots, types.Type.Equals) &&
		slices.EqualFunc(target.Typ.Returns, root.Typ.Returns, types.Type.Equals)
}

// add appends act and its spans as a member.
func (t *translation) add(act activation, spans []span) {
	offset := len(t.spans)
	act.first = offset
	for _, s := range spans {
		s.act = len(t.acts)
		s.flow, s.succs = shifted(s.flow, offset), shifted(s.succs, offset)
		t.spans = append(t.spans, s)
	}
	t.acts = append(t.acts, act)
}

// analyze returns the facts live at each span's entry, which spans are
// reached from root, and the tail call targets walkers proved; nothing flows
// past a span that ends in an exit. A reached span's stack is nil, not just
// absent, when nothing is live there (e.g. an empty operand stack at a loop
// header): seen is what tells reached apart from never visited. A tail
// call's span continues into the first span of the member it loops to,
// found as it is translated.
func (t *translation) analyze(root int, in facts) (reach, bool) {
	r := reach{states: make([]facts, len(t.spans)), seen: make([]bool, len(t.spans)), jumps: map[int]int{}}
	r.seen[root] = true
	r.states[root] = in
	work := []int{root}
	for len(work) > 0 {
		id := work[len(work)-1]
		work = work[:len(work)-1]
		w, term, ok := t.transfer(t.spans[id], r.states[id])
		if !ok {
			return reach{}, false
		}
		if w.proof.function != nil && !slices.Contains(r.proofs, w.proof) {
			r.proofs = append(r.proofs, w.proof)
		}
		if t.spans[id].tail && w.jump >= 0 {
			r.jumps[id] = w.jump
		}
		if term.Op == ssa.OpExit {
			continue
		}
		exit := facts{stack: make([]fact, len(w.stack)), closures: w.closures}
		for i, o := range w.stack {
			exit.stack[i] = o.fact
		}
		for _, succ := range r.flow(t.spans[id], id) {
			if !r.seen[succ] {
				r.seen[succ] = true
				r.states[succ] = facts{stack: slices.Clone(exit.stack), closures: slices.Clone(exit.closures)}
				work = append(work, succ)
				continue
			}
			changed, ok := r.states[succ].merge(exit)
			if !ok {
				return reach{}, false
			}
			if changed {
				work = append(work, succ)
			}
		}
	}
	return r, true
}

// transfer walks s from in and returns the walker, which holds the facts at
// s's end, the span a tail call there looped to and the target it proved,
// and the terminator of s.
func (t *translation) transfer(s span, in facts) (*walker, ssa.Terminator, bool) {
	b := ssa.New("")
	block := b.Block()
	w, ok := newWalker(t, b, block, s, in)
	if !ok {
		return nil, ssa.Terminator{}, false
	}
	term, ok := w.translate(s)
	return w, term, ok
}

// build emits the spans r reached, in the order a walk from root over their
// successors visits them.
func (t *translation) build(r reach, root int) *ssa.Function {
	queued := make([]bool, len(t.spans))
	queued[root] = true
	order := []int{root}
	for n := 0; n < len(order); n++ {
		for _, successor := range r.succs(t.spans[order[n]], order[n]) {
			if r.seen[successor] && !queued[successor] {
				queued[successor] = true
				order = append(order, successor)
			}
		}
	}
	ids := make([]int, len(t.spans))
	for i := range ids {
		ids[i] = -1
	}
	act := t.acts[0]
	b := ssa.New(fmt.Sprintf("%d:%d", act.address, t.spans[root].start))
	b.Entry(ssa.Frame{Address: act.address, IP: t.spans[root].start, Returns: act.returns()})
	for _, id := range order {
		ids[id] = b.Block()
	}

	for _, id := range order {
		w, ok := newWalker(t, b, ids[id], t.spans[id], r.states[id])
		if !ok {
			return nil
		}
		term, ok := w.translate(t.spans[id])
		if !ok {
			return nil
		}
		if term.Op != ssa.OpExit {
			if term.Edges, ok = w.edges(r.succs(t.spans[id], id), r.states, ids); !ok {
				return nil
			}
		}
		b.Term(ids[id], term)
	}
	return b.Build()
}

// flow is the spans facts at s's end flow into: for a tail span, the member
// span its call jumps to, which only analysis knows.
func (r reach) flow(s span, id int) []int {
	if s.tail {
		return r.jumped(id)
	}
	return s.flow
}

// succs is the spans s continues into, by the same rule as flow.
func (r reach) succs(s span, id int) []int {
	if s.tail {
		return r.jumped(id)
	}
	return s.succs
}

func (r reach) jumped(id int) []int {
	if j, ok := r.jumps[id]; ok {
		return []int{j}
	}
	return nil
}

// shifted returns offsets moved by offset, in a new slice.
func shifted(offsets []int, offset int) []int {
	if offsets == nil {
		return nil
	}
	out := make([]int, len(offsets))
	for i, o := range offsets {
		out[i] = o + offset
	}
	return out
}

// find returns the index of the member at address.
func (m members) find(address int) (int, bool) {
	for i, act := range m {
		if act.address == address {
			return i, true
		}
	}
	return 0, false
}
