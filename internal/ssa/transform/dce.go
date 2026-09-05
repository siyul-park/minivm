package transform

import (
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// DCEPass removes what running the function can never need: a block nothing
// reaches from the entry, and an operation whose result nothing live reads
// and which instr's effect model says does nothing on its own. It is the SSA
// counterpart of transform.DCEPass, though the two do not overlap - the
// bytecode pass repairs branch offsets and exception tables a byte-shifting
// deletion invalidates, none of which exists here, where a deletion is just
// one fewer operation in a rebuilt block.
//
// instr's effect model, not a value's own use count, decides what survives
// regardless of it: an OpExec that reads or writes anything (op.Code is not
// IsPure()), an OpStore, a guard of any of the four kinds, a retain, a
// release, or a bridge runs for what it does, not for what it returns, so
// none of them is ever pruned even with zero uses of its result. From those
// roots, liveness runs backward through every value an operation reads - its
// arguments, an OpState's own frame stacks, and the state any of the above
// resumes into - so a value named only inside a live deopt's frame chain is
// exactly as live as one an ordinary argument names, and a pure operation
// feeding a live guard or store survives by that chain even though it is not
// itself a root. An OpState that nothing still live resumes into is pruned
// like anything else; running GuardPass first is what leaves one behind for
// this pass to sweep up, once the guard that alone resumed into it is gone.
//
// A block parameter is never pruned in this phase: dropping an unread one
// would also have to drop the matching argument from every predecessor's
// edge, a second cascading rewrite this pass does not attempt.
type DCEPass struct{}

var _ pass.Pass[*ssa.Function] = (*DCEPass)(nil)

func NewDCEPass() *DCEPass {
	return &DCEPass{}
}

func (p *DCEPass) Run(_ *pass.Manager, fn *ssa.Function) (pass.Preserved, error) {
	blocks := order(fn)
	live := liveOps(fn, blocks)

	rb := newRebuilder(fn)
	changed := len(blocks) != fn.Len()
	for _, block := range blocks {
		id := rb.block(block)
		blk := fn.Block(block)
		for _, param := range blk.Params {
			rb.alias(param, rb.b.Param(id, fn.Type(param)))
		}
		for i, op := range blk.Ops {
			if !live[site{block, i}] {
				changed = true
				continue
			}
			rb.b.Add(id, rb.define(fn, rb.operation(op)))
		}
		rb.b.Term(id, rb.terminator(blk.Term))
	}

	if !changed {
		return pass.PreserveAll(), nil
	}
	next := rb.b.Build()
	*fn = *next
	return pass.PreserveNone(), nil
}

// site names one operation: the block holding it and its index within that
// block's Ops.
type site struct {
	block int
	index int
}

// liveOps runs the mark phase of mark-sweep DCE over fn's reachable blocks:
// every operation instr's effect model says runs unconditionally is a root,
// every terminator's operands are roots, and liveness is then propagated
// backward from each root's own operands - including an OpState's frame
// stacks - to whatever defines them.
func liveOps(fn *ssa.Function, blocks []int) map[site]bool {
	defs := map[ssa.Value]site{}
	for _, b := range blocks {
		for i, op := range fn.Block(b).Ops {
			for _, r := range op.Results {
				defs[r] = site{b, i}
			}
		}
	}

	live := map[site]bool{}
	var queue []ssa.Value
	push := func(v ssa.Value) {
		if v != ssa.NoValue {
			queue = append(queue, v)
		}
	}
	mark := func(s site, op ssa.Operation) {
		if live[s] {
			return
		}
		live[s] = true
		for _, a := range op.Args {
			push(a)
		}
		for _, fr := range op.Frames {
			for _, v := range fr.Stack {
				push(v)
			}
		}
		push(op.State)
	}

	for _, b := range blocks {
		blk := fn.Block(b)
		for i, op := range blk.Ops {
			if effectful(op) {
				mark(site{b, i}, op)
			}
		}
		for _, a := range blk.Term.Args {
			push(a)
		}
		for _, e := range blk.Term.Edges {
			for _, a := range e.Args {
				push(a)
			}
		}
		push(blk.Term.State)
	}

	for len(queue) > 0 {
		v := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		s, ok := defs[v]
		if !ok {
			continue
		}
		mark(s, fn.Block(s.block).Ops[s.index])
	}
	return live
}

// effectful reports whether op runs for what it does rather than for what it
// returns, so DCEPass keeps it regardless of whether anything reads its
// results.
func effectful(op ssa.Operation) bool {
	switch op.Op {
	case ssa.OpStore, ssa.OpGuardKind, ssa.OpGuardShape, ssa.OpGuardBounds, ssa.OpGuardValue,
		ssa.OpRetain, ssa.OpRelease, ssa.OpBridge:
		return true
	case ssa.OpExec:
		return !op.Code.IsPure()
	default:
		return false
	}
}
