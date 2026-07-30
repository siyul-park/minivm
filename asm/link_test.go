package asm_test

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
)

func TestLink(t *testing.T) {
	t.Run("nil buffer", func(t *testing.T) {
		_, err := asm.Link(nil, arm64.New(), nil)
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	if runtime.GOARCH != "arm64" {
		t.Skipf("native invoke requires arm64, got %s", runtime.GOARCH)
	}

	t.Run("keeps earlier entries callable after the buffer grows", func(t *testing.T) {
		// A buffer sized for one entry must grow on the second install. The
		// outgoing mapping is retained and resealed, so the first Callable
		// stays executable and its Addr keeps pointing at live code.
		arch := arm64.New()

		buffer, err := asm.NewBuffer(1)
		require.NoError(t, err)
		defer buffer.Free()

		callables := make([]asm.Callable, 2)
		for i := range callables {
			assembler := asm.New(arch)
			ctx := assembler.Reg(asm.RegTypeInt, asm.Width64)
			value := assembler.Reg(asm.RegTypeInt, asm.Width64)
			require.NoError(t, assembler.Pin(ctx, arm64.X0))

			assembler.Emit(arm64.LDI(value, uint64(i+1))...)
			assembler.Emit(arm64.STR(value, ctx, 0))
			// Pad past a page so the second install cannot share the first
			// mapping.
			for range 1024 {
				assembler.Emit(arm64.ADDI(value, value, 0))
			}
			assembler.Emit(arm64.RET())

			code, err := assembler.Build()
			require.NoError(t, err)

			callables[i], err = asm.Link(buffer, arch, code)
			require.NoError(t, err)
			require.NotNil(t, callables[i].Addr())
		}

		for i, callable := range callables {
			slot := [1]uint64{}
			require.NoError(t, callable.Call(unsafe.Pointer(&slot[0])))
			require.Equal(t, uint64(i+1), slot[0])
		}
	})
}
