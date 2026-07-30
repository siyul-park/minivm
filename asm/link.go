package asm

import (
	"fmt"
)

// Link installs one Assembler.Build result into buf and binds it through the
// matching target ABI. The returned Callable stays valid until buf is freed.
func Link(buf *Buffer, abi ABI, code []byte) (Callable, error) {
	if buf == nil {
		return nil, fmt.Errorf("%w: nil buffer", ErrInvalidArgs)
	}
	if abi == nil {
		return nil, fmt.Errorf("%w: nil ABI", ErrInvalidArgs)
	}
	if len(code) == 0 {
		return nil, fmt.Errorf("%w: empty code", ErrInvalidArgs)
	}
	addr, err := buf.install(code)
	if err != nil {
		return nil, err
	}
	return abi.NewCallable(addr)
}
