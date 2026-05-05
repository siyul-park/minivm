package arm64

import (
	"fmt"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
)

type caller struct {
	header  uint64
	chunk   *asm.Chunk
	params  []asm.RegType
	returns []asm.RegType
}

var _ asm.Caller = (*caller)(nil)

const (
	maxParams  = 8
	maxReturns = 8
)

func NewCaller(sig *asm.Signature, chunk *asm.Chunk) (asm.Caller, error) {
	if len(sig.Params) > maxParams {
		return nil, fmt.Errorf("%w: %d", asm.ErrTooManyParams, len(sig.Params))
	}
	if len(sig.Returns) > maxReturns {
		return nil, fmt.Errorf("%w: %d", asm.ErrTooManyReturns, len(sig.Returns))
	}

	params := append([]asm.RegType(nil), sig.Params...)
	returns := append([]asm.RegType(nil), sig.Returns...)

	var paramTypes, returnTypes uint8
	for i, t := range params {
		if t == asm.RegTypeFloat {
			paramTypes |= 1 << uint(i)
		}
	}
	for i, t := range returns {
		if t == asm.RegTypeFloat {
			returnTypes |= 1 << uint(i)
		}
	}

	header := uint64(len(params)) | uint64(len(returns))<<8 | uint64(paramTypes)<<16 | uint64(returnTypes)<<24

	return &caller{
		header:  header,
		chunk:   chunk,
		params:  params,
		returns: returns,
	}, nil
}

func (c *caller) Params() []asm.RegType {
	return c.params
}

func (c *caller) Returns() []asm.RegType {
	return c.returns
}

func (c *caller) Call(args []uint64) ([]uint64, error) {
	var stack [1 + 8]uint64
	needed := 1 + max(len(c.params), len(c.returns))
	argv := stack[:needed]
	argv[0] = c.header
	copy(argv[1:], args[:min(len(args), len(c.params))])
	invoke(uintptr(c.chunk.Ptr()), uintptr(unsafe.Pointer(&argv[0])))
	return argv[1 : 1+len(c.returns)], nil
}
