package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewAssembler(t *testing.T) {
	buf, err := NewBuffer(256)
	require.NoError(t, err)
	defer buf.Free()

	a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)
	require.NotNil(t, a)
}

func TestAssembler_NewVReg(t *testing.T) {
	buf, err := NewBuffer(256)
	require.NoError(t, err)
	defer buf.Free()
	a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

	r0 := a.NewVReg(RegTypeInt, Width64)
	r1 := a.NewVReg(RegTypeFloat, Width32)
	require.Equal(t, int32(0), r0.ID())
	require.Equal(t, RegTypeInt, r0.Type())
	require.Equal(t, int32(1), r1.ID())
	require.Equal(t, RegTypeFloat, r1.Type())
}

func TestAssembler_Take(t *testing.T) {
	t.Run("from stack", func(t *testing.T) {
		buf, err := NewBuffer(256)
		require.NoError(t, err)
		defer buf.Free()
		a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

		r := a.NewVReg(RegTypeInt, Width64)
		a.Push(r)
		taken, ok := a.Take(RegTypeInt, Width64)
		require.True(t, ok)
		require.Equal(t, r, taken)
		require.Empty(t, a.Params())
	})
	t.Run("type mismatch", func(t *testing.T) {
		buf, err := NewBuffer(256)
		require.NoError(t, err)
		defer buf.Free()
		a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

		a.Push(a.NewVReg(RegTypeInt, Width64))
		_, ok := a.Take(RegTypeFloat, Width32)
		require.False(t, ok)
	})
	t.Run("empty stack creates param", func(t *testing.T) {
		buf, err := NewBuffer(256)
		require.NoError(t, err)
		defer buf.Free()
		a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

		taken, ok := a.Take(RegTypeInt, Width64)
		require.True(t, ok)
		require.Equal(t, RegTypeInt, taken.Type())
		params := a.Params()
		require.Len(t, params, 1)
		require.Equal(t, taken, params[0])
	})
}

func TestAssembler_Top(t *testing.T) {
	t.Run("empty stack", func(t *testing.T) {
		buf, err := NewBuffer(256)
		require.NoError(t, err)
		defer buf.Free()
		a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

		_, ok := a.Top(0)
		require.False(t, ok)
	})
	t.Run("first and second element", func(t *testing.T) {
		buf, err := NewBuffer(256)
		require.NoError(t, err)
		defer buf.Free()
		a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

		r0, r1 := a.NewVReg(RegTypeInt, Width64), a.NewVReg(RegTypeInt, Width64)
		a.Push(r0)
		a.Push(r1)
		top0, ok := a.Top(0)
		require.True(t, ok)
		require.Equal(t, r1, top0)
		top1, ok := a.Top(1)
		require.True(t, ok)
		require.Equal(t, r0, top1)
	})
}

func TestAssembler_Push(t *testing.T) {
	buf, err := NewBuffer(256)
	require.NoError(t, err)
	defer buf.Free()
	a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

	r := a.NewVReg(RegTypeInt, Width64)
	a.Push(r)
	require.Len(t, a.Returns(), 1)
}

func TestAssembler_Pop(t *testing.T) {
	t.Run("non-empty stack", func(t *testing.T) {
		buf, err := NewBuffer(256)
		require.NoError(t, err)
		defer buf.Free()
		a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

		r := a.NewVReg(RegTypeInt, Width64)
		a.Push(r)
		got, ok := a.Pop()
		require.True(t, ok)
		require.Equal(t, r, got)
	})
	t.Run("empty stack", func(t *testing.T) {
		buf, err := NewBuffer(256)
		require.NoError(t, err)
		defer buf.Free()
		a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

		_, ok := a.Pop()
		require.False(t, ok)
	})
}

func TestAssembler_Params(t *testing.T) {
	buf, err := NewBuffer(256)
	require.NoError(t, err)
	defer buf.Free()
	a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

	require.Empty(t, a.Params())
	a.Take(RegTypeInt, Width64)
	require.Len(t, a.Params(), 1)
}

func TestAssembler_Returns(t *testing.T) {
	buf, err := NewBuffer(256)
	require.NoError(t, err)
	defer buf.Free()
	a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

	require.Empty(t, a.Returns())
	a.Push(a.NewVReg(RegTypeInt, Width64))
	require.Len(t, a.Returns(), 1)
}

func TestAssembler_Emit(t *testing.T) {
	buf, err := NewBuffer(256)
	require.NoError(t, err)
	defer buf.Free()
	a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

	r0 := NewPReg(0, RegTypeInt, Width64)
	r1 := NewPReg(1, RegTypeInt, Width64)
	idx0 := a.Emit(Instruction{Op: 1, Dst: P(r0), Src1: P(r1)})
	idx1 := a.Emit(Instruction{Op: 2})
	require.Equal(t, 0, idx0)
	require.Equal(t, 1, idx1)
}

func TestAssembler_Reset(t *testing.T) {
	buf, err := NewBuffer(256)
	require.NoError(t, err)
	defer buf.Free()
	a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

	a.Take(RegTypeInt, Width64)
	a.Push(a.NewVReg(RegTypeInt, Width64))
	a.Emit(Instruction{Op: 1})
	a.Reset()

	require.Empty(t, a.Params())
	require.Empty(t, a.Returns())
	require.Equal(t, int32(0), a.NewVReg(RegTypeInt, Width64).ID())
}
