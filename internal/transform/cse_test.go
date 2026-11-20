package transform_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/transform"
	"github.com/siyul-park/minivm/pass"
)

func TestNewCSEPass(t *testing.T) {
	t.Run("returns a pass over ssa.Function", func(t *testing.T) {
		var p pass.Pass[*ssa.Function] = transform.NewCSEPass()
		require.NotNil(t, p)
	})
}

func TestCSEPass_Run(t *testing.T) {
	t.Run("collapses a repeated pure computation within one block", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, y := b.Param(entry, ssa.TypeI32), b.Param(entry, ssa.TypeI32)
		first, second := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{first}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{second}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{first, second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewCSEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		out := ssa.Format(fn)
		require.Equal(t, 1, strings.Count(out, "int.add"))
		require.Equal(t, "func f\nblk0: (v1:i32, v2:i32)\n\tv3:state = state {addr=1 base=0 ip=0 returns=0 stack=[]}\n\tv4:i32 = int.add v1, v2 state v3\n\tv5:state = state {addr=1 base=0 ip=0 returns=0 stack=[]}\n\treturn v4, v4\n", out)
	})

	t.Run("collapses a commutative computation with swapped operands but not a subtraction", func(t *testing.T) {
		for _, c := range []struct {
			op   ssa.Op
			cond ssa.Cond
			typ  ssa.Type
			want int
		}{
			{ssa.OpIntMul, 0, ssa.TypeI32, 1},
			{ssa.OpIntAdd, 0, ssa.TypeI64, 1},
			{ssa.OpIntCmp, ssa.CondEQ, ssa.TypeI32, 1},
			{ssa.OpIntSub, 0, ssa.TypeI32, 2},
			{ssa.OpIntCmp, ssa.CondSLT, ssa.TypeI64, 2},
		} {
			t.Run(c.op.String()+" "+c.cond.String(), func(t *testing.T) {
				b := ssa.New("f")
				entry := b.Block()
				x, y := b.Param(entry, c.typ), b.Param(entry, c.typ)
				rt := c.typ
				if c.op == ssa.OpIntCmp {
					rt = ssa.TypeI1
				}
				first, second := b.Value(rt), b.Value(rt)
				b.Add(entry, ssa.Operation{Op: c.op, Cond: c.cond, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{first}})
				b.Add(entry, ssa.Operation{Op: c.op, Cond: c.cond, Args: []ssa.Value{y, x}, State: deoptState(b, entry), Results: []ssa.Value{second}})
				b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{first, second}})
				fn := b.Build()
				require.NoError(t, ssa.Verify(fn))

				_, err := transform.NewCSEPass().Run(pass.NewManager(), fn)

				require.NoError(t, err)
				require.NoError(t, ssa.Verify(fn))
				require.Equal(t, c.want, strings.Count(ssa.Format(fn), c.op.String()))
			})
		}
	})

	t.Run("collapses a computation a dominated block repeats", func(t *testing.T) {
		b := ssa.New("f")
		entry, next := b.Block(), b.Block()
		x, y := b.Param(entry, ssa.TypeI32), b.Param(entry, ssa.TypeI32)
		first := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{first}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: next}}})
		second := b.Value(ssa.TypeI32)
		b.Add(next, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{second}})
		b.Term(next, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewCSEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		out := ssa.Format(fn)
		require.Equal(t, 1, strings.Count(out, "int.add"))
		require.Contains(t, out, "return v4")
	})

	t.Run("does not collapse a computation two sibling arms repeat independently", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right, join := b.Block(), b.Block(), b.Block(), b.Block()
		x, y := b.Param(entry, ssa.TypeI32), b.Param(entry, ssa.TypeI32)
		cond := b.Param(entry, ssa.TypeI1)
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
		l := b.Value(ssa.TypeI32)
		b.Add(left, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{l}})
		b.Term(left, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{l}}}})
		r := b.Value(ssa.TypeI32)
		b.Add(right, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{r}})
		b.Term(right, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{r}}}})
		param := b.Param(join, ssa.TypeI32)
		b.Term(join, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{param}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewCSEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 2, strings.Count(ssa.Format(fn), "int.add"))
	})

	t.Run("collapses a repeated constant across blocks", func(t *testing.T) {
		b := ssa.New("f")
		entry, next := b.Block(), b.Block()
		first := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 7, Results: []ssa.Value{first}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: next}}})
		second := b.Value(ssa.TypeI32)
		b.Add(next, ssa.Operation{Op: ssa.OpConst, Const: 7, Results: []ssa.Value{second}})
		b.Term(next, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{second}})
		fn := b.Build()

		preserved, err := transform.NewCSEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 1, strings.Count(ssa.Format(fn), "const 7"))
	})

	t.Run("keeps an i32, an i64 and an f64 constant of word 0 apart", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		i32, i64, f64 := b.Value(ssa.TypeI32), b.Value(ssa.TypeI64), b.Value(ssa.TypeF64)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{i32}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{i64}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{f64}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{i32, i64, f64}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewCSEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 3, strings.Count(ssa.Format(fn), "const 0"))
	})

	t.Run("redirects a deopt frame's reference when the value it names collapses into an earlier one", func(t *testing.T) {
		b := ssa.New("f")
		entry, next := b.Block(), b.Block()
		x, y := b.Param(entry, ssa.TypeI32), b.Param(entry, ssa.TypeI32)
		first := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{first}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: next}}})
		second, state := b.Value(ssa.TypeI32), b.Value(ssa.TypeState)
		b.Add(next, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{second}})
		b.Add(next, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Stack: []ssa.Operand{{Value: second}}}}, Results: []ssa.Value{state}})
		b.Term(next, ssa.Terminator{Term: ssa.TermExit, State: state})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewCSEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 1, strings.Count(ssa.Format(fn), "int.add"))
		require.Contains(t, ssa.Format(fn), "stack=[v4]")
	})
}
