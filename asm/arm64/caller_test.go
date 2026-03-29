package arm64

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCaller_Call(t *testing.T) {
	m, err := asm.Alloc(64)
	require.NoError(t, err)

	err = m.Write([]byte{
		0x00, 0x00, 0x00, 0x8B, // ADD X0, X0, X0
		0xC0, 0x03, 0x5F, 0xD6, // RET
	})
	require.NoError(t, err)

	err = m.Executable()
	require.NoError(t, err)

	h := NewHeader([]asm.RegType{asm.TypeInt}, []asm.RegType{asm.TypeInt})
	c := NewCaller(m, h)

	rets, err := c.Call([]uint64{1})
	require.NoError(t, err)
	require.Len(t, rets, 1)
	require.Equal(t, []uint64{2}, rets)
}
