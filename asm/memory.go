package asm

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

type memory []byte

func allocMemory(size int) (memory, error) {
	if size <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidSize, size)
	}

	page := os.Getpagesize()
	size = (size + page - 1) &^ (page - 1)

	data, err := syscall.Mmap(
		-1, 0, size,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_ANON|syscall.MAP_PRIVATE,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMmapFailed, err)
	}
	return data, nil
}

func (m memory) executable() error {
	if len(m) == 0 {
		return nil
	}
	m.flushICache()
	_, _, errno := syscall.Syscall(
		syscall.SYS_MPROTECT,
		uintptr(unsafe.Pointer(&m[0])),
		uintptr(len(m)),
		uintptr(syscall.PROT_READ|syscall.PROT_EXEC),
	)
	if errno != 0 {
		return fmt.Errorf("%w: %w", ErrMprotectFailed, errno)
	}
	return nil
}

func (m memory) writable() error {
	if len(m) == 0 {
		return nil
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_MPROTECT,
		uintptr(unsafe.Pointer(&m[0])),
		uintptr(len(m)),
		uintptr(syscall.PROT_READ|syscall.PROT_WRITE),
	)
	if errno != 0 {
		return fmt.Errorf("%w: %w", ErrMprotectFailed, errno)
	}
	return nil
}

func (m memory) ptr() unsafe.Pointer {
	if len(m) == 0 {
		return nil
	}
	return unsafe.Pointer(&m[0])
}

func (m memory) free() error {
	if len(m) == 0 {
		return nil
	}
	if err := syscall.Munmap(m); err != nil {
		return fmt.Errorf("%w: %w", ErrMunmapFailed, err)
	}
	return nil
}
