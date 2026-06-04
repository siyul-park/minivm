package amd64_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/amd64"
)

func TestArch(t *testing.T) {
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
		_, err := arch.ABI().NewCallable(asm.Signature{}, nil)
		require.ErrorIs(t, err, asm.ErrNotImplemented)
	})
}

func TestABI(t *testing.T) {
	ab := amd64.New().ABI()

	require.Zero(t, ab.MaxArgs())
	require.Zero(t, ab.MaxReturns())
	require.Equal(t, asm.NewPReg(1, asm.RegTypeInt, asm.Width64), ab.Arg(1, asm.RegTypeInt, asm.Width64))
	require.Equal(t, asm.NewPReg(2, asm.RegTypeFloat, asm.Width32), ab.Return(2, asm.RegTypeFloat, asm.Width32))
	require.Empty(t, ab.Scratch())
}
