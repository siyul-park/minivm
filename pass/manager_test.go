package pass

import (
	"errors"
	"testing"

	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// funcPass adapts a closure into a Pass for tests.
type funcPass[T any] struct {
	run func(*Manager) (T, error)
}

func (p funcPass[T]) Run(m *Manager) (T, error) {
	return p.run(m)
}

func TestManager_Register(t *testing.T) {
	t.Run("valid pass", func(t *testing.T) {
		m := NewManager()
		err := m.Register(funcPass[*program.Program]{run: func(m *Manager) (*program.Program, error) {
			var prog *program.Program
			if err := m.Load(&prog); err != nil {
				return nil, err
			}
			return prog, nil
		}})
		require.NoError(t, err)
	})

	t.Run("invalid pass", func(t *testing.T) {
		m := NewManager()
		require.ErrorIs(t, m.Register(struct{}{}), ErrInvalidPassType)
	})
}

func TestManager_Convert(t *testing.T) {
	t.Run("child pipeline", func(t *testing.T) {
		m := NewManager()
		require.NoError(t, m.Register(funcPass[*program.Program]{run: func(m *Manager) (*program.Program, error) {
			var prog *program.Program
			if err := m.Load(&prog); err != nil {
				return nil, err
			}
			var fn *types.Function
			if err := m.Convert(types.NewFunctionBuilder(nil).Emit(prog.Code).Build(), &fn); err != nil {
				return nil, err
			}
			return prog, nil
		}}))
		require.NoError(t, m.Register(funcPass[*types.Function]{run: func(m *Manager) (*types.Function, error) {
			var fn *types.Function
			if err := m.Load(&fn); err != nil {
				return nil, err
			}
			return fn, nil
		}}))

		err := m.Run(program.New(nil))
		require.NoError(t, err)
	})

	t.Run("propagates child error", func(t *testing.T) {
		want := errors.New("fail")
		m := NewManager()
		require.NoError(t, m.Register(funcPass[*types.Function]{run: func(m *Manager) (*types.Function, error) {
			return nil, want
		}}))

		var fn *types.Function
		err := m.Convert(types.NewFunction(nil, nil, nil), &fn)
		require.ErrorIs(t, err, want)
	})
}

func TestManager_Load(t *testing.T) {
	t.Run("cached value", func(t *testing.T) {
		m := NewManager()
		prog := program.New(nil)
		require.NoError(t, m.Run(prog))

		var got *program.Program
		require.NoError(t, m.Load(&got))
		require.Same(t, prog, got)
	})

	t.Run("runs registered pass once", func(t *testing.T) {
		calls := 0
		m := NewManager()
		require.NoError(t, m.Register(funcPass[*types.Function]{run: func(m *Manager) (*types.Function, error) {
			calls++
			return types.NewFunction(nil, nil, nil), nil
		}}))

		var first *types.Function
		require.NoError(t, m.Load(&first))
		var second *types.Function
		require.NoError(t, m.Load(&second))

		require.Same(t, first, second)
		require.Equal(t, 1, calls)
	})

	t.Run("loads from parent", func(t *testing.T) {
		m := NewManager()
		prog := program.New(nil)
		require.NoError(t, m.Run(prog))

		var got *program.Program
		require.NoError(t, m.Convert(types.NewFunction(nil, nil, nil), &got))
		require.Same(t, prog, got)
	})

	t.Run("invalid destination", func(t *testing.T) {
		m := NewManager()
		require.ErrorIs(t, m.Load((*program.Program)(nil)), ErrUnregisteredPassType)
		require.ErrorIs(t, m.Load(program.New(nil)), ErrUnregisteredPassType)
	})

	t.Run("unregistered type", func(t *testing.T) {
		m := NewManager()
		var prog *program.Program
		require.ErrorIs(t, m.Load(&prog), ErrUnregisteredPassType)
	})

	t.Run("propagates pass error", func(t *testing.T) {
		want := errors.New("fail")
		m := NewManager()
		require.NoError(t, m.Register(funcPass[*program.Program]{run: func(m *Manager) (*program.Program, error) {
			return nil, want
		}}))

		var prog *program.Program
		require.ErrorIs(t, m.Load(&prog), want)
	})
}

func TestManager_Run(t *testing.T) {
	t.Run("runs pass", func(t *testing.T) {
		m := NewManager()
		require.NoError(t, m.Register(funcPass[*program.Program]{run: func(m *Manager) (*program.Program, error) {
			var prog *program.Program
			if err := m.Load(&prog); err != nil {
				return nil, err
			}
			return prog, nil
		}}))

		err := m.Run(program.New(nil))
		require.NoError(t, err)
	})

	t.Run("propagates pass error", func(t *testing.T) {
		want := errors.New("fail")
		m := NewManager()
		require.NoError(t, m.Register(funcPass[*program.Program]{run: func(m *Manager) (*program.Program, error) {
			return nil, want
		}}))

		require.ErrorIs(t, m.Run(program.New(nil)), want)
	})
}
