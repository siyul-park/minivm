package transform

import (
	"sort"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// HoistPass moves a side-effect-free, non-trapping operation out of a natural
// loop and into its preheader when every argument it reads is itself defined
// outside the loop (or was itself just hoisted), so the operation runs once
// per loop entry instead of once per iteration: textbook loop-invariant code
// motion. It is internal/jit's traceplan.go hoistable generalized the way
// docs/jit-internals.md's "No hoist, no carry" paragraph always said a
// dominance-based optimizer pass could: no one-container-per-loop limit, no
// MaxHoistSlot, and it runs on any *ssa.Function, not only one a trace
// produced.
//
// hoistable's three restrictions turn out to divide two ways once reread
// against what this pass actually needs to be sound:
//
//   - "One container per loop" is a Plan.Hoist representation artifact
//     (internal/jit/plan.go): the field is a single *Hoist, not a slice,
//     because a trace-compiled loop caches its winning container's data
//     pointer and length in fixed machine registers that stay live across
//     the back-edge, and a backend has only so many to spend. This pass has
//     no register budget to protect - it hoists every eligible operation.
//   - MaxHoistSlot is an ARM64 load-immediate encoding bound
//     (internal/jit/plan.go), not an IR-level hazard. Nothing here reasons
//     about slots or immediates at all.
//   - "No ref arrays" is real, but not a hazard this pass can trigger: it
//     exists because internal/jit/arm64/control.go's hoist additionally
//     caches a raw slice header - a data pointer and length read once and
//     trusted for the rest of the native entry - so a ref element's
//     retain/release accounting (normally paid on every ARRAY_GET/ARRAY_SET)
//     never happens for a hoisted access. This pass hoists no such cache: it
//     never moves an OpLoad, an OpStore, or any OpExec that reads or writes
//     Heap (see below), so it never bypasses a retain or a release to begin
//     with. The hazard is real, but it belongs to retain/release pairing,
//     which docs/jit-internals.md already defers until the backend consumes
//     SSA.
//
// Eligibility is deliberately narrow, for two hazards hoistable never had to
// face because a trace only ever compiles a path it already ran at least
// once:
//
//   - Effects. Eligibility is OpConst, or an OpExec whose instr.Opcode.IsPure()
//     holds: instr's own Reads and Writes are both empty (see
//     instr.Opcode.IsPure). That is a strictly stronger rule than "does not
//     read what the loop writes" - it excludes OpLoad, OpStore, and every
//     OpExec that touches Local, Global, Upval, Heap, Frame, or Branch
//     outright, so a heap read a loop's own heap write could invalidate (an
//     ARRAY_GET aliasing an ARRAY_SET elsewhere in the body, say) is refused
//     with no alias analysis at all - not because this pass proved the
//     specific loop has no such write, but because it never asks. Redundant
//     mutable loads across a loop are a dataflow problem CSEPass's own
//     documentation already leaves for "the eventual redundant-load pass";
//     this one is smaller still.
//   - Guards, traps, and deopt state. A loop's preheader runs exactly once
//     per entry, even when the loop body itself runs zero times - a
//     conditional loop tests before its first iteration, and this IR has no
//     general way to prove a body always runs at least once. Hoisting an
//     operation that can fail (a guard) or fault (integer division or
//     remainder by a divisor that might be zero) into the preheader would
//     make it run - and possibly fail or fault - on an execution the
//     original program never reached with that operation at all. Every one
//     of the four guards always carries deopt State (instr's own resume()
//     rule, enforced by ssa.Verify), and OpState's Frame chain names the
//     bytecode position and stack the guard's own site was speculated at -
//     valid to resume into as recorded, not valid to resume into from a
//     point before the loop ever ran. This pass sidesteps both problems by
//     refusing anything that carries State: no guard, no OpStore, no
//     OpRelease, no OpBridge, and no OpExec that writes Frame or that both
//     reads and writes Heap is ever a candidate (ssa.Verify's own resume()
//     rule already makes every one of those state-bearing, so this check is
//     belt-and-suspenders over IsPure() rather than a second, independent
//     gate). Integer division and remainder are IsPure() by instr's own
//     table - no Reads, no Writes - yet a zero divisor faults the
//     interpreter, so they are excluded by name (see speculatable) the same
//     way FoldPass declines to fold a literal-zero divisor at compile time
//     rather than pre-empt that same trap.
//
// Retain/release pairing is out of scope, per docs/jit-internals.md and
// docs/architecture.md: this pass never moves an OpRetain or an OpRelease,
// and the operations it does move never carry one of their own to begin
// with (a pure operation, by instr's own definition, touches nothing an
// ownership pair would need to track).
//
// Preheader: this pass only ever hoists into a preheader that already
// exists - a block that already is the loop header's one predecessor from
// outside the loop - and never inserts one by splitting an edge. A header
// with more than one outside predecessor (no preheader) or exactly zero (an
// entry block that is itself a loop header) is left alone entirely: nothing
// in its body ever hoists, however invariant, rather than growing the block
// graph to make room for it. internal/graph gives dominance and loop headers
// but no preheader-insertion helper, and edge-splitting correctly - rewiring
// every predecessor's edge, not just one, and reordering the rebuilt block
// list - is a second mechanism this phase does not need to build to hoist
// the class of operations it targets. A future phase that wants to hoist a
// speculatable guard past a proven-non-zero trip count, say, may need one;
// this one does not.
//
// Nesting: loop headers are processed from the smallest natural loop body to
// the largest, so an operation hoisted out of an inner loop into its
// preheader - itself still inside the enclosing loop's body - is considered
// again once the enclosing loop is processed, cascading a doubly
// loop-invariant operation all the way out in one Run.
//
// Ordering against this package's other three passes: none of them is a
// correctness precondition. HoistPass never unifies two values (only
// CSEPass and GuardPass's shared dedup does that) and never touches a guard
// or anything else that carries State (only GuardPass's target), so it is
// sound run first, last, or anywhere between them. Running it after
// FoldPass and CSEPass still reads better: a computation FoldPass has
// already reduced to an OpConst is trivially invariant with no argument
// analysis needed, and a computation CSEPass has already deduplicated into
// one dominance-scoped instance is one operation for this pass to move
// instead of several identical ones. Running it before DCEPass keeps the
// existing convention that liveness-based sweeping runs last, over
// whatever placement every earlier pass settled on.
type HoistPass struct{}

var _ pass.Pass[*ssa.Function] = (*HoistPass)(nil)

func NewHoistPass() *HoistPass {
	return &HoistPass{}
}

func (p *HoistPass) Run(_ *pass.Manager, fn *ssa.Function) (pass.Preserved, error) {
	dom := graph.NewDominance(fn)
	headers := graph.LoopHeaders(fn, dom)
	if len(headers) == 0 {
		return pass.PreserveAll(), nil
	}

	bodies := make(map[int]map[int]bool, len(headers))
	preheaders := make(map[int]int, len(headers))
	for _, h := range headers {
		b := loopBody(fn, dom, h)
		bodies[h] = b
		if p, ok := preheader(fn, b, h); ok {
			preheaders[h] = p
		}
	}
	sort.Slice(headers, func(i, j int) bool {
		return len(bodies[headers[i]]) < len(bodies[headers[j]])
	})

	blocks := order(fn)
	defSite := map[ssa.Value]site{}
	paramOf := map[ssa.Value]int{}
	for _, b := range blocks {
		blk := fn.Block(b)
		for _, v := range blk.Params {
			paramOf[v] = b
		}
		for i, op := range blk.Ops {
			for _, r := range op.Results {
				defSite[r] = site{b, i}
			}
		}
	}

	// dest names, for a site that has been decided eligible, the block its
	// operation now lands in; a site absent from dest still lands in its own
	// original block. current and location read through it so a value
	// hoisted by an inner loop is already seen at its new position when an
	// outer loop asks where it lives.
	dest := map[site]int{}
	current := func(s site) int {
		if b, ok := dest[s]; ok {
			return b
		}
		return s.block
	}
	location := func(v ssa.Value) (int, bool) {
		if b, ok := paramOf[v]; ok {
			return b, true
		}
		if s, ok := defSite[v]; ok {
			return current(s), true
		}
		return 0, false
	}

	changed := false
	for _, h := range headers {
		p, ok := preheaders[h]
		if !ok {
			continue
		}
		body := bodies[h]
		for _, b := range blocks {
			if !body[b] {
				continue
			}
			ops := fn.Block(b).Ops
			for i, op := range ops {
				s := site{b, i}
				if !body[current(s)] {
					continue
				}
				if !hoistable(op) {
					continue
				}
				invariant := true
				for _, a := range op.Args {
					loc, ok := location(a)
					if !ok || body[loc] {
						invariant = false
						break
					}
				}
				if invariant {
					dest[s] = p
					changed = true
				}
			}
		}
	}

	if !changed {
		return pass.PreserveAll(), nil
	}

	rb := newRebuilder(fn)
	for _, b := range blocks {
		id := rb.block(b)
		blk := fn.Block(b)
		for _, v := range blk.Params {
			rb.alias(v, rb.b.Param(id, fn.Type(v)))
		}
		for i, op := range blk.Ops {
			target := rb.block(current(site{b, i}))
			rb.b.Add(target, rb.define(fn, rb.operation(op)))
		}
		rb.b.Term(id, rb.terminator(blk.Term))
	}
	next := rb.b.Build()
	*fn = *next
	return pass.PreserveNone(), nil
}

// hoistable reports whether op may ever move: an OpConst, which reads
// nothing, or an OpExec whose opcode both IsPure() (no Reads, no Writes -
// see instr.Opcode.IsPure) and is speculatable (never faults regardless of
// its operands). Neither ever carries deopt State by ssa.Verify's own
// resume() rule, and the explicit check here documents that this pass
// depends on it rather than only inheriting it silently.
func hoistable(op ssa.Operation) bool {
	if op.State != ssa.NoValue {
		return false
	}
	switch op.Op {
	case ssa.OpConst:
		return true
	case ssa.OpExec:
		return op.Code.IsPure() && speculatable(op.Code)
	default:
		return false
	}
}

// speculatable reports whether a pure opcode is safe to run at a program
// point the original bytecode might never have reached, which hoisting into
// a preheader always risks when a loop's trip count could be zero. Every
// IsPure() opcode but integer division and remainder qualifies: arithmetic,
// bitwise, and comparison opcodes cannot fault on any operand value, shifts
// mask their amount, and a narrowing conversion saturates rather than traps.
// Integer division and remainder by a zero divisor panic the interpreter -
// exactly the fault FoldPass also declines to pre-empt for a literal zero
// divisor at compile time - and a loop-invariant divisor is invariant
// precisely because it is the same value on every iteration the loop would
// have run, including zero of them.
func speculatable(code instr.Opcode) bool {
	switch code {
	case instr.I32_DIV_S, instr.I32_DIV_U, instr.I32_REM_S, instr.I32_REM_U,
		instr.I64_DIV_S, instr.I64_DIV_U, instr.I64_REM_S, instr.I64_REM_U:
		return false
	default:
		return true
	}
}

// loopBody returns the natural loop of header: header itself plus every
// block that can reach a back edge into header without passing through
// header again - the standard construction (Aho, Sethi, and Ullman),
// computed by walking predecessors backward from every block whose edge to
// header is a back edge (header dominates the source).
func loopBody(fn *ssa.Function, dom *graph.Dominance, header int) map[int]bool {
	body := map[int]bool{header: true}
	var stack []int
	for _, p := range fn.Pred(header) {
		if dom.Dominates(header, p) && !body[p] {
			body[p] = true
			stack = append(stack, p)
		}
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, p := range fn.Pred(n) {
			if !body[p] {
				body[p] = true
				stack = append(stack, p)
			}
		}
	}
	return body
}

// preheader returns header's one predecessor outside body, or false when
// header has no such predecessor (it is itself unreachable from outside the
// loop) or more than one (control enters the loop by more than one path, and
// this pass does not split an edge to give it a single one).
func preheader(fn *ssa.Function, body map[int]bool, header int) (int, bool) {
	found, ok := -1, false
	for _, p := range fn.Pred(header) {
		if body[p] {
			continue
		}
		if ok {
			return 0, false
		}
		found, ok = p, true
	}
	return found, ok
}
