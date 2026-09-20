package transform

import (
	"sort"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// HoistPass moves a side-effect-free, non-trapping operation out of a natural
// loop and into its preheader when every argument it reads is defined
// outside the loop (or was itself just hoisted): textbook loop-invariant
// code motion, the dominance-based pass the loop-invariant design notes anticipate. It generalizes the old
// trace-plan hoistable rule to any *ssa.Function: that mechanism's
// one-container-per-loop and MaxHoistSlot limits were a trace-compiled
// loop's register-budget and ARM64-encoding artifacts, not IR-level hazards
// this pass has to honor, and its "no ref arrays" restriction does not carry
// over either, since this pass never moves an OpLoad, an OpStore, or any
// Heap-touching OpExec and so never bypasses the retain/release accounting
// that restriction protected.
//
// Eligibility is OpConst or a pure, speculatable OpExec (see speculatable)
// whose native lowering cannot exit through a representation guard.
// Excluding every OpLoad, OpStore, and Heap-touching OpExec
// refuses a heap read a loop's own write could invalidate with no alias
// analysis at all - not because this pass proved the specific loop has no
// such write, but because it never asks.
//
// This pass never moves an OpRetain or an OpRelease, and the operations it
// does move never carry one of their own: a pure operation, by instr's own
// definition, touches nothing an ownership pair would track.
//
// It hoists only into a preheader that already exists (see preheader) and
// never inserts one by splitting an edge. Loop headers are processed from
// the smallest natural loop body to the largest, so an operation hoisted out
// of an inner loop is reconsidered once its enclosing loop is processed,
// cascading a doubly loop-invariant operation out in one Run.
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
// see instr.Opcode.IsPure()) and is speculatable (never faults regardless of
// its operands), and whose native lowering has no representation guard that
// could exit on a zero-trip-count path (ssa.OverflowsI64).
func hoistable(op ssa.Operation) bool {
	switch op.Op {
	case ssa.OpConst:
		return true
	case ssa.OpExec:
		return op.Code.IsPure() && speculatable(op.Code) && !ssa.OverflowsI64(op.Code)
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
