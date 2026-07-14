package pass

import (
	"errors"
	"testing"

	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestNewPipeline(t *testing.T) {
	require.NotNil(t, NewPipeline[*program.Program]())
}

func TestPipeline_AddPass(t *testing.T) {
	var log []string
	pipeline := NewPipeline[*program.Program]()
	pipeline.AddPass(runner[*program.Program, Preserved](func(*Manager, *program.Program) (Preserved, error) {
		log = append(log, "added")
		return PreserveAll(), nil
	}))
	_, err := pipeline.Run(NewManager(), program.New(nil))
	require.NoError(t, err)
	require.Equal(t, []string{"added"}, log)
}

func TestPipeline_Run(t *testing.T) {
	t.Run("runs passes in order", func(t *testing.T) {
		var log []string
		pl := NewPipeline[*program.Program]()
		pl.AddPass(runner[*program.Program, Preserved](func(*Manager, *program.Program) (Preserved, error) {
			log = append(log, "a")
			return PreserveAll(), nil
		}))
		pl.AddPass(runner[*program.Program, Preserved](func(*Manager, *program.Program) (Preserved, error) {
			log = append(log, "b")
			return PreserveAll(), nil
		}))

		prog := program.New(nil)
		got, err := pl.Run(NewManager(), prog)
		require.NoError(t, err)
		require.Same(t, prog, got)
		require.Equal(t, []string{"a", "b"}, log)
	})

	t.Run("stops on error", func(t *testing.T) {
		want := errors.New("fail")
		var log []string
		pl := NewPipeline[*program.Program]()
		pl.AddPass(runner[*program.Program, Preserved](func(*Manager, *program.Program) (Preserved, error) {
			log = append(log, "a")
			return PreserveAll(), want
		}))
		pl.AddPass(runner[*program.Program, Preserved](func(*Manager, *program.Program) (Preserved, error) {
			log = append(log, "b")
			return PreserveAll(), nil
		}))

		_, err := pl.Run(NewManager(), program.New(nil))
		require.ErrorIs(t, err, want)
		require.Equal(t, []string{"a"}, log)
	})
}
