package asm

import (
	"errors"
	"fmt"
	"unsafe"
)

type Buffer struct {
	mem    Memory
	chunks []*Chunk
	offset int
	sealed bool
}

type Chunk struct {
	buf    *Buffer
	offset int
	size   int
}

var (
	ErrBufferSealed  = errors.New("asm: buffer is sealed")
	ErrChunkNotFound = errors.New("asm: chunk not found in buffer")
)

func NewBuffer(size int) (*Buffer, error) {
	mem, err := Alloc(size)
	if err != nil {
		return nil, err
	}
	return &Buffer{mem: mem}, nil
}

func (b *Buffer) Append(code []byte) (*Chunk, error) {
	if b.sealed {
		return nil, ErrBufferSealed
	}
	offset := b.used()
	end := offset + len(code)
	if end > len(b.mem) {
		return nil, fmt.Errorf("%w: need %d, have %d", ErrCodeTooLarge, end, len(b.mem))
	}
	copy(b.mem[offset:], code)
	chunk := &Chunk{buf: b, offset: offset, size: len(code)}
	b.chunks = append(b.chunks, chunk)
	return chunk, nil
}

func (b *Buffer) Remove(chunk *Chunk) error {
	if b.sealed {
		return ErrBufferSealed
	}
	idx := -1
	for i, c := range b.chunks {
		if c == chunk {
			idx = i
			break
		}
	}
	if idx == -1 {
		return ErrChunkNotFound
	}

	end := chunk.offset + chunk.size
	used := b.used()
	copy(b.mem[chunk.offset:], b.mem[end:used])
	clear(b.mem[used-chunk.size : used])

	for _, c := range b.chunks[idx+1:] {
		c.offset -= chunk.size
	}

	b.chunks = append(b.chunks[:idx], b.chunks[idx+1:]...)
	return nil
}

func (b *Buffer) Seal() error {
	if b.sealed {
		return nil
	}
	if err := b.mem.Executable(); err != nil {
		return err
	}
	b.sealed = true
	return nil
}

func (b *Buffer) Unseal() error {
	if !b.sealed {
		return nil
	}
	if err := b.mem.Writable(); err != nil {
		return err
	}
	b.sealed = false
	return nil
}

func (b *Buffer) Free() error {
	return b.mem.Free()
}

func (b *Buffer) Sealed() bool {
	return b.sealed
}

func (b *Buffer) bounds(chunk Memory) (start, end int, err error) {
	base := uintptr(unsafe.Pointer(&b.mem[0]))
	chunkPtr := uintptr(unsafe.Pointer(&chunk[0]))
	if chunkPtr < base || chunkPtr+uintptr(len(chunk)) > base+uintptr(b.offset) {
		return 0, 0, ErrChunkNotFound
	}
	start = int(chunkPtr - base)
	end = start + len(chunk)
	return start, end, nil
}

func (b *Buffer) used() int {
	if len(b.chunks) == 0 {
		return 0
	}
	last := b.chunks[len(b.chunks)-1]
	return last.offset + last.size
}

func (c *Chunk) Func() unsafe.Pointer {
	return unsafe.Pointer(&c.buf.mem[c.offset])
}

func (c *Chunk) Memory() Memory {
	return c.buf.mem[c.offset : c.offset+c.size]
}
