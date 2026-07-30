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
	pipeline.Add(runner[*program.Program, pass.Preserved](func(*pass.Manager, *program.Program) (pass.Preserved, error) {
		log = append(log, "added")
		return pass.PreserveAll(), nil
	}))
	_, err := pipeline.Run(pass.NewManager(), program.New(nil))
	require.NoError(t, err)
	require.Equal(t, []string{"added"}, log)
}

func TestPipeline_Run(t *testing.T) {
	t.Run("runs passes in order", func(t *testing.T) {
		var log []string
		pl := pass.NewPipeline[*program.Program]()
		pl.Add(runner[*program.Program, pass.Preserved](func(*pass.Manager, *program.Program) (pass.Preserved, error) {
			log = append(log, "a")
			return pass.PreserveAll(), nil
		}))
		pl.Add(runner[*program.Program, pass.Preserved](func(*pass.Manager, *program.Program) (pass.Preserved, error) {
			log = append(log, "b")
			return pass.PreserveAll(), nil
		}))

		prog := program.New(nil)
		got, err := pl.Run(pass.NewManager(), prog)
		require.NoError(t, err)
		require.Same(t, prog, got)
		require.Equal(t, []string{"a", "b"}, log)
	})

	t.Run("stops on error", func(t *testing.T) {
		want := errors.New("fail")
		var log []string
		pl := pass.NewPipeline[*program.Program]()
		pl.Add(runner[*program.Program, pass.Preserved](func(*pass.Manager, *program.Program) (pass.Preserved, error) {
			log = append(log, "a")
			return pass.PreserveAll(), want
		}))
		pl.Add(runner[*program.Program, pass.Preserved](func(*pass.Manager, *program.Program) (pass.Preserved, error) {
			log = append(log, "b")
			return pass.PreserveAll(), nil
		}))

		_, err := pl.Run(pass.NewManager(), program.New(nil))
		require.ErrorIs(t, err, want)
		require.Equal(t, []string{"a"}, log)
	})
}
