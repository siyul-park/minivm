//go:build arm64

package arm64

import (
	"testing"

	"github.com/siyul-park/minivm/asm"
	"github.com/stretchr/testify/require"
)

func TestAssembler_Compile(t *testing.T) {
	buffer, err := asm.NewBuffer(256)
	require.NoError(t, err)
	defer buffer.Free()

	a := asm.NewAssembler(Arch, buffer)

	left := a.NewVReg(asm.RegTypeInt, asm.Width64)
	right := a.NewVReg(asm.RegTypeInt, asm.Width64)
	result := a.NewVReg(asm.RegTypeInt, asm.Width64)
	require.NoError(t, a.Pin(left, asm.NewPReg(0, asm.RegTypeInt, asm.Width64)))
	require.NoError(t, a.Pin(right, asm.NewPReg(1, asm.RegTypeInt, asm.Width64)))
	require.NoError(t, a.Pin(result, asm.NewPReg(0, asm.RegTypeInt, asm.Width64)))
	a.Site(0, []asm.VReg{right, left})

	a.Emit(ADD(result, left, right))
	idx := a.Index()
	a.Site(idx, []asm.VReg{result})
	a.Emit(RET())

	obj, err := a.Compile()
	require.NoError(t, err)

	callers, err := a.Link([]*asm.RelocObject{obj})
	require.NoError(t, err)
	require.Len(t, callers, 1)
	require.NotNil(t, callers[0])

	out, err := callers[0].Call([]asm.Value{asm.I64(3), asm.I64(5)}, nil)
	require.NoError(t, err)
	require.Equal(t, []asm.Value{asm.I64(8)}, out)
}

func TestAssembler_CallerAt(t *testing.T) {
	buffer, err := asm.NewBuffer(256)
	require.NoError(t, err)
	defer buffer.Free()

	a := asm.NewAssembler(Arch, buffer)
	value := a.NewVReg(asm.RegTypeInt, asm.Width64)
	first := a.NewLabel()
	second := a.NewLabel()
	a.Entry(first, []asm.VReg{value})
	a.Emit(ADDI(value, value, 1))
	a.Entry(second, []asm.VReg{value})
	a.Emit(ADDI(value, value, 1))
	a.Site(a.Index(), []asm.VReg{value})
	a.Emit(RET())

	obj, err := a.Compile()
	require.NoError(t, err)
	_, err = a.Link([]*asm.RelocObject{obj})
	require.NoError(t, err)

	fromFirst, err := a.CallerAt(obj, first)
	require.NoError(t, err)
	fromSecond, err := a.CallerAt(obj, second)
	require.NoError(t, err)

	out, err := fromFirst.Call([]asm.Value{asm.I64(1)}, nil)
	require.NoError(t, err)
	require.Equal(t, []asm.Value{asm.I64(3)}, out)
	out, err = fromSecond.Call([]asm.Value{asm.I64(1)}, nil)
	require.NoError(t, err)
	require.Equal(t, []asm.Value{asm.I64(2)}, out)
}
