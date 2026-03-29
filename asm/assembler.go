package asm

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type Assembler struct {
	buf     []byte
	labels  map[string]*Label
	patches []patch
}

type Label struct {
	name   string
	offset int
}

type patch struct {
	label  *Label
	offset int
	fn     patchFn
}

type patchFn func(pc, target int) Instruction

var ErrUnboundLabel = errors.New("asm: unbound label")

func NewAssembler() *Assembler {
	return &Assembler{
		labels: make(map[string]*Label),
	}
}

func (a *Assembler) Emit(instr Instruction) {
	a.buf = append(a.buf, instr...)
}

func (a *Assembler) Label(name string) *Label {
	if l, ok := a.labels[name]; ok {
		return l
	}
	l := &Label{name: name, offset: -1}
	a.labels[name] = l
	return l
}

func (a *Assembler) Bind(l *Label) {
	l.offset = len(a.buf)
}

func (a *Assembler) Branch(l *Label, fn func(pc, target int) Instruction) {
	pc := len(a.buf)
	if l.offset >= 0 {
		a.Emit(fn(pc, l.offset))
	} else {
		placeholder := fn(pc, pc)
		a.Emit(placeholder)
		a.patches = append(a.patches, patch{label: l, offset: pc, fn: fn})
	}
}

func (a *Assembler) Patch() error {
	for _, p := range a.patches {
		if p.label.offset < 0 {
			return fmt.Errorf("%w: %s", ErrUnboundLabel, p.label.name)
		}
		instr := p.fn(p.offset, p.label.offset)
		copy(a.buf[p.offset:], instr)
	}
	a.patches = nil
	return nil
}

func (a *Assembler) Bytes() []byte {
	return a.buf
}

func (a *Assembler) Len() int {
	return len(a.buf)
}

func (a *Assembler) Reset() {
	a.buf = a.buf[:0]
	a.patches = nil
	clear(a.labels)
}

func (a *Assembler) ReadUint32(offset int) uint32 {
	return binary.LittleEndian.Uint32(a.buf[offset:])
}
