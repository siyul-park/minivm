//go:build arm64

package arm64

import (
	"testing"

	"github.com/siyul-park/minivm/asm"
	"github.com/stretchr/testify/require"
)

func TestAssembler_Build(t *testing.T) {
	buffer, err := asm.NewBuffer(256)
	require.NoError(t, err)
	defer buffer.Free()

	a := asm.NewAssembler(Arch, buffer)

	left, _ := a.Take(asm.RegTypeInt)
	right, _ := a.Take(asm.RegTypeInt)
	result := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Push(result)
	a.Emit(ADD(result, left, right))
	a.Emit(RET())

	caller, err := a.Build()
	require.NoError(t, err)

	out, err := caller.Call([]uint64{3, 5})
	require.NoError(t, err)
	require.Equal(t, []uint64{8}, out)
}
