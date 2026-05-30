package amd64_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/amd64"
)

func TestArch(t *testing.T) {
	t.Run("encoder reports not implemented", func(t *testing.T) {
		arch := amd64.New()
		_, err := arch.Encoder().Encode(asm.Instruction{})
		require.ErrorIs(t, err, asm.ErrNotImplemented)
	})

	t.Run("ABI.NewCallable reports not implemented", func(t *testing.T) {
		arch := amd64.New()
		_, err := arch.ABI().NewCallable(asm.Signature{}, nil)
		require.ErrorIs(t, err, asm.ErrNotImplemented)
	})
}
