package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/transform"
	"github.com/siyul-park/minivm/pass"
)

func TestNewStripPass(t *testing.T) {
	t.Run("returns a pass over ssa.Function", func(t *testing.T) {
		var p pass.Pass[*ssa.Function] = transform.NewStripPass()
		require.NotNil(t, p)
	})
}

func TestStripPass_Run(t *testing.T) {
	t.Run("removes every state and the state each operation and terminator holds", func(t *testing.T) {
		b := ssa.New("f")
		entry, exit := b.Block(), b.Block()
		x, y, quotient := b.Param(entry, ssa.TypeI32), b.Param(entry, ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_DIV_S, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{quotient}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, State: deoptState(b, entry), Edges: []ssa.Edge{{Block: exit, Args: []ssa.Value{quotient}}}})
		result := b.Param(exit, ssa.TypeI32)
		b.Term(exit, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{result}})
		fn := b.Build()
		require.Contains(t, ssa.Format(fn), "state")

		preserved, err := transform.NewStripPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NotContains(t, ssa.Format(fn), "state")
		require.Contains(t, ssa.Format(fn), "i32.div_s")
		require.Contains(t, ssa.Format(fn), "return")
	})

	t.Run("leaves the values a frame named in place", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, state := b.Value(ssa.TypeI32), b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Locals: []ssa.Local{{Index: 0, Value: x}}}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		fn := b.Build()

		_, err := transform.NewStripPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NotContains(t, ssa.Format(fn), "state")
		require.Contains(t, ssa.Format(fn), "const 1")
	})

	t.Run("reports a function without states as preserved", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{x}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{x}})
		fn := b.Build()

		preserved, err := transform.NewStripPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
	})
}
