package transform

import (
	"slices"

	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// LICMPass moves safe loop-invariant operations to preheaders. A shape guard
// and a load of a running-frame local the loop never stores hoist from any
// loop that makes no call: representation is immutable and nothing but a call
// writes a local or runs host code. A call is excluded because a hoisted value
// live across it spills for its whole life, and its reload costs more than the
// load and guard it replaced. An array op of a quiet loop whose container is a
// loop-invariant array guard reads it through one OpArrayView in the preheader of
// the outermost such loop: nothing in a quiet loop resizes or replaces a
// container.
type LICMPass struct{}

var _ pass.Pass[*ssa.Function] = (*LICMPass)(nil)

// NewLICMPass returns the pass.
func NewLICMPass() *LICMPass {
	return &LICMPass{}
}

// Run applies the pass to one SSA function.
func (p *LICMPass) Run(_ *pass.Manager, function *ssa.Function) (bool, error) {
	nest := newNest(function)
	dominance := nest.dominance
	loops := nest.loops
	if len(loops) == 0 {
		return true, nil
	}

	blocks := graph.Order(function)
	calling := make([]bool, len(loops))
	quiets := make([]bool, len(loops))
	stored := make([]map[ssa.Slot]bool, len(loops))
	for k, loop := range loops {
		quiets[k], stored[k] = true, map[ssa.Slot]bool{}
		for b := range loop.Body {
			for _, operation := range function.Block(b).Operations {
				quiets[k] = quiets[k] && quiet(function, operation)
				calling[k] = calling[k] || operation.Op == ssa.OpCall
				if operation.Op == ssa.OpStore {
					stored[k][operation.Slot] = true
				}
			}
		}
	}

	dest := map[site]int{}
	entries := map[site]int{}
	sliced := map[site]int{}
	frames := map[int][]ssa.Frame{}
	current := func(s site) int {
		if b, ok := dest[s]; ok {
			return b
		}
		return s.block
	}
	location := func(v ssa.Value) (int, bool) {
		if b, ok := nest.params[v]; ok {
			return b, true
		}
		if s, ok := nest.sites[v]; ok {
			return current(s), true
		}
		return 0, false
	}
	outside := func(v ssa.Value, body map[int]bool) bool {
		loc, ok := location(v)
		return ok && !body[loc]
	}

	changed := false
	for k, loop := range loops {
		preheader, body := loop.Preheader, loop.Body
		if preheader < 0 {
			continue
		}
		invariant := func(v ssa.Value) bool { return outside(v, body) }
		for _, b := range blocks {
			if !body[b] {
				continue
			}
			ops := function.Block(b).Operations
			for i, operation := range ops {
				s := site{b, i}
				if !body[current(s)] {
					continue
				}
				switch {
				case operation.Op == ssa.OpGuardShape:
					if calling[k] || !invariant(operation.Args[0]) {
						continue
					}
					// Only a guard every completed iteration runs: one under a
					// condition may never run, and hoisted it would deopt at
					// each loop entry and refute the unit.
					if slices.ContainsFunc(function.Pred(loop.Header), func(latch int) bool {
						return body[latch] && !dominance.Dominates(current(s), latch)
					}) {
						continue
					}
					entry, ok := inlet(function, nest.sites, loop.Header, preheader, invariant)
					if !ok {
						continue
					}
					frames[k], entries[s] = entry, k
				case sliceable(function, nest.sites, operation):
					if quiets[k] && invariant(operation.Args[0]) {
						sliced[s] = k
						changed = true
					}
					continue
				case operation.Op == ssa.OpLoad:
					if stored[k][operation.Slot] || !quiets[k] && (calling[k] || operation.Slot.Space != ssa.SpaceLocal || operation.Slot.Base != 0) {
						continue
					}
				case hoistable(function, operation):
					if slices.ContainsFunc(operation.Args, func(v ssa.Value) bool { return !invariant(v) }) {
						continue
					}
					if operation.State != ssa.NoValue && !invariant(operation.State) {
						continue
					}
				default:
					continue
				}
				dest[s] = preheader
				changed = true
			}
		}
	}

	if !changed {
		return true, nil
	}

	r := newRebuilder(function)
	states := map[int]ssa.Value{}
	views := map[[2]int]ssa.Value{}
	for _, b := range blocks {
		id := r.open(b)
		for i, operation := range function.Block(b).Operations {
			target := r.block(current(site{b, i}))
			args := operation.Args
			operation = r.operation(operation)
			if k, ok := sliced[site{b, i}]; ok {
				key := [2]int{int(args[0]), k}
				if _, ok := views[key]; !ok {
					v := r.builder.Value(ssa.TypeRef)
					r.builder.Add(r.block(loops[k].Preheader), ssa.Operation{Op: ssa.OpArrayView, Args: []ssa.Value{operation.Args[0]}, Results: []ssa.Value{v}})
					views[key] = v
				}
				operation.Args[0] = views[key]
			}
			if k, ok := entries[site{b, i}]; ok {
				state, ok := states[k]
				if !ok {
					state = r.builder.Value(ssa.TypeState)
					r.builder.Add(target, r.operation(ssa.Operation{Op: ssa.OpState, Frames: frames[k], Results: []ssa.Value{state}}))
					states[k] = state
				}
				operation.State = state
			}
			r.builder.Add(target, r.define(operation))
		}
		r.builder.Term(id, r.terminator(function.Block(b).Terminator))
	}
	*function = *r.builder.Build()
	return false, nil
}

// quiet reports whether operation runs no code outside the unit and
// allocates, releases, resizes, and replaces nothing: no call, allocation
// (a boxed i64 included), or reference drop. A container's representation
// never changes (interp.Interpreter.Store refuses it), but host code,
// reachable only through those, may resize or replace the storage a hoisted
// slice names, so a loop of quiet operations keeps it; a quiet loop also
// writes storage only through its own OpStores.
func quiet(function *ssa.Function, operation ssa.Operation) bool {
	switch operation.Op {
	case ssa.OpRefRelease:
		return false
	case ssa.OpStore:
		t := function.Type(operation.Args[0])
		return t != ssa.TypeRef && t != ssa.TypeI64
	default:
		switch {
		case operation.Writes() == 0:
			return true
		case operation.Op == ssa.OpArraySet, operation.Op == ssa.OpStructSet:
			t := function.Type(operation.Args[len(operation.Args)-1])
			return t != ssa.TypeRef && t != ssa.TypeI64
		default:
			return false
		}
	}
}

// sliceable reports whether operation is an array op whose container is a
// guard admitting an array: it reads the container only through the array's
// element pointer and length.
func sliceable(function *ssa.Function, sites map[ssa.Value]site, operation ssa.Operation) bool {
	switch operation.Op {
	case ssa.OpArrayGet, ssa.OpArraySet, ssa.OpArrayLen:
	default:
		return false
	}
	s, ok := sites[operation.Args[0]]
	if !ok {
		return false
	}
	guard := function.Block(s.block).Operations[s.index]
	return guard.Op == ssa.OpGuardShape && guard.Shape.Form == ssa.FormArray
}

func hoistable(function *ssa.Function, operation ssa.Operation) bool {
	switch {
	case operation.Op == ssa.OpConst:
		return true
	case operation.Op == ssa.OpExec || operation.Op.Data():
		return operation.Pure() && !operation.Traps() && !overflows(function, operation)
	default:
		return false
	}
}

// overflows reports i64 operations whose result can exceed the boxed i64 payload.
func overflows(function *ssa.Function, operation ssa.Operation) bool {
	switch operation.Op {
	case ssa.OpIntAdd, ssa.OpIntSub, ssa.OpIntMul, ssa.OpIntShl, ssa.OpIntLShr, ssa.OpIntSDiv, ssa.OpIntUDiv:
		return function.Type(operation.Results[0]) == ssa.TypeI64
	default:
		return false
	}
}
