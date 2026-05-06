package arm64

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewABI(t *testing.T) {
	a := NewABI()
	require.NotNil(t, a)
}

func TestABI_MaxParams(t *testing.T) {
	a := NewABI()
	require.GreaterOrEqual(t, a.MaxParams(), 4)
}

func TestABI_MaxReturns(t *testing.T) {
	a := NewABI()
	require.GreaterOrEqual(t, a.MaxReturns(), 1)
}
