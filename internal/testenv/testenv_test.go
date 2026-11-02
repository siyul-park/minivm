package testenv_test

import (
	"runtime"
	"testing"

	"github.com/siyul-park/minivm/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestMustHaveNative(t *testing.T) {
	var skipped bool
	t.Run("probe", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		testenv.MustHaveNative(t)
	})

	require.Equal(t, runtime.GOARCH != "arm64", skipped)
}
