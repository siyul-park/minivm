package ssa_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

var _ graph.Graph = (*ssa.Function)(nil)

func TestFunction_Name(t *testing.T) {
	t.Run("returns the name given to New", func(t *testing.T) {
		b := ssa.New("gcd")
		entry := b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		require.Equal(t, "gcd", b.Build().Name())
	})
}

func TestFunction_Len(t *testing.T) {
	t.Run("counts the blocks built", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		require.Equal(t, 1, b.Build().Len())
	})

	t.Run("counts no block before one is built", func(t *testing.T) {
		require.Equal(t, 0, ssa.New("f").Build().Len())
	})
}

func TestFunction_Successors(t *testing.T) {
	t.Run("lists the blocks a terminator reaches in edge order", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right, join := b.Block(), b.Block(), b.Block(), b.Block()
		cond := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{cond}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
		b.Term(left, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
		b.Term(right, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
		b.Term(join, ssa.Terminator{Op: ssa.OpComplete})

		f := b.Build()
		require.Equal(t, []int{left, right}, f.Succ(entry))
		require.Equal(t, []int{join}, f.Succ(left))
		require.Empty(t, f.Succ(join))
	})
}

func TestFunction_Predecessors(t *testing.T) {
	t.Run("names each incoming block once", func(t *testing.T) {
		b := ssa.New("f")
		entry, join := b.Block(), b.Block()
		cond := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{cond}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: join}, {Block: join}}})
		b.Term(join, ssa.Terminator{Op: ssa.OpComplete})

		f := b.Build()
		require.Empty(t, f.Pred(entry))
		require.Equal(t, []int{entry}, f.Pred(join))
	})

	t.Run("serves the dominance and loop-header algorithms", func(t *testing.T) {
		b := ssa.New("f")
		entry, header, done := b.Block(), b.Block(), b.Block()
		cond := b.Value(ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header}}})
		b.Add(header, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{cond}})
		b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: header}, {Block: done}}})
		b.Term(done, ssa.Terminator{Op: ssa.OpComplete})

		f := b.Build()
		dom := graph.NewDominance(f)
		require.True(t, dom.Dominates(entry, done))
		require.False(t, dom.Dominates(done, header))
		require.Equal(t, []int{header}, graph.Headers(f, dom))
	})
}

func TestFunction_Block(t *testing.T) {
	t.Run("returns the block at an id", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		param := b.Param(entry, ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{param}})

		block := b.Build().Block(entry)
		require.Equal(t, []ssa.Value{param}, block.Params)
		require.Empty(t, block.Operations)
		require.Equal(t, ssa.OpReturn, block.Terminator.Op)
	})
}

func TestFunction_Type(t *testing.T) {
	t.Run("returns the type a value was reserved with", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		v := b.Value(ssa.TypeF64)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxF64(1.5), Results: []ssa.Value{v}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		require.Equal(t, ssa.TypeF64, b.Build().Type(v))
	})

	t.Run("returns the invalid type for a value it does not hold", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})

		f := b.Build()
		require.Equal(t, "invalid", f.Type(ssa.NoValue).String())
		require.Equal(t, "invalid", f.Type(ssa.Value(99)).String())
	})
}

func TestFunction_Values(t *testing.T) {
	t.Run("bounds every value reserved", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Param(entry, ssa.TypeI32)
		v := b.Value(ssa.TypeF64)
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		require.Equal(t, int(v)+1, b.Build().Values())
	})

	t.Run("bounds no value before one is reserved", func(t *testing.T) {
		require.Equal(t, int(ssa.NoValue)+1, ssa.New("f").Build().Values())
	})
}

func TestFunction_Entry(t *testing.T) {
	t.Run("returns the frame Builder.Entry set", func(t *testing.T) {
		b := ssa.New("f")
		b.Term(b.Block(), ssa.Terminator{Op: ssa.OpComplete})
		b.Entry(ssa.Frame{Address: 4, IP: 9, Returns: 1})

		require.Equal(t, ssa.Frame{Address: 4, IP: 9, Returns: 1}, b.Build().Entry())
	})

	t.Run("is zero when Builder.Entry was never called", func(t *testing.T) {
		b := ssa.New("f")
		b.Term(b.Block(), ssa.Terminator{Op: ssa.OpComplete})

		require.Zero(t, b.Build().Entry())
	})
}
