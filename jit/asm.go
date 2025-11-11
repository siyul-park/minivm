package jit

import (
	"encoding/binary"
)

type Register uint8

const (
	RAX Register = 0
	RCX Register = 1
	RDX Register = 2
	RBX Register = 3
	RSP Register = 4
	RBP Register = 5
	RSI Register = 6
	RDI Register = 7
	R8  Register = 8
	R9  Register = 9
	R10 Register = 10
	R11 Register = 11
	R12 Register = 12
	R13 Register = 13
	R14 Register = 14
	R15 Register = 15
)

type Assembler struct {
	code []byte
}

func NewAssembler() *Assembler {
	return &Assembler{
		code: make([]byte, 0, 256),
	}
}

func (a *Assembler) Bytes() []byte {
	return a.code
}

func (a *Assembler) Len() int {
	return len(a.code)
}

func (a *Assembler) Reset() {
	a.code = a.code[:0]
}

func (a *Assembler) emit(b byte) {
	a.code = append(a.code, b)
}

func (a *Assembler) emitBytes(bs ...byte) {
	a.code = append(a.code, bs...)
}

func (a *Assembler) emitUint32(v uint32) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, v)
	a.code = append(a.code, buf...)
}

func (a *Assembler) emitUint64(v uint64) {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, v)
	a.code = append(a.code, buf...)
}

func (a *Assembler) emitRex(w, r, x, b bool) {
	rex := byte(0x40)
	if w {
		rex |= 0x08
	}
	if r {
		rex |= 0x04
	}
	if x {
		rex |= 0x02
	}
	if b {
		rex |= 0x01
	}
	a.emit(rex)
}

func (a *Assembler) emitModRM(mod, reg, rm byte) {
	a.emit((mod << 6) | (reg << 3) | rm)
}

func (a *Assembler) MovImm32ToReg(reg Register, imm int32) {
	if reg >= R8 {
		a.emit(0x41)
	}
	a.emit(0xB8 + byte(reg&7))
	a.emitUint32(uint32(imm))
}

func (a *Assembler) MovImm64ToReg(reg Register, imm int64) {
	a.emitRex(true, false, false, reg >= R8)
	a.emit(0xB8 + byte(reg&7))
	a.emitUint64(uint64(imm))
}

func (a *Assembler) MovRegToReg32(dst, src Register) {
	if dst >= R8 || src >= R8 {
		a.emitRex(false, src >= R8, false, dst >= R8)
	}
	a.emit(0x89)
	a.emitModRM(3, byte(src&7), byte(dst&7))
}

func (a *Assembler) MovRegToReg64(dst, src Register) {
	a.emitRex(true, src >= R8, false, dst >= R8)
	a.emit(0x89)
	a.emitModRM(3, byte(src&7), byte(dst&7))
}

func (a *Assembler) AddRegToReg32(dst, src Register) {
	if dst >= R8 || src >= R8 {
		a.emitRex(false, src >= R8, false, dst >= R8)
	}
	a.emit(0x01)
	a.emitModRM(3, byte(src&7), byte(dst&7))
}

func (a *Assembler) SubRegFromReg32(dst, src Register) {
	if dst >= R8 || src >= R8 {
		a.emitRex(false, src >= R8, false, dst >= R8)
	}
	a.emit(0x29)
	a.emitModRM(3, byte(src&7), byte(dst&7))
}

func (a *Assembler) ImulRegReg32(dst, src Register) {
	if dst >= R8 || src >= R8 {
		a.emitRex(false, dst >= R8, false, src >= R8)
	}
	a.emitBytes(0x0F, 0xAF)
	a.emitModRM(3, byte(dst&7), byte(src&7))
}

func (a *Assembler) Idiv32(reg Register) {
	if reg >= R8 {
		a.emitRex(false, false, false, true)
	}
	a.emit(0xF7)
	a.emitModRM(3, 7, byte(reg&7))
}

func (a *Assembler) Cdq() {
	a.emit(0x99)
}

func (a *Assembler) Xor32(dst, src Register) {
	if dst >= R8 || src >= R8 {
		a.emitRex(false, src >= R8, false, dst >= R8)
	}
	a.emit(0x31)
	a.emitModRM(3, byte(src&7), byte(dst&7))
}

func (a *Assembler) PushReg(reg Register) {
	if reg >= R8 {
		a.emit(0x41)
	}
	a.emit(0x50 + byte(reg&7))
}

func (a *Assembler) PopReg(reg Register) {
	if reg >= R8 {
		a.emit(0x41)
	}
	a.emit(0x58 + byte(reg&7))
}

func (a *Assembler) Ret() {
	a.emit(0xC3)
}
