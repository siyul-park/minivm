package testenv_test

import (
	"runtime"
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestMustHaveNative(t *testing.T) {
	var skipped bool
	t.Run("probe", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		testenv.MustHaveNative(t)
	})

	require.Equal(t, !asm.Native, skipped)
}

func TestMustHaveArch(t *testing.T) {
	skips := func(archs ...string) bool {
		var skipped bool
		t.Run("probe", func(t *testing.T) {
			defer func() { skipped = t.Skipped() }()
			testenv.MustHaveArch(t, archs...)
		})
		return skipped
	}

	require.False(t, skips(runtime.GOARCH))
	require.False(t, skips("other", runtime.GOARCH))
	require.True(t, skips("other"))
	require.True(t, skips())
}
