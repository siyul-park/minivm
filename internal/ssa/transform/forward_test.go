package transform_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/ssa/transform"
	"github.com/siyul-park/minivm/pass"
)

func TestNewForwardPass(t *testing.T) {
	t.Run("returns a pass over ssa.Function", func(t *testing.T) {
		var p pass.Pass[*ssa.Function] = transform.NewForwardPass()
		require.NotNil(t, p)
	})
}

func TestForwardPass_Run(t *testing.T) {
	local := func(index int) ssa.Slot {
		return ssa.Slot{Space: ssa.SpaceLocal, Index: index}
	}

	t.Run("forwards a repeated read of one slot onto the first", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		first, second := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: local(0), Results: []ssa.Value{first}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: local(0), Results: []ssa.Value{second}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{first, second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewForwardPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, "func f\nblk0: ()\n\tv1:i32 = load local[0]\n\treturn v1, v1\n", ssa.Format(fn))
	})

	t.Run("forwards into a block only one edge reaches", func(t *testing.T) {
		b := ssa.New("f")
		entry, next := b.Block(), b.Block()
		first, second := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: local(0), Results: []ssa.Value{first}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: next}}})
		b.Add(next, ssa.Operation{Op: ssa.OpLoad, Slot: local(0), Results: []ssa.Value{second}})
		b.Term(next, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{first, second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewForwardPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 1, strings.Count(ssa.Format(fn), "load local[0]"))
	})

	t.Run("keeps a read a store to the same slot separates", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		first, second, state := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: local(0), Results: []ssa.Value{first}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: local(0), Args: []ssa.Value{first}, State: state})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: local(0), Results: []ssa.Value{second}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewForwardPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveAll(), preserved)
		require.Equal(t, 2, strings.Count(ssa.Format(fn), "load local[0]"))
	})

	t.Run("keeps a read a store to another slot does not separate", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		first, second, state := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: local(0), Results: []ssa.Value{first}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: local(1), Args: []ssa.Value{first}, State: state})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: local(0), Results: []ssa.Value{second}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewForwardPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 1, strings.Count(ssa.Format(fn), "load local[0]"))
	})

	t.Run("keeps a global read a call separates but forwards a local one", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		callee := b.Param(entry, ssa.TypeRef)
		global, held, state := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceGlobal}, Results: []ssa.Value{global}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: local(0), Results: []ssa.Value{held}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{callee}, State: state})
		again, local0 := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceGlobal}, Results: []ssa.Value{again}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: local(0), Results: []ssa.Value{local0}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{global, held, again, local0}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewForwardPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		out := ssa.Format(fn)
		require.Equal(t, 2, strings.Count(out, "load global[0]"))
		require.Equal(t, 1, strings.Count(out, "load local[0]"))
	})

	t.Run("keeps a read in a block more than one edge reaches", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right, join := b.Block(), b.Block(), b.Block(), b.Block()
		cond := b.Param(entry, ssa.TypeI1)
		first := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: local(0), Results: []ssa.Value{first}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
		b.Term(left, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
		stored, state := b.Value(ssa.TypeI32), b.Value(ssa.TypeState)
		b.Add(right, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{stored}})
		b.Add(right, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		b.Add(right, ssa.Operation{Op: ssa.OpStore, Slot: local(0), Args: []ssa.Value{stored}, State: state})
		b.Term(right, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
		second := b.Value(ssa.TypeI32)
		b.Add(join, ssa.Operation{Op: ssa.OpLoad, Slot: local(0), Results: []ssa.Value{second}})
		b.Term(join, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{first, second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewForwardPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveAll(), preserved)
		require.Equal(t, 2, strings.Count(ssa.Format(fn), "load local[0]"))
	})
}
