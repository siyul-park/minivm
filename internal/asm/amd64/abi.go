package amd64

import (
	"unsafe"

	"github.com/siyul-park/minivm/internal/asm"
)

type abi struct{}

var _ asm.ABI = abi{}

func (abi) NewCallable(_ unsafe.Pointer) (asm.Callable, error) {
	return nil, asm.ErrNotImplemented
}
