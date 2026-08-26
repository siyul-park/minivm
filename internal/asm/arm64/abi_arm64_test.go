//go:build arm64

package arm64_test

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
)

// TestNew assembles, links, and executes a minimal ARM64 routine through the
// public asm.Callable interface.
//
// This previously asserted linked.(*caller) so escape analysis could keep ctx
// on the goroutine's stack across invoke, exercising a Go-stack-resident
// context pointer surviving a stack growth and relocation. Calling through the
// interface heap-escapes ctx, so that property is no longer covered here;
// exporting the concrete type solely to preserve it would be test-only API
// widening, which docs/coding-patterns.md §12.2 forbids. The interp JIT tests
// exercise the same trampoline under real stack growth.
func TestNew(t *testing.T) {
	a := asm.New(arm64.New())
	ctx := a.Reg(asm.RegTypeInt, asm.Width64)
	left := a.Reg(asm.RegTypeInt, asm.Width64)
	right := a.Reg(asm.RegTypeInt, asm.Width64)
	result := a.Reg(asm.RegTypeInt, asm.Width64)
	require.NoError(t, a.Pin(ctx, arm64.X0))
	a.Emit(arm64.LDR(left, ctx, 0))
	a.Emit(arm64.LDR(right, ctx, 8))
	a.Emit(arm64.ADD(result, left, right))
	a.Emit(arm64.STR(result, ctx, 16))
	a.Emit(arm64.RET())

	code, err := a.Build()
	require.NoError(t, err)
	buf, err := asm.NewBuffer(4096)
	require.NoError(t, err)
	defer func() { require.NoError(t, buf.Free()) }()
	linked, err := asm.Link(buf, arm64.New().ABI(), code)
	require.NoError(t, err)

	errs := make(chan error, 1)
	done := make(chan [3]uint64, 1)
	go func() {
		ctx := [3]uint64{20, 22}
		errs <- linked.Call(unsafe.Pointer(&ctx[0]))
		done <- ctx
	}()
	require.NoError(t, <-errs)
	require.Equal(t, [3]uint64{20, 22, 42}, <-done)
}
