package asm

import "fmt"

// Link publishes code into buf and returns its executable address.
func Link(buf *Buffer, code []byte) (uintptr, error) {
	if buf == nil {
		return 0, fmt.Errorf("%w: nil buffer", ErrInvalidArgs)
	}
	if len(code) == 0 {
		return 0, fmt.Errorf("%w: empty code", ErrInvalidArgs)
	}
	addr, err := buf.install(code)
	if err != nil {
		return 0, err
	}
	return uintptr(addr), nil
}
