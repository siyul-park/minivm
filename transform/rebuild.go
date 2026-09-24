package transform

import "github.com/siyul-park/minivm/internal/ssa"

type rebuilder struct {
	builder *ssa.Builder
	blocks  map[int]int
	values  map[ssa.Value]ssa.Value
}

func newRebuilder(function *ssa.Function) *rebuilder {
	builder := ssa.New(function.Name())
	builder.Entry(function.Entry())
	return &rebuilder{
		builder: builder,
		blocks:  map[int]int{},
		values:  map[ssa.Value]ssa.Value{},
	}
}

func (r *rebuilder) block(block int) int {
	if id, ok := r.blocks[block]; ok {
		return id
	}
	id := r.builder.Block()
	r.blocks[block] = id
	return id
}

func (r *rebuilder) value(value ssa.Value) ssa.Value {
	if value == ssa.NoValue {
		return ssa.NoValue
	}
	return r.values[value]
}

func (r *rebuilder) list(values []ssa.Value) []ssa.Value {
	if len(values) == 0 {
		return nil
	}
	out := make([]ssa.Value, len(values))
	for i, value := range values {
		out[i] = r.value(value)
	}
	return out
}

func (r *rebuilder) stack(operands []ssa.Operand) []ssa.Operand {
	if len(operands) == 0 {
		return nil
	}
	out := make([]ssa.Operand, len(operands))
	for i, o := range operands {
		out[i] = ssa.Operand{Value: r.value(o.Value), Owned: o.Owned}
	}
	return out
}

func (r *rebuilder) locals(locals []ssa.Local) []ssa.Local {
	if len(locals) == 0 {
		return nil
	}
	out := make([]ssa.Local, len(locals))
	for i, l := range locals {
		out[i] = ssa.Local{Index: l.Index, Value: r.value(l.Value)}
	}
	return out
}

func (r *rebuilder) alias(old, at ssa.Value) {
	r.values[old] = at
}

func (r *rebuilder) operation(operation ssa.Operation) ssa.Operation {
	operation.Args = r.list(operation.Args)
	if len(operation.Frames) > 0 {
		frames := make([]ssa.Frame, len(operation.Frames))
		for i, frame := range operation.Frames {
			frame.Stack, frame.Locals = r.stack(frame.Stack), r.locals(frame.Locals)
			frames[i] = frame
		}
		operation.Frames = frames
	}
	operation.State = r.value(operation.State)
	return operation
}

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

func (r *rebuilder) define(function *ssa.Function, operation ssa.Operation) ssa.Operation {
	if len(operation.Results) == 0 {
		return operation
	}
	results := make([]ssa.Value, len(operation.Results))
	for i, old := range operation.Results {
		nv := r.builder.Value(function.Type(old))
		r.alias(old, nv)
		results[i] = nv
	}
	operation.Results = results
	return operation
}
