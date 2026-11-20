package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/transform"
	"github.com/siyul-park/minivm/pass"
)

func TestNewSimplifyPass(t *testing.T) {
	t.Run("returns a pass over ssa.Function", func(t *testing.T) {
		var p pass.Pass[*ssa.Function] = transform.NewSimplifyPass()
		require.NotNil(t, p)
	})
}

func TestSimplifyPass_Run(t *testing.T) {
	t.Run("keeps a join param only a frame local reads", func(t *testing.T) {
		b := ssa.New("f")
		entry, mid, head := b.Block(), b.Block(), b.Block()
		c, zero, one := b.Param(entry, ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{zero}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{c}, Edges: []ssa.Edge{{Block: head, Args: []ssa.Value{zero}}, {Block: mid}}})
		b.Add(mid, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Term(mid, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: head, Args: []ssa.Value{one}}}})
		kept, state := b.Param(head, ssa.TypeI32), b.Value(ssa.TypeState)
		b.Add(head, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Locals: []ssa.Local{{Index: 0, Value: kept}}}}, Results: []ssa.Value{state}})
		b.Term(head, ssa.Terminator{Term: ssa.TermExit, State: state})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewDCEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Len(t, fn.Block(2).Params, 1)
	})

	t.Run("replaces a join param both edges feed the same value", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right, join := b.Block(), b.Block(), b.Block(), b.Block()
		c, v := b.Param(entry, ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 7, Results: []ssa.Value{v}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{c}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
		b.Term(left, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{v}}}})
		b.Term(right, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{v}}}})
		p := b.Param(join, ssa.TypeI32)
		b.Term(join, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{p}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewSimplifyPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Empty(t, fn.Block(3).Params)
		require.Empty(t, fn.Block(1).Terminator.Edges[0].Args)
		require.Empty(t, fn.Block(2).Terminator.Edges[0].Args)
		require.Equal(t, fn.Block(0).Operations[0].Results[0], fn.Block(3).Terminator.Args[0])
	})

	t.Run("replaces a loop param only its initial value and itself feed", func(t *testing.T) {
		b := ssa.New("f")
		entry, head, body, exit := b.Block(), b.Block(), b.Block(), b.Block()
		b.Param(entry, ssa.TypeI32)
		init := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 5, Results: []ssa.Value{init}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: head, Args: []ssa.Value{init}}}})
		k := b.Param(head, ssa.TypeI32)
		cond := b.Value(ssa.TypeI32)
		b.Add(head, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_EQZ, Args: []ssa.Value{k}, State: deoptState(b, head), Results: []ssa.Value{cond}})
		b.Term(head, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: body}, {Block: exit}}})
		b.Term(body, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: head, Args: []ssa.Value{k}}}})
		b.Term(exit, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{k}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewSimplifyPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Empty(t, fn.Block(1).Params)
		require.Empty(t, fn.Block(2).Terminator.Edges[0].Args)
		require.Equal(t, fn.Block(0).Operations[0].Results[0], fn.Block(3).Terminator.Args[0])
	})

	t.Run("replaces a param fed another trivial param", func(t *testing.T) {
		b := ssa.New("f")
		entry, head, body, exit := b.Block(), b.Block(), b.Block(), b.Block()
		b.Param(entry, ssa.TypeI32)
		v := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 5, Results: []ssa.Value{v}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: head, Args: []ssa.Value{v, v}}}})
		p1, p2 := b.Param(head, ssa.TypeI32), b.Param(head, ssa.TypeI32)
		cond := b.Value(ssa.TypeI32)
		b.Add(head, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_EQZ, Args: []ssa.Value{p2}, State: deoptState(b, head), Results: []ssa.Value{cond}})
		b.Term(head, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: body}, {Block: exit}}})
		b.Term(body, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: head, Args: []ssa.Value{p1, p2}}}})
		b.Term(exit, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{p2}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewSimplifyPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Empty(t, fn.Block(1).Params)
		require.Empty(t, fn.Block(2).Terminator.Edges[0].Args)
		require.Equal(t, fn.Block(0).Operations[0].Results[0], fn.Block(3).Terminator.Args[0])
	})

	t.Run("keeps a param whose type differs from its only incoming value", func(t *testing.T) {
		b := ssa.New("f")
		entry, join := b.Block(), b.Block()
		v := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 5, Results: []ssa.Value{v}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{v}}}})
		p := b.Param(join, ssa.TypeI64)
		b.Term(join, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{p}})
		fn := b.Build()

		preserved, err := transform.NewSimplifyPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Len(t, fn.Block(1).Params, 1)
	})

	t.Run("keeps a param fed two different values", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right, join := b.Block(), b.Block(), b.Block(), b.Block()
		c, x, y := b.Param(entry, ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{y}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{c}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
		b.Term(left, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{x}}}})
		b.Term(right, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{y}}}})
		p := b.Param(join, ssa.TypeI32)
		b.Term(join, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{p}})
		fn := b.Build()

		preserved, err := transform.NewSimplifyPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Len(t, fn.Block(3).Params, 1)
	})

	t.Run("keeps an entry param fed only itself by a back edge", func(t *testing.T) {
		b := ssa.New("f")
		entry, body, exit := b.Block(), b.Block(), b.Block()
		i := b.Param(entry, ssa.TypeI32)
		below := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_EQZ, Args: []ssa.Value{i}, State: deoptState(b, entry), Results: []ssa.Value{below}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{below}, Edges: []ssa.Edge{{Block: body}, {Block: exit}}})
		b.Term(body, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: entry, Args: []ssa.Value{i}}}})
		b.Term(exit, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{i}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewSimplifyPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Len(t, fn.Block(0).Params, 1)
		require.Len(t, fn.Block(1).Terminator.Edges[0].Args, 1)
	})

	t.Run("rewrites a frame local naming a replaced param to its value", func(t *testing.T) {
		b := ssa.New("f")
		entry, join := b.Block(), b.Block()
		v := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 5, Results: []ssa.Value{v}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{v}}}})
		p, state := b.Param(join, ssa.TypeI32), b.Value(ssa.TypeState)
		b.Add(join, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Locals: []ssa.Local{{Index: 0, Value: p}}}}, Results: []ssa.Value{state}})
		b.Term(join, ssa.Terminator{Term: ssa.TermExit, State: state})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewSimplifyPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 1, fn.Len())
		require.Equal(t, fn.Block(0).Operations[0].Results[0], fn.Block(0).Operations[1].Frames[0].Locals[0].Value)
	})

	t.Run("merges a block into its sole predecessor's jump", func(t *testing.T) {
		b := ssa.New("f")
		a, next := b.Block(), b.Block()
		x, y, sum := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(a, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{x}})
		b.Term(a, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: next}}})
		b.Add(next, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{y}})
		b.Add(next, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, y}, State: deoptState(b, next), Results: []ssa.Value{sum}})
		b.Term(next, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{sum}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewSimplifyPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 1, fn.Len())
		require.Equal(t, ssa.TermReturn, fn.Block(0).Terminator.Term)
		require.Equal(t, ssa.OpConst, fn.Block(0).Operations[0].Op)
		require.Equal(t, ssa.OpIntAdd, fn.Block(0).Operations[len(fn.Block(0).Operations)-1].Op)
	})

	t.Run("merges a chain of single-predecessor jumps into one block", func(t *testing.T) {
		b := ssa.New("f")
		a, mid, last := b.Block(), b.Block(), b.Block()
		x, y := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(a, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{x}})
		b.Term(a, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: mid}}})
		b.Add(mid, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_EQZ, Args: []ssa.Value{x}, State: deoptState(b, mid), Results: []ssa.Value{y}})
		b.Term(mid, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: last}}})
		b.Term(last, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{y}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewSimplifyPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 1, fn.Len())
		require.Equal(t, ssa.TermReturn, fn.Block(0).Terminator.Term)
	})

	t.Run("keeps a block with two predecessors apart", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right, join := b.Block(), b.Block(), b.Block(), b.Block()
		c := b.Param(entry, ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{c}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
		b.Term(left, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join}}})
		b.Term(right, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join}}})
		b.Term(join, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{c}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewSimplifyPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, 4, fn.Len())
	})

	t.Run("keeps a self loop and a loop header apart", func(t *testing.T) {
		b := ssa.New("f")
		entry, head, body := b.Block(), b.Block(), b.Block()
		c := b.Param(entry, ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: head}}})
		b.Term(head, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{c}, Edges: []ssa.Edge{{Block: body}, {Block: head}}})
		b.Term(body, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{c}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewSimplifyPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, 3, fn.Len())
	})

	t.Run("keeps a jump to itself", func(t *testing.T) {
		b := ssa.New("f")
		entry, spin := b.Block(), b.Block()
		c := b.Param(entry, ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{c}, Edges: []ssa.Edge{{Block: spin}, {Block: spin}}})
		b.Term(spin, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: spin}}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewSimplifyPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, 2, fn.Len())
	})

	t.Run("keeps a block whose param is not replaced apart", func(t *testing.T) {
		b := ssa.New("f")
		entry, join := b.Block(), b.Block()
		v := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 5, Results: []ssa.Value{v}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{v}}}})
		p := b.Param(join, ssa.TypeI64)
		b.Term(join, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{p}})
		fn := b.Build()

		_, err := transform.NewSimplifyPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, 2, fn.Len())
	})
}
