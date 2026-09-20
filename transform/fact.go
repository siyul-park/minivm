package transform

import (
	"fmt"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// backing identifies where a ref value derives its reference count.
type backing uint8

// facts is the read-only evidence a translation resolves value kinds and
// container shapes against: the module-wide constant, global, and
// declared-type tables the module carries, plus the one fact derived from
// them once per translation. Nothing here changes while a function is
// translated.
type facts struct {
	constants []types.Boxed
	globals   []types.Kind
	objects   Objects
	decl      []types.Type
	// declared reports whether a declared array type may answer elem. It
	// holds only in a call-free function: a function that also calls another
	// one cannot tell a declared array type apart from one a call already
	// narrowed by observing it.
	declared bool
}

// frame is the activation being translated: the function it runs, the
// address it is published at, and the slots it declares.
type frame struct {
	fn    *types.Function
	addr  int
	slots []types.Type
}

// fact is what the forward walk knows about one operand: its kind, where its
// reference count lives, and the compile-time identities that let a
// container, a callee, or a dynamic arity resolve statically. A reference is
// one such identity: knowing which cell an operand names resolves the
// container it accesses or the function it calls. Ownership is explicit in
// the emitted IR, but deciding where a retain belongs still needs the
// borrowed value's source, which backing and offset name.
type fact struct {
	kind    types.Kind
	backing backing
	offset  int

	ref      int
	refKnown bool
	styp     *types.StructType
	atyp     *types.ArrayType
	val      int32
	valKnown bool
}

// operand is one value on the abstract operand stack: the SSA value holding
// it, and what the walk knows about it.
type operand struct {
	value ssa.Value
	fact
}

const (
	backingStack  backing = iota // retain lives on the operand stack copy
	backingConst                 // compile-time constant, never retained
	backingLocal                 // deferred to a VM stack local slot
	backingGlobal                // deferred to a global slot
	backingUpval                 // deferred to a closure upval slot
)

// values names the SSA value each operand holds.
func values(stack []operand) []ssa.Value {
	out := make([]ssa.Value, len(stack))
	for i, o := range stack {
		out[i] = o.value
	}
	return out
}

// deoptOperands states what a deopt is handed: each value, and whether that
// stack entry carries the reference count the interpreter adopts. Only a
// reference backed by the stack copy itself does; every other backing defers
// the count to storage that still holds it, so a cold path retains it before
// handing it over.
func deoptOperands(stack []operand) []ssa.Operand {
	out := make([]ssa.Operand, len(stack))
	for i, o := range stack {
		out[i] = ssa.Operand{Value: o.value, Owned: o.kind == types.KindRef && o.backing == backingStack}
	}
	return out
}

// resolve gives every span the facts its operands carry on entry, as the
// least fixpoint over the edges execution takes. It reports false when the
// walk cannot model an opcode or when two paths reach one span with stacks
// that cannot meet: neither leaves a function this translation can express.
//
// A span the fixpoint never reaches is left without a state rather than
// refused: it is dead code, and build simply does not emit it.
func (f facts) resolve(entry frame, spans []span) ([][]fact, bool) {
	if len(entry.fn.Handlers) > 0 {
		return nil, false
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
			return nil, false
		}
		for _, succ := range spans[id].flow {
			if !seen[succ] {
				seen[succ] = true
				states[succ] = append([]fact(nil), exit...)
				work = append(work, succ)
				continue
			}
			if len(states[succ]) != len(exit) {
				return nil, false
			}
			changed := false
			for i := range exit {
				moved, ok := states[succ][i].merge(exit[i])
				if !ok {
					return nil, false
				}
				changed = changed || moved
			}
			if changed {
				work = append(work, succ)
			}
		}
	}
	return states, true
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
	w := &walk{facts: f, b: b, block: block, fr: fr, stack: stack}
	if _, ok := w.run(s); !ok {
		return nil, false
	}
	out := make([]fact, len(w.stack))
	for i, o := range w.stack {
		out[i] = o.fact
	}
	return out, true
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
		f.backing, f.offset = backingStack, 0
		changed = true
	}
	if f.refKnown && (!src.refKnown || f.ref != src.ref) {
		f.ref, f.refKnown = 0, false
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

// returns is how many results the function this frame runs hands back.
func (fr frame) returns() int {
	if fr.fn.Typ == nil {
		return 0
	}
	return len(fr.fn.Typ.Returns)
}

// build emits every span reachable from the entry, entry first, and returns
// the assembled function, or nil when a span's operands or successors cannot
// be represented.
func (f facts) build(entry frame, spans []span, states [][]fact) *ssa.Function {
	order := reach(spans)
	ids := make([]int, len(spans))
	for i := range ids {
		ids[i] = -1
	}
	b := ssa.New(fmt.Sprintf("%d:%d", entry.addr, spans[0].start))
	for _, id := range order {
		ids[id] = b.Block()
	}

	for _, id := range order {
		// Every block takes its live operands as parameters, so a merge
		// carries its own dataflow and the entry states what a native entry
		// is handed.
		stack := make([]operand, len(states[id]))
		for i, e := range states[id] {
			t, ok := typ(e.kind)
			if !ok {
				return nil
			}
			stack[i] = operand{value: b.Param(ids[id], t), fact: e}
		}
		w := &walk{facts: f, b: b, block: ids[id], fr: entry, stack: stack}
		term, ok := w.run(spans[id])
		if !ok {
			return nil
		}
		if term.Edges, ok = w.edges(spans[id], states, ids); !ok {
			return nil
		}
		b.Term(ids[id], term)
	}
	return b.Build()
}
