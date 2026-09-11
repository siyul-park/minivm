package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/ssa/transform"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

func TestNewDCEPass(t *testing.T) {
	t.Run("returns a pass over ssa.Function", func(t *testing.T) {
		var p pass.Pass[*ssa.Function] = transform.NewDCEPass()
		require.NotNil(t, p)
	})
}

func TestDCEPass_Run(t *testing.T) {
	t.Run("removes a pure operation whose result nothing reads", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, unused := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(2), Results: []ssa.Value{unused}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{x}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewDCEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, "func f\nblk0: ()\n\tv1:i32 = const 1\n\treturn v1\n", ssa.Format(fn))
	})

	t.Run("keeps an operation with an effect even when its result is unused", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		array := b.Param(entry, ssa.TypeRef)
		length := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_LEN, Args: []ssa.Value{array}, Results: []ssa.Value{length}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewDCEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveAll(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Contains(t, ssa.Format(fn), "array.len")
	})

	t.Run("removes a block unreachable from the entry", func(t *testing.T) {
		b := ssa.New("f")
		entry, live := b.Block(), b.Block()
		orphan := b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: live}}})
		b.Term(live, ssa.Terminator{Op: ssa.OpComplete})
		// Nothing names orphan on any edge; ssa.Builder does not judge that, so
		// this "before" state is deliberately not ssa.Verify-clean.
		b.Term(orphan, ssa.Terminator{Op: ssa.OpComplete})
		fn := b.Build()
		require.Equal(t, 3, fn.Len())

		preserved, err := transform.NewDCEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 2, fn.Len())
	})

	t.Run("keeps a pure operation a deopt frame alone still references", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, y, sum, state := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(2), Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(3), Results: []ssa.Value{y}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{sum}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, Stack: []ssa.Operand{{Value: sum}}}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpExit, State: state})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewDCEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveAll(), preserved)
		require.NoError(t, ssa.Verify(fn))
		out := ssa.Format(fn)
		require.Contains(t, out, "i32.add")
		require.Contains(t, out, "stack=[v3]")
	})

	t.Run("keeps a reference only a frame's owned entry names, still owned once renumbered", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		array, dead, state := b.Value(ssa.TypeRef), b.Value(ssa.TypeI32), b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxRef(3), Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(9), Results: []ssa.Value{dead}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, Stack: []ssa.Operand{{Value: array, Owned: true}}}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpExit, State: state})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewDCEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, "func f\nblk0: ()\n\tv1:ref = const 3\n\tv2:state = state {addr=1 base=0 ip=0 returns=0 stack=[v1 owned]}\n\texit state v2\n", ssa.Format(fn))
	})

	t.Run("removes an OpState nothing still resumes into, once the guard that alone used it is gone", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewDCEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.NotContains(t, ssa.Format(fn), "state")
	})

	t.Run("keeps a pure operation that feeds a live guard even though nothing else reads it", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		array := b.Param(entry, ssa.TypeRef)
		zero := b.Value(ssa.TypeI32)
		state := b.Value(ssa.TypeState)
		length := b.Value(ssa.TypeI32)
		refined := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(0), Results: []ssa.Value{zero}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_LEN, Args: []ssa.Value{array}, Results: []ssa.Value{length}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardValue, Args: []ssa.Value{length, zero}, State: state, Results: []ssa.Value{refined}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewDCEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveAll(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Contains(t, ssa.Format(fn), "const 0")
	})
}
