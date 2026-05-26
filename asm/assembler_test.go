package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewAssembler(t *testing.T) {
	buf, err := NewBuffer(256)
	require.NoError(t, err)
	defer buf.Free()

	a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil), ABI: testABI{}}, buf)
	require.NotNil(t, a)
}

type testABI struct{}

func (testABI) MaxParams() int  { return 8 }
func (testABI) MaxReturns() int { return 8 }
func (testABI) NewCaller(*Signature, *Chunk) (Caller, error) {
	return nil, nil
}

type testEncoder struct{}

func (testEncoder) Encode(Instruction) ([]byte, error) { return []byte{0}, nil }

func TestAssembler_NewVReg(t *testing.T) {
	buf, err := NewBuffer(256)
	require.NoError(t, err)
	defer buf.Free()
	a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil), ABI: testABI{}}, buf)

	r0 := a.NewVReg(RegTypeInt, Width64)
	r1 := a.NewVReg(RegTypeFloat, Width32)
	require.Equal(t, int32(0), r0.ID())
	require.Equal(t, RegTypeInt, r0.Type())
	require.Equal(t, int32(1), r1.ID())
	require.Equal(t, RegTypeFloat, r1.Type())
}

func TestAssembler_Pin(t *testing.T) {
	t.Run("single", func(t *testing.T) {
		buf, err := NewBuffer(256)
		require.NoError(t, err)
		defer buf.Free()
		a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil), ABI: testABI{}}, buf)

		v := a.NewVReg(RegTypeInt, Width64)
		require.NoError(t, a.Pin(v, NewPReg(0, RegTypeInt, Width64)))
	})

	t.Run("conflict", func(t *testing.T) {
		buf, err := NewBuffer(256)
		require.NoError(t, err)
		defer buf.Free()
		a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil), ABI: testABI{}}, buf)

		v := a.NewVReg(RegTypeInt, Width64)
		require.NoError(t, a.Pin(v, NewPReg(0, RegTypeInt, Width64)))
		err = a.Pin(v, NewPReg(1, RegTypeInt, Width64))
		require.ErrorIs(t, err, ErrConflictingPin)
	})
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

func TestAssembler_Site(t *testing.T) {
	buf, err := NewBuffer(256)
	require.NoError(t, err)
	defer buf.Free()
	a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil), ABI: testABI{}, Encoder: testEncoder{}}, buf)

	left := a.NewVReg(RegTypeInt, Width64)
	right := a.NewVReg(RegTypeInt, Width64)
	require.NoError(t, a.Pin(left, NewPReg(0, RegTypeInt, Width64)))
	require.NoError(t, a.Pin(right, NewPReg(1, RegTypeInt, Width64)))
	a.Site(0, []VReg{left})

	a.Emit(Instruction{Op: 1})
	first := a.Index()
	a.Site(first, []VReg{left})
	a.Emit(Instruction{Op: 2})
	second := a.Index()
	a.Site(second, []VReg{left, right})

	obj, err := a.Compile()
	require.NoError(t, err)
	require.Equal(t, []PReg{NewPReg(0, RegTypeInt, Width64)}, obj.Sig.Params)
	require.Equal(t, []PReg{NewPReg(0, RegTypeInt, Width64)}, obj.Sig.Returns[first])
	require.Equal(t, []PReg{
		NewPReg(0, RegTypeInt, Width64),
		NewPReg(1, RegTypeInt, Width64),
	}, obj.Sig.Returns[second])
}

func TestAssembler_Reset(t *testing.T) {
	buf, err := NewBuffer(256)
	require.NoError(t, err)
	defer buf.Free()
	a := NewAssembler(&Arch{Registers: NewRegInfo(8, 4, nil, nil)}, buf)

	v := a.NewVReg(RegTypeInt, Width64)
	_ = a.Pin(v, NewPReg(0, RegTypeInt, Width64))
	a.Site(0, []VReg{v})
	a.Emit(Instruction{Op: 1})
	a.Reset()

	require.Equal(t, int32(0), a.NewVReg(RegTypeInt, Width64).ID())
	require.Equal(t, 0, a.Index())
}
