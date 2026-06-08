package arm64

import (
	"fmt"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
)

// abi implements asm.ABI for AArch64 scratch-register invocation.
type abi struct{}

// caller implements asm.Callable for an arm64 native entry. The trampoline
// at addr is invoked with an argv buffer carrying scratch slots only.
type caller struct {
	addr     unsafe.Pointer
	nScratch int
}

const (
	// maxScratch is the upper bound on scratch slots carried across the
	// invoke trampoline. The trampoline maps slots onto X10–X14.
	maxScratch = 5
)

var (
	_ asm.ABI      = abi{}
	_ asm.Callable = (*caller)(nil)
)

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
	if len(sig.Scratch) > maxScratch {
		return nil, fmt.Errorf("%w: %d scratch registers exceed trampoline limit of %d",
			asm.ErrInvalidArgs, len(sig.Scratch), maxScratch)
	}

	for i, p := range sig.Scratch {
		if p.ID() < 10 || p.ID() > 14 || p.Type() != asm.RegTypeInt || p.Width() != asm.Width64 {
			return nil, fmt.Errorf("%w: scratch[%d] %v outside X10-X14 scratch range",
				asm.ErrInvalidArgs, i, p)
		}
	}

	return &caller{addr: addr, nScratch: len(sig.Scratch)}, nil
}

func (c *caller) Call(argv []uint64) error {
	if len(argv) < c.nScratch {
		return fmt.Errorf("%w: got %d scratch values, want %d", asm.ErrInvalidArgs, len(argv), c.nScratch)
	}
	// Build the header-prefixed buffer the trampoline expects: buf[0] is the
	// scratch count it reads to load exactly nScratch registers, buf[1:]
	// carries the values in and out. External callers never set the header.
	// The buffer is stack-allocated, so this adds no heap traffic on the hot
	// invocation path.
	var buf [maxScratch + 1]uint64
	buf[0] = uint64(c.nScratch)
	copy(buf[1:1+c.nScratch], argv[:c.nScratch])
	invoke(uintptr(c.addr), uintptr(unsafe.Pointer(&buf[0])))
	copy(argv[:c.nScratch], buf[1:1+c.nScratch])
	return nil
}
