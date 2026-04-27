package asm

import (
	"errors"
	"fmt"
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
		return nil, fmt.Errorf("%w: code size %d exceeds memory size %d", ErrCodeTooLarge, end, len(b.mem))
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
