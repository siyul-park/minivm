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
	require.Equal(t, maxParams, a.MaxParams())
}

func TestABI_MaxReturns(t *testing.T) {
	a := NewABI()
	require.Equal(t, maxReturns, a.MaxReturns())
}
