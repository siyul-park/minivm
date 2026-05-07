package arm64

import (
	"fmt"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
)

// Header bit layout:
//
//	bits[7:0]   = nParams
//	bits[15:8]  = nReturns
//	bits[23:16] = nReserved
//	bits[31:24] = paramTypes  (float bitmask: bit i set ↔ Params[i] is float)
//	bits[39:32] = returnTypes (float bitmask)
//	bits[47:40] = reservedTypes (always 0 — scratch regs are always int)

type caller struct {
	header    uint64
	chunk     *asm.Chunk
	params    []asm.PReg
	returns   []asm.PReg
	nReserved int
}

var _ asm.Caller = (*caller)(nil)

const abiRegs = 8 // X0-X7 / D0-D7

func NewCaller(sig *asm.Signature, chunk *asm.Chunk) (asm.Caller, error) {
	for _, p := range sig.Params {
		if p.ID() >= abiRegs {
			return nil, fmt.Errorf("%w: param[%d] register %v is outside", asm.ErrTooManyParams, len(sig.Params), p)
		}
	}
	for i, p := range sig.Returns {
		if p.ID() >= abiRegs {
			return nil, fmt.Errorf("%w: return[%d] register %v is outside", asm.ErrTooManyReturns, i, p)
		}
	}
	for i, p := range sig.Reserved {
		if p.ID() < abiRegs {
			return nil, fmt.Errorf("%w: reserved[%d] register %v outside", asm.ErrInvalidArgs, i, p)
		}
	}

	var paramTypes, returnTypes uint8
	for i, p := range sig.Params {
		if p.Type() == asm.RegTypeFloat {
			paramTypes |= 1 << uint(i)
		}
	}
	for i, p := range sig.Returns {
		if p.Type() == asm.RegTypeFloat {
			returnTypes |= 1 << uint(i)
		}
	}

	nReserved := len(sig.Reserved)
	header := uint64(len(sig.Params)) |
		uint64(len(sig.Returns))<<8 |
		uint64(nReserved)<<16 |
		uint64(paramTypes)<<24 |
		uint64(returnTypes)<<32

	return &caller{
		header:    header,
		chunk:     chunk,
		params:    append([]asm.PReg(nil), sig.Params...),
		returns:   append([]asm.PReg(nil), sig.Returns...),
		nReserved: nReserved,
	}, nil
}

func (c *caller) Params() []asm.RegType {
	out := make([]asm.RegType, len(c.params))
	for i, p := range c.params {
		out[i] = p.Type()
	}
	return out
}

func (c *caller) Returns() []asm.RegType {
	out := make([]asm.RegType, len(c.returns))
	for i, p := range c.returns {
		out[i] = p.Type()
	}
	return out
}

func (c *caller) Call(params []uint64, rsv *[]uint64) ([]uint64, error) {
	var stack [1 + 8]uint64
	needed := 1 + max(len(c.params), len(c.returns))
	argv := stack[:needed]
	argv[0] = c.header
	copy(argv[1:], params[:min(len(params), len(c.params))])

	var rsvPtr uintptr
	if rsv != nil && len(*rsv) > 0 {
		rsvPtr = uintptr(unsafe.Pointer(&(*rsv)[0]))
	}

	invoke(uintptr(c.chunk.Ptr()), uintptr(unsafe.Pointer(&argv[0])), rsvPtr)
	return argv[1 : 1+len(c.returns)], nil
}
