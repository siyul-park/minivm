package frontend

import (
	"fmt"
	"sort"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// replay turns one recorded tree into SSA: it lays the root recording out as a
// chain of blocks, folds the continuations recorded at its hot exits into the
// same graph, and leaves every target no recording covers as a deopt. A
// recording is linear, so the block a path continues in is the one the walk
// keeps filling; only a fold and a back-edge are real merges, and those are the
// only blocks anything joins.
type replay struct {
	facts
	b      *ssa.Builder
	anchor jit.Anchor
	legs   []*jit.Trace

	blocks  map[jit.Anchor]int
	params  map[int][]operand
	pending []work
}

// work is one recording whose block is laid out but whose ops are not yet
// translated: the frames and operands the edge that reached it left behind, and
// where it ranks among the continuations.
type work struct {
	trace  *jit.Trace
	rank   int
	block  int
	frames []frame
	stack  []operand
}

// Trace returns the SSA the recording rooted at root plans: the recorded path
// specialized to what it observed, with the legs recorded at its hot exits
// folded into the same native entry. It returns nil when root carries no
// recording a native entry can be anchored on, which is not a failure - the
// caller falls back to the static frontend.
func Trace(input *jit.Input, root jit.Anchor) *ssa.Function {
	if input == nil || input.Traces == nil || input.Function == nil || root.Addr != input.Address {
		return nil
	}
	tree := input.Traces.RootAt(root)
	if tree == nil || tree.Root == nil || !usable(tree.Root, root) {
		return nil
	}
	r := &replay{
		// A recording states the shape it ran against, so the declared-type
		// fallback would only add speculation the recording never made.
		facts:  facts{constants: input.Constants, globals: input.Globals, objects: input.Objects, decl: input.Decl},
		b:      ssa.New(fmt.Sprintf("%d:%d", root.Addr, root.IP)),
		anchor: root,
		legs:   legs(tree),
		blocks: map[jit.Anchor]int{},
		params: map[int][]operand{},
	}
	if !r.plan(tree.Root, input.Function) {
		return nil
	}
	return r.b.Build()
}

// usable reports whether a recording can anchor a native entry. A loop anchor
// takes a looping root or a returned straight-line one: a body whose terminal
// boundary deopts before the back-edge still runs as a per-entry prefix that
// re-enters at the header next iteration. It refuses a root whose anchor frame
// held live operands, because nothing records what those operands were and the
// entry block would have to state their types.
func usable(tr *jit.Trace, root jit.Anchor) bool {
	switch tr.Status {
	case jit.StatusFallback, jit.StatusCompleted, jit.StatusPartial:
		return root.IP == 0
	case jit.StatusReturned:
		return root.IP == 0 || !tr.Carried
	case jit.StatusLoop:
		return root.IP != 0 && !tr.Carried
	default:
		return false
	}
}

// legs returns the continuations one tree's hot exits recorded, hottest first.
// An aborted fragment is never one. Neither is a loop: anchored at this header
// it is the root recording itself, and anchored anywhere else it is another
// loop, whose body belongs to its own native entry rather than inlined here.
func legs(tree *jit.Tree) []*jit.Trace {
	type leg struct {
		trace *jit.Trace
		hits  int64
	}
	var found []leg
	for id, tr := range tree.Branches {
		if tr == nil {
			continue
		}
		switch tr.Status {
		case jit.StatusFallback, jit.StatusReturned, jit.StatusCompleted, jit.StatusPartial:
		default:
			continue
		}
		hits := int64(0)
		if id >= 0 && id < len(tree.Hits) {
			hits = tree.Hits[id]
		}
		found = append(found, leg{trace: tr, hits: hits})
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].hits != found[j].hits {
			return found[i].hits > found[j].hits
		}
		if found[i].trace.Anchor.Addr != found[j].trace.Anchor.Addr {
			return found[i].trace.Anchor.Addr < found[j].trace.Anchor.Addr
		}
		return found[i].trace.Anchor.IP < found[j].trace.Anchor.IP
	})
	out := make([]*jit.Trace, len(found))
	for i, leg := range found {
		out[i] = leg.trace
	}
	return out
}

// plan translates the root recording and every continuation reachable from it.
func (r *replay) plan(root *jit.Trace, fn *types.Function) bool {
	id := r.b.Block()
	r.blocks[root.Anchor] = id
	r.pending = []work{{
		trace:  root,
		rank:   -1,
		block:  id,
		frames: []frame{{fn: fn, addr: r.anchor.Addr, slots: fn.Declared()}},
	}}
	for len(r.pending) > 0 {
		if !r.run(r.take()) {
			return false
		}
	}
	return true
}

