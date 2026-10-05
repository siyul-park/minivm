package asm

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// Buffer owns mmap'd executable memory. Link installs each machine-code block
// into a fresh mapping and seals it executable before publication. Installs
// serialize, while published mappings remain immutable and executable.
//
// Free must not run while any code installed in the Buffer can still run or
// be entered.
type Buffer struct {
	maps   []memory
	mem    memory
	size   int
	sealed bool

	mu sync.Mutex
}

// memory is a page-aligned mapping owned by a Buffer.
type memory []byte

// Stable buffer errors.
var (
	ErrBufferFull     = errors.New("buffer full")
	ErrInvalidSize    = errors.New("invalid size")
	ErrMmapFailed     = errors.New("mmap failed")
	ErrMprotectFailed = errors.New("mprotect failed")
	ErrMunmapFailed   = errors.New("munmap failed")
)

// NewBuffer allocates an executable buffer with the given initial mapping
// capacity, rounded up to a page boundary.
func NewBuffer(size int) (*Buffer, error) {
	mem, err := allocMemory(size)
	if err != nil {
		return nil, err
	}
	return &Buffer{mem: mem, size: len(mem)}, nil
}

// Link publishes code into b in an immutable executable mapping and returns
// its entry address.
func (b *Buffer) Link(code []byte) (uintptr, error) {
	if b == nil {
		return 0, fmt.Errorf("%w: nil buffer", ErrInvalidArgs)
	}
	if len(code) == 0 {
		return 0, fmt.Errorf("%w: empty code", ErrInvalidArgs)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size == 0 {
		return 0, fmt.Errorf("%w: freed buffer", ErrInvalidArgs)
	}
	mem := b.mem
	replace := b.sealed || len(code) > len(mem)
	if replace {
		size := max(b.size, len(code))
		var err error
		mem, err = allocMemory(size)
		if err != nil {
			return 0, fmt.Errorf("%w: allocate %d bytes: %w", ErrBufferFull, size, err)
		}
	}

	copy(mem, code)
	if err := executable(mem); err != nil {
		if replace {
			if freeErr := freeMemory(mem); freeErr != nil {
				b.maps = append(b.maps, mem)
				err = errors.Join(err, freeErr)
			}
		}
		return 0, err
	}
	if replace {
		if b.sealed {
			b.maps = append(b.maps, b.mem)
		} else if err := freeMemory(b.mem); err != nil {
			b.maps = append(b.maps, b.mem)
		}
		b.mem = mem
	}
	b.sealed = true
	return uintptr(unsafe.Pointer(&mem[0])), nil
}

// Free releases every mapping. Repeated calls are no-ops; a freed Buffer
// cannot be reused.
func (b *Buffer) Free() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.size = 0
	var err error
	kept := b.maps[:0]
	for _, m := range b.maps {
		if freeErr := freeMemory(m); freeErr != nil {
			err = errors.Join(err, freeErr)
			kept = append(kept, m)
		}
	}
	clear(b.maps[len(kept):])
	b.maps = kept
	if freeErr := freeMemory(b.mem); freeErr != nil {
		err = errors.Join(err, freeErr)
	} else {
		b.mem = nil
		b.sealed = false
	}
	return err
}
