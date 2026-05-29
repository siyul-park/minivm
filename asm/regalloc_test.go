package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRegAlloc(t *testing.T) {
	ra := NewRegAlloc(NewRegInfo(4, 2, nil, nil))
	require.NotNil(t, ra)
}

func TestRegAlloc_Alloc(t *testing.T) {
	t.Run("int", func(t *testing.T) {
		ra := NewRegAlloc(NewRegInfo(4, 2, nil, nil))
		v := NewVReg(0, RegTypeInt, Width64)
		p, err := ra.Alloc(v)
		require.NoError(t, err)
		require.Equal(t, RegTypeInt, p.Type())
	})
	t.Run("float", func(t *testing.T) {
		ra := NewRegAlloc(NewRegInfo(0, 2, nil, nil))
		v := NewVReg(0, RegTypeFloat, Width32)
		p, err := ra.Alloc(v)
		require.NoError(t, err)
		require.Equal(t, RegTypeFloat, p.Type())
	})
	t.Run("same vreg returns same preg", func(t *testing.T) {
		ra := NewRegAlloc(NewRegInfo(4, 2, nil, nil))
		v := NewVReg(0, RegTypeInt, Width64)
		p1, _ := ra.Alloc(v)
		p2, err := ra.Alloc(v)
		require.NoError(t, err)
		require.Equal(t, p1, p2)
	})
	t.Run("exhausted", func(t *testing.T) {
		ra := NewRegAlloc(NewRegInfo(2, 0, nil, nil))
		_, _ = ra.Alloc(NewVReg(0, RegTypeInt, Width64))
		_, _ = ra.Alloc(NewVReg(1, RegTypeInt, Width64))
		_, err := ra.Alloc(NewVReg(2, RegTypeInt, Width64))
		require.ErrorIs(t, err, ErrNoRegistersAvailable)
	})
}

func TestRegAlloc_Reserve(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		ra := NewRegAlloc(NewRegInfo(4, 2, nil, nil))
		v := NewVReg(0, RegTypeInt, Width64)
		p := NewPReg(2, RegTypeInt, Width64)
		require.NoError(t, ra.Reserve(v, p))
		got, err := ra.Alloc(v)
		require.NoError(t, err)
		require.Equal(t, uint8(2), got.ID())
	})
	t.Run("conflict", func(t *testing.T) {
		ra := NewRegAlloc(NewRegInfo(4, 2, nil, nil))
		p := NewPReg(0, RegTypeInt, Width64)
		require.NoError(t, ra.Reserve(NewVReg(0, RegTypeInt, Width64), p))
		require.ErrorIs(t, ra.Reserve(NewVReg(1, RegTypeInt, Width64), p), ErrNoRegistersAvailable)
	})
	t.Run("same mapping is idempotent", func(t *testing.T) {
		ra := NewRegAlloc(NewRegInfo(4, 2, nil, nil))
		v := NewVReg(0, RegTypeInt, Width64)
		p := NewPReg(0, RegTypeInt, Width64)
		require.NoError(t, ra.Reserve(v, p))
		require.NoError(t, ra.Reserve(v, p))
	})
}

func TestRegAlloc_Free(t *testing.T) {
	t.Run("frees slot for reuse", func(t *testing.T) {
		ra := NewRegAlloc(NewRegInfo(1, 0, nil, nil))
		v0 := NewVReg(0, RegTypeInt, Width64)
		_, _ = ra.Alloc(v0)
		ra.Free(v0)
		_, err := ra.Alloc(NewVReg(1, RegTypeInt, Width64))
		require.NoError(t, err)
	})
	t.Run("free unknown vreg is noop", func(t *testing.T) {
		ra := NewRegAlloc(NewRegInfo(4, 2, nil, nil))
		ra.Free(NewVReg(99, RegTypeInt, Width64))
	})
}

func TestRegAlloc_Reset(t *testing.T) {
	ra := NewRegAlloc(NewRegInfo(1, 0, nil, nil))
	v := NewVReg(0, RegTypeInt, Width64)
	ra.Alloc(v)
	ra.Reset()

	// The single register is available again after reset.
	_, err := ra.Alloc(NewVReg(1, RegTypeInt, Width64))
	require.NoError(t, err)
}
