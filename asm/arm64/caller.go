package arm64

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
)

type caller struct {
	header  uint64
	ptr     unsafe.Pointer
	params  []asm.RegType
	returns []asm.RegType
}

var _ asm.Caller = (*caller)(nil)

var (
	ErrTooManyParams  = errors.New("arm64: too many params (max 8)")
	ErrTooManyReturns = errors.New("arm64: too many returns (max 8)")
)

func NewCaller(sig *asm.Signature, chunk *asm.Chunk) (asm.Caller, error) {
	if len(sig.Params) > 8 {
		return nil, fmt.Errorf("%w: %d", ErrTooManyParams, len(sig.Params))
	}
	if len(sig.Returns) > 8 {
		return nil, fmt.Errorf("%w: %d", ErrTooManyReturns, len(sig.Returns))
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
		ptr:     chunk.Ptr(),
		params:  params,
		returns: returns,
	}, nil
}

func (c *caller) Call(args []uint64) ([]uint64, error) {
	if len(args) != len(c.params) {
		return nil, fmt.Errorf("%w: expected %d, got %d", asm.ErrInvalidArgs, len(c.params), len(args))
	}

	var stack [1 + 8]uint64
	needed := 1 + max(len(c.params), len(c.returns))
	argv := stack[:needed]
	argv[0] = c.header
	copy(argv[1:], args)

	invoke(uintptr(c.ptr), uintptr(unsafe.Pointer(&argv[0])))

	return argv[1 : 1+len(c.returns)], nil
}
