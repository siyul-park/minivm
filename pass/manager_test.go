package pass

import (
	"testing"

	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestManager_Register(t *testing.T) {
	m := NewManager()
	err := m.Register(NewPass[*program.Program](func(m *Manager) (*program.Program, error) {
		var prog *program.Program
		if err := m.Load(&prog); err != nil {
			return nil, err
		}
		return prog, nil
	}))
	require.NoError(t, err)
}

func TestManager_Convert(t *testing.T) {
	m := NewManager()
	_ = m.Register(NewPass[*program.Program](func(m *Manager) (*program.Program, error) {
		var prog *program.Program
		if err := m.Load(&prog); err != nil {
			return nil, err
		}
		var fn *types.Function
		if err := m.Convert(types.NewFunction(types.NewFunctionSignature(), prog.Code), &fn); err != nil {
			return nil, err
		}
		return prog, nil
	}))
	_ = m.Register(NewPass[*types.Function](func(m *Manager) (*types.Function, error) {
		var fn *types.Function
		if err := m.Load(&fn); err != nil {
			return nil, err
		}
		return fn, nil
	}))

	err := m.Run(program.New(nil))
	require.NoError(t, err)
}

func TestManager_Run(t *testing.T) {
	m := NewManager()
	_ = m.Register(NewPass[*program.Program](func(m *Manager) (*program.Program, error) {
		var prog *program.Program
		if err := m.Load(&prog); err != nil {
			return nil, err
		}
		return prog, nil
	}))

	err := m.Run(program.New(nil))
	require.NoError(t, err)
}
