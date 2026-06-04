package jit_test

import (
	"runtime"
	"testing"

	"github.com/siyul-park/minivm/jit"
	_ "github.com/siyul-park/minivm/jit/amd64"
	_ "github.com/siyul-park/minivm/jit/arm64"
	"github.com/stretchr/testify/require"
)

func TestLookup(t *testing.T) {
	t.Run("amd64 stub stays unregistered", func(t *testing.T) {
		require.Nil(t, jit.Lookup("amd64"))
	})

	t.Run("arm64 backend registers", func(t *testing.T) {
		require.NotNil(t, jit.Lookup("arm64"))
	})
}

func TestActive(t *testing.T) {
	t.Run("reports no jit on amd64", func(t *testing.T) {
		if runtime.GOARCH != "amd64" {
			t.Skipf("requires amd64, got %s", runtime.GOARCH)
		}
		require.Nil(t, jit.Active())
	})

	t.Run("reports registered lowerer on arm64", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skipf("requires arm64, got %s", runtime.GOARCH)
		}
		require.NotNil(t, jit.Active())
	})
}
