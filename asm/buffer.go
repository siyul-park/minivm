package asm

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// Buffer is an mmap'd executable memory region. Link installs machine code
// into it; the Buffer owns the W^X transition around every install, so code
// is executable whenever no install is in flight. Installs serialize on an
// internal lock, and executing installed code concurrently is safe.
//
// A region that runs out of space is replaced by a larger mapping and
// retained, so entry pointers callers already hold stay valid and
// executable.
type Buffer struct {
	old    []memory
	mem    memory
	offset int

	mu sync.Mutex
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

// Free releases the current and retained mmap mappings.
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

// install appends code to the buffer and returns the address it starts at.
// The buffer is executable on return, whether or not the append succeeded.
func (b *Buffer) install(code []byte) (unsafe.Pointer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.mem.writable(); err != nil {
		return nil, err
	}
	addr, err := b.append(code)
	if sealErr := b.mem.executable(); err == nil {
		err = sealErr
	}
	if err != nil {
		return nil, err
	}
	return addr, nil
}

// append copies code at the current offset, growing the mapping first when
// it no longer fits. The caller must hold b.mu and have made the current
// mapping writable.
func (b *Buffer) append(code []byte) (unsafe.Pointer, error) {
	end := b.offset + len(code)
	if end > len(b.mem) {
		if err := b.grow(len(code)); err != nil {
			return nil, fmt.Errorf("%w: grow to %d", ErrBufferFull, end)
		}
		end = len(code)
	}

	copy(b.mem[b.offset:end], code)
	addr := unsafe.Pointer(&b.mem[b.offset])
	b.offset = end
	return addr, nil
}

// grow retains the current mapping — sealed executable so pointers into it
// keep working — and installs a freshly mapped writable region at least need
// bytes long. The caller must hold b.mu.
func (b *Buffer) grow(need int) error {
	size := max(len(b.mem)*2, need)
	mem, err := allocMemory(size)
	if err != nil {
		return err
	}
	if err := b.mem.executable(); err != nil {
		_ = mem.free()
		return err
	}
	b.old = append(b.old, b.mem)
	b.mem = mem
	b.offset = 0
	return nil
}
