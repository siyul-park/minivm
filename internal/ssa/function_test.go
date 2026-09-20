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

func TestFunction_Succ(t *testing.T) {
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

func TestFunction_Pred(t *testing.T) {
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
		require.Equal(t, []int{header}, graph.LoopHeaders(f, dom))
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
		require.Empty(t, block.Ops)
		require.Equal(t, ssa.OpReturn, block.Term.Op)
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
