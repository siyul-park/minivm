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
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{first}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{second}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{first, second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewCSEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		out := ssa.Format(fn)
		require.Equal(t, 1, strings.Count(out, "i32.add"))
		require.Equal(t, "func f\nblk0: (v1:i32, v2:i32)\n\tv3:i32 = i32.add v1, v2\n\treturn v3, v3\n", out)
	})

	t.Run("collapses a computation a dominated block repeats", func(t *testing.T) {
		b := ssa.New("f")
		entry, next := b.Block(), b.Block()
		x, y := b.Param(entry, ssa.TypeI32), b.Param(entry, ssa.TypeI32)
		first := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{first}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: next}}})
		second := b.Value(ssa.TypeI32)
		b.Add(next, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{second}})
		b.Term(next, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{second}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewCSEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		out := ssa.Format(fn)
		require.Equal(t, 1, strings.Count(out, "i32.add"))
		require.Contains(t, out, "return v3")
	})

	t.Run("does not collapse a computation two sibling arms repeat independently", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right, join := b.Block(), b.Block(), b.Block(), b.Block()
		x, y := b.Param(entry, ssa.TypeI32), b.Param(entry, ssa.TypeI32)
		cond := b.Param(entry, ssa.TypeI1)
		b.Term(entry, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
		l := b.Value(ssa.TypeI32)
		b.Add(left, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{l}})
		b.Term(left, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{l}}}})
		r := b.Value(ssa.TypeI32)
		b.Add(right, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{r}})
		b.Term(right, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{r}}}})
		param := b.Param(join, ssa.TypeI32)
		b.Term(join, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{param}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewCSEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveAll(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 2, strings.Count(ssa.Format(fn), "i32.add"))
	})

	t.Run("collapses a repeated constant across blocks", func(t *testing.T) {
		b := ssa.New("f")
		entry, next := b.Block(), b.Block()
		first := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(7), Results: []ssa.Value{first}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: next}}})
		second := b.Value(ssa.TypeI32)
		b.Add(next, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(7), Results: []ssa.Value{second}})
		b.Term(next, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{second}})
		fn := b.Build()

		preserved, err := transform.NewCSEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 1, strings.Count(ssa.Format(fn), "const 7"))
	})

	t.Run("redirects a deopt frame's reference when the value it names collapses into an earlier one", func(t *testing.T) {
		b := ssa.New("f")
		entry, next := b.Block(), b.Block()
		x, y := b.Param(entry, ssa.TypeI32), b.Param(entry, ssa.TypeI32)
		first := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{first}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: next}}})
		second, state := b.Value(ssa.TypeI32), b.Value(ssa.TypeState)
		b.Add(next, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{second}})
		// second's only use is inside a deopt frame, not an ordinary argument.
		b.Add(next, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, Stack: []ssa.Operand{{Value: second}}}}, Results: []ssa.Value{state}})
		b.Term(next, ssa.Terminator{Op: ssa.OpExit, State: state})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewCSEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 1, strings.Count(ssa.Format(fn), "i32.add"))
		require.Contains(t, ssa.Format(fn), "stack=[v3]")
	})
}
