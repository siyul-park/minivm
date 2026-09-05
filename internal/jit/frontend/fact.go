package frontend

import (
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// facts is the read-only evidence a plan resolves value kinds and container
// shapes against: the module-wide constant, global, heap-object, and
// declared-type tables the snapshot carries, plus the one fact derived from
// them once per plan. Nothing here changes while a function is planned.
type facts struct {
	constants []types.Boxed
	globals   []types.Kind
	objects   jit.Objects
	decl      []types.Type
	// declared reports whether a declared array type may answer elem. It holds
	// only in a call-free function: the general array path combined with a
	// native call corrupted native state, and while that cause is fixed the
	// wider planning it buys measured worse (see docs/jit-internals.md).
	declared bool
}

// frame is one activation a walk translates in: the function it runs, the
// address that function is published at, the slots it declares, the VM stack
// floor it sits on relative to the entry frame, and the operand-stack index
// its own operands start at. A plan built from bytecode has exactly one; a
// recording pushes one more per callee it inlined.
type frame struct {
	fn     *types.Function
	addr   int
	slots  []types.Type
	base   int
	origin int
	// ip is where this frame resumes once the call it is suspended on
	// returns, meaningful only while an inner frame is on top of it.
	ip int
	// after is the block the caller carries on in once this frame returns,
	// shared with every recording that can return into it. It is nil for the
	// entry frame, which returns out of the function instead.
	after *int
}

// fact is what the forward walk knows about one operand: its kind, where its
// reference count lives, and the compile-time identities that let a container,
// a callee, or a dynamic arity resolve statically. Ownership is explicit in the
// emitted IR, but deciding where a retain belongs still needs the borrowed
// value's source, which backing and offset name.
type fact struct {
	kind    types.Kind
	backing jit.Backing
	offset  int

	ref         int
	refKnown    bool
	callee      int
	calleeKnown bool
	styp        *types.StructType
	atyp        *types.ArrayType
	val         int32
	valKnown    bool
}

// operand is one value on the abstract operand stack: the SSA value holding it,
// and what the walk knows about it.
type operand struct {
	value ssa.Value
	fact
}

// resolve gives every span the facts its operands carry on entry, as the least
// fixpoint over the edges execution takes, and reports which spans that
// fixpoint reached. It reports false when the walk cannot model an opcode or
// when two paths reach one span with stacks that cannot meet: neither leaves a
// function this frontend can plan.
//
// A span the fixpoint never reaches is left without a state rather than
// refused. It is dead code, and build simply does not emit it; whether a
// function is allowed to hold one at all is the caller's rule, not this one's.
func (f facts) resolve(entry frame, spans []span) ([][]fact, []bool, bool) {
	if len(entry.fn.Handlers) > 0 {
		return nil, nil, false
	}
	states := make([][]fact, len(spans))
	seen := make([]bool, len(spans))
	seen[0] = true
	work := []int{0}
	for len(work) > 0 {
		id := work[len(work)-1]
		work = work[:len(work)-1]
		exit, ok := f.transfer(entry, spans[id], states[id])
		if !ok {
			return nil, nil, false
		}
		for _, succ := range spans[id].flow {
			if !seen[succ] {
				seen[succ] = true
				states[succ] = append([]fact(nil), exit...)
				work = append(work, succ)
				continue
			}
			if len(states[succ]) != len(exit) {
				return nil, nil, false
			}
			changed := false
			for i := range exit {
				moved, ok := states[succ][i].merge(exit[i])
				if !ok {
					return nil, nil, false
				}
				changed = changed || moved
			}
			if changed {
				work = append(work, succ)
			}
		}
	}
	return states, seen, true
}

// transfer returns the facts one span leaves behind. It runs the same walk
// that emits the block, into a function thrown away here, so the transfer
// function and the translation can never disagree about an opcode's effect.
func (f facts) transfer(fr frame, s span, in []fact) ([]fact, bool) {
	b := ssa.New("")
	block := b.Block()
	stack := make([]operand, len(in))
	for i, e := range in {
		t, ok := typ(e.kind)
		if !ok {
			return nil, false
		}
		stack[i] = operand{value: b.Param(block, t), fact: e}
	}
	w := &walk{facts: f, b: b, block: block, frames: []frame{fr}, stack: stack}
	if _, ok := w.run(s); !ok {
		return nil, false
	}
	out := make([]fact, len(w.stack))
	for i, o := range w.stack {
		out[i] = o.fact
	}
	return out, true
}

// widen drops the compile-time identities one path observed, leaving what every
// path reaching a block agrees on: the value's kind and where its reference
// count lives. A block more than one recording enters reads its parameters
// through their facts, so it may state only what each of them carries.
func (f fact) widen() fact {
	return fact{kind: f.kind, backing: f.backing, offset: f.offset}
}

// merge narrows a fact to what it and src agree on, reporting whether it
// changed and whether the two can meet at all. Only disagreeing kinds cannot:
// every other identity is a hint, and losing one costs speculation, not
// soundness.
func (f *fact) merge(src fact) (bool, bool) {
	if f.kind != src.kind {
		return false, false
	}
	changed := false
	if f.backing != src.backing || f.offset != src.offset {
		f.backing, f.offset = jit.BackingStack, 0
		changed = true
	}
	if f.refKnown && (!src.refKnown || f.ref != src.ref) {
		f.ref, f.refKnown = 0, false
		changed = true
	}
	if f.calleeKnown && (!src.calleeKnown || f.callee != src.callee) {
		f.callee, f.calleeKnown = 0, false
		changed = true
	}
	if f.styp != nil && f.styp != src.styp {
		f.styp = nil
		changed = true
	}
	if f.atyp != nil && f.atyp != src.atyp {
		f.atyp = nil
		changed = true
	}
	if f.valKnown && (!src.valKnown || f.val != src.val) {
		f.val, f.valKnown = 0, false
		changed = true
	}
	return changed, true
}

// holds reads what a slot's declared type states about the value in it.
func holds(t types.Type) fact {
	if t == nil {
		return fact{}
	}
	f := fact{kind: t.Kind()}
	f.styp, _ = t.(*types.StructType)
	f.atyp, _ = t.(*types.ArrayType)
	return f
}

// typ is the SSA type a kind is held in, and false for a kind the IR has no
// type for.
func typ(kind types.Kind) (ssa.Type, bool) {
	t := ssa.TypeOf(kind)
	return t, t != 0
}
