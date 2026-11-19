package transform

import (
	"slices"
	"sort"

	"github.com/siyul-park/minivm/instr"
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
// loop-invariant array guard reads it through one OpSlice in the preheader of
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
	l := newLoops(function)
	dominance := l.dominance
	headers := graph.Headers(function, dominance)
	if len(headers) == 0 {
		return true, nil
	}

	bodies := make(map[int]map[int]bool, len(headers))
	preheaders := make(map[int]int, len(headers))
	for _, h := range headers {
		b := graph.Body(function, dominance, h)
		bodies[h] = b
		if p, ok := graph.Preheader(function, b, h); ok {
			preheaders[h] = p
		}
	}
	sort.Slice(headers, func(i, j int) bool {
		return len(bodies[headers[i]]) < len(bodies[headers[j]])
	})

	blocks := graph.Order(function)
	calling := make(map[int]bool, len(headers))
	quiets := make(map[int]bool, len(headers))
	stored := make(map[int]map[ssa.Slot]bool, len(headers))
	for _, h := range headers {
		quiets[h], stored[h] = true, map[ssa.Slot]bool{}
		for b := range bodies[h] {
			for _, operation := range function.Block(b).Operations {
				quiets[h] = quiets[h] && quiet(function, operation)
				calling[h] = calling[h] || operation.Op == ssa.OpExec && operation.Code == instr.CALL
				if operation.Op == ssa.OpStore {
					stored[h][operation.Slot] = true
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
		if b, ok := l.params[v]; ok {
			return b, true
		}
		if s, ok := l.sites[v]; ok {
			return current(s), true
		}
		return 0, false
	}
	outside := func(v ssa.Value, body map[int]bool) bool {
		loc, ok := location(v)
		return ok && !body[loc]
	}

	changed := false
	for _, h := range headers {
		preheader, ok := preheaders[h]
		if !ok {
			continue
		}
		body := bodies[h]
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
					if calling[h] || !invariant(operation.Args[0]) {
						continue
					}
					// Only a guard every completed iteration runs: one under a
					// condition may never run, and hoisted it would deopt at
					// each loop entry and refute the unit.
					if slices.ContainsFunc(function.Pred(h), func(latch int) bool {
						return body[latch] && !dominance.Dominates(current(s), latch)
					}) {
						continue
					}
					entry, ok := inlet(function, l.sites, h, preheader, invariant)
					if !ok {
						continue
					}
					frames[h], entries[s] = entry, h
				case sliceable(function, l.sites, operation):
					if quiets[h] && invariant(operation.Args[0]) {
						sliced[s] = h
						changed = true
					}
					continue
				case operation.Op == ssa.OpLoad:
					if stored[h][operation.Slot] || !quiets[h] && (calling[h] || operation.Slot.Space != ssa.SpaceLocal || operation.Slot.Base != 0) {
						continue
					}
				case hoistable(operation):
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
			if h, ok := sliced[site{b, i}]; ok {
				key := [2]int{int(args[0]), h}
				if _, ok := views[key]; !ok {
					v := r.builder.Value(ssa.TypeRef)
					r.builder.Add(r.block(preheaders[h]), ssa.Operation{Op: ssa.OpSlice, Args: []ssa.Value{operation.Args[0]}, Results: []ssa.Value{v}})
					views[key] = v
				}
				operation.Args[0] = views[key]
			}
			if h, ok := entries[site{b, i}]; ok {
				state, ok := states[h]
				if !ok {
					state = r.builder.Value(ssa.TypeState)
					r.builder.Add(target, r.operation(ssa.Operation{Op: ssa.OpState, Frames: frames[h], Results: []ssa.Value{state}}))
					states[h] = state
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
	case ssa.OpRelease:
		return false
	case ssa.OpStore:
		t := function.Type(operation.Args[0])
		return t != ssa.TypeRef && t != ssa.TypeI64
	case ssa.OpExec:
		switch {
		case instr.TypeOf(operation.Code).Writes == 0:
			return true
		case operation.Code == instr.ARRAY_SET, operation.Code == instr.STRUCT_SET:
			t := function.Type(operation.Args[len(operation.Args)-1])
			return t != ssa.TypeRef && t != ssa.TypeI64
		default:
			return false
		}
	default:
		return true
	}
}

// sliceable reports whether operation is an array op whose container is a
// guard admitting an array: it reads the container only through the array's
// element pointer and length.
func sliceable(function *ssa.Function, sites map[ssa.Value]site, operation ssa.Operation) bool {
	if operation.Op != ssa.OpExec {
		return false
	}
	switch operation.Code {
	case instr.ARRAY_GET, instr.ARRAY_SET, instr.ARRAY_LEN:
	default:
		return false
	}
	s, ok := sites[operation.Args[0]]
	if !ok {
		return false
	}
	guard := function.Block(s.block).Operations[s.index]
	return guard.Op == ssa.OpGuardShape && !guard.Shape.Struct && guard.Shape.Function == 0
}

func hoistable(operation ssa.Operation) bool {
	switch operation.Op {
	case ssa.OpConst:
		return true
	case ssa.OpExec:
		return operation.Code.IsPure() && !operation.Code.Traps() && !overflows(operation.Code)
	default:
		return false
	}
}

// overflows reports opcodes that can exceed the boxed i64 payload.
func overflows(code instr.Opcode) bool {
	switch code {
	case instr.I64_ADD, instr.I64_SUB, instr.I64_MUL, instr.I64_SHL, instr.I64_SHR_U,
		instr.I64_DIV_S, instr.I64_DIV_U:
		return true
	default:
		return false
	}
}

// inlet is the deopt state at loop header's entry, as seen from preheader:
// the state of header's first operation that has one, with header's params
// replaced by preheader's edge arguments. It fails when an effect precedes
// that operation in header or the state names a value invariant rejects.
func inlet(function *ssa.Function, sites map[ssa.Value]site, header, preheader int, invariant func(ssa.Value) bool) ([]ssa.Frame, bool) {
	block := function.Block(header)
	args := map[ssa.Value]ssa.Value{}
	for i, param := range block.Params {
		args[param] = function.Block(preheader).Terminator.Edges[0].Args[i]
	}
	at := ssa.NoValue
	for _, operation := range block.Operations {
		if operation.State != ssa.NoValue {
			at = operation.State
			break
		}
		if operation.Op != ssa.OpConst && operation.Op != ssa.OpLoad && operation.Op != ssa.OpState {
			return nil, false
		}
	}
	s, ok := sites[at]
	if !ok {
		return nil, false
	}
	state := function.Block(s.block).Operations[s.index]
	substitute := func(v ssa.Value) (ssa.Value, bool) {
		if arg, ok := args[v]; ok {
			return arg, true
		}
		return v, invariant(v)
	}
	frames := make([]ssa.Frame, len(state.Frames))
	for i, frame := range state.Frames {
		stack := make([]ssa.Operand, len(frame.Stack))
		for j, operand := range frame.Stack {
			if stack[j].Value, ok = substitute(operand.Value); !ok {
				return nil, false
			}
			stack[j].Owned = operand.Owned
		}
		locals := make([]ssa.Local, len(frame.Locals))
		for j, local := range frame.Locals {
			if locals[j].Value, ok = substitute(local.Value); !ok {
				return nil, false
			}
			locals[j].Index = local.Index
		}
		frame.Stack, frame.Locals = stack, locals
		frames[i] = frame
	}
	return frames, true
}
