package transform

import "github.com/siyul-park/minivm/internal/ssa"

// rebuilder assembles a replacement function over an existing one's reachable
// blocks, renumbering every surviving value as it goes and translating every
// value an operation, its state, or its frames read through whatever a pass
// has already decided that value now stands for. A pass that only renumbers
// aliases every old value to its own new one; a pass that also elides an
// operation aliases its old result to whichever surviving value now stands in
// for it instead. Every pass in this package shares this one
// construction: cse.go and guard.go through dedup, dce.go, fold.go, and
// forward.go alike.
type rebuilder struct {
	b      *ssa.Builder
	blocks map[int]int
	values map[ssa.Value]ssa.Value
}

func newRebuilder(fn *ssa.Function) *rebuilder {
	return &rebuilder{
		b:      ssa.New(fn.Name()),
		blocks: map[int]int{},
		values: map[ssa.Value]ssa.Value{},
	}
}

// block returns block's replacement, allocating it on first reference - from
// the pass's own traversal or from an edge reaching it before that traversal
// gets there.
func (r *rebuilder) block(block int) int {
	if id, ok := r.blocks[block]; ok {
		return id
	}
	id := r.b.Block()
	r.blocks[block] = id
	return id
}

// value returns v's current replacement, or NoValue for NoValue.
func (r *rebuilder) value(v ssa.Value) ssa.Value {
	if v == ssa.NoValue {
		return ssa.NoValue
	}
	return r.values[v]
}

func (r *rebuilder) list(vs []ssa.Value) []ssa.Value {
	if len(vs) == 0 {
		return nil
	}
	out := make([]ssa.Value, len(vs))
	for i, v := range vs {
		out[i] = r.value(v)
	}
	return out
}

// stack translates the operands one frame resumes with, each keeping the
// ownership its entry carries: renumbering a value never moves a retain.
func (r *rebuilder) stack(os []ssa.Operand) []ssa.Operand {
	if len(os) == 0 {
		return nil
	}
	out := make([]ssa.Operand, len(os))
	for i, o := range os {
		out[i] = ssa.Operand{Value: r.value(o.Value), Owned: o.Owned}
	}
	return out
}

// alias records that old now reads back as at: at is at's own renumbering
// when old survives, or the survivor standing in for old when a pass elides
// old's defining operation.
func (r *rebuilder) alias(old, at ssa.Value) {
	r.values[old] = at
}

// operation returns op with every value it reads - its arguments, the frames
// an OpState carries, and the state it resumes into - translated through the
// rebuilder's current substitution. Its results are left as op's own old
// values; a caller that keeps op still has to allocate and alias their
// replacements itself.
func (r *rebuilder) operation(op ssa.Operation) ssa.Operation {
	op.Args = r.list(op.Args)
	if len(op.Frames) > 0 {
		frames := make([]ssa.Frame, len(op.Frames))
		for i, fr := range op.Frames {
			fr.Stack = r.stack(fr.Stack)
			frames[i] = fr
		}
		op.Frames = frames
	}
	op.State = r.value(op.State)
	return op
}

// terminator returns t with every value and edge it names translated the
// same way operation does, allocating the replacement for an edge's target
// block if nothing has referenced it yet.
func (r *rebuilder) terminator(t ssa.Terminator) ssa.Terminator {
	t.Args = r.list(t.Args)
	if len(t.Edges) > 0 {
		edges := make([]ssa.Edge, len(t.Edges))
		for i, e := range t.Edges {
			e.Block = r.block(e.Block)
			e.Args = r.list(e.Args)
			edges[i] = e
		}
		t.Edges = edges
	}
	t.State = r.value(t.State)
	return t
}

// define allocates op's replacement results in fn's own type vocabulary,
// aliasing each old result to its new one, and returns op with those results
// installed - ready to add to the rebuilder's builder.
func (r *rebuilder) define(fn *ssa.Function, op ssa.Operation) ssa.Operation {
	if len(op.Results) == 0 {
		return op
	}
	results := make([]ssa.Value, len(op.Results))
	for i, old := range op.Results {
		nv := r.b.Value(fn.Type(old))
		r.alias(old, nv)
		results[i] = nv
	}
	op.Results = results
	return op
}
