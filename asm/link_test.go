package asm_test

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/stretchr/testify/require"
)

func TestLink(t *testing.T) {
	t.Run("nil buffer", func(t *testing.T) {
		_, err := asm.Link(nil, arm64.New(), nil, nil)
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	if runtime.GOARCH != "arm64" {
		t.Skipf("native invoke requires arm64, got %s", runtime.GOARCH)
	}

	t.Run("exposes internal entry", func(t *testing.T) {
		arch := arm64.New()

		a := asm.New(arch)
		ctx := a.Reg(asm.RegTypeInt, asm.Width64)
		v := a.Reg(asm.RegTypeInt, asm.Width64)
		require.NoError(t, a.Pin(ctx, arm64.X0))

		entry := a.Label()
		a.Emit(arm64.LDI(v, 3)...)
		a.Emit(arm64.STR(v, ctx, 0))
		a.Emit(arm64.RET())
		a.Entry(entry)
		a.Emit(arm64.LDR(v, ctx, 0))
		a.Emit(arm64.ADDI(v, v, 1))
		a.Emit(arm64.STR(v, ctx, 0))
		a.Emit(arm64.RET())

		code, err := a.Build()
		require.NoError(t, err)

		buf, err := asm.NewBuffer(4096)
		require.NoError(t, err)
		defer buf.Free()

		linked, err := asm.Link(buf, arch, []*asm.Code{code}, nil)
		require.NoError(t, err)
		require.Len(t, linked, 1)
		require.Contains(t, linked[0].Entries, entry)

		ctxBuf := []uint64{0}
		require.NoError(t, linked[0].Callable.Call(unsafe.Pointer(&ctxBuf[0])))
		require.Equal(t, uint64(3), ctxBuf[0])

		ctxBuf[0] = 41
		require.NoError(t, linked[0].Entries[entry].Call(unsafe.Pointer(&ctxBuf[0])))
		require.Equal(t, uint64(42), ctxBuf[0])
	})
}
