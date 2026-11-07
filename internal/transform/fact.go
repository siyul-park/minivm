package transform

import (
	"slices"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

type activation struct {
	function *types.Function
	address  int
	slots    []types.Type
	// first is the index of the function's first span.
	first int
	// profile is the feedback recorded for the function.
	profile Profile
}

// facts is what a span's entry knows: its operand stack, and, per
// slot, the function of the closure this unit stored there, 0 when unknown.
type facts struct {
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
	mapType        *types.MapType
	value          int32
	valueKnown     bool
}

type backing uint8

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

func (a activation) returns() int {
	if a.function.Typ == nil {
		return 0
	}
	return len(a.function.Typ.Returns)
}

// merge joins src into f and reports whether f changed; a stack shape
// mismatch cannot join.
func (f *facts) merge(src facts) (bool, bool) {
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

// owning is the state an OSR unit enters at a loop header: every reference
// owned, and no closure it built itself.
func (f facts) owning() facts {
	out := slices.Clone(f.stack)
	for i := range out {
		if out[i].kind == types.KindRef {
			out[i].backing, out[i].offset = backingStack, 0
		}
		out[i].closure = 0
	}
	return facts{stack: out, closures: make([]int, len(f.closures))}
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
	if f.mapType != nil && f.mapType != src.mapType {
		f.mapType = nil
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

func holds(t types.Type) fact {
	if t == nil {
		return fact{}
	}
	f := fact{kind: t.Kind()}
	f.structType, _ = t.(*types.StructType)
	f.arrayType, _ = t.(*types.ArrayType)
	f.mapType, _ = t.(*types.MapType)
	return f
}

func typ(kind types.Kind) (ssa.Type, bool) {
	t := ssa.TypeOf(kind)
	return t, t != 0
}
