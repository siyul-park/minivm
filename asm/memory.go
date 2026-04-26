package asm

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	ErrInvalidSize    = errors.New("asm: invalid size")
	ErrCodeTooLarge   = errors.New("asm: code too large for memory")
	ErrMmapFailed     = errors.New("asm: mmap failed")
	ErrMprotectFailed = errors.New("asm: mprotect failed")
	ErrMunmapFailed   = errors.New("asm: munmap failed")
)

type Memory []byte

func Alloc(size int) (Memory, error) {
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

func (m Memory) Write(code []byte) error {
	if len(code) > len(m) {
		return fmt.Errorf("%w: code size %d exceeds memory size %d", ErrCodeTooLarge, len(code), len(m))
	}
	copy(m, code)
	return nil
}

func (m Memory) Executable() error {
	if len(m) == 0 {
		return nil
	}
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

func (m Memory) Writable() error {
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

func (m Memory) Ptr() unsafe.Pointer {
	if len(m) == 0 {
		return nil
	}
	return unsafe.Pointer(&m[0])
}

func (m Memory) Free() error {
	if len(m) == 0 {
		return nil
	}
	if err := syscall.Munmap(m); err != nil {
		return fmt.Errorf("%w: %w", ErrMunmapFailed, err)
	}
	return nil
}
