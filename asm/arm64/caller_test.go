//go:build arm64

package arm64

import (
	"testing"

	"github.com/siyul-park/minivm/asm"
	"github.com/stretchr/testify/require"
)

func TestCaller_Call(t *testing.T) {
	buf, err := asm.NewBuffer(64)
	require.NoError(t, err)

	defer buf.Free()

	chk, err := buf.Append([]byte{
		0x00, 0x00, 0x00, 0x8B, // ADD X0, X0, X0
		0xC0, 0x03, 0x5F, 0xD6, // RET
	})
	require.NoError(t, err)
	require.NoError(t, buf.Seal())

	sig := &asm.Signature{
		Params:  []asm.PReg{asm.NewPReg(0, asm.RegTypeInt, asm.Width64)},
		Returns: []asm.PReg{asm.NewPReg(0, asm.RegTypeInt, asm.Width64)},
	}

	c, err := NewCaller(sig, chk)
	require.NoError(t, err)

	rets, err := c.Call([]uint64{1}, nil)
	require.NoError(t, err)
	require.Len(t, rets, 1)
	require.Equal(t, []uint64{2}, rets)
}

func TestCaller_CallReservedInputOutput(t *testing.T) {
	buf, err := asm.NewBuffer(128)
	require.NoError(t, err)
	defer buf.Free()

	a := asm.NewAssembler(Arch, buf)
	stack := a.Reserve()
	heap := a.Reserve()
	next := a.Reserve()
	a.Emit(ADD(next, stack, heap))
	a.Emit(RET())

	obj, err := a.Compile()
	require.NoError(t, err)
	callers, err := a.Link([]*asm.RelocObject{obj})
	require.NoError(t, err)
	require.Len(t, callers, 1)

	rsv := []uint64{11, 31, 0}
	_, err = callers[0].Call(nil, &rsv)
	require.NoError(t, err)
	require.Equal(t, uint64(42), rsv[2])
}
