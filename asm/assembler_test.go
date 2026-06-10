package asm_test

import (
	"runtime"
	"testing"

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

	t.Run("scratch argv round trip", func(t *testing.T) {
		arch := arm64.New()
		ab := arch.ABI()
		scratch := ab.Scratch()

		a := asm.New(arch)
		va := a.Reg(asm.RegTypeInt, asm.Width64)
		vb := a.Reg(asm.RegTypeInt, asm.Width64)
		vr := a.Reg(asm.RegTypeInt, asm.Width64)

		require.NoError(t, a.Pin(va, scratch[0]))
		require.NoError(t, a.Pin(vb, scratch[1]))
		require.NoError(t, a.Pin(vr, scratch[2]))

		a.Emit(arm64.ADD(vr, va, vb))
		a.Emit(arm64.RET())

		code, err := a.Build(asm.Signature{Scratch: scratch[:3]})
		require.NoError(t, err)
		require.NotEmpty(t, code.Bytes)
		require.Empty(t, code.Relocs)

		buf, err := asm.NewBuffer(4096)
		require.NoError(t, err)
		defer buf.Free()

		linked, err := asm.Link(buf, arch, []*asm.Code{code}, nil)
		require.NoError(t, err)
		require.Len(t, linked, 1)

		argv := []uint64{3, 4, 0, 0, 0}
		require.NoError(t, linked[0].Callable.Call(argv))
		require.Equal(t, []uint64{3, 4, 7, 0, 0}, argv)
	})

	t.Run("spills under register pressure", func(t *testing.T) {
		arch := arm64.New()
		scratch := arch.ABI().Scratch()

		a := asm.New(arch)

		// Hold far more values live at once than the integer bank has
		// allocatable registers, forcing the allocator to spill. Every
		// value stays live until the final fold, so the allocator must
		// keep spilling and reloading; a balanced SP frame is proven by
		// the call returning cleanly with the correct sum.
		const n = 64
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

		out := a.Reg(asm.RegTypeInt, asm.Width64)
		require.NoError(t, a.Pin(out, scratch[0]))
		a.Emit(arm64.MOV(out, acc))
		a.Emit(arm64.RET())

		code, err := a.Build(asm.Signature{Scratch: scratch[:1]})
		require.NoError(t, err)
		require.NotEmpty(t, code.Bytes)

		buf, err := asm.NewBuffer(4096)
		require.NoError(t, err)
		defer buf.Free()

		linked, err := asm.Link(buf, arch, []*asm.Code{code}, nil)
		require.NoError(t, err)

		argv := []uint64{0, 0, 0, 0, 0}
		require.NoError(t, linked[0].Callable.Call(argv))
		require.Equal(t, want, argv[0])
	})

	t.Run("variable scratch count", func(t *testing.T) {
		arch := arm64.New()
		scratch := arch.ABI().Scratch()

		a := asm.New(arch)
		v := a.Reg(asm.RegTypeInt, asm.Width64)
		require.NoError(t, a.Pin(v, scratch[0]))
		a.Emit(arm64.ADDI(v, v, 1))
		a.Emit(arm64.RET())

		code, err := a.Build(asm.Signature{Scratch: scratch[:1]})
		require.NoError(t, err)

		buf, err := asm.NewBuffer(4096)
		require.NoError(t, err)
		defer buf.Free()

		linked, err := asm.Link(buf, arch, []*asm.Code{code}, nil)
		require.NoError(t, err)

		// A single-scratch callable accepts an argv sized to its own
		// scratch count — no padding to the trampoline maximum.
		argv := []uint64{41}
		require.NoError(t, linked[0].Callable.Call(argv))
		require.Equal(t, []uint64{42}, argv)

		require.ErrorIs(t, linked[0].Callable.Call(nil), asm.ErrInvalidArgs)
	})
}

func TestAssembler_Pin(t *testing.T) {
	arch := arm64.New()
	ab := arch.ABI()
	x0 := ab.Scratch()[0]
	x1 := ab.Scratch()[1]

	a := asm.New(arch)
	v := a.Reg(asm.RegTypeInt, asm.Width64)

	require.NoError(t, a.Pin(v, x0))
	require.NoError(t, a.Pin(v, x0))
	require.ErrorIs(t, a.Pin(v, x1), asm.ErrConflictingPin)

	_, err := a.Build(asm.Signature{})
	require.ErrorIs(t, err, asm.ErrConflictingPin)
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
		ab := arch.ABI()
		scratch := ab.Scratch()

		a := asm.New(arch)
		v := a.Reg(asm.RegTypeInt, asm.Width64)
		require.NoError(t, a.Pin(v, scratch[0]))

		entry := a.Label()
		a.Emit(arm64.LDI(v, 3)...)
		a.Emit(arm64.RET())
		a.Entry(entry, asm.Signature{Scratch: scratch[:1]})
		a.Emit(arm64.ADDI(v, v, 1))
		a.Emit(arm64.RET())

		code, err := a.Build(asm.Signature{Scratch: scratch[:1]})
		require.NoError(t, err)

		buf, err := asm.NewBuffer(4096)
		require.NoError(t, err)
		defer buf.Free()

		linked, err := asm.Link(buf, arch, []*asm.Code{code}, nil)
		require.NoError(t, err)
		require.Len(t, linked, 1)
		require.Contains(t, linked[0].Entries, entry)

		argv := []uint64{0, 0, 0, 0, 0}
		require.NoError(t, linked[0].Callable.Call(argv))
		require.Equal(t, uint64(3), argv[0])

		argv[0] = 41
		require.NoError(t, linked[0].Entries[entry].Call(argv))
		require.Equal(t, uint64(42), argv[0])
	})
}
