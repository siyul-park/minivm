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
//	bits[23:16] = nReserved  (≤ 6; scratch registers X10–X15 in order)
//	bits[31:24] = paramTypes  (float bitmask: bit i set ↔ Params[i] is float)
//	bits[39:32] = returnTypes (float bitmask)
//
// argv layout passed to invoke:
//
//	argv[0]:              header
//	argv[1..nReserved]:   scratch outputs — written after the call
//	argv[nReserved+1..]:  params in / returns out

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
	for i, p := range sig.Params {
		if p.ID() >= abiRegs {
			return nil, fmt.Errorf("%w: param[%d] register %v is outside", asm.ErrTooManyParams, i, p)
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
	nRsv := c.nReserved
	nParams := len(c.params)
	nReturns := len(c.returns)
	// argv: [header, reserved×nRsv, values×max(nParams,nReturns)]
	var stack [1 + 6 + 8]uint64 // 1 header + max 6 reserved + max 8 ABI regs
	argv := stack[:1+nRsv+max(nParams, nReturns)]
	argv[0] = c.header
	copy(argv[1+nRsv:], params[:min(len(params), nParams)])

	invoke(uintptr(c.chunk.Ptr()), uintptr(unsafe.Pointer(&argv[0])))

	if rsv != nil && nRsv > 0 {
		copy(*rsv, argv[1:1+nRsv])
	}
	return argv[1+nRsv : 1+nRsv+nReturns], nil
}
