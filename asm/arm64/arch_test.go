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
