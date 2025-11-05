//go:build unix || darwin || linux

package jit

import (
	"syscall"
	"unsafe"
)

func allocExecutable(size int) (uintptr, error) {
	data, err := syscall.Mmap(
		-1,
		0,
		size,
		syscall.PROT_READ|syscall.PROT_WRITE|syscall.PROT_EXEC,
		syscall.MAP_PRIVATE|syscall.MAP_ANONYMOUS,
	)
	if err != nil {
		return 0, err
	}
	return uintptr(unsafe.Pointer(&data[0])), nil
}

func freeExecutable(ptr uintptr, size int) error {
	data := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), size)
	return syscall.Munmap(data)
}
