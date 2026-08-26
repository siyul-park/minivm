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
// Free must not run until every Callable installed in the Buffer is idle and
// no longer needed.
type Buffer struct {
	maps   []memory
	mem    memory
	size   int
	sealed bool

	mu sync.Mutex
}

var ErrBufferFull = errors.New("buffer full")

// NewBuffer allocates an executable buffer with the given initial mapping
// capacity, rounded up to a page boundary.
func NewBuffer(size int) (*Buffer, error) {
	mem, err := allocMemory(size)
	if err != nil {
		return nil, err
	}
	return &Buffer{mem: mem, size: len(mem)}, nil
}

// Free releases every mapping. Repeated calls are no-ops; a freed Buffer
// cannot be reused.
func (b *Buffer) Free() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.size = 0
	var err error
	maps := b.maps
	kept := b.maps[:0]
	for _, m := range b.maps {
		if freeErr := m.free(); freeErr != nil {
			err = errors.Join(err, freeErr)
			kept = append(kept, m)
		}
	}
	clear(maps[len(kept):])
	b.maps = kept
	if freeErr := b.mem.free(); freeErr != nil {
		err = errors.Join(err, freeErr)
	} else {
		b.mem = nil
		b.sealed = false
	}
	return err
}

// install publishes code in an immutable executable mapping and returns its
// entry address.
func (b *Buffer) install(code []byte) (unsafe.Pointer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size == 0 {
		return nil, fmt.Errorf("%w: freed buffer", ErrInvalidArgs)
	}
	mem := b.mem
	replace := b.sealed || len(code) > len(mem)
	if replace {
		size := max(b.size, len(code))
		var err error
		mem, err = allocMemory(size)
		if err != nil {
			return nil, fmt.Errorf("%w: allocate %d bytes: %w", ErrBufferFull, size, err)
		}
	}

	copy(mem, code)
	if err := mem.executable(); err != nil {
		if replace {
			if freeErr := mem.free(); freeErr != nil {
				b.maps = append(b.maps, mem)
				err = errors.Join(err, freeErr)
			}
		}
		return nil, err
	}
	if replace {
		if b.sealed {
			b.maps = append(b.maps, b.mem)
		} else if err := b.mem.free(); err != nil {
			b.maps = append(b.maps, b.mem)
		}
		b.mem = mem
	}
	b.sealed = true
	return unsafe.Pointer(&mem[0]), nil
}
