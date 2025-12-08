package asm

import "encoding/binary"

type Emitter struct {
	code []byte
}

func NewEmitter() *Emitter {
	return &Emitter{
		code: make([]byte, 0, 256),
	}
}

func (e *Emitter) Emit(v []byte) {
	e.code = append(e.code, v...)
}

func (e *Emitter) Emit32(val uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], val)
	e.code = append(e.code, buf[:]...)
}

func (e *Emitter) Emit64(val uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], val)
	e.code = append(e.code, buf[:]...)
}

func (e *Emitter) Put(off int, v []byte) {
	if off < 0 || off+len(v) > len(e.code) {
		return
	}
	copy(e.code[off:off+len(v)], v)
}

func (e *Emitter) Put32(off int, val uint32) {
	if off < 0 || off+4 > len(e.code) {
		return
	}
	binary.LittleEndian.PutUint32(e.code[off:off+4], val)
}

func (e *Emitter) Put64(off int, val uint64) {
	if off < 0 || off+8 > len(e.code) {
		return
	}
	binary.LittleEndian.PutUint64(e.code[off:off+8], val)
}

func (e *Emitter) PC() int {
	return len(e.code)
}

func (e *Emitter) Bytes() []byte {
	return e.code
}

func (e *Emitter) Reset() {
	e.code = e.code[:0]
}
