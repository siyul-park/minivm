package asm_test

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/stretchr/testify/require"
)

func TestLink(t *testing.T) {
	t.Run("nil buffer", func(t *testing.T) {
		_, err := asm.Link(nil, arm64.New().ABI(), nil)
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	t.Run("nil ABI", func(t *testing.T) {
		buffer, err := asm.NewBuffer(1)
		require.NoError(t, err)
		defer func() { require.NoError(t, buffer.Free()) }()

		_, err = asm.Link(buffer, nil, []byte{0})
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	t.Run("empty code", func(t *testing.T) {
		buffer, err := asm.NewBuffer(1)
		require.NoError(t, err)
		defer func() { require.NoError(t, buffer.Free()) }()

		_, err = asm.Link(buffer, arm64.New().ABI(), nil)
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	t.Run("freed buffer", func(t *testing.T) {
		buffer, err := asm.NewBuffer(1)
		require.NoError(t, err)
		require.NoError(t, buffer.Free())

		_, err = asm.Link(buffer, arm64.New().ABI(), []byte{0})
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	if runtime.GOARCH != "arm64" {
		t.Skipf("native invoke requires arm64, got %s", runtime.GOARCH)
	}

	t.Run("keeps earlier entries callable after later installs", func(t *testing.T) {
		// Every install publishes a separate immutable mapping, so later
		// installs cannot invalidate an earlier asm.Callable or its Addr.
		arch := arm64.New()

		buffer, err := asm.NewBuffer(1)
		require.NoError(t, err)
		defer func() { require.NoError(t, buffer.Free()) }()

		callables := make([]asm.Callable, 2)
		for i := range callables {
			assembler := asm.New(arch)
			ctx := assembler.Reg(asm.RegTypeInt, asm.Width64)
			value := assembler.Reg(asm.RegTypeInt, asm.Width64)
			require.NoError(t, assembler.Pin(ctx, arm64.X0))

			assembler.Emit(arm64.LDI(value, uint64(i+1))...)
			assembler.Emit(arm64.STR(value, ctx, 0))
			// Span multiple pages to exercise differently sized mappings.
			for range 1024 {
				assembler.Emit(arm64.ADDI(value, value, 0))
			}
			assembler.Emit(arm64.RET())

			code, err := assembler.Build()
			require.NoError(t, err)

			callables[i], err = asm.Link(buffer, arch.ABI(), code)
			require.NoError(t, err)
			require.NotNil(t, callables[i].Addr())
		}

		for i, callable := range callables {
			slot := [1]uint64{}
			require.NoError(t, callable.Call(unsafe.Pointer(&slot[0])))
			require.Equal(t, uint64(i+1), slot[0])
		}
	})

	t.Run("keeps an entry executable during later installs", func(t *testing.T) {
		previous := runtime.GOMAXPROCS(2)
		defer runtime.GOMAXPROCS(previous)

		arch := arm64.New()
		assembler := asm.New(arch)
		ctx := assembler.Reg(asm.RegTypeInt, asm.Width64)
		started := assembler.Reg(asm.RegTypeInt, asm.Width64)
		stop := assembler.Reg(asm.RegTypeInt, asm.Width64)
		loop := assembler.Label()
		require.NoError(t, assembler.Pin(ctx, arm64.X0))

		assembler.Emit(arm64.LDI(started, 1)...)
		// Pair native ordinary accesses with Go's atomic acquire/release
		// operations using ARM64's full inner-shareable barrier.
		assembler.Emit(arm64.DMB())
		assembler.Emit(arm64.STR(started, ctx, 0))
		assembler.Bind(loop)
		assembler.Emit(arm64.LDR(stop, ctx, 8))
		assembler.Emit(arm64.DMB())
		assembler.Emit(arm64.CBZLabel(stop, loop))
		assembler.Emit(arm64.RET())

		code, err := assembler.Build()
		require.NoError(t, err)

		buffer, err := asm.NewBuffer(4096)
		require.NoError(t, err)

		callable, err := asm.Link(buffer, arch.ABI(), code)
		require.NoError(t, err)

		state := [2]uint64{}
		done := make(chan struct{})
		var callErr error
		go func() {
			callErr = callable.Call(unsafe.Pointer(&state[0]))
			close(done)
		}()
		joined := false
		defer func() {
			atomic.StoreUint64(&state[1], 1)
			if !joined {
				select {
				case <-done:
					require.NoError(t, callErr)
				case <-time.After(time.Second):
					t.Error("callable did not return during cleanup")
					return
				}
			}
			require.NoError(t, buffer.Free())
		}()

		require.Eventually(t, func() bool {
			return atomic.LoadUint64(&state[0]) == 1
		}, time.Second, time.Millisecond)

		for range 32 {
			_, err = asm.Link(buffer, arch.ABI(), code)
			require.NoError(t, err)
		}

		atomic.StoreUint64(&state[1], 1)
		select {
		case <-done:
			joined = true
			require.NoError(t, callErr)
		case <-time.After(time.Second):
			t.Fatal("callable did not return")
		}
	})
}
