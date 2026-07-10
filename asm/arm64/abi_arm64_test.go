//go:build arm64

package arm64

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
)

func TestNew(t *testing.T) {
	a := asm.New(New())
	ctx := a.Reg(asm.RegTypeInt, asm.Width64)
	left := a.Reg(asm.RegTypeInt, asm.Width64)
	right := a.Reg(asm.RegTypeInt, asm.Width64)
	result := a.Reg(asm.RegTypeInt, asm.Width64)
	require.NoError(t, a.Pin(ctx, X0))
	a.Emit(LDR(left, ctx, 0))
	a.Emit(LDR(right, ctx, 8))
	a.Emit(ADD(result, left, right))
	a.Emit(STR(result, ctx, 16))
	a.Emit(RET())

	code, err := a.Build()
	require.NoError(t, err)
	buf, err := asm.NewBuffer(4096)
	require.NoError(t, err)
	defer buf.Free()
	linked, err := asm.Link(buf, New(), []*asm.Code{code}, nil)
	require.NoError(t, err)
	// Use the concrete caller so escape analysis keeps the fresh goroutine's
	// context on its stack while invoke grows and relocates that stack.
	callable, ok := linked[0].Callable.(*caller)
	require.True(t, ok)

	errs := make(chan error, 1)
	done := make(chan [3]uint64, 1)
	go func() {
		ctx := [3]uint64{20, 22}
		errs <- callable.Call(unsafe.Pointer(&ctx[0]))
		done <- ctx
	}()
	require.NoError(t, <-errs)
	require.Equal(t, [3]uint64{20, 22, 42}, <-done)
}
