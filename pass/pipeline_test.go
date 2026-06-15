package pass

import (
	"errors"
	"testing"

	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

// recordPass is a test transform that records its name when run.
type recordPass struct {
	name      string
	log       *[]string
	preserved Preserved
	err       error
}

func (p recordPass) Run(m *Manager, prog *program.Program) (Preserved, error) {
	*p.log = append(*p.log, p.name)
	return p.preserved, p.err
}

func TestPipeline_Run(t *testing.T) {
	t.Run("runs passes in order", func(t *testing.T) {
		var log []string
		pl := NewPipeline[*program.Program]()
		pl.AddPass(recordPass{name: "a", log: &log, preserved: PreserveAll()})
		pl.AddPass(recordPass{name: "b", log: &log, preserved: PreserveAll()})

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
		pl.AddPass(recordPass{name: "a", log: &log, preserved: PreserveAll(), err: want})
		pl.AddPass(recordPass{name: "b", log: &log, preserved: PreserveAll()})

		_, err := pl.Run(NewManager(), program.New(nil))
		require.ErrorIs(t, err, want)
		require.Equal(t, []string{"a"}, log)
	})
}
