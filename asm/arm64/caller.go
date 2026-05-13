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
	chunk   *asm.Chunk
	sig     *asm.Signature
	scratch []asm.PReg
}

var _ asm.Caller = (*caller)(nil)

const (
	abiRegs    = 8 // X0–X7 / F0–F7: ABI parameter/return registers
	maxScratch = 5 // X10–X14: slots available in invoke trampoline
)

func NewCaller(sig *asm.Signature, chunk *asm.Chunk) (asm.Caller, error) {
	params := sig.Params(sig.Entry)
	if len(params) > abiRegs {
		return nil, fmt.Errorf("%w: %d params exceed ABI limit of %d",
			asm.ErrTooManyParams, len(params), abiRegs)
	}
	for idx, regs := range sig.Outputs {
		if len(regs) > abiRegs {
			return nil, fmt.Errorf("%w: %d returns at idx %d exceed ABI limit of %d",
				asm.ErrTooManyReturns, len(regs), idx, abiRegs)
		}
	}
	if len(sig.Scratch) > maxScratch {
		return nil, fmt.Errorf("%w: %d scratch registers exceed trampoline limit of %d",
			asm.ErrInvalidArgs, len(sig.Scratch), maxScratch)
	}
	for i, p := range params {
		if p.ID() >= abiRegs {
			return nil, fmt.Errorf("%w: param[%d] register %v (id=%d) outside ABI range [0,%d)",
				asm.ErrTooManyParams, i, p, p.ID(), abiRegs)
		}
	}
	for idx, regs := range sig.Outputs {
		for i, p := range regs {
			if p.ID() >= abiRegs {
				return nil, fmt.Errorf("%w: return[%d] at idx %d register %v (id=%d) outside ABI range [0,%d)",
					asm.ErrTooManyReturns, i, idx, p, p.ID(), abiRegs)
			}
		}
	}
	for i, p := range sig.Scratch {
		// Scratch registers must be outside the ABI param/return range (X0–X7).
		// The invoke trampoline reserves X10–X14 for this purpose.
		if p.ID() < abiRegs {
			return nil, fmt.Errorf("%w: scratch[%d] register %v (id=%d) overlaps ABI range [0,%d)",
				asm.ErrInvalidArgs, i, p, p.ID(), abiRegs)
		}
	}
	return &caller{
		chunk:   chunk,
		sig:     sig,
		scratch: append([]asm.PReg(nil), sig.Scratch...),
	}, nil
}

// Header encodes the ABI calling convention header for the invoke trampoline.
// R15 carries this value in/out across the call boundary (custom ABI).
// Any JIT callee must write R15 before returning to provide the output header.
func Header(params, returns []asm.PReg, nScratch int) uint64 {
	var pTyp, rTyp, pWid, rWid uint8
	for i, p := range params {
		if p.Type() == asm.RegTypeFloat {
			pTyp |= 1 << uint(i)
		}
		if p.Width() == asm.Width64 {
			pWid |= 1 << uint(i)
		}
	}
	for i, p := range returns {
		if p.Type() == asm.RegTypeFloat {
			rTyp |= 1 << uint(i)
		}
		if p.Width() == asm.Width64 {
			rWid |= 1 << uint(i)
		}
	}
	return uint64(len(params)) |
		uint64(len(returns))<<8 |
		uint64(nScratch)<<16 |
		uint64(pTyp)<<24 |
		uint64(rTyp)<<32 |
		uint64(pWid)<<40 |
		uint64(rWid)<<48
}

func (c *caller) Params(idx int) []asm.PReg  { return c.sig.Params(idx) }
func (c *caller) Returns(idx int) []asm.PReg { return c.sig.Returns(idx) }

func (c *caller) Call(params []asm.Value, rsv *[]uint64) ([]asm.Value, error) {
	nParams := len(params)
	nReturns := c.sig.MaxReturns()
	nScratch := len(c.scratch)
	slots := max(nParams, nReturns)

	// Build param physical registers and validate ABI range.
	paramRegs := make([]asm.PReg, nParams)
	iReg, fReg := uint8(0), uint8(0)
	for i, v := range params {
		if v.RegType() == asm.RegTypeFloat {
			if fReg >= abiRegs {
				return nil, fmt.Errorf("%w: too many float params", asm.ErrTooManyParams)
			}
			paramRegs[i] = asm.NewPReg(fReg, v.RegType(), v.Width())
			fReg++
		} else {
			if iReg >= abiRegs {
				return nil, fmt.Errorf("%w: too many int params", asm.ErrTooManyParams)
			}
			paramRegs[i] = asm.NewPReg(iReg, v.RegType(), v.Width())
			iReg++
		}
	}

	// argv layout: [ header | scratch×nScratch | values×slots ]
	argv := make([]uint64, 1+nScratch+slots)
	argv[0] = Header(paramRegs, c.sig.Returns(c.sig.Entry), nScratch)

	if rsv != nil && nScratch > 0 {
		copy(argv[1:], (*rsv)[:min(nScratch, len(*rsv))])
	}
	for i, v := range params {
		argv[1+nScratch+i] = v.Bits()
	}

	invoke(uintptr(c.chunk.Ptr()), uintptr(unsafe.Pointer(&argv[0])))

	h := argv[0]
	nReturns = int((h >> 8) & 0xFF)
	if nReturns > slots {
		return nil, fmt.Errorf("callee returned %d values but argv only has %d slots", nReturns, slots)
	}

	if rsv != nil && nScratch > 0 {
		if len(*rsv) < nScratch {
			*rsv = append(*rsv, make([]uint64, nScratch-len(*rsv))...)
		}
		copy(*rsv, argv[1:1+nScratch])
	}

	retTypes := uint8((h >> 32) & 0xFF)
	retWidths := uint8((h >> 48) & 0xFF)
	rets := make([]asm.Value, nReturns)
	for i, b := range argv[1+nScratch : 1+nScratch+nReturns] {
		isFloat := retTypes&(1<<uint(i)) != 0
		is64 := retWidths&(1<<uint(i)) != 0
		switch {
		case isFloat && is64:
			rets[i] = asm.F64(b)
		case isFloat:
			rets[i] = asm.F32(uint32(b))
		case is64:
			rets[i] = asm.I64(b)
		default:
			rets[i] = asm.I32(uint32(b))
		}
	}
	return rets, nil
}
