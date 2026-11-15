package transform_test

import "github.com/siyul-park/minivm/internal/ssa"

func deoptState(b *ssa.Builder, block int) ssa.Value {
	state := b.Value(ssa.TypeState)
	b.Add(block, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
	return state
}
