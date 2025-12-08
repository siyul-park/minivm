package arm64

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/stretchr/testify/require"
	"testing"
	"unsafe"
)

func TestInvoke(t *testing.T) {
	e := asm.NewEmitter()
	e.Emit32(MOVZ(X0, 42, 0))
	e.Emit32(RET())

	c, err := asm.NewCode(e.Bytes())
	require.NoError(t, err)

	addr := c.Ptr()

	argv := []uint64{
		1,
		1,
		0,
		0,

		0,
		0,
		0,
		0,
		0,
		0,
		0,
		0,
	}
	invoke(uintptr(addr), uintptr((unsafe.Pointer)(&argv[0])))
	require.Equal(t, uint64(42), argv[4])
}
