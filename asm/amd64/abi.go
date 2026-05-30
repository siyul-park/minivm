package amd64

import (
	"unsafe"

	"github.com/siyul-park/minivm/asm"
)

type abi struct{}

var _ asm.ABI = abi{}

func (abi) MaxArgs() int    { return 0 }
func (abi) MaxReturns() int { return 0 }

func (abi) Arg(idx int, t asm.RegType, w asm.RegWidth) asm.PReg {
	return asm.NewPReg(uint8(idx), t, w)
}

func (abi) Return(idx int, t asm.RegType, w asm.RegWidth) asm.PReg {
	return asm.NewPReg(uint8(idx), t, w)
}

func (abi) Scratch() []asm.PReg { return nil }

func (abi) NewCallable(_ asm.Signature, _ unsafe.Pointer) (asm.Callable, error) {
	return nil, asm.ErrNotImplemented
}
