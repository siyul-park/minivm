package asm

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// Buffer is an mmap'd executable memory region. It manages its own W^X
// transitions: callers append bytes via Write, then read back the resulting
// entry pointer via Ptr. Concurrent reads are safe; writes serialize on an
// internal lock and remap pages to RW for the duration of the write.
type Buffer struct {
	mu     sync.Mutex
	old    []memory // sealed regions kept alive; live Callable values hold pointers into them
	mem    memory
	offset int
	sealed bool
}

var ErrBufferFull = errors.New("buffer full")

// NewBuffer allocates a fresh executable buffer with the given byte
// capacity, rounded up to a page boundary.
func NewBuffer(size int) (*Buffer, error) {
	mem, err := allocMemory(size)
	if err != nil {
		return nil, err
	}
	return &Buffer{mem: mem}, nil
}

// Write appends bytes to the buffer and returns a pointer to the start of
// the written region. The pointer is stable for the lifetime of the Buffer
// (or until grow re-mmap's). The buffer is left sealed (executable) on
// return.
func (b *Buffer) Write(code []byte) (unsafe.Pointer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.unseal(); err != nil {
		return nil, err
	}

	end := b.offset + len(code)
	if end > len(b.mem) {
		if err := b.grow(end); err != nil {
			_ = b.seal()
			return nil, err
		}
		end = b.offset + len(code)
	}

	copy(b.mem[b.offset:end], code)
	ptr := unsafe.Pointer(&b.mem[b.offset])
	b.offset = end

	if err := b.seal(); err != nil {
		return nil, err
	}
	return ptr, nil
}

// Free releases all underlying mmap regions.
func (b *Buffer) Free() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.old {
		if err := r.free(); err != nil {
			return err
		}
	}
	b.old = nil
	return b.mem.free()
}

// writeAt overwrites the bytes starting at ptr with code. ptr must point
// inside any region managed by this buffer (current or previously grown).
// Used by Link to patch relocations.
func (b *Buffer) writeAt(ptr unsafe.Pointer, code []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Current region: use unseal/seal to keep b.sealed consistent.
	if len(b.mem) > 0 {
		base := uintptr(unsafe.Pointer(&b.mem[0]))
		off := uintptr(ptr) - base
		if off <= uintptr(len(b.mem)) && off+uintptr(len(code)) <= uintptr(len(b.mem)) {
			if err := b.unseal(); err != nil {
				return 0, err
			}
			copy(b.mem[off:off+uintptr(len(code))], code)
			if err := b.seal(); err != nil {
				return 0, err
			}
			return len(code), nil
		}
	}

	// Archived regions: temporarily make writable, patch, re-seal.
	for _, r := range b.old {
		if len(r) == 0 {
			continue
		}
		base := uintptr(unsafe.Pointer(&r[0]))
		off := uintptr(ptr) - base
		if off > uintptr(len(r)) || off+uintptr(len(code)) > uintptr(len(r)) {
			continue
		}
		if err := r.writable(); err != nil {
			return 0, err
		}
		copy(r[off:off+uintptr(len(code))], code)
		if err := r.executable(); err != nil {
			return 0, err
		}
		return len(code), nil
	}
	return 0, fmt.Errorf("%w: writeAt out of range", ErrInvalidArgs)
}

func (b *Buffer) unseal() error {
	if !b.sealed {
		return nil
	}
	if err := b.mem.writable(); err != nil {
		return err
	}
	b.sealed = false
	return nil
}

func (b *Buffer) seal() error {
	if b.sealed {
		return nil
	}
	if err := b.mem.executable(); err != nil {
		return err
	}
	b.sealed = true
	return nil
}

func (b *Buffer) grow(need int) error {
	size := len(b.mem) * 2
	if size < need {
		size = need
	}

	mem, err := allocMemory(size)
	if err != nil {
		return fmt.Errorf("%w: grow to %d", ErrBufferFull, size)
	}

	// Seal current region before archiving so it stays executable.
	// b.sealed is false here (Write calls unseal before grow).
	if err := b.mem.executable(); err != nil {
		_ = mem.free()
		return err
	}
	// Retire without freeing: live Callable values hold pointers into it.
	b.old = append(b.old, b.mem)
	b.mem = mem
	b.offset = 0
	// b.sealed stays false; new region is writable from allocMemory.
	return nil
}
