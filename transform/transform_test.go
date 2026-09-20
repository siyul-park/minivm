package transform_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
)

func TestPassOrder(t *testing.T) {
	t.Run("folds, deduplicates, eliminates a redundant guard, and sweeps the dead code left behind", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.AddBlock()
		array := b.Param(entry, ssa.TypeRef)

		x, y := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(2), Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(3), Results: []ssa.Value{y}})
		sum1 := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{sum1}})
		sum2 := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{sum2}})

		unused := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(99), Results: []ssa.Value{unused}})

		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		first := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Tag: 4}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{first}})
		second := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Tag: 4}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{second}})

		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{sum1, sum2, second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewFoldPass())
		pipeline.Add(transform.NewCSEPass())
		pipeline.Add(transform.NewGuardPass())
		pipeline.Add(transform.NewDCEPass())
		out, err := pipeline.Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Same(t, fn, out)
		require.NoError(t, ssa.Verify(fn))

		got := ssa.Format(fn)
		require.NotContains(t, got, "i32.add")
		require.NotContains(t, got, "const 99")
		require.Equal(t, 1, strings.Count(got, "guard.shape"))

		require.Equal(t, 1, strings.Count(got, "const 5"))
	})

	t.Run("hoists a loop-invariant computation once CSE has unified a redundant guard, and DCE sweeps the rest", func(t *testing.T) {
		b := ssa.New("f")
		pre, header, body, exit := b.AddBlock(), b.AddBlock(), b.AddBlock(), b.AddBlock()
		array := b.Param(pre, ssa.TypeRef)
		x, y := b.Param(pre, ssa.TypeI32), b.Param(pre, ssa.TypeI32)

		bound := b.Value(ssa.TypeI32)
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(10), Results: []ssa.Value{bound}})
		zero := b.Value(ssa.TypeI32)
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(0), Results: []ssa.Value{zero}})
		b.Term(pre, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{zero}}}})

		counter := b.Param(header, ssa.TypeI32)
		cond := b.Value(ssa.TypeI1)
		b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_LT_S, Args: []ssa.Value{counter, bound}, State: deoptState(b, pre), Results: []ssa.Value{cond}})
		b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: body}, {Block: exit}}})

		sum := b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, State: deoptState(b, pre), Results: []ssa.Value{sum}})
		state := b.Value(ssa.TypeState)
		b.Add(body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		guardA := b.Value(ssa.TypeRef)
		b.Add(body, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Tag: 4}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{guardA}})
		guardB := b.Value(ssa.TypeRef)
		b.Add(body, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Tag: 4}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{guardB}})
		length := b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_LEN, Args: []ssa.Value{guardB}, State: deoptState(b, pre), Results: []ssa.Value{length}})
		next := b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{counter, sum}, State: deoptState(b, pre), Results: []ssa.Value{next}})
		b.Term(body, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{next}}}})

		b.Term(exit, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{counter}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewFoldPass())
		pipeline.Add(transform.NewCSEPass())
		pipeline.Add(transform.NewGuardPass())
		pipeline.Add(transform.NewHoistPass())
		pipeline.Add(transform.NewDCEPass())
		out, err := pipeline.Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Same(t, fn, out)
		require.NoError(t, ssa.Verify(fn))

		got := ssa.Format(fn)
		require.Equal(t, 1, strings.Count(got, "guard.shape"))
		require.Equal(t, 2, strings.Count(got, "i32.add"))
		require.Equal(t, 1, strings.Count(blockChunk(got, 0), "i32.add"))
	})
}

func deoptState(b *ssa.Builder, block int) ssa.Value {
	state := b.Value(ssa.TypeState)
	b.Add(block, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
	return state
}
