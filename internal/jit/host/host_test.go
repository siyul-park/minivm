package host_test

import (
	"runtime"
	"testing"

	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit/host"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	m, ok := host.New()

	if runtime.GOARCH != "arm64" {
		require.False(t, ok)
		require.Nil(t, m)
		return
	}
	require.True(t, ok)
	require.Equal(t, arm64.New(), m.Arch())
}
