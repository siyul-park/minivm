package arm64

import (
	"unsafe"

	"github.com/siyul-park/minivm/asm"
)

// abi implements asm.ABI for AArch64 context-pointer invocation.
type abi struct{}

// caller implements asm.Callable for an arm64 native entry. The trampoline
// passes ctx in X0 and preserves callee-saved registers used by JIT code.
type caller struct {
	addr unsafe.Pointer
}

var (
	_ asm.ABI      = abi{}
	_ asm.Callable = (*caller)(nil)
)

func (abi) NewCallable(addr unsafe.Pointer) (asm.Callable, error) {
	return &caller{addr: addr}, nil
}

func (c *caller) Call(ctx unsafe.Pointer) error {
	invoke(uintptr(c.addr), ctx)
	return nil
}

func (c *caller) Addr() unsafe.Pointer { return c.addr }