// take removes the hottest recording still waiting. Which block a plan lays out
// first is then the order the hit counts rank the continuations in, rather than
// the order the edges reaching them happened to be built.
func (r *replay) take() work {
	at := 0
	for i := range r.pending {
		if r.pending[i].rank < r.pending[at].rank {
			at = i
		}
	}
	out := r.pending[at]
	r.pending = append(r.pending[:at], r.pending[at+1:]...)
	return out
}

// run translates one recording, filling the block it was reached in and every
// block its branches open, and ends it on the terminator its status names.
func (r *replay) run(item work) bool {
	tr := item.trace
	w := &walk{facts: r.facts, b: r.b, block: item.block, frames: item.frames, stack: item.stack}
	seam := r.seams(tr)
	base := len(w.frames)
	for idx := 0; idx < len(tr.Ops); idx++ {
		op := tr.Ops[idx]
		if op.Cut {
			return r.cut(w, tr, idx)
		}
		fr := w.frame()
		if op.Fn != fr.addr || op.IP < 0 || op.IP >= len(fr.fn.Code) {
			return false
		}
		inst := instr.Instruction(fr.fn.Code[op.IP:])
		w.begin(op.IP)
		w.seen = op.Step

		switch op.Op {
		case instr.BR, instr.BR_IF, instr.BR_TABLE:
			next, ok := r.branch(w, tr, idx, inst)
			if !ok {
				return false
			}
			if next < 0 {
				return true
			}
			w.block = next
			continue
		case instr.RETURN:
			if len(w.frames) > base {
				if !r.rejoin(w, seam[idx]) {
					return false
				}
				continue
			}
			return r.retire(w, base)
		case instr.CALL:
			// The recording says whether the call was entered: the ops after an
			// inlined one run one frame deeper. Anything else - a host call, a
			// recursion the recorder stepped over - stays one operation.
			if idx+1 < len(tr.Ops) && tr.Ops[idx+1].Depth > op.Depth {
				if len(w.stack) == 0 {
					return false
				}
				target := w.callee(w.stack[len(w.stack)-1].fact)
				if target == nil || !w.enter(op.Callee, op.IP+inst.Width(), target) {
					return false
				}
				continue
			}
		}
		// A recording ends at the operation it could not step past, so the
		// interpreter takes that one over with nothing to resume into. Every
		// other bridgeable opcode still resumes natively after it.
		if jit.Bridgeable(op.Op) && idx+1 == len(tr.Ops) {
			w.exit(op.IP)
			return true
		}
		if !w.perform(inst) {
			return false
		}
	}
	return r.close(w, tr)
}

// branch ends a block on the branch recorded at idx and returns the block the
// recording carries on in, or -1 when it carries on nowhere: the recorded path
// is one edge, and every other edge leaves for a continuation or a deopt.
func (r *replay) branch(w *walk, tr *jit.Trace, idx int, inst instr.Instruction) (int, bool) {
	op := tr.Ops[idx]
	targets, hot, ok := edges(w.frame().fn.Code, op, inst)
	if !ok {
		return 0, false
	}

	term := ssa.Terminator{Op: ssa.OpJump}
	if op.Op != instr.BR {
		if len(w.stack) == 0 {
			return 0, false
		}
		term.Op, term.Args = ssa.OpBranch, []ssa.Value{w.stack[len(w.stack)-1].value}
		if op.Op == instr.BR_TABLE {
			term.Op = ssa.OpTable
		}
		w.stack = w.stack[:len(w.stack)-1]
	}
	// The recorded path continues in a block of its own unless the recording
	// stops here, in which case its own terminator settles where that edge
	// goes - the header a loop closes on, or a deopt.
	chain := idx+1 < len(tr.Ops) && !backedge(r.anchor, tr, idx+1, targets[hot])
	anchors := make([]jit.Anchor, len(targets))
	for i, target := range targets {
		anchors[i] = jit.Anchor{Addr: op.Fn, IP: target}
	}
	if !r.settle(w, anchors, chain, hot) {
		return 0, false
	}

	next, args := -1, values(w.stack)
	var carry []operand
	// A continuation folded here needs somewhere to carry on once the frame it
	// was recorded inside returns, which only a recording that leaves that frame
	// again has.
	folds := len(w.frames) == 1 || drop(tr, idx) > 0
	term.Edges = make([]ssa.Edge, len(targets))
	for i := range targets {
		if i == hot && chain {
			next, carry = r.lay(w, false)
			term.Edges[i] = ssa.Edge{Block: next, Args: args}
			continue
		}
		edge, ok := r.reach(w, anchors[i], i == hot || folds)
		if !ok {
			return 0, false
		}
		term.Edges[i] = edge
	}
	r.b.Term(w.block, term)
	if next >= 0 {
		w.stack = carry
	}
	return next, true
}

