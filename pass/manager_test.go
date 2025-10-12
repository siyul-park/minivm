package pass

import (
	"testing"

	"github.com/siyul-park/minivm/program"
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
