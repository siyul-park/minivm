package pass_test

import (
	"errors"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

type runner[U, R any] func(*pass.Manager, U) (R, error)

func (r runner[U, R]) Run(m *pass.Manager, unit U) (R, error) {
	return r(m, unit)
}

func TestNewManager(t *testing.T) {
	require.NotNil(t, pass.NewManager())
}

func TestRegister(t *testing.T) {
	m := pass.NewManager()
	pass.Register[*program.Program, int](m, runner[*program.Program, int](func(_ *pass.Manager, prog *program.Program) (int, error) {
		return len(prog.Code), nil
	}))

	got, err := pass.GetResult[int](m, program.New([]instr.Instruction{instr.New(instr.NOP)}))
	require.NoError(t, err)
	require.Equal(t, 1, got)
}

func TestGetResult(t *testing.T) {
	t.Run("computes and caches", func(t *testing.T) {
		calls := 0
		m := pass.NewManager()
		pass.Register[*program.Program, int](m, runner[*program.Program, int](func(_ *pass.Manager, prog *program.Program) (int, error) {
			calls++
			return len(prog.Code), nil
		}))

		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		first, err := pass.GetResult[int](m, prog)
		require.NoError(t, err)
		second, err := pass.GetResult[int](m, prog)
		require.NoError(t, err)

		require.Equal(t, first, second)
		require.Equal(t, 1, calls)
	})

	t.Run("unregistered analysis", func(t *testing.T) {
		m := pass.NewManager()
		_, err := pass.GetResult[int](m, program.New(nil))
		require.ErrorIs(t, err, pass.ErrUnregisteredAnalysis)
	})

	t.Run("propagates error", func(t *testing.T) {
		want := errors.New("fail")
		m := pass.NewManager()
		pass.Register[*program.Program, int](m, runner[*program.Program, int](func(*pass.Manager, *program.Program) (int, error) {
			return 0, want
		}))

		_, err := pass.GetResult[int](m, program.New(nil))
		require.ErrorIs(t, err, want)
	})
}

func TestManager_Invalidate(t *testing.T) {
	t.Run("drops cached results", func(t *testing.T) {
		calls := 0
		m := pass.NewManager()
		pass.Register[*program.Program, int](m, runner[*program.Program, int](func(_ *pass.Manager, prog *program.Program) (int, error) {
			calls++
			return len(prog.Code), nil
		}))

		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		_, err := pass.GetResult[int](m, prog)
		require.NoError(t, err)
		m.Invalidate(pass.PreserveNone())
		_, err = pass.GetResult[int](m, prog)
		require.NoError(t, err)

		require.Equal(t, 2, calls)
	})

	t.Run("preserve all keeps results", func(t *testing.T) {
		calls := 0
		m := pass.NewManager()
		pass.Register[*program.Program, int](m, runner[*program.Program, int](func(_ *pass.Manager, prog *program.Program) (int, error) {
			calls++
			return len(prog.Code), nil
		}))

		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		_, err := pass.GetResult[int](m, prog)
		require.NoError(t, err)
		m.Invalidate(pass.PreserveAll())
		_, err = pass.GetResult[int](m, prog)
		require.NoError(t, err)

		require.Equal(t, 1, calls)
	})
}

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
