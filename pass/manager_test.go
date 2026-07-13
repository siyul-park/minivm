package pass

import (
	"errors"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

// lenAnalysis is a test analysis whose result is the program's code length; it
// records how many times it runs so caching and invalidation can be observed.
type lenAnalysis struct{ calls *int }

func (a lenAnalysis) Run(m *Manager, prog *program.Program) (int, error) {
	if a.calls != nil {
		*a.calls++
	}
	return len(prog.Code), nil
}

// failAnalysis is a test analysis that always fails.
type failAnalysis struct{ err error }

func (a failAnalysis) Run(m *Manager, prog *program.Program) (int, error) {
	return 0, a.err
}

func TestNewManager(t *testing.T) {
	require.NotNil(t, NewManager())
}

func TestRegister(t *testing.T) {
	m := NewManager()
	Register[*program.Program, int](m, lenAnalysis{})

	got, err := GetResult[int](m, program.New([]instr.Instruction{instr.New(instr.NOP)}))
	require.NoError(t, err)
	require.Equal(t, 1, got)
}

func TestGetResult(t *testing.T) {
	t.Run("computes and caches", func(t *testing.T) {
		calls := 0
		m := NewManager()
		Register[*program.Program, int](m, lenAnalysis{calls: &calls})

		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		first, err := GetResult[int](m, prog)
		require.NoError(t, err)
		second, err := GetResult[int](m, prog)
		require.NoError(t, err)

		require.Equal(t, first, second)
		require.Equal(t, 1, calls)
	})

	t.Run("unregistered analysis", func(t *testing.T) {
		m := NewManager()
		_, err := GetResult[int](m, program.New(nil))
		require.ErrorIs(t, err, ErrUnregisteredAnalysis)
	})

	t.Run("propagates error", func(t *testing.T) {
		want := errors.New("fail")
		m := NewManager()
		Register[*program.Program, int](m, failAnalysis{err: want})

		_, err := GetResult[int](m, program.New(nil))
		require.ErrorIs(t, err, want)
	})
}

func TestManager_Invalidate(t *testing.T) {
	t.Run("drops cached results", func(t *testing.T) {
		calls := 0
		m := NewManager()
		Register[*program.Program, int](m, lenAnalysis{calls: &calls})

		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		_, err := GetResult[int](m, prog)
		require.NoError(t, err)
		m.Invalidate(PreserveNone())
		_, err = GetResult[int](m, prog)
		require.NoError(t, err)

		require.Equal(t, 2, calls)
	})

	t.Run("preserve all keeps results", func(t *testing.T) {
		calls := 0
		m := NewManager()
		Register[*program.Program, int](m, lenAnalysis{calls: &calls})

		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		_, err := GetResult[int](m, prog)
		require.NoError(t, err)
		m.Invalidate(PreserveAll())
		_, err = GetResult[int](m, prog)
		require.NoError(t, err)

		require.Equal(t, 1, calls)
	})
}

func TestPreserveAll(t *testing.T) {
	calls := 0
	m := NewManager()
	Register[*program.Program, int](m, lenAnalysis{calls: &calls})
	prog := program.New(nil)
	_, err := GetResult[int](m, prog)
	require.NoError(t, err)
	m.Invalidate(PreserveAll())
	_, err = GetResult[int](m, prog)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
}

func TestPreserveNone(t *testing.T) {
	calls := 0
	m := NewManager()
	Register[*program.Program, int](m, lenAnalysis{calls: &calls})
	prog := program.New(nil)
	_, err := GetResult[int](m, prog)
	require.NoError(t, err)
	m.Invalidate(PreserveNone())
	_, err = GetResult[int](m, prog)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}
