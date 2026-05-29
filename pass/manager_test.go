package pass

import (
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
	m := NewManager()
	err := m.Register(funcPass[*program.Program]{run: func(m *Manager) (*program.Program, error) {
		var prog *program.Program
		if err := m.Load(&prog); err != nil {
			return nil, err
		}
		return prog, nil
	}})
	require.NoError(t, err)
}

func TestManager_Convert(t *testing.T) {
	m := NewManager()
	_ = m.Register(funcPass[*program.Program]{run: func(m *Manager) (*program.Program, error) {
		var prog *program.Program
		if err := m.Load(&prog); err != nil {
			return nil, err
		}
		var fn *types.Function
		if err := m.Convert(types.NewFunctionBuilder(nil).Emit(prog.Code).Build(), &fn); err != nil {
			return nil, err
		}
		return prog, nil
	}})
	_ = m.Register(funcPass[*types.Function]{run: func(m *Manager) (*types.Function, error) {
		var fn *types.Function
		if err := m.Load(&fn); err != nil {
			return nil, err
		}
		return fn, nil
	}})

	err := m.Run(program.New(nil))
	require.NoError(t, err)
}

func TestManager_Run(t *testing.T) {
	m := NewManager()
	_ = m.Register(funcPass[*program.Program]{run: func(m *Manager) (*program.Program, error) {
		var prog *program.Program
		if err := m.Load(&prog); err != nil {
			return nil, err
		}
		return prog, nil
	}})

	err := m.Run(program.New(nil))
	require.NoError(t, err)
}
