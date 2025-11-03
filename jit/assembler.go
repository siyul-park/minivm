package jit

type assembler struct {
	buf []byte
}

func newAssembler() *assembler {
	return &assembler{
		buf: make([]byte, 0, 4096),
	}
}

func (a *assembler) emit(b ...byte) {
	a.buf = append(a.buf, b...)
}

func (a *assembler) emitInt32(v int32) {
	a.emit(
		byte(v),
		byte(v>>8),
		byte(v>>16),
		byte(v>>24),
	)
}

func (a *assembler) emitInt64(v int64) {
	for i := 0; i < 8; i++ {
		a.emit(byte(v >> (i * 8)))
	}
}

func (a *assembler) finalize() []byte {
	return a.buf
}
