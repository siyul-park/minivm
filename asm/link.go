package asm

import (
	"fmt"
)

// Link installs one Assembler.Build result into buf and binds it as a
// Callable through the architecture's ABI. The returned Callable stays valid
// for the lifetime of buf.
func Link(buf *Buffer, arch Arch, code []byte) (Callable, error) {
	if buf == nil {
		return nil, fmt.Errorf("%w: nil buffer", ErrInvalidArgs)
	}
	addr, err := buf.install(code)
	if err != nil {
		return nil, err
	}
	return arch.ABI().NewCallable(addr)
}