// settle owns every operand a successor already laid out holds owned, before
// any edge is built. A reference two successors disagree about cannot be handed
// to both, and taking the retain up front keeps which edge was built first out
// of the answer.
func (r *replay) settle(w *walk, targets []jit.Anchor, chain bool, hot int) bool {
	for i, target := range targets {
		if i == hot && chain {
			continue
		}
		id, ok := r.blocks[target]
		if !ok {
			continue
		}
		params := r.params[id]
		if len(params) != len(w.stack) {
			return false
		}
		for at := range params {
			if params[at].backing == jit.BackingStack {
				w.own(at)
			}
		}
	}
	return true
}

// retire ends a recording on the return that leaves the frame it was recorded
// in. The root recording leaves the function; one cut out of an inlined callee
// hands its results back to the caller and branches to the block that caller
// carries on in, which the recording it was cut out of laid out at exactly the
// point its own depth dropped back.
func (r *replay) retire(w *walk, base int) bool {
	if base == 1 {
		if len(w.stack) < w.returns() {
			return false
		}
		r.b.Term(w.block, w.leave())
		return true
	}
	after := w.frame().after
	if !w.stitch() {
		return false
	}
	if after == nil || *after < 0 {
		return false
	}
	args, ok := w.join(r.params[*after])
	if !ok {
		return false
	}
	r.b.Term(w.block, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: *after, Args: args}}})
	return true
}

// rejoin closes an inlined frame and, when a continuation can rejoin here,
// opens the block it rejoins into. Everything the caller does next belongs to
// that block, so the recording that was cut out of this one branches to it once
// the frame it was recorded inside returns.
func (r *replay) rejoin(w *walk, seam bool) bool {
	after := w.frame().after
	if !w.stitch() {
		return false
	}
	if !seam {
		return true
	}
	args := values(w.stack)
	id, params := r.lay(w, true)
	r.b.Term(w.block, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: id, Args: args}}})
	w.block, w.stack = id, params
	*after = id
	return true
}

// cut ends a recording at a boundary the recorder inserted. A cut that lands on
// this plan's own header, in the frame that anchors it, is the loop back-edge:
// folding it onto the root block keeps the loop inside native code instead of
// paying a deopt and a re-entry every iteration. Every other cut leaves.
func (r *replay) cut(w *walk, tr *jit.Trace, idx int) bool {
	if len(w.frames) == 1 && backedge(r.anchor, tr, idx, r.anchor.IP) {
		edge, ok := r.reach(w, r.anchor, true)
		if !ok {
			return false
		}
		r.b.Term(w.block, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{edge}})
		return true
	}
	w.exit(tr.Ops[idx].Target)
	return true
}

// close ends a recording that ran out of ops on the terminator its status
// names. A returned one gets here only after a boundary it performed but could
// not continue past, so the interpreter picks up at the operation after it.
func (r *replay) close(w *walk, tr *jit.Trace) bool {
	switch tr.Status {
	case jit.StatusFallback:
		w.exit(tr.Anchor.IP)
	case jit.StatusCompleted:
		r.b.Term(w.block, w.complete())
	case jit.StatusLoop:
		edge, ok := r.reach(w, tr.Anchor, true)
		if !ok {
			return false
		}
		r.b.Term(w.block, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{edge}})
	case jit.StatusReturned:
		last := tr.Ops[len(tr.Ops)-1]
		if !last.Terminal || last.Fn != w.frame().addr {
			return false
		}
		w.exit(last.IP + instr.Instruction(w.frame().fn.Code[last.IP:]).Width())
	default:
		return false
	}
	return true
}

