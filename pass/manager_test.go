package pass

import (
	"testing"

	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestManager_Register(t *testing.T) {
	m := NewManager()
	err := m.Register(NewPass[any](func(m *Manager) (any, error) {
		return nil, nil
	}))
	require.NoError(t, err)
}

func TestManager_Run(t *testing.T) {
	m := NewManager()
	_ = m.Register(NewPass[any](func(m *Manager) (any, error) {
		return nil, nil
	}))

	err := m.Run(program.New(nil))
	require.NoError(t, err)
}
