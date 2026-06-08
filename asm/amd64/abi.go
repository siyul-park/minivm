package amd64

import (
	"unsafe"

	"github.com/siyul-park/minivm/asm"
)

type abi struct{}

var _ asm.ABI = abi{}

func (abi) Scratch() []asm.PReg { return nil }

func (abi) NewCallable(_ asm.Signature, _ unsafe.Pointer) (asm.Callable, error) {
	return nil, asm.ErrNotImplemented
}
