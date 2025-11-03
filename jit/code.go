package jit

import (
	"syscall"
	"unsafe"
)

type Code struct {
	code  []byte
	fnPtr unsafe.Pointer
	size  int
}

func newCode(mmapData []byte, fnPtr unsafe.Pointer, size int) *Code {
	return &Code{
		code:  mmapData,
		fnPtr: fnPtr,
		size:  size,
	}
}

func (c *Code) Release() error {
	return syscall.Munmap(c.code)
}

func (c *Code) Execute(interpreter unsafe.Pointer) {
	fn := *(*func(unsafe.Pointer))(c.fnPtr)
	fn(interpreter)
}
