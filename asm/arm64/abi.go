package arm64

import (
	"fmt"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
)

// abi implements asm.ABI for AArch64 + Go-style integer/float register
// passing (X0–X7 for ints, D0–D7 for floats). Up to abiArgs args and
// returns are supported.
type abi struct{}

// caller implements asm.Callable for an arm64 native entry. The trampoline
// at addr is invoked with a packed argv buffer carrying the header, scratch
// slots, and argument values; results land in the same argv buffer.
//
// The header is described in detail in abi_arm64.s.
type caller struct {
	addr unsafe.Pointer

	header   uint64
	argv     []uint64
	rets     []asm.Value
	retTypes []asm.PReg
	nArgs    int
	nScratch int
}

const (
	// abiArgs is the maximum number of ABI args and returns the
	// trampoline supports — matching the AArch64 PCS register slots.
	abiArgs = 8

	// maxScratch is the upper bound on scratch slots carried across the
	// invoke trampoline. The trampoline preserves X10–X14.
	maxScratch = 5
)

var (
	_ asm.ABI      = abi{}
	_ asm.Callable = (*caller)(nil)
)

func (abi) MaxArgs() int    { return abiArgs }
func (abi) MaxReturns() int { return abiArgs }

func (abi) Arg(idx int, t asm.RegType, w asm.RegWidth) asm.PReg {
	return asm.NewPReg(uint8(idx), t, w)
}

func (abi) Return(idx int, t asm.RegType, w asm.RegWidth) asm.PReg {
	return asm.NewPReg(uint8(idx), t, w)
}

// Scratch returns the architectural scratch registers carried across the
// trampoline boundary: X10..X14. Each is 64-bit integer.
func (abi) Scratch() []asm.PReg {
	return []asm.PReg{
		asm.NewPReg(10, asm.RegTypeInt, asm.Width64),
		asm.NewPReg(11, asm.RegTypeInt, asm.Width64),
		asm.NewPReg(12, asm.RegTypeInt, asm.Width64),
		asm.NewPReg(13, asm.RegTypeInt, asm.Width64),
		asm.NewPReg(14, asm.RegTypeInt, asm.Width64),
	}
}

func (abi) NewCallable(sig asm.Signature, addr unsafe.Pointer) (asm.Callable, error) {
	return newCaller(sig, addr)
}

func newCaller(sig asm.Signature, addr unsafe.Pointer) (*caller, error) {
	if len(sig.Args) > abiArgs {
		return nil, fmt.Errorf("%w: %d args exceed ABI limit of %d",
			asm.ErrTooManyArgs, len(sig.Args), abiArgs)
	}
	if len(sig.Returns) > abiArgs {
		return nil, fmt.Errorf("%w: %d returns exceed ABI limit of %d",
			asm.ErrTooManyReturns, len(sig.Returns), abiArgs)
	}
	if len(sig.Scratch) > maxScratch {
		return nil, fmt.Errorf("%w: %d scratch registers exceed trampoline limit of %d",
			asm.ErrInvalidArgs, len(sig.Scratch), maxScratch)
	}

	for i, p := range sig.Args {
		if p.ID() >= abiArgs {
			return nil, fmt.Errorf("%w: arg[%d] %v outside ABI range",
				asm.ErrTooManyArgs, i, p)
		}
	}
	for i, p := range sig.Returns {
		if p.ID() >= abiArgs {
			return nil, fmt.Errorf("%w: return[%d] %v outside ABI range",
				asm.ErrTooManyReturns, i, p)
		}
	}
	for i, p := range sig.Scratch {
		if p.ID() < abiArgs {
			return nil, fmt.Errorf("%w: scratch[%d] %v overlaps ABI range",
				asm.ErrInvalidArgs, i, p)
		}
	}

	returns := append([]asm.PReg(nil), sig.Returns...)
	nScratch := len(sig.Scratch)

	var aTyp, rTyp, aWid, rWid uint8
	for i, p := range sig.Args {
		if p.Type() == asm.RegTypeFloat {
			aTyp |= 1 << uint(i)
		}
		if p.Width() == asm.Width64 {
			aWid |= 1 << uint(i)
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

	c := &caller{
		addr: addr,
		header: uint64(len(sig.Args)) |
			uint64(len(returns))<<8 |
			uint64(nScratch)<<16 |
			uint64(aTyp)<<24 |
			uint64(rTyp)<<32 |
			uint64(aWid)<<40 |
			uint64(rWid)<<48,
		argv:     make([]uint64, 1+nScratch+abiArgs),
		rets:     make([]asm.Value, abiArgs),
		retTypes: returns,
		nArgs:    len(sig.Args),
		nScratch: nScratch,
	}
	return c, nil
}

func (c *caller) Addr() unsafe.Pointer { return c.addr }

func (c *caller) Call(args []asm.Value, scratch []uint64) ([]asm.Value, error) {
	if len(args) != c.nArgs {
		return nil, fmt.Errorf("%w: got %d args, want %d", asm.ErrInvalidArgs, len(args), c.nArgs)
	}

	nScratch := c.nScratch
	argv := c.argv
	argv[0] = c.header

	if nScratch > 0 && len(scratch) > 0 {
		copy(argv[1:1+nScratch], scratch[:min(nScratch, len(scratch))])
	}
	for i, v := range args {
		argv[1+nScratch+i] = v.Bits()
	}

	invoke(uintptr(c.addr), uintptr(unsafe.Pointer(&argv[0])))

	h := argv[0]
	nReturns := int((h >> 8) & 0xFF)
	if nReturns > abiArgs {
		return nil, fmt.Errorf("%w: callee returned %d values, max %d",
			asm.ErrTooManyReturns, nReturns, abiArgs)
	}

	if nScratch > 0 && len(scratch) >= nScratch {
		copy(scratch, argv[1:1+nScratch])
	}

	retTypes := uint8((h >> 32) & 0xFF)
	retWidths := uint8((h >> 48) & 0xFF)
	rets := c.rets[:nReturns]
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
