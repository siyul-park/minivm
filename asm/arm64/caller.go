package arm64

import (
	"fmt"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
)

type caller struct {
	header  uint64
	chunk   *asm.Chunk
	params  []asm.RegType // Reserved + Params (native input ABI order)
	returns []asm.RegType // Reserved + Returns (native output ABI order)
}

var _ asm.Caller = (*caller)(nil)

const (
	maxParams  = 8
	maxReturns = 8
)

func NewCaller(sig *asm.Signature, chunk *asm.Chunk) (asm.Caller, error) {
	// Reserved slots lead both the input and output register lists.
	allParams := make([]asm.RegType, 0, len(sig.Reserved)+len(sig.Params))
	allParams = append(allParams, sig.Reserved...)
	allParams = append(allParams, sig.Params...)

	allReturns := make([]asm.RegType, 0, len(sig.Reserved)+len(sig.Returns))
	allReturns = append(allReturns, sig.Reserved...)
	allReturns = append(allReturns, sig.Returns...)

	if len(allParams) > maxParams {
		return nil, fmt.Errorf("%w: %d", asm.ErrTooManyParams, len(allParams))
	}
	if len(allReturns) > maxReturns {
		return nil, fmt.Errorf("%w: %d", asm.ErrTooManyReturns, len(allReturns))
	}

	var paramTypes, returnTypes uint8
	for i, t := range allParams {
		if t == asm.RegTypeFloat {
			paramTypes |= 1 << uint(i)
		}
	}
	for i, t := range allReturns {
		if t == asm.RegTypeFloat {
			returnTypes |= 1 << uint(i)
		}
	}

	header := uint64(len(allParams)) |
		uint64(len(allReturns))<<8 |
		uint64(paramTypes)<<16 |
		uint64(returnTypes)<<24

	return &caller{
		header:  header,
		chunk:   chunk,
		params:  allParams,
		returns: allReturns,
	}, nil
}

func (c *caller) Params() []asm.RegType  { return c.params }
func (c *caller) Returns() []asm.RegType { return c.returns }

func (c *caller) Call(args []uint64) ([]uint64, error) {
	var stack [1 + 8]uint64
	needed := 1 + max(len(c.params), len(c.returns))
	argv := stack[:needed]
	argv[0] = c.header
	copy(argv[1:], args[:min(len(args), len(c.params))])
	invoke(uintptr(c.chunk.Ptr()), uintptr(unsafe.Pointer(&argv[0])))
	return argv[1 : 1+len(c.returns)], nil
}
