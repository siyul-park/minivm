package arm64

import (
	"unsafe"

	"github.com/siyul-park/minivm/asm"
)

// abi implements asm.ABI for AArch64 + Go-style integer/float register
// passing (X0–X7 for ints, D0–D7 for floats). Up to abiArgs args and
// returns are supported.
type abi struct{}

const (
	// abiArgs is the maximum number of ABI args and returns the
	// trampoline supports — matching the AArch64 PCS register slots.
	abiArgs = 8

	// maxScratch is the upper bound on scratch slots carried across the
	// invoke trampoline. The trampoline preserves X10–X14.
	maxScratch = 5
)

var _ asm.ABI = abi{}

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
