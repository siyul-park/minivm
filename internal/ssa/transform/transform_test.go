package transform_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/ssa/transform"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// TestPassOrder composes this package's four passes into a caller-owned
// pipeline, exactly as internal/jit or a future bytecode-to-SSA route would,
// and asserts the ordering fact this package itself no longer enforces:
// CSEPass must run before GuardPass, because a guard's operand is only
// recognizably equal to an earlier guard's once CSEPass has unified the
// values they read, and DCEPass must run last to sweep up what folding,
// deduplicating, and guard elimination leave behind.
func TestPassOrder(t *testing.T) {
	t.Run("folds, deduplicates, eliminates a redundant guard, and sweeps the dead code left behind", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		array := b.Param(entry, ssa.TypeRef)

		x, y := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(2), Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(3), Results: []ssa.Value{y}})
		sum1 := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{sum1}})
		sum2 := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{sum2}})

		unused := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(99), Results: []ssa.Value{unused}})

		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		first := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Itab: 4}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{first}})
		second := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Itab: 4}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{second}})

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
		require.Same(t, fn, out, "the pipeline mutates the function in place and returns it")
		require.NoError(t, ssa.Verify(fn))

		got := ssa.Format(fn)
		require.NotContains(t, got, "i32.add", "both additions folded to the same constant")
		require.NotContains(t, got, "const 99", "the unused constant was swept once folding and CSE left it with no use")
		require.Equal(t, 1, strings.Count(got, "guard.shape"), "the second, redundant guard was eliminated")

		// sum1, sum2, and the survivor of the two additions all collapse to the
		// same folded-and-deduplicated constant.
		require.Equal(t, 1, strings.Count(got, "const 5"))
	})

	t.Run("hoists a loop-invariant computation once CSE has unified a redundant guard, and DCE sweeps the rest", func(t *testing.T) {
		// HoistPass carries no hard ordering requirement against the other
		// three: it never unifies a value (only CSEPass and GuardPass's
		// shared dedup do that) and never moves a guard or anything else
		// that carries deopt state (only GuardPass's target), so it is sound
		// wherever it runs in the sequence. It still reads best placed after
		// FoldPass and CSEPass - so it moves one canonical instance rather
		// than a would-be duplicate - and before DCEPass, matching this
		// package's existing convention that liveness-based sweeping runs
		// last over whatever placement every earlier pass settled on.
		b := ssa.New("f")
		pre, header, body, exit := b.Block(), b.Block(), b.Block(), b.Block()
		array := b.Param(pre, ssa.TypeRef)
		// x and y are parameters, not constants: FoldPass cannot reduce
		// their sum to a literal, so it stays an i32.add for HoistPass to
		// actually move rather than something FoldPass already erased.
		x, y := b.Param(pre, ssa.TypeI32), b.Param(pre, ssa.TypeI32)

		bound := b.Value(ssa.TypeI32)
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(10), Results: []ssa.Value{bound}})
		zero := b.Value(ssa.TypeI32)
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(0), Results: []ssa.Value{zero}})
		b.Term(pre, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{zero}}}})

		counter := b.Param(header, ssa.TypeI32)
		cond := b.Value(ssa.TypeI1)
		b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_LT_S, Args: []ssa.Value{counter, bound}, Results: []ssa.Value{cond}})
		b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: body}, {Block: exit}}})

		// sum is loop-invariant (x and y both come from pre) and eligible to
		// hoist. state, guardA, and guardB are a redundant pair of guards
		// CSEPass and GuardPass collapse to one before this pass ever looks
		// at the loop, and remain a guard regardless - HoistPass never moves
		// them, carrying deopt state as they do. next reads sum, keeping it
		// live (and loop-variant itself, since it also reads counter), so
		// the hoisted addition survives DCEPass rather than being swept as
		// dead code regardless of where it sits.
		sum := b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{sum}})
		state := b.Value(ssa.TypeState)
		b.Add(body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		guardA := b.Value(ssa.TypeRef)
		b.Add(body, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Itab: 4}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{guardA}})
		guardB := b.Value(ssa.TypeRef)
		b.Add(body, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Itab: 4}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{guardB}})
		length := b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_LEN, Args: []ssa.Value{guardB}, Results: []ssa.Value{length}})
		next := b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{counter, sum}, Results: []ssa.Value{next}})
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
		require.Equal(t, 1, strings.Count(got, "guard.shape"), "GuardPass still collapses the redundant guard with HoistPass in the pipeline")
		require.Equal(t, 2, strings.Count(got, "i32.add"), "x+y hoisted out of the loop; counter+sum, which reads it, is the other survivor")
		require.Equal(t, 1, strings.Count(blockChunk(got, 0), "i32.add"), "x+y specifically now lives in the preheader")
	})
}
