package asm

import (
	"errors"
	"unsafe"
)

type Buffer struct {
	mem    Memory
	offset int
	sealed bool
}

type Chunk struct {
	buf    *Buffer
	offset int
	size   int
}

var (
	ErrBufferSealed = errors.New("buffer is sealed")
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

	end := b.offset + len(code)
	if end > len(b.mem) {
		if err := b.grow(end); err != nil {
			return nil, err
		}
	}

	copy(b.mem[b.offset:end], code)

	chunk := &Chunk{
		buf:    b,
		offset: b.offset,
		size:   len(code),
	}

	b.offset = end
	return chunk, nil
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

func (c *Chunk) Ptr() unsafe.Pointer {
	return unsafe.Pointer(&c.buf.mem[c.offset])
}

func (c *Chunk) Size() int {
	return c.size
}

// Slice returns a sub-Chunk starting at the given byte offset.
func (c *Chunk) Slice(offset int) (*Chunk, error) {
	if offset < 0 || offset > c.size {
		return nil, ErrInvalidArgs
	}
	return &Chunk{
		buf:    c.buf,
		offset: c.offset + offset,
		size:   c.size - offset,
	}, nil
}

func (b *Buffer) grow(s int) error {
	size := len(b.mem) * 2
	if size < s {
		size = s
	}

	mem, err := Alloc(size)
	if err != nil {
		return err
	}

	copy(mem, b.mem[:b.offset])

	if err := b.mem.Free(); err != nil {
		return err
	}

	b.mem = mem
	return nil
}
