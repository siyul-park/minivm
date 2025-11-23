package asm

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

func (e *Emitter) Put(off int, v []byte) {
	if off < 0 || off+len(v) > len(e.code) {
		return
	}
	copy(e.code[off:off+len(v)], v)
}

func (e *Emitter) PC() int {
	return len(e.code)
}

func (e *Emitter) Bytes() []byte {
	return e.code
}