// reach returns the edge one target is entered through: the block a recording
// already laid out for it, a fresh one for a continuation still to be folded
// in, or a deopt for a target no recording folded here covers.
func (r *replay) reach(w *walk, target jit.Anchor, folds bool) (ssa.Edge, bool) {
	if id, ok := r.blocks[target]; ok {
		args, ok := w.join(r.params[id])
		return ssa.Edge{Block: id, Args: args}, ok
	}
	leg, rank := r.leg(target)
	if folds && leg != nil {
		args := values(w.stack)
		id, params := r.lay(w, true)
		r.blocks[target] = id
		r.pending = append(r.pending, work{
			trace:  leg,
			rank:   rank,
			block:  id,
			frames: append([]frame(nil), w.frames...),
			stack:  params,
		})
		return ssa.Edge{Block: id, Args: args}, true
	}
	args := values(w.stack)
	id, params := r.lay(w, false)
	out := &walk{facts: r.facts, b: r.b, block: id, frames: w.frames, stack: params}
	out.exit(target.IP)
	return ssa.Edge{Block: id, Args: args}, true
}

// lay opens a block, taking the operands live here as its parameters. That is
// what lets a fold, a back-edge, and a caller continuation meet: the operands
// cross on the edge, so no block has to reload them or assume what it was
// entered with. A shared one - anything more than this path can enter - widens
// them first, because it may read its parameters only through facts every
// recording entering it carries.
func (r *replay) lay(w *walk, shared bool) (int, []operand) {
	id := r.b.Block()
	params := make([]operand, len(w.stack))
	for i, o := range w.stack {
		if shared {
			o.fact = o.fact.widen()
		}
		params[i] = operand{value: r.b.Param(id, r.b.Type(o.value)), fact: o.fact}
	}
	r.params[id] = params
	return id, params
}

// leg returns the continuation recorded at target and where it ranks among the
// others by hit count, or nil when none was recorded there.
func (r *replay) leg(target jit.Anchor) (*jit.Trace, int) {
	for i, leg := range r.legs {
		if leg.Anchor == target {
			return leg, i
		}
	}
	return nil, len(r.legs)
}

// seams returns the recorded returns a folded continuation can rejoin at. A
// continuation anchored inside an inlined frame ends where that frame returns,
// and the caller carries on at the point the recording it was cut out of
// reached once its own depth dropped back - so the return before that point has
// to end its block, giving the continuation somewhere to branch to.
func (r *replay) seams(tr *jit.Trace) map[int]bool {
	out := map[int]bool{}
	for idx, op := range tr.Ops {
		if op.Cut || op.Depth == 0 || !r.cold(tr, idx) {
			continue
		}
		if at := drop(tr, idx); at > 0 {
			out[at-1] = true
		}
	}
	return out
}

// cold reports whether the branch at idx leaves for a continuation recorded
// elsewhere. Only an unrecorded edge does: the path the recording took carries
// on in this plan's own blocks.
func (r *replay) cold(tr *jit.Trace, idx int) bool {
	op := tr.Ops[idx]
	fn := r.objects.Function(op.Fn)
	if fn == nil || op.IP < 0 || op.IP >= len(fn.Code) {
		return false
	}
	targets, hot, ok := edges(fn.Code, op, instr.Instruction(fn.Code[op.IP:]))
	if !ok || op.Op == instr.BR {
		return false
	}
	for i, target := range targets {
		if leg, _ := r.leg(jit.Anchor{Addr: op.Fn, IP: target}); i != hot && leg != nil {
			return true
		}
	}
	return false
}

// edges resolves the targets a recorded branch names and which of them the
// recording took.
func edges(code []byte, op jit.Record, inst instr.Instruction) ([]int, int, bool) {
	switch op.Op {
	case instr.BR:
		return []int{op.Target}, 0, true
	case instr.BR_IF:
		hot := 0
		if !op.Taken {
			hot = 1
		}
		return []int{op.Target, op.IP + inst.Width()}, hot, true
	default:
		targets := instr.Targets(code, op.IP)
		for i, target := range targets {
			if target == op.Target {
				return targets, i, true
			}
		}
		return nil, 0, false
	}
}

// drop is where a recording first leaves the frame the op at idx runs in.
func drop(tr *jit.Trace, idx int) int {
	for at := idx + 1; at < len(tr.Ops); at++ {
		if tr.Ops[at].Depth < tr.Ops[idx].Depth {
			return at
		}
	}
	return -1
}

// backedge reports whether the record at idx is a cut that closes the loop this
// plan is anchored on, reached from target. The recording took the branch
// and stopped there, so the branch's own edge is the back-edge and chaining an
// empty block onto it would only stand between them.
func backedge(root jit.Anchor, tr *jit.Trace, idx, target int) bool {
	op := tr.Ops[idx]
	return op.Cut && op.Depth == 0 && root.IP != 0 &&
		op.Fn == root.Addr && op.Target == root.IP && target == root.IP
}
