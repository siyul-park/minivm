//go:build darwin || linux

package asm

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

type memory []byte

var (
	ErrInvalidSize    = errors.New("invalid size")
	ErrMmapFailed     = errors.New("mmap failed")
	ErrMprotectFailed = errors.New("mprotect failed")
	ErrMunmapFailed   = errors.New("munmap failed")
)

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

// within reports whether ptr's n-byte range lies inside m. The returned
// offset is the byte index of ptr from the start of m.
func (m memory) within(ptr unsafe.Pointer, n int) (int, bool) {
	if len(m) == 0 {
		return 0, false
	}
	base := uintptr(unsafe.Pointer(&m[0]))
	off := uintptr(ptr) - base
	if off > uintptr(len(m)) || off+uintptr(n) > uintptr(len(m)) {
		return 0, false
	}
	return int(off), true
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
