package asm

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

type memory []byte

// region is a growable mmap-backed area. Older mappings are retained so
// pointers stamped into them by linked code remain valid. Concurrent access
// serializes on mu.
type region struct {
	old    []memory
	mem    memory
	offset int

	mu sync.Mutex
}

// free releases the current and archived mappings.
func (r *region) free() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.old {
		if err := m.free(); err != nil {
			return err
		}
	}
	r.old = nil
	return r.mem.free()
}

// grow archives the current mapping and installs a freshly mapped region at
// least need bytes long. archive, when non-nil, runs on the outgoing region
// before it is retained (e.g. to flip code back to executable). The caller
// must hold r.mu.
func (r *region) grow(need int, archive func(memory) error) error {
	size := len(r.mem) * 2
	if size < need {
		size = need
	}
	mem, err := allocMemory(size)
	if err != nil {
		return err
	}
	if archive != nil {
		if err := archive(r.mem); err != nil {
			_ = mem.free()
			return err
		}
	}
	r.old = append(r.old, r.mem)
	r.mem = mem
	r.offset = 0
	return nil
}

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
