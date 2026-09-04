package frontend

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// facts is the read-only static evidence a plan resolves value kinds and
// container shapes against: the function being planned, the module-wide
// constant, global, heap-object, and declared-type tables the snapshot carries,
// and the two facts derived from them once per plan. Nothing here changes while
// a function is planned.
type facts struct {
	fn        *types.Function
	addr      int
	constants []types.Boxed
	globals   []types.Kind
	objects   jit.Objects
	decl      []types.Type
	slots     []types.Type
	// declared reports whether a declared array type may answer elem. It holds
	// only in a call-free function: the general array path combined with a
	// native call corrupted native state, and while that cause is fixed the
	// wider planning it buys measured worse (see docs/jit-internals.md).
	declared bool
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
// fixpoint over the edges execution takes. It reports false when the walk
// cannot model an opcode, when two paths reach one span with stacks that cannot
// meet, or when a span is unreachable from the function entry: none of those
// leave a function this frontend can plan.
func (f facts) resolve(spans []span) ([][]fact, bool) {
	if len(f.fn.Handlers) > 0 {
		return nil, false
	}
	states := make([][]fact, len(spans))
	seen := make([]bool, len(spans))
	seen[0] = true
	work := []int{0}
	for len(work) > 0 {
		id := work[len(work)-1]
		work = work[:len(work)-1]
		exit, ok := f.trace(spans[id], states[id])
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
	for id := range states {
		if !seen[id] {
			return nil, false
		}
	}
	return states, true
}

// trace returns the facts one span leaves behind. It runs the same walk that
// emits the block, into a function thrown away here, so the transfer function
// and the translation can never disagree about an opcode's effect.
func (f facts) trace(s span, entry []fact) ([]fact, bool) {
	b := ssa.New("")
	block := b.Block()
	stack := make([]operand, len(entry))
	for i, e := range entry {
		t, ok := typ(e.kind)
		if !ok {
			return nil, false
		}
		stack[i] = operand{value: b.Param(block, t), fact: e}
	}
	w := &walk{facts: f, b: b, block: block, stack: stack}
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

// elem resolves an array's element kind. A container whose identity is known
// resolves from the cell the snapshot recorded there; otherwise its declared
// array type answers, and only in a call-free function. Both are hints the
// shape guard verifies before any access, so a slot declared as an array that
// currently holds null or a differently shaped array deopts instead of being
// read.
func (f facts) elem(array fact) (types.Kind, bool) {
	if !array.refKnown || array.ref <= 0 {
		if f.declared && array.atyp != nil && array.atyp.ElemKind != instr.KindAny {
			return array.atyp.ElemKind, true
		}
		return 0, false
	}
	shape, ok := jit.ElemShapeByItab(f.objects[array.ref].Array)
	return shape.Kind, ok
}

// field resolves a struct field's kind: the container must carry a struct type
// and the field index must be a known in-bounds constant.
func (f facts) field(container, index fact) (types.Kind, bool) {
	typ := f.record(container)
	if typ == nil || !index.valKnown || index.val < 0 || int(index.val) >= len(typ.Fields) {
		return 0, false
	}
	return typ.Fields[index.val].Kind, true
}

// record resolves the struct type a container carries: the one its declared
// type or a ref.cast states, or the one the snapshot recorded for a constant
// cell.
func (f facts) record(container fact) *types.StructType {
	if container.styp != nil {
		return container.styp
	}
	if container.refKnown && container.ref > 0 {
		return f.objects[container.ref].Typ
	}
	return nil
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
