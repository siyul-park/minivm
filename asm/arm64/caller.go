package arm64

import (
	"fmt"
	"github.com/siyul-park/minivm/asm"
	"unsafe"
)

type caller struct {
	header     Header
	executable asm.Executable
	params     []asm.RegType
	returns    []asm.RegType
}

var _ asm.Caller = (*caller)(nil)

func NewCaller(header Header, mem asm.Executable) asm.Caller {
	return &caller{
		header:     header,
		executable: mem,
		params:     header.Params(),
		returns:    header.Returns(),
	}
}

func (c *caller) Params() []asm.RegType {
	return c.params
}

func (c *caller) Returns() []asm.RegType {
	return c.returns
}

func (c *caller) Call(args []uint64) ([]uint64, error) {
	if len(args) != len(c.params) {
		return nil, fmt.Errorf("%w: expected %d, got %d", asm.ErrInvalidArgs, len(c.params), len(args))
	}

	argv := make([]uint64, 1+max(len(c.params), len(c.returns)))
	argv[0] = uint64(c.header)
	copy(argv[1:], args)

	invoke(uintptr(c.executable.Func()), uintptr(unsafe.Pointer(&argv[0])))

	return argv[1 : 1+len(c.returns)], nil
}
