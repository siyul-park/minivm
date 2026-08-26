package pass_test

import (
	"testing"

	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestPreserveAll(t *testing.T) {
	calls := 0
	m := pass.NewManager()
	pass.Register[*program.Program, int](m, runner[*program.Program, int](func(_ *pass.Manager, prog *program.Program) (int, error) {
		calls++
		return len(prog.Code), nil
	}))
	prog := program.New(nil)
	_, err := pass.GetResult[int](m, prog)
	require.NoError(t, err)
	m.Invalidate(pass.PreserveAll())
	_, err = pass.GetResult[int](m, prog)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
}

func TestPreserveNone(t *testing.T) {
	calls := 0
	m := pass.NewManager()
	pass.Register[*program.Program, int](m, runner[*program.Program, int](func(_ *pass.Manager, prog *program.Program) (int, error) {
		calls++
		return len(prog.Code), nil
	}))
	prog := program.New(nil)
	_, err := pass.GetResult[int](m, prog)
	require.NoError(t, err)
	m.Invalidate(pass.PreserveNone())
	_, err = pass.GetResult[int](m, prog)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}
