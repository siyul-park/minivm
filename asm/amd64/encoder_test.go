package amd64_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/amd64"
)

func TestNew(t *testing.T) {
	t.Run("register info is present", func(t *testing.T) {
		arch := amd64.New()
		require.NotZero(t, arch.Registers())
	})

	t.Run("encoder reports not implemented", func(t *testing.T) {
		arch := amd64.New()
		_, err := arch.Encoder().Encode(asm.Instruction{})
		require.ErrorIs(t, err, asm.ErrNotImplemented)
	})

	t.Run("ABI.NewCallable reports not implemented", func(t *testing.T) {
		arch := amd64.New()
		_, err := arch.ABI().NewCallable(nil)
		require.ErrorIs(t, err, asm.ErrNotImplemented)
	})

	t.Run("frame is unsupported", func(t *testing.T) {
		arch := amd64.New()
		require.Nil(t, arch.Frame())
	})
}
