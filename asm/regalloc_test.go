package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func makeTestRegInfo() RegInfo {
	return NewRegInfo(8, 4, []uint8{}, []uint8{})
}

func TestNewRegAlloc(t *testing.T) {
	ra := NewRegAlloc(makeTestRegInfo())
	require.NotNil(t, ra)
}

func TestRegAlloc_Alloc(t *testing.T) {
	ra := NewRegAlloc(makeTestRegInfo())
	v0 := NewVReg(0, RegTypeInt, Width64)
	v1 := NewVReg(1, RegTypeFloat, Width32)

	p0, err := ra.Alloc(v0)
	require.NoError(t, err)
	require.Equal(t, RegTypeInt, p0.Type())

	p1, err := ra.Alloc(v1)
	require.NoError(t, err)
	require.Equal(t, RegTypeFloat, p1.Type())

	// Alloc same vreg returns same preg
	p0again, err := ra.Alloc(v0)
	require.NoError(t, err)
	require.Equal(t, p0, p0again)
}

func TestRegAlloc_Alloc_Exhausted(t *testing.T) {
	ri := NewRegInfo(2, 0, []uint8{}, []uint8{})
	ra := NewRegAlloc(ri)

	_, err := ra.Alloc(NewVReg(0, RegTypeInt, Width64))
	require.NoError(t, err)
	_, err = ra.Alloc(NewVReg(1, RegTypeInt, Width64))
	require.NoError(t, err)
	_, err = ra.Alloc(NewVReg(2, RegTypeInt, Width64))
	require.ErrorIs(t, err, ErrNoRegistersAvailable)
}

func TestRegAlloc_Reserve(t *testing.T) {
	ra := NewRegAlloc(makeTestRegInfo())
	v := NewVReg(0, RegTypeInt, Width64)
	p := NewPReg(3, RegTypeInt, Width64)

	err := ra.Reserve(v, p)
	require.NoError(t, err)

	got, ok := ra.Get(v)
	require.True(t, ok)
	require.Equal(t, p, got)

	// Same vreg, same preg: idempotent
	err = ra.Reserve(v, p)
	require.NoError(t, err)

	// Same vreg, different preg: conflict
	p2 := NewPReg(4, RegTypeInt, Width64)
	err = ra.Reserve(v, p2)
	require.ErrorIs(t, err, ErrNoRegistersAvailable)
}

func TestRegAlloc_Reserve_PRegAlreadyTaken(t *testing.T) {
	ra := NewRegAlloc(makeTestRegInfo())
	v0 := NewVReg(0, RegTypeInt, Width64)
	v1 := NewVReg(1, RegTypeInt, Width64)
	p := NewPReg(0, RegTypeInt, Width64)

	require.NoError(t, ra.Reserve(v0, p))
	err := ra.Reserve(v1, p)
	require.ErrorIs(t, err, ErrNoRegistersAvailable)
}

func TestRegAlloc_Free(t *testing.T) {
	ra := NewRegAlloc(makeTestRegInfo())
	v := NewVReg(0, RegTypeInt, Width64)

	p, err := ra.Alloc(v)
	require.NoError(t, err)

	ra.Free(v)
	_, ok := ra.Get(v)
	require.False(t, ok)

	// After free, the physical register should be available again
	v2 := NewVReg(1, RegTypeInt, Width64)
	p2, err := ra.Alloc(v2)
	require.NoError(t, err)
	require.Equal(t, p.ID(), p2.ID())

	// Freeing non-existent vreg is a no-op
	ra.Free(NewVReg(99, RegTypeInt, Width64))
}

func TestRegAlloc_Get(t *testing.T) {
	ra := NewRegAlloc(makeTestRegInfo())
	v := NewVReg(0, RegTypeInt, Width64)

	_, ok := ra.Get(v)
	require.False(t, ok)

	p, _ := ra.Alloc(v)
	got, ok := ra.Get(v)
	require.True(t, ok)
	require.Equal(t, p, got)
}

func TestRegAlloc_Reset(t *testing.T) {
	ra := NewRegAlloc(makeTestRegInfo())
	v := NewVReg(0, RegTypeInt, Width64)
	ra.Alloc(v)

	ra.Reset()
	_, ok := ra.Get(v)
	require.False(t, ok)

	// After reset all registers should be available again
	ri := NewRegInfo(2, 0, []uint8{}, []uint8{})
	ra2 := NewRegAlloc(ri)
	ra2.Alloc(NewVReg(0, RegTypeInt, Width64))
	ra2.Alloc(NewVReg(1, RegTypeInt, Width64))
	ra2.Reset()
	_, err := ra2.Alloc(NewVReg(2, RegTypeInt, Width64))
	require.NoError(t, err)
	_, err = ra2.Alloc(NewVReg(3, RegTypeInt, Width64))
	require.NoError(t, err)
}

func TestRegAlloc_Float_Free(t *testing.T) {
	ra := NewRegAlloc(makeTestRegInfo())
	v := NewVReg(0, RegTypeFloat, Width64)

	p, err := ra.Alloc(v)
	require.NoError(t, err)
	require.Equal(t, RegTypeFloat, p.Type())

	ra.Free(v)
	_, ok := ra.Get(v)
	require.False(t, ok)
}
