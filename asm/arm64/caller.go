package arm64

import (
	"fmt"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
)

// Header bit layout (must match abi_arm64.s):
//
//	bits[ 7: 0] = nParams
//	bits[15: 8] = nReturns
//	bits[23:16] = nScratch
//	bits[31:24] = paramTypes   (float bitmask, 1=float)
//	bits[39:32] = returnTypes  (float bitmask, 1=float)
//	bits[47:40] = paramWidths  (width bitmask, 1=64-bit)
//	bits[55:48] = returnWidths (width bitmask, 1=64-bit)

type caller struct {
	header  uint64
	chunk   *asm.Chunk
	params  []asm.PReg
	returns []asm.PReg
	scratch []asm.PReg
}

var _ asm.Caller = (*caller)(nil)

const (
	abiRegs    = 8 // X0–X7 / F0–F7: ABI parameter/return registers
	maxScratch = 6 // X10–X15: slots available in invoke trampoline
)

func NewCaller(sig *asm.Signature, chunk *asm.Chunk) (asm.Caller, error) {
	if len(sig.Params) > abiRegs {
		return nil, fmt.Errorf("%w: %d params exceed ABI limit of %d",
			asm.ErrTooManyParams, len(sig.Params), abiRegs)
	}
	if len(sig.Returns) > abiRegs {
		return nil, fmt.Errorf("%w: %d returns exceed ABI limit of %d",
			asm.ErrTooManyReturns, len(sig.Returns), abiRegs)
	}
	if len(sig.Scratch) > maxScratch {
		return nil, fmt.Errorf("%w: %d scratch registers exceed trampoline limit of %d",
			asm.ErrInvalidArgs, len(sig.Scratch), maxScratch)
	}

	for i, p := range sig.Params {
		if p.ID() >= abiRegs {
			return nil, fmt.Errorf("%w: param[%d] register %v (id=%d) outside ABI range [0,%d)",
				asm.ErrTooManyParams, i, p, p.ID(), abiRegs)
		}
	}
	for i, p := range sig.Returns {
		if p.ID() >= abiRegs {
			return nil, fmt.Errorf("%w: return[%d] register %v (id=%d) outside ABI range [0,%d)",
				asm.ErrTooManyReturns, i, p, p.ID(), abiRegs)
		}
	}
	for i, p := range sig.Scratch {
		// Scratch registers must be outside the ABI param/return range (X0–X7).
		// The invoke trampoline reserves X10–X15 for this purpose.
		if p.ID() < abiRegs {
			return nil, fmt.Errorf("%w: scratch[%d] register %v (id=%d) overlaps ABI range [0,%d)",
				asm.ErrInvalidArgs, i, p, p.ID(), abiRegs)
		}
	}

	var paramTypes, returnTypes uint8
	var paramWidths, returnWidths uint8

	for i, p := range sig.Params {
		if p.Type() == asm.RegTypeFloat {
			paramTypes |= 1 << uint(i)
		}
		if p.Width() == asm.Width64 {
			paramWidths |= 1 << uint(i)
		}
	}
	for i, p := range sig.Returns {
		if p.Type() == asm.RegTypeFloat {
			returnTypes |= 1 << uint(i)
		}
		if p.Width() == asm.Width64 {
			returnWidths |= 1 << uint(i)
		}
	}

	header := uint64(len(sig.Params)) |
		uint64(len(sig.Returns))<<8 |
		uint64(len(sig.Scratch))<<16 |
		uint64(paramTypes)<<24 |
		uint64(returnTypes)<<32 |
		uint64(paramWidths)<<40 |
		uint64(returnWidths)<<48

	return &caller{
		header:  header,
		chunk:   chunk,
		scratch: append([]asm.PReg(nil), sig.Scratch...),
		params:  append([]asm.PReg(nil), sig.Params...),
		returns: append([]asm.PReg(nil), sig.Returns...),
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
	nParams := len(c.params)
	nReturns := len(c.returns)
	nScratch := len(c.scratch)

	slots := nParams
	if nReturns > slots {
		slots = nReturns
	}

	// argv layout: [ header | scratch×nScratch | values×slots ]
	argv := make([]uint64, 1+nScratch+slots)
	argv[0] = c.header

	if rsv != nil && nScratch > 0 {
		n := nScratch
		if len(*rsv) < n {
			n = len(*rsv)
		}
		copy(argv[1:], (*rsv)[:n])
	}

	n := nParams
	if len(params) < n {
		n = len(params)
	}
	copy(argv[1+nScratch:], params[:n])

	invoke(uintptr(c.chunk.Ptr()), uintptr(unsafe.Pointer(&argv[0])))

	if rsv != nil && nScratch > 0 {
		if len(*rsv) < nScratch {
			*rsv = append((*rsv)[:len(*rsv)], make([]uint64, nScratch-len(*rsv))...)
		}
		copy(*rsv, argv[1:1+nScratch])
	}

	return append([]uint64(nil), argv[1+nScratch:1+nScratch+nReturns]...), nil
}
