package pass_test

import (
	"errors"
	"testing"

	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestNewPipeline(t *testing.T) {
	require.NotNil(t, pass.NewPipeline[*program.Program]())
}

func TestPipeline_Add(t *testing.T) {
	var log []string
	pipeline := pass.NewPipeline[*program.Program]()
	pipeline.Add(runner[*program.Program, bool](func(*pass.Manager, *program.Program) (bool, error) {
		log = append(log, "added")
		return true, nil
	}))
	_, err := pipeline.Run(pass.NewManager(), program.New(nil))
	require.NoError(t, err)
	require.Equal(t, []string{"added"}, log)
}

func TestPipeline_Run(t *testing.T) {
	t.Run("runs passes in order", func(t *testing.T) {
		var log []string
		pl := pass.NewPipeline[*program.Program]()
		pl.Add(runner[*program.Program, bool](func(*pass.Manager, *program.Program) (bool, error) {
			log = append(log, "a")
			return true, nil
		}))
		pl.Add(runner[*program.Program, bool](func(*pass.Manager, *program.Program) (bool, error) {
			log = append(log, "b")
			return true, nil
		}))

		prog := program.New(nil)
		got, err := pl.Run(pass.NewManager(), prog)
		require.NoError(t, err)
		require.Same(t, prog, got)
		require.Equal(t, []string{"a", "b"}, log)
	})

	t.Run("invalidates analyses when a pass does not preserve them", func(t *testing.T) {
		calls := 0
		m := pass.NewManager()
		pass.Register[*program.Program, int](m, runner[*program.Program, int](func(_ *pass.Manager, prog *program.Program) (int, error) {
			calls++
			return len(prog.Code), nil
		}))
		prog := program.New(nil)
		_, err := pass.GetResult[int](m, prog)
		require.NoError(t, err)

		pl := pass.NewPipeline[*program.Program]()
		pl.Add(runner[*program.Program, bool](func(*pass.Manager, *program.Program) (bool, error) {
			return false, nil
		}))

		_, err = pl.Run(m, prog)
		require.NoError(t, err)
		_, err = pass.GetResult[int](m, prog)

		require.NoError(t, err)
		require.Equal(t, 2, calls)
	})

	t.Run("keeps analyses when a pass preserves them", func(t *testing.T) {
		calls := 0
		m := pass.NewManager()
		pass.Register[*program.Program, int](m, runner[*program.Program, int](func(_ *pass.Manager, prog *program.Program) (int, error) {
			calls++
			return len(prog.Code), nil
		}))
		prog := program.New(nil)
		_, err := pass.GetResult[int](m, prog)
		require.NoError(t, err)

		pl := pass.NewPipeline[*program.Program]()
		pl.Add(runner[*program.Program, bool](func(*pass.Manager, *program.Program) (bool, error) {
			return true, nil
		}))

		_, err = pl.Run(m, prog)
		require.NoError(t, err)
		_, err = pass.GetResult[int](m, prog)

		require.NoError(t, err)
		require.Equal(t, 1, calls)
	})

	t.Run("invalidates analyses on error", func(t *testing.T) {
		calls := 0
		m := pass.NewManager()
		pass.Register[*program.Program, int](m, runner[*program.Program, int](func(_ *pass.Manager, prog *program.Program) (int, error) {
			calls++
			return len(prog.Code), nil
		}))
		prog := program.New(nil)
		_, err := pass.GetResult[int](m, prog)
		require.NoError(t, err)

		pl := pass.NewPipeline[*program.Program]()
		pl.Add(runner[*program.Program, bool](func(*pass.Manager, *program.Program) (bool, error) {
			return true, errors.New("fail")
		}))

		_, err = pl.Run(m, prog)
		require.EqualError(t, err, "fail")
		_, err = pass.GetResult[int](m, prog)

		require.NoError(t, err)
		require.Equal(t, 2, calls)
	})

	t.Run("stops on error", func(t *testing.T) {
		want := errors.New("fail")
		var log []string
		pl := pass.NewPipeline[*program.Program]()
		pl.Add(runner[*program.Program, bool](func(*pass.Manager, *program.Program) (bool, error) {
			log = append(log, "a")
			return true, want
		}))
		pl.Add(runner[*program.Program, bool](func(*pass.Manager, *program.Program) (bool, error) {
			log = append(log, "b")
			return true, nil
		}))

		_, err := pl.Run(pass.NewManager(), program.New(nil))
		require.ErrorIs(t, err, want)
		require.Equal(t, []string{"a"}, log)
	})
}
