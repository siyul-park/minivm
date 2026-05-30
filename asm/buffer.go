package asm

import (
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
	mem    memory
	offset int
	sealed bool
}

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
	}

	copy(b.mem[b.offset:end], code)
	ptr := unsafe.Pointer(&b.mem[b.offset])
	b.offset = end

	if err := b.seal(); err != nil {
		return nil, err
	}
	return ptr, nil
}

// Free releases the underlying mmap region.
func (b *Buffer) Free() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.mem.free()
}

// writeAt overwrites the bytes starting at ptr with code. ptr must point
// inside this buffer's mmap region. Used by Link to patch relocations.
func (b *Buffer) writeAt(ptr unsafe.Pointer, code []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	base := uintptr(unsafe.Pointer(&b.mem[0]))
	off := uintptr(ptr) - base
	if off > uintptr(len(b.mem)) || off+uintptr(len(code)) > uintptr(len(b.mem)) {
		return 0, fmt.Errorf("%w: writeAt out of range", ErrInvalidArgs)
	}

	if err := b.unseal(); err != nil {
		return 0, err
	}
	copy(b.mem[off:off+uintptr(len(code))], code)
	if err := b.seal(); err != nil {
		return 0, err
	}
	return len(code), nil
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
	copy(mem, b.mem[:b.offset])

	if err := b.mem.free(); err != nil {
		_ = mem.free()
		return err
	}
	b.mem = mem
	return nil
}
