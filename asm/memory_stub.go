//go:build !darwin && !linux

package asm

import (
	"errors"
	"fmt"
	"runtime"
)

type memory []byte

var (
	ErrInvalidSize    = errors.New("invalid size")
	ErrMmapFailed     = errors.New("mmap failed")
	ErrMprotectFailed = errors.New("mprotect failed")
	ErrMunmapFailed   = errors.New("munmap failed")
)

func (m memory) executable() error {
	if len(m) == 0 {
		return nil
	}
	return fmt.Errorf("%w: unsupported platform %s/%s", ErrMprotectFailed, runtime.GOOS, runtime.GOARCH)
}

func (m memory) free() error {
	if len(m) == 0 {
		return nil
	}
	return fmt.Errorf("%w: unsupported platform %s/%s", ErrMunmapFailed, runtime.GOOS, runtime.GOARCH)
}

func allocMemory(size int) (memory, error) {
	if size <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidSize, size)
	}
	return nil, fmt.Errorf("%w: unsupported platform %s/%s", ErrMmapFailed, runtime.GOOS, runtime.GOARCH)
}
