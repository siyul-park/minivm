package arm64

import (
	"fmt"
	"github.com/siyul-park/minivm/asm"
	"unsafe"
)

type caller struct {
	mem    asm.Memory
	header Header
}

var _ asm.Caller = (*caller)(nil)

func NewCaller(mem asm.Memory, header Header) asm.Caller {
	return &caller{
		mem:    mem,
		header: header,
	}
}

func (c *caller) Call(args []uint64) ([]uint64, error) {
	if len(args) != c.header.Params() {
		return nil, fmt.Errorf("%w: expected %d, got %d", asm.ErrInvalidArgs, c.header.Params(), len(args))
	}

	argv := make([]uint64, 1+max(c.header.Params(), c.header.Returns()))
	argv[0] = uint64(c.header)
	copy(argv[1:], args)

	invoke(uintptr(c.mem.Func()), uintptr(unsafe.Pointer(&argv[0])))

	return argv[1 : 1+c.header.Returns()], nil
}
