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

	t.Run("add two args, return", func(t *testing.T) {
		arch := arm64.New()
		ab := arch.ABI()
		x0 := ab.Arg(0, asm.RegTypeInt, asm.Width64)
		x1 := ab.Arg(1, asm.RegTypeInt, asm.Width64)
		ret := ab.Return(0, asm.RegTypeInt, asm.Width64)

		a := asm.New(arch)
		va := a.Reg(asm.RegTypeInt, asm.Width64)
		vb := a.Reg(asm.RegTypeInt, asm.Width64)
		vr := a.Reg(asm.RegTypeInt, asm.Width64)

		require.NoError(t, a.Pin(va, x0))
		require.NoError(t, a.Pin(vb, x1))
		require.NoError(t, a.Pin(vr, ret))

		a.Emit(arm64.ADD(vr, va, vb))
		a.Emit(arm64.RET())

		code, err := a.Build(asm.Signature{Args: []asm.PReg{x0, x1}, Returns: []asm.PReg{ret}})
		require.NoError(t, err)
		require.NotEmpty(t, code.Bytes)
		require.Empty(t, code.Relocs)

		buf, err := asm.NewBuffer(4096)
		require.NoError(t, err)
		defer buf.Free()

		callables, err := asm.Link(buf, arch, []*asm.Code{code}, nil)
		require.NoError(t, err)
		require.Len(t, callables, 1)

		got, err := callables[0].Call([]asm.Value{asm.I64(3), asm.I64(4)}, nil)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, uint64(7), got[0].Bits())
	})
}

func TestAssembler_CanPin(t *testing.T) {
	arch := arm64.New()
	ab := arch.ABI()
	x0 := ab.Arg(0, asm.RegTypeInt, asm.Width64)
	x1 := ab.Arg(1, asm.RegTypeInt, asm.Width64)
	w0 := ab.Arg(0, asm.RegTypeInt, asm.Width32)
	d0 := ab.Arg(0, asm.RegTypeFloat, asm.Width64)

	a := asm.New(arch)
	v := a.Reg(asm.RegTypeInt, asm.Width64)

	require.True(t, a.CanPin(v, x0))
	require.NoError(t, a.Pin(v, x0))
	require.True(t, a.CanPin(v, x0))
	require.True(t, a.CanPin(v, w0))
	require.False(t, a.CanPin(v, x1))
	require.False(t, a.CanPin(v, d0))
}

func TestLinkAll(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skipf("native invoke requires arm64, got %s", runtime.GOARCH)
	}

	t.Run("exposes internal entry", func(t *testing.T) {
		arch := arm64.New()
		ab := arch.ABI()
		x0 := ab.Arg(0, asm.RegTypeInt, asm.Width64)
		ret := ab.Return(0, asm.RegTypeInt, asm.Width64)

		a := asm.New(arch)
		v := a.Reg(asm.RegTypeInt, asm.Width64)
		require.NoError(t, a.Pin(v, ret))

		entry := a.Label()
		a.Emit(arm64.LDI(v, 3)...)
		a.Emit(arm64.RET())
		a.Entry(entry, asm.Signature{Args: []asm.PReg{x0}, Returns: []asm.PReg{ret}})
		a.Emit(arm64.ADDI(v, v, 1))
		a.Emit(arm64.RET())

		code, err := a.Build(asm.Signature{Returns: []asm.PReg{ret}})
		require.NoError(t, err)

		buf, err := asm.NewBuffer(4096)
		require.NoError(t, err)
		defer buf.Free()

		linked, err := asm.LinkAll(buf, arch, []*asm.Code{code}, nil)
		require.NoError(t, err)
		require.Len(t, linked, 1)
		require.Contains(t, linked[0].Entries, entry)

		got, err := linked[0].Callable.Call(nil, nil)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, uint64(3), got[0].Bits())

		got, err = linked[0].Entries[entry].Call([]asm.Value{asm.I64(41)}, nil)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, uint64(42), got[0].Bits())
	})
}
