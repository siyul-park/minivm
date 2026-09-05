package transform_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/ssa/transform"
	"github.com/siyul-park/minivm/pass"
)

func TestNewGuardPass(t *testing.T) {
	t.Run("returns a pass over ssa.Function", func(t *testing.T) {
		var p pass.Pass[*ssa.Function] = transform.NewGuardPass()
		require.NotNil(t, p)
	})
}

func TestGuardPass_Run(t *testing.T) {
	t.Run("collapses a repeated shape guard over the same operand", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		array := b.Param(entry, ssa.TypeRef)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		first := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Itab: 5}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{first}})
		second := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Itab: 5}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{second}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewGuardPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		out := ssa.Format(fn)
		require.Equal(t, 1, strings.Count(out, "guard.shape"), "only one guard should remain")
		require.Contains(t, out, "return v3", "the eliminated guard's uses now read the surviving guard's result")
	})

	t.Run("does not collapse guards admitting different shapes", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		array := b.Param(entry, ssa.TypeRef)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		first := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Itab: 5}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{first}})
		second := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Itab: 6}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{second}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{first, second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewGuardPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveAll(), preserved, "the second guard admits a different shape, so it is not redundant")
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 2, strings.Count(ssa.Format(fn), "guard.shape"))
	})

	t.Run("collapses a repeated kind guard narrowing to the same kind", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		dyn := b.Param(entry, ssa.TypeRef)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		first := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{dyn}, State: state, Results: []ssa.Value{first}})
		second := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{dyn}, State: state, Results: []ssa.Value{second}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewGuardPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 1, strings.Count(ssa.Format(fn), "guard.kind"))
	})

	t.Run("does not collapse kind guards narrowing to different kinds", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		dyn := b.Param(entry, ssa.TypeRef)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		asI32 := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{dyn}, State: state, Results: []ssa.Value{asI32}})
		asF64 := b.Value(ssa.TypeF64)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{dyn}, State: state, Results: []ssa.Value{asF64}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{asI32, asF64}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewGuardPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveAll(), preserved, "the two guards narrow the same operand to different kinds")
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 2, strings.Count(ssa.Format(fn), "guard.kind"))
	})

	t.Run("collapses a repeated value guard specializing to the same observed value", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		value, observed := b.Param(entry, ssa.TypeI32), b.Param(entry, ssa.TypeI32)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		first := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardValue, Args: []ssa.Value{value, observed}, State: state, Results: []ssa.Value{first}})
		second := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardValue, Args: []ssa.Value{value, observed}, State: state, Results: []ssa.Value{second}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewGuardPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 1, strings.Count(ssa.Format(fn), "guard.value"))
	})

	t.Run("collapses a repeated bounds guard, which has no result to redirect", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		index, length := b.Param(entry, ssa.TypeI32), b.Param(entry, ssa.TypeI32)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardBounds, Args: []ssa.Value{index, length}, State: state})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardBounds, Args: []ssa.Value{index, length}, State: state})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewGuardPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 1, strings.Count(ssa.Format(fn), "guard.bounds"))
	})

	t.Run("preserves a deopt frame's own values while eliminating a redundant guard sharing its state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		array, extra := b.Param(entry, ssa.TypeRef), b.Param(entry, ssa.TypeI32)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, Stack: []ssa.Value{extra}}}, Results: []ssa.Value{state}})
		first := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Itab: 9}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{first}})
		second := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Itab: 9}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{second}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewGuardPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn), "the surviving guard's state must still resolve, and its frame must still name a real value")
		require.Contains(t, ssa.Format(fn), "stack=[v2]", "extra, named only from the frame, survives the rebuild untouched")
	})
}
