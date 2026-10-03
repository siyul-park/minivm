package transform

import (
	"math"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// BoundPass drops the bounds check of an array op on a slice indexed by a
// loop's induction variable i, which counts up from a constant c ≥ 0 by
// constant steps and stays below the limit n the loop header tests, signed:
// then 0 ≤ i < n holds in the loop. The index becomes an OpBound. n ≤ length
// is checked once by an OpGuardBounds in the loop's preheader that deopts at the header's entry
// state. A loop whose header entry offset is refuted keeps its checks.
// Even n = array.len is checked: a shrinking op between it and the
// preheader would leave n above the length.
type BoundPass struct {
	refuted map[int]bool
}

// induction is a loop the pass bounds: its preheader, induction variable,
// limit, the block the header's stay edge enters, and the header's entry
// state.
type induction struct {
	preheader    int
	index, limit ssa.Value
	stay         int
	frames       []ssa.Frame
}

var _ pass.Pass[*ssa.Function] = (*BoundPass)(nil)

// NewBoundPass returns the pass; refuted holds the offsets whose guard native
// code has seen fail (Module.Refuted).
func NewBoundPass(refuted map[int]bool) *BoundPass {
	return &BoundPass{refuted: refuted}
}

// Run applies the pass to one SSA function.
func (p *BoundPass) Run(_ *pass.Manager, function *ssa.Function) (bool, error) {
	dominance := graph.NewDominance(function)
	sites := map[ssa.Value]site{}
	params := map[ssa.Value]int{}
	for b := 0; b < function.Len(); b++ {
		for _, v := range function.Block(b).Params {
			params[v] = b
		}
		for i, operation := range function.Block(b).Operations {
			for _, r := range operation.Results {
				sites[r] = site{b, i}
			}
		}
	}
	define := func(v ssa.Value) (ssa.Operation, bool) {
		s, ok := sites[v]
		if !ok {
			return ssa.Operation{}, false
		}
		return function.Block(s.block).Operations[s.index], true
	}
	bounded := map[site]bool{}
	guards := map[int][]ssa.Operation{}
	for _, h := range graph.Headers(function, dominance) {
		body := graph.Body(function, dominance, h)
		loop, ok := p.induction(function, dominance, body, h, sites, params, define)
		if !ok {
			continue
		}
		guarded := map[ssa.Value]bool{}
		for b := range body {
			if !dominance.Dominates(loop.stay, b) {
				continue
			}
			for i, operation := range function.Block(b).Operations {
				if operation.Op != ssa.OpExec || (operation.Code != instr.ARRAY_GET && operation.Code != instr.ARRAY_SET) || operation.Args[1] != loop.index {
					continue
				}
				slice, ok := define(operation.Args[0])
				if !ok || slice.Op != ssa.OpSlice {
					continue
				}
				if loop.frames == nil || !dominance.Dominates(sites[operation.Args[0]].block, loop.preheader) {
					continue
				}
				if !guarded[operation.Args[0]] {
					guarded[operation.Args[0]] = true
					guards[loop.preheader] = append(guards[loop.preheader], ssa.Operation{Args: []ssa.Value{loop.limit, operation.Args[0]}, Frames: loop.frames})
				}
				bounded[site{b, i}] = true
			}
		}
	}
	if len(bounded) == 0 {
		return true, nil
	}

	r := newRebuilder(function)
	for _, b := range graph.Order(function) {
		id := r.open(b)
		for i, operation := range function.Block(b).Operations {
			operation = r.operation(operation)
			if bounded[site{b, i}] {
				v := r.builder.Value(ssa.TypeI32)
				r.builder.Add(id, ssa.Operation{Op: ssa.OpBound, Args: []ssa.Value{operation.Args[1], operation.Args[0]}, Results: []ssa.Value{v}})
				operation.Args[1] = v
			}
			r.builder.Add(id, r.define(operation))
		}
		for _, guard := range guards[b] {
			state, length := r.builder.Value(ssa.TypeState), r.builder.Value(ssa.TypeI32)
			r.builder.Add(id, r.operation(ssa.Operation{Op: ssa.OpState, Frames: guard.Frames, Results: []ssa.Value{state}}))
			r.builder.Add(id, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_LEN, Args: []ssa.Value{r.value(guard.Args[1])}, State: state, Results: []ssa.Value{length}})
			r.builder.Add(id, ssa.Operation{Op: ssa.OpGuardBounds, Args: []ssa.Value{r.value(guard.Args[0]), length}, State: state})
		}
		r.builder.Term(id, r.terminator(function.Block(b).Terminator))
	}
	*function = *r.builder.Build()
	return false, nil
}

