package transform

import (
	"math"

	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// BCEPass drops the bounds check of an array op on a slice indexed by a
// loop's induction variable i, which counts up from a constant c ≥ 0 by
// constant steps and stays below the limit n the loop header tests, signed:
// then 0 ≤ i < n holds in the loop. The index becomes an OpArrayBound. n ≤ length
// is checked once by an OpGuardBounds in the loop's preheader that deopts at
// the header's entry state, even when n is the array's own length: a
// shrinking op between the two would leave n above it. A loop whose header
// entry offset is refuted keeps its checks.
type BCEPass struct {
	profiles Profiles
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

var _ pass.Pass[*ssa.Function] = (*BCEPass)(nil)

// NewBCEPass returns the pass; profiles holds, per function, the offsets
// whose guard native code has seen fail (Profile.Refuted).
func NewBCEPass(profiles Profiles) *BCEPass {
	return &BCEPass{profiles: profiles}
}

// Run applies the pass to one SSA function.
func (p *BCEPass) Run(_ *pass.Manager, function *ssa.Function) (bool, error) {
	bounded, guards := newNest(function).bounds(p.profiles)
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
				r.builder.Add(id, ssa.Operation{Op: ssa.OpArrayBound, Args: []ssa.Value{operation.Args[1], operation.Args[0]}, Results: []ssa.Value{v}})
				operation.Args[1] = v
			}
			r.builder.Add(id, r.define(operation))
		}
		// Each guard reads the array's length under the loop's entry state,
		// then compares it against the limit.
		for _, guard := range guards[b] {
			r.builder.At(guard.Source)
			state, length := r.builder.Value(ssa.TypeState), r.builder.Value(ssa.TypeI32)
			r.builder.Add(id, r.operation(ssa.Operation{Op: ssa.OpState, Frames: guard.Frames, Results: []ssa.Value{state}}))
			r.builder.Add(id, ssa.Operation{Op: ssa.OpArrayLen, Args: []ssa.Value{r.value(guard.Args[1])}, State: state, Results: []ssa.Value{length}})
			r.builder.Add(id, ssa.Operation{Op: ssa.OpGuardBounds, Args: []ssa.Value{r.value(guard.Args[0]), length}, State: state})
		}
		r.builder.Term(id, r.terminator(function.Block(b).Terminator))
	}
	*function = *r.builder.Build()
	return false, nil
}

// bounds reports the array ops whose index becomes an OpArrayBound and, per
// preheader, the guards that make it sound.
func (n *nest) bounds(profiles Profiles) (map[site]bool, map[int][]ssa.Operation) {
	bounded := map[site]bool{}
	guards := map[int][]ssa.Operation{}
	for _, natural := range n.loops {
		body := natural.Body
		loop, ok := n.induction(natural, profiles)
		if !ok {
			continue
		}
		guarded := map[ssa.Value]bool{}
		for b := range body {
			if !n.dominance.Dominates(loop.stay, b) {
				continue
			}
			for i, operation := range n.function.Block(b).Operations {
				if !n.indexed(loop, operation) {
					continue
				}
				if !guarded[operation.Args[0]] {
					guarded[operation.Args[0]] = true
					guards[loop.preheader] = append(guards[loop.preheader], ssa.Operation{Args: []ssa.Value{loop.limit, operation.Args[0]}, Frames: loop.frames, Source: operation.Source})
				}
				bounded[site{b, i}] = true
			}
		}
	}
	return bounded, guards
}

// indexed reports that operation reads or writes, at the loop's induction
// variable, a slice defined before the loop's preheader.
func (n *nest) indexed(loop induction, operation ssa.Operation) bool {
	if (operation.Op != ssa.OpArrayGet && operation.Op != ssa.OpArraySet) || operation.Args[1] != loop.index {
		return false
	}
	slice, ok := n.op(operation.Args[0])
	return ok && slice.Op == ssa.OpArrayView && loop.frames != nil && n.dominance.Dominates(n.sites[operation.Args[0]].block, loop.preheader)
}

// induction reports the loop at header h when its branch tests i <s n on a
// header param i that enters at a constant ≥ 0 and steps up by constants
// along every latch, n is loop-invariant, and no step wraps i past n.
// frames is the header's entry state, nil when it has none usable or its
// offset is refuted: the loop then keeps its checks.
func (n *nest) induction(natural graph.Loop, profiles Profiles) (induction, bool) {
	body, h, preheader := natural.Body, natural.Header, natural.Preheader
	if preheader < 0 {
		return induction{}, false
	}
	t := n.function.Block(h).Terminator
	if t.Term != ssa.TermBrIf || len(t.Edges) != 2 {
		return induction{}, false
	}
	test, ok := n.op(t.Args[0])
	if !ok || n.sites[t.Args[0]].block != h || test.Op != ssa.OpIntCmp {
		return induction{}, false
	}
	loop := induction{preheader: preheader}
	stay := 0
	switch test.Cond {
	case ssa.CondSGE:
		loop.index, loop.limit, stay = test.Args[0], test.Args[1], 1
	case ssa.CondSLT:
		loop.index, loop.limit = test.Args[0], test.Args[1]
	case ssa.CondSLE:
		loop.limit, loop.index, stay = test.Args[0], test.Args[1], 1
	case ssa.CondSGT:
		loop.limit, loop.index = test.Args[0], test.Args[1]
	default:
		return induction{}, false
	}
	loop.stay = t.Edges[stay].Block
	if n.params[loop.index] != h || n.function.Type(loop.index) != ssa.TypeI32 || !body[loop.stay] || len(n.function.Pred(loop.stay)) != 1 {
		return induction{}, false
	}
	invariant := func(v ssa.Value) bool {
		if b, ok := n.params[v]; ok {
			return !body[b]
		}
		s, ok := n.sites[v]
		return ok && !body[s.block]
	}
	if !invariant(loop.limit) {
		return induction{}, false
	}

	k := 0
	for k < len(n.function.Block(h).Params) && n.function.Block(h).Params[k] != loop.index {
		k++
	}
	constant := func(v ssa.Value) (int64, bool) {
		op, ok := n.op(v)
		return int64(int32(op.Const)), ok && op.Op == ssa.OpConst
	}
	step := int64(0)
	for _, pred := range n.function.Pred(h) {
		for _, e := range n.function.Block(pred).Terminator.Edges {
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
			add, ok := n.op(v)
			if !ok || add.Op != ssa.OpIntAdd || !n.dominance.Dominates(loop.stay, n.sites[v].block) {
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
	if c, ok := constant(loop.limit); ok {
		if c-1+step > math.MaxInt32 {
			return induction{}, false
		}
	} else if step != 1 {
		return induction{}, false
	}

	if frames, ok := inlet(n.function, n.sites, h, preheader, invariant); ok {
		if at := frames[len(frames)-1]; !profiles.Of(at.Address).Refuted[at.IP] {
			loop.frames = frames
		}
	}
	return loop, true
}
