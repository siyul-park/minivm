package arm64

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestADD(t *testing.T) {
	a := asm.NewAssembler()

	a.Emit(ADD(X0, X0, X0))
	a.Emit(RET())

	b, err := asm.NewBuffer(64)
	require.NoError(t, err)
	defer b.Free()

	ch, err := b.Append(a.Bytes())
	require.NoError(t, err)

	err = b.Seal()
	require.NoError(t, err)

	h := NewHeader([]asm.RegType{asm.TypeInt}, []asm.RegType{asm.TypeInt})
	c := NewCaller(ch, h)

	rets, err := c.Call([]uint64{1})
	require.NoError(t, err)
	require.Len(t, rets, 1)
	require.Equal(t, []uint64{2}, rets)
}
