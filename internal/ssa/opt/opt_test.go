package opt_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/ssa/opt"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

func TestNew(t *testing.T) {
	t.Run("returns an optimizer", func(t *testing.T) {
		require.NotNil(t, opt.New())
	})
}

func TestOptimizer_Optimize(t *testing.T) {
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

		out, err := opt.New().Optimize(fn)

		require.NoError(t, err)
		require.Same(t, fn, out, "Optimize mutates the function in place and returns it")
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

func TestOptimizer_Add(t *testing.T) {
	t.Run("runs a custom pass appended to the pipeline", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		fn := b.Build()

		o := opt.New()
		ran := false
		o.Add(recordingPass{ran: &ran})

		_, err := o.Optimize(fn)

		require.NoError(t, err)
		require.True(t, ran, "the appended pass must run as part of the pipeline")
	})
}

// recordingPass is a minimal pass.Pass[*ssa.Function] that only records that
// it ran, proving Optimizer.Add wires a custom pass into the same pipeline
// the built-in passes run in rather than something Optimize ignores.
type recordingPass struct {
	ran *bool
}

var _ pass.Pass[*ssa.Function] = recordingPass{}

func (p recordingPass) Run(_ *pass.Manager, _ *ssa.Function) (pass.Preserved, error) {
	*p.ran = true
	return pass.PreserveAll(), nil
}
