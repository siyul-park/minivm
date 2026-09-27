package transform

import (
	"fmt"
	"slices"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

type backing uint8

type facts struct {
	constants []types.Boxed
	globals   []types.Kind
	objects   Objects
	types     []types.Type
	// callees is a dynamic CALL's ip to its recorded single callee (see
	// Module.Callees).
	callees map[int]Callee
}

type activation struct {
	function *types.Function
	address  int
	slots    []types.Type
}

// frame is the facts live at a span's entry: its operand stack, and, per
// slot, the function of the closure this unit stored there, 0 when unknown.
type frame struct {
	stack    []fact
	closures []int
}

type fact struct {
	kind    types.Kind
	backing backing
	offset  int
	// closure is the function of the closure this unit built with
	// CLOSURE_NEW that the value holds, 0 when unknown.
	closure int

	reference      int
	referenceKnown bool
	structType     *types.StructType
	arrayType      *types.ArrayType
	value          int32
	valueKnown     bool
}

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

func deopt(stack []operand) []ssa.Operand {
	out := make([]ssa.Operand, len(stack))
	for i, o := range stack {
		out[i] = ssa.Operand{Value: o.value, Owned: o.kind == types.KindRef && o.backing == backingStack}
	}
	return out
}

// analyze returns the facts live at each span's entry and which spans are
// reached from root. A reached span's stack is nil, not just absent, when
// nothing is live there (e.g. an empty operand stack at a loop header): seen
// is what tells reached apart from never visited.
func (f facts) analyze(entry activation, spans []span, root int, in frame) ([]frame, []bool, bool) {
	if len(entry.function.Handlers) > 0 {
		return nil, nil, false
	}
	states := make([]frame, len(spans))
	seen := make([]bool, len(spans))
	seen[root] = true
	states[root] = in
	work := []int{root}
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
				states[succ] = frame{stack: slices.Clone(exit.stack), closures: slices.Clone(exit.closures)}
				work = append(work, succ)
				continue
			}
			changed, ok := states[succ].merge(exit)
			if !ok {
				return nil, nil, false
			}
			if changed {
				work = append(work, succ)
			}
		}
	}
	return states, seen, true
}

func (f facts) transfer(activation activation, s span, in frame) (frame, bool) {
	b := ssa.New("")
	block := b.Block()
	stack := make([]operand, len(in.stack))
	for i, e := range in.stack {
		t, ok := typ(e.kind)
		if !ok {
			return frame{}, false
		}
		stack[i] = operand{value: b.Param(block, t), fact: e}
	}
	w := &walker{facts: f, builder: b, block: block, activation: activation, stack: stack, closures: slices.Clone(in.closures)}
	if _, ok := w.translate(s); !ok {
		return frame{}, false
	}
	out := make([]fact, len(w.stack))
	for i, o := range w.stack {
		out[i] = o.fact
	}
	return frame{stack: out, closures: w.closures}, true
}

// merge joins src into f and reports whether f changed; a stack shape
// mismatch cannot join.
func (f *frame) merge(src frame) (bool, bool) {
	if len(f.stack) != len(src.stack) {
		return false, false
	}
	changed := false
	for i := range src.stack {
		moved, ok := f.stack[i].merge(src.stack[i])
		if !ok {
			return false, false
		}
		changed = changed || moved
	}
	for i, c := range src.closures {
		if f.closures[i] != c && f.closures[i] != 0 {
			f.closures[i] = 0
			changed = true
		}
	}
	return changed, true
}

func (f *fact) merge(src fact) (bool, bool) {
	if f.kind != src.kind {
		return false, false
	}
	changed := false
	if f.backing != src.backing || f.offset != src.offset {
		f.backing, f.offset = backingStack, 0
		changed = true
	}
	if f.referenceKnown && (!src.referenceKnown || f.reference != src.reference) {
		f.reference, f.referenceKnown = 0, false
		changed = true
	}
	if f.structType != nil && f.structType != src.structType {
		f.structType = nil
		changed = true
	}
	if f.arrayType != nil && f.arrayType != src.arrayType {
		f.arrayType = nil
		changed = true
	}
	if f.valueKnown && (!src.valueKnown || f.value != src.value) {
		f.value, f.valueKnown = 0, false
		changed = true
	}
	if f.closure != src.closure && f.closure != 0 {
		f.closure = 0
		changed = true
	}
	return changed, true
}

// owned is the state an OSR unit enters at a loop header: every reference
// owned, and no closure it built itself.
func owned(in frame) frame {
	out := append([]fact(nil), in.stack...)
	for i := range out {
		if out[i].kind == types.KindRef {
			out[i].backing, out[i].offset = backingStack, 0
		}
		out[i].closure = 0
	}
	return frame{stack: out, closures: make([]int, len(in.closures))}
}

func holds(t types.Type) fact {
	if t == nil {
		return fact{}
	}
	f := fact{kind: t.Kind()}
	f.structType, _ = t.(*types.StructType)
	f.arrayType, _ = t.(*types.ArrayType)
	return f
}

func typ(kind types.Kind) (ssa.Type, bool) {
	t := ssa.TypeOf(kind)
	return t, t != 0
}

func (activation activation) returns() int {
	if activation.function.Typ == nil {
		return 0
	}
	return len(activation.function.Typ.Returns)
}

func (f facts) build(entry activation, spans []span, states []frame, root int) *ssa.Function {
	seen := make([]bool, len(spans))
	seen[root] = true
	order := []int{root}
	for n := 0; n < len(order); n++ {
		for _, successor := range spans[order[n]].succs {
			if !seen[successor] {
				seen[successor] = true
				order = append(order, successor)
			}
		}
	}
	ids := make([]int, len(spans))
	for i := range ids {
		ids[i] = -1
	}
	b := ssa.New(fmt.Sprintf("%d:%d", entry.address, spans[root].start))
	b.Entry(ssa.Frame{Address: entry.address, IP: spans[root].start, Returns: entry.returns()})
	for _, id := range order {
		ids[id] = b.Block()
	}

	for _, id := range order {
		stack := make([]operand, len(states[id].stack))
		for i, e := range states[id].stack {
			t, ok := typ(e.kind)
			if !ok {
				return nil
			}
			stack[i] = operand{value: b.Param(ids[id], t), fact: e}
		}
		w := &walker{facts: f, builder: b, block: ids[id], activation: entry, stack: stack, closures: slices.Clone(states[id].closures)}
		term, ok := w.translate(spans[id])
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