// induction reports the loop at header h when its branch tests i <s n on a
// header param i that enters at a constant ≥ 0 and steps up by constants
// along every latch, n is loop-invariant, and no step wraps i past n.
// frames is the header's entry state, nil when it has none usable or its
// offset is refuted: the loop then keeps its checks.
func (p *BoundPass) induction(function *ssa.Function, dominance *graph.Dominance, body map[int]bool, h int, sites map[ssa.Value]site, params map[ssa.Value]int, define func(ssa.Value) (ssa.Operation, bool)) (induction, bool) {
	preheader, ok := graph.Preheader(function, body, h)
	if !ok {
		return induction{}, false
	}
	t := function.Block(h).Terminator
	if t.Op != ssa.OpBranch || len(t.Edges) != 2 {
		return induction{}, false
	}
	test, ok := define(t.Args[0])
	if !ok || sites[t.Args[0]].block != h || test.Op != ssa.OpExec {
		return induction{}, false
	}
	loop := induction{preheader: preheader}
	stay := 0
	switch test.Code {
	case instr.I32_GE_S:
		loop.index, loop.limit, stay = test.Args[0], test.Args[1], 1
	case instr.I32_LT_S:
		loop.index, loop.limit = test.Args[0], test.Args[1]
	case instr.I32_LE_S:
		loop.limit, loop.index, stay = test.Args[0], test.Args[1], 1
	case instr.I32_GT_S:
		loop.limit, loop.index = test.Args[0], test.Args[1]
	default:
		return induction{}, false
	}
	loop.stay = t.Edges[stay].Block
	if params[loop.index] != h || function.Type(loop.index) != ssa.TypeI32 || !body[loop.stay] || len(function.Pred(loop.stay)) != 1 {
		return induction{}, false
	}
	invariant := func(v ssa.Value) bool {
		if b, ok := params[v]; ok {
			return !body[b]
		}
		s, ok := sites[v]
		return ok && !body[s.block]
	}
	if !invariant(loop.limit) {
		return induction{}, false
	}

	k := 0
	for k < len(function.Block(h).Params) && function.Block(h).Params[k] != loop.index {
		k++
	}
	constant := func(v ssa.Value) (int64, bool) {
		op, ok := define(v)
		return int64(int32(op.Const)), ok && op.Op == ssa.OpConst
	}
	step := int64(0)
	for _, pred := range function.Pred(h) {
		for _, e := range function.Block(pred).Terminator.Edges {
			if e.Block != h {
				continue
			}
			v := e.Args[k]
			if !body[pred] {
				if c, ok := constant(v); !ok || c < 0 {
					return induction{}, false
				}
				continue
			}
			add, ok := define(v)
			if !ok || add.Op != ssa.OpExec || add.Code != instr.I32_ADD || !dominance.Dominates(loop.stay, sites[v].block) {
				return induction{}, false
			}
			by := add.Args[1]
			if add.Args[0] != loop.index {
				by = add.Args[0]
				if add.Args[1] != loop.index {
					return induction{}, false
				}
			}
			s, ok := constant(by)
			if !ok || s < 1 {
				return induction{}, false
			}
			step = max(step, s)
		}
	}
	if n, ok := constant(loop.limit); ok {
		if n-1+step > math.MaxInt32 {
			return induction{}, false
		}
	} else if step != 1 {
		return induction{}, false
	}

	if frames, ok := inlet(function, sites, h, preheader, invariant); ok && !p.refuted[frames[len(frames)-1].IP] {
		loop.frames = frames
	}
	return loop, true
}
