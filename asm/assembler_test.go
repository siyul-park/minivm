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

func TestNew(t *testing.T) {
	require.NotNil(t, asm.New(arm64.New()))
}

func TestAssembler_Reg(t *testing.T) {
	assembler := asm.New(arm64.New())
	first := assembler.Reg(asm.RegTypeInt, asm.Width64)
	second := assembler.Reg(asm.RegTypeFloat, asm.Width32)
	require.Equal(t, asm.NewVReg(0, asm.RegTypeInt, asm.Width64), first)
	require.Equal(t, asm.NewVReg(1, asm.RegTypeFloat, asm.Width32), second)
}

func TestAssembler_Label(t *testing.T) {
	assembler := asm.New(arm64.New())
	require.NotEqual(t, assembler.Label(), assembler.Label())
}

func TestAssembler_Bind(t *testing.T) {
	assembler := asm.New(arm64.New())
	label := assembler.Label()
	assembler.Bind(label)
	code, err := assembler.Build()
	require.NoError(t, err)
	require.Equal(t, 0, code.Labels[label])
}

func TestAssembler_Emit(t *testing.T) {
	assembler := asm.New(arm64.New())
	assembler.Emit(arm64.RET())
	code, err := assembler.Build()
	require.NoError(t, err)
	require.NotEmpty(t, code.Bytes)
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

func TestAssembler_Entry(t *testing.T) {
	// A non-primary entry only runs the shared epilogue on return, never the
	// prologue that reserves the spill area, so Build must reject the
	// combination explicitly instead of a call through that entry silently
	// corrupting SP.
	a := asm.New(arm64.New())

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

	entry := a.Label()
	a.Entry(entry)
	a.Emit(arm64.RET())

	_, err := a.Build()
	require.ErrorIs(t, err, asm.ErrEntryRequiresFrame)
}

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

	t.Run("arch without a frame rejects spilling", func(t *testing.T) {
		// An Arch whose Frame() returns nil disables spilling: allocation
		// fails with ErrNoRegistersAvailable instead of inserting a spill
		// frame. Callers that need this (e.g. interp's JIT policy for a
		// trace ending in a terminal heap mutation) wrap an existing Arch
		// rather than the Assembler exposing a dedicated toggle.
		a := asm.New(noFrameArch{arm64.New()})

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
	})

	t.Run("relaxes out-of-range branches", func(t *testing.T) {
		// Each case emits a branch whose target is pushed past its +-1MB
		// imm19 range by over 1MB of filler, forcing Assembler.encode to relax it.
		branches := []struct {
			name string
			emit func(a *asm.Assembler, flag asm.VReg, zero asm.Label)
		}{
			{"CBZ", func(a *asm.Assembler, flag asm.VReg, zero asm.Label) {
				a.Emit(arm64.CBZLabel(flag, zero))
			}},
			{"B.cond", func(a *asm.Assembler, flag asm.VReg, zero asm.Label) {
				a.Emit(arm64.CMPI(flag, 0))
				a.Emit(arm64.BCondLabel(arm64.OpBEQ, zero))
			}},
		}
		for _, tc := range branches {
			t.Logf("branch flavor: %s", tc.name)

			arch := arm64.New()

			a := asm.New(arch)
			ctx := a.Reg(asm.RegTypeInt, asm.Width64)
			flag := a.Reg(asm.RegTypeInt, asm.Width64)
			filler := a.Reg(asm.RegTypeInt, asm.Width64)
			result := a.Reg(asm.RegTypeInt, asm.Width64)

			require.NoError(t, a.Pin(ctx, arm64.X0))

			zero := a.Label()

			a.Emit(arm64.LDR(flag, ctx, 0))
			tc.emit(a, flag, zero)

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
		}
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

	t.Run("keeps the spill frame balanced across an external BL", func(t *testing.T) {
		// Regression check: the spill-frame injector used to re-reserve the
		// spill area after every BL to a LabelOperand, whether the label
		// resolved inside this Code (a self-call, which does run this Code's
		// epilogue on return) or externally via Link (which does not). The
		// stray reservation was never released, drifting SP on every call.
		arch := arm64.New()

		enc := arm64.NewEncoder()
		retBytes, err := enc.Encode(arm64.RET())
		require.NoError(t, err)

		buf, err := asm.NewBuffer(4096)
		require.NoError(t, err)
		defer buf.Free()

		// A bare RET stands in for an external callee: it proves nothing
		// about the callee's own frame, only that the caller's SP survives
		// a call whose target label is never bound in the caller's Code.
		stub, err := buf.Write(retBytes)
		require.NoError(t, err)

		a := asm.New(arch)
		ctx := a.Reg(asm.RegTypeInt, asm.Width64)
		require.NoError(t, a.Pin(ctx, arm64.X0))

		external := a.Label() // never bound in this Code: an external reloc

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

		// BL clobbers LR; save/restore it around the call as any real caller
		// must, so the test isolates the spill-frame bug rather than a
		// missing link-register save.
		savedLR := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.MOV(savedLR, arm64.LR))
		a.Emit(arm64.BLLabel(external))
		a.Emit(arm64.MOV(arm64.LR, savedLR))

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
		require.NotEmpty(t, code.Relocs)

		resolve := func(id asm.Label) (unsafe.Pointer, error) {
			if id == external {
				return stub, nil
			}
			return nil, asm.ErrUnresolvedLabel
		}
		linked, err := asm.Link(buf, arch, []*asm.Code{code}, resolve)
		require.NoError(t, err)

		for range 8 {
			ctxBuf := [1]uint64{}
			require.NoError(t, linked[0].Callable.Call(unsafe.Pointer(&ctxBuf[0])))
			require.Equal(t, want, ctxBuf[0])
		}
	})
}
