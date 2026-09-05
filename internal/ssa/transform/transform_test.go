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
}
