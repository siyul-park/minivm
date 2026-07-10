package asm_test

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
)

// TestAssembler_Build covers the public round-trip: Assembler.Build emits a
// Code with the encoded bytes for a trivial add+ret, then Link binds it to
// a buffer and the Callable executes natively.
func TestAssembler_Build(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skipf("native invoke requires arm64, got %s", runtime.GOARCH)
	}

	t.Run("context pointer round trip", func(t *testing.T) {
		arch := arm64.New()

		a := asm.New(arch)
		ctx := a.Reg(asm.RegTypeInt, asm.Width64)
		va := a.Reg(asm.RegTypeInt, asm.Width64)
		vb := a.Reg(asm.RegTypeInt, asm.Width64)
		vr := a.Reg(asm.RegTypeInt, asm.Width64)

		require.NoError(t, a.Pin(ctx, arm64.X0))

		a.Emit(arm64.LDR(va, ctx, 0))
		a.Emit(arm64.LDR(vb, ctx, 8))
		a.Emit(arm64.ADD(vr, va, vb))
		a.Emit(arm64.STR(vr, ctx, 16))
		a.Emit(arm64.RET())

		code, err := a.Build()
		require.NoError(t, err)
		require.NotEmpty(t, code.Bytes)
		require.Empty(t, code.Relocs)

		buf, err := asm.NewBuffer(4096)
		require.NoError(t, err)
		defer buf.Free()

		linked, err := asm.Link(buf, arch, []*asm.Code{code}, nil)
		require.NoError(t, err)
		require.Len(t, linked, 1)

		ctxBuf := [3]uint64{3, 4, 0}
		require.NoError(t, linked[0].Callable.Call(unsafe.Pointer(&ctxBuf[0])))
		require.Equal(t, [3]uint64{3, 4, 7}, ctxBuf)
	})

	t.Run("relaxes an out-of-range CBZ branch", func(t *testing.T) {
		arch := arm64.New()

		a := asm.New(arch)
		ctx := a.Reg(asm.RegTypeInt, asm.Width64)
		flag := a.Reg(asm.RegTypeInt, asm.Width64)
		filler := a.Reg(asm.RegTypeInt, asm.Width64)
		result := a.Reg(asm.RegTypeInt, asm.Width64)

		require.NoError(t, a.Pin(ctx, arm64.X0))

		zero := a.Label()

		a.Emit(arm64.LDR(flag, ctx, 0))
		a.Emit(arm64.CBZLabel(flag, zero))

		// Over 1MB of filler instructions pushes the CBZ's target past
		// its +-1MB imm19 range, forcing Assembler.encode to relax it.
		const fillerCount = 280_000
		a.Emit(arm64.LDI(filler, 1)...)
		for i := 0; i < fillerCount; i++ {
			a.Emit(arm64.ADDI(filler, filler, 1))
		}

		a.Emit(arm64.LDI(result, 1)...)
		a.Emit(arm64.STR(result, ctx, 8))
		a.Emit(arm64.RET())

		a.Bind(zero)
		a.Emit(arm64.LDI(result, 0)...)
		a.Emit(arm64.STR(result, ctx, 8))
		a.Emit(arm64.RET())

		code, err := a.Build()
		require.NoError(t, err)
		require.Greater(t, len(code.Bytes), 1<<20)

		buf, err := asm.NewBuffer(len(code.Bytes) + 4096)
		require.NoError(t, err)
		defer buf.Free()

		linked, err := asm.Link(buf, arch, []*asm.Code{code}, nil)
		require.NoError(t, err)

		notTaken := []uint64{1, 0xFF}
		require.NoError(t, linked[0].Callable.Call(unsafe.Pointer(&notTaken[0])))
		require.Equal(t, uint64(1), notTaken[1])

		taken := []uint64{0, 0xFF}
		require.NoError(t, linked[0].Callable.Call(unsafe.Pointer(&taken[0])))
		require.Equal(t, uint64(0), taken[1])
	})

	t.Run("relaxes an out-of-range B.cond branch", func(t *testing.T) {
		arch := arm64.New()

		a := asm.New(arch)
		ctx := a.Reg(asm.RegTypeInt, asm.Width64)
		flag := a.Reg(asm.RegTypeInt, asm.Width64)
		filler := a.Reg(asm.RegTypeInt, asm.Width64)
		result := a.Reg(asm.RegTypeInt, asm.Width64)

		require.NoError(t, a.Pin(ctx, arm64.X0))

		zero := a.Label()

		a.Emit(arm64.LDR(flag, ctx, 0))
		a.Emit(arm64.CMPI(flag, 0))
		a.Emit(arm64.BCondLabel(arm64.OpBEQ, zero))

		const fillerCount = 280_000
		a.Emit(arm64.LDI(filler, 1)...)
		for i := 0; i < fillerCount; i++ {
			a.Emit(arm64.ADDI(filler, filler, 1))
		}

		a.Emit(arm64.LDI(result, 1)...)
		a.Emit(arm64.STR(result, ctx, 8))
		a.Emit(arm64.RET())

		a.Bind(zero)
		a.Emit(arm64.LDI(result, 0)...)
		a.Emit(arm64.STR(result, ctx, 8))
		a.Emit(arm64.RET())

		code, err := a.Build()
		require.NoError(t, err)
		require.Greater(t, len(code.Bytes), 1<<20)

		buf, err := asm.NewBuffer(len(code.Bytes) + 4096)
		require.NoError(t, err)
		defer buf.Free()

		linked, err := asm.Link(buf, arch, []*asm.Code{code}, nil)
		require.NoError(t, err)

		notTaken := []uint64{1, 0xFF}
		require.NoError(t, linked[0].Callable.Call(unsafe.Pointer(&notTaken[0])))
		require.Equal(t, uint64(1), notTaken[1])

		taken := []uint64{0, 0xFF}
		require.NoError(t, linked[0].Callable.Call(unsafe.Pointer(&taken[0])))
		require.Equal(t, uint64(0), taken[1])
	})

	t.Run("spills under register pressure", func(t *testing.T) {
		arch := arm64.New()

		a := asm.New(arch)
		ctx := a.Reg(asm.RegTypeInt, asm.Width64)
		require.NoError(t, a.Pin(ctx, arm64.X0))

		// Hold far more values live at once than the integer bank has
		// allocatable registers, forcing the allocator to spill. Every
		// value stays live until the final fold, so the allocator must
		// keep spilling and reloading; a balanced SP frame is proven by
		// the call returning cleanly with the correct sum.
		const n = 256
		var want uint64
		vals := make([]asm.VReg, n)
		for i := 0; i < n; i++ {
			v := a.Reg(asm.RegTypeInt, asm.Width64)
			val := uint64(i*7 + 1)
			want += val
			a.Emit(arm64.LDI(v, val)...)
			vals[i] = v
		}

		acc := vals[0]
		for i := 1; i < n; i++ {
			next := a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.ADD(next, acc, vals[i]))
			acc = next
		}

		a.Emit(arm64.STR(acc, ctx, 0))
		a.Emit(arm64.RET())

		code, err := a.Build()
		require.NoError(t, err)
		require.NotEmpty(t, code.Bytes)

		buf, err := asm.NewBuffer(4096)
		require.NoError(t, err)
		defer buf.Free()

		linked, err := asm.Link(buf, arch, []*asm.Code{code}, nil)
		require.NoError(t, err)

		for range 64 {
			ctxBuf := [1]uint64{}
			done := make(chan error, 1)
			go func() {
				done <- linked[0].Callable.Call(unsafe.Pointer(&ctxBuf[0]))
			}()
			require.NoError(t, <-done)
			require.Equal(t, want, ctxBuf[0])
		}
	})
}

func TestAssembler_Pin(t *testing.T) {
	arch := arm64.New()

	a := asm.New(arch)
	v := a.Reg(asm.RegTypeInt, asm.Width64)

	require.NoError(t, a.Pin(v, arm64.X0))
	require.NoError(t, a.Pin(v, arm64.X0))
	require.ErrorIs(t, a.Pin(v, arm64.X1), asm.ErrConflictingPin)

	_, err := a.Build()
	require.ErrorIs(t, err, asm.ErrConflictingPin)
}

func TestAssembler_DisableSpilling(t *testing.T) {
	a := asm.New(arm64.New())
	a.DisableSpilling()

	const n = 64
	vals := make([]asm.VReg, n)
	for i := range vals {
		vals[i] = a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(vals[i], uint64(i+1))...)
	}
	acc := vals[0]
	for i := 1; i < len(vals); i++ {
		next := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADD(next, acc, vals[i]))
		acc = next
	}
	a.Emit(arm64.RET())

	_, err := a.Build()
	require.ErrorIs(t, err, asm.ErrNoRegistersAvailable)
}

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
