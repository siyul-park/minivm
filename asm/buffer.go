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
//
// Older mappings are retained in old so pointers stamped into them by linked
// code stay valid after a grow.
type Buffer struct {
	old    []memory
	mem    memory
	offset int

	mu     sync.Mutex
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

	ptr, err := b.write(code)
	if err != nil {
		_ = b.seal()
		return nil, err
	}

	if err := b.seal(); err != nil {
		return nil, err
	}
	return ptr, nil
}

func (b *Buffer) writeBatch(codes [][]byte, patch func([]unsafe.Pointer) error) ([]unsafe.Pointer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.unseal(); err != nil {
		return nil, err
	}

	bases := make([]unsafe.Pointer, len(codes))
	for i, code := range codes {
		base, err := b.write(code)
		if err != nil {
			_ = b.seal()
			return nil, err
		}
		bases[i] = base
	}

	if patch != nil {
		if err := patch(bases); err != nil {
			_ = b.seal()
			return nil, err
		}
	}

	if err := b.seal(); err != nil {
		return nil, err
	}
	return bases, nil
}

func (b *Buffer) write(code []byte) (unsafe.Pointer, error) {
	end := b.offset + len(code)
	if end > len(b.mem) {
		// Seal the outgoing region before retention so pointers callers
		// stamped into it stay executable.
		if err := b.grow(end, memory.executable); err != nil {
			return nil, fmt.Errorf("%w: grow to %d", ErrBufferFull, end)
		}
		end = b.offset + len(code)
	}

	copy(b.mem[b.offset:end], code)
	ptr := unsafe.Pointer(&b.mem[b.offset])
	b.offset = end

	return ptr, nil
}

// Free releases the current and archived mmap mappings.
func (b *Buffer) Free() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, m := range b.old {
		if err := m.free(); err != nil {
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

	return b.patch(ptr, code, false)
}

func (b *Buffer) patch(ptr unsafe.Pointer, code []byte, batch bool) (int, error) {
	mem, off, current, ok := b.locate(ptr, len(code))
	if !ok {
		return 0, fmt.Errorf("%w: writeAt out of range", ErrInvalidArgs)
	}
	if batch && current {
		copy(mem[off:off+len(code)], code)
		return len(code), nil
	}

	open, close := mem.writable, mem.executable
	if current {
		open, close = b.unseal, b.seal
	}
	if err := open(); err != nil {
		return 0, err
	}
	copy(mem[off:off+len(code)], code)
	if err := close(); err != nil {
		return 0, err
	}
	return len(code), nil
}

// locate finds the region containing ptr's n-byte range. current is true
// when the hit is the active mapping (callers must round-trip seal/unseal
// to keep b.sealed consistent).
func (b *Buffer) locate(ptr unsafe.Pointer, n int) (memory, int, bool, bool) {
	if off, ok := b.mem.within(ptr, n); ok {
		return b.mem, off, true, true
	}
	for _, r := range b.old {
		if off, ok := r.within(ptr, n); ok {
			return r, off, false, true
		}
	}
	return nil, 0, false, false
}

// grow archives the current mapping and installs a freshly mapped region at
// least need bytes long. archive, when non-nil, runs on the outgoing region
// before it is retained (e.g. to flip code back to executable). The caller
// must hold b.mu.
func (b *Buffer) grow(need int, archive func(memory) error) error {
	size := len(b.mem) * 2
	if size < need {
		size = need
	}
	mem, err := allocMemory(size)
	if err != nil {
		return err
	}
	if archive != nil {
		if err := archive(b.mem); err != nil {
			_ = mem.free()
			return err
		}
	}
	b.old = append(b.old, b.mem)
	b.mem = mem
	b.offset = 0
	return nil
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
