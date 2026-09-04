package ssa_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Run("names the function it builds", func(t *testing.T) {
		require.Equal(t, "func fib\n", ssa.Format(ssa.New("fib").Build()))
	})
}

func TestBuilder_Block(t *testing.T) {
	t.Run("numbers blocks from the entry upward", func(t *testing.T) {
		b := ssa.New("f")
		entry, second := b.Block(), b.Block()
		require.Equal(t, 0, entry)
		require.Equal(t, 1, second)

		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: second}}})
		b.Term(second, ssa.Terminator{Op: ssa.OpComplete})
		require.Equal(t, "func f\nblk0: ()\n\tjump blk1()\nblk1: () <-- (blk0)\n\tcomplete\n", ssa.Format(b.Build()))
	})
}

func TestBuilder_Param(t *testing.T) {
	t.Run("appends parameters in order", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		first := b.Param(entry, ssa.TypeI32)
		second := b.Param(entry, ssa.TypeRef)
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{first, second}})

		f := b.Build()
		require.Equal(t, []ssa.Value{first, second}, f.Block(entry).Params)
		require.Equal(t, "func f\nblk0: (v1:i32, v2:ref)\n\treturn v1, v2\n", ssa.Format(f))
	})
}

func TestBuilder_Value(t *testing.T) {
	t.Run("reserves a distinct typed value each time", func(t *testing.T) {
		b := ssa.New("f")
		first := b.Value(ssa.TypeI32)
		second := b.Value(ssa.TypeF64)
		require.NotEqual(t, first, second)
		require.NotEqual(t, ssa.NoValue, first)

		f := b.Build()
		require.Equal(t, ssa.TypeI32, f.Type(first))
		require.Equal(t, ssa.TypeF64, f.Type(second))
	})
}

func TestBuilder_Add(t *testing.T) {
	t.Run("appends instructions in order", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		one := b.Value(ssa.TypeI32)
		doubled := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Instruction{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
		b.Add(entry, ssa.Instruction{Op: ssa.OpPure, Code: instr.I32_ADD, Args: []ssa.Value{one, one}, Results: []ssa.Value{doubled}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{doubled}})

		require.Equal(t,
			"func f\nblk0: ()\n\tv1:i32 = const 1\n\tv2:i32 = i32.add v1, v1\n\treturn v2\n",
			ssa.Format(b.Build()))
	})
}

func TestBuilder_Term(t *testing.T) {
	t.Run("replaces the terminator already set", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})
		require.Equal(t, "func f\nblk0: ()\n\treturn\n", ssa.Format(b.Build()))
	})
}

func TestBuilder_Build(t *testing.T) {
	t.Run("resolves successors and predecessors", func(t *testing.T) {
		b := ssa.New("f")
		entry, join := b.Block(), b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
		b.Term(join, ssa.Terminator{Op: ssa.OpComplete})

		f := b.Build()
		require.Equal(t, []int{join}, f.Succ(entry))
		require.Equal(t, []int{entry}, f.Pred(join))
	})

	t.Run("hands over its storage and starts over", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})

		built := b.Build()
		next := b.Block()
		b.Term(next, ssa.Terminator{Op: ssa.OpReturn})

		require.Equal(t, "func f\nblk0: ()\n\tcomplete\n", ssa.Format(built))
		require.Equal(t, "func f\nblk0: ()\n\treturn\n", ssa.Format(b.Build()))
	})
}
