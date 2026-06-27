//go:build !darwin && !linux

package asm

import (
	"errors"
	"fmt"
	"runtime"
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
	return nil, fmt.Errorf("%w: unsupported platform %s/%s", ErrMmapFailed, runtime.GOOS, runtime.GOARCH)
}

func (m memory) executable() error {
	if len(m) == 0 {
		return nil
	}
	return fmt.Errorf("%w: unsupported platform %s/%s", ErrMprotectFailed, runtime.GOOS, runtime.GOARCH)
}

func (m memory) writable() error {
	if len(m) == 0 {
		return nil
	}
	return fmt.Errorf("%w: unsupported platform %s/%s", ErrMprotectFailed, runtime.GOOS, runtime.GOARCH)
}

func (m memory) ptr() unsafe.Pointer {
	if len(m) == 0 {
		return nil
	}
	return unsafe.Pointer(&m[0])
}

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
	return fmt.Errorf("%w: unsupported platform %s/%s", ErrMunmapFailed, runtime.GOOS, runtime.GOARCH)
}
