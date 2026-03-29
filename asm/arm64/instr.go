package arm64

import "github.com/siyul-park/minivm/asm"

func ADD(dst, src1, src2 asm.Register) asm.Instruction {
	return encode(0x8B000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID()))
}

func ADDI(dst, src asm.Register, imm uint16) asm.Instruction {
	return encode(0x91000000 | uint32(imm)<<10 | uint32(src.ID())<<5 | uint32(dst.ID()))
}

func ADDS(dst, src1, src2 asm.Register) asm.Instruction {
	return encode(0xAB000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID()))
}

func ADDSI(dst, src asm.Register, imm uint16) asm.Instruction {
	return encode(0xB1000000 | uint32(imm)<<10 | uint32(src.ID())<<5 | uint32(dst.ID()))
}

func SUB(dst, src1, src2 asm.Register) asm.Instruction {
	return encode(0xCB000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID()))
}

func SUBI(dst, src asm.Register, imm uint16) asm.Instruction {
	return encode(0xD1000000 | uint32(imm)<<10 | uint32(src.ID())<<5 | uint32(dst.ID()))
}

func SUBS(dst, src1, src2 asm.Register) asm.Instruction {
	return encode(0xEB000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID()))
}

func SUBSI(dst, src asm.Register, imm uint16) asm.Instruction {
	return encode(0xF1000000 | uint32(imm)<<10 | uint32(src.ID())<<5 | uint32(dst.ID()))
}

func MUL(dst, src1, src2 asm.Register) asm.Instruction {
	return encode(0x9B007C00 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID()))
}

func SDIV(dst, src1, src2 asm.Register) asm.Instruction {
	return encode(0x9AC00C00 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID()))
}

// 비트 연산 (레지스터)
func AND(dst, src1, src2 asm.Register) asm.Instruction {
	return encode(0x8A000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID()))
}

func ORR(dst, src1, src2 asm.Register) asm.Instruction {
	return encode(0xAA000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID()))
}

func EOR(dst, src1, src2 asm.Register) asm.Instruction {
	return encode(0xCA000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID()))
}

func LSL(dst, src asm.Register, shift uint8) asm.Instruction {
	return encode(0xD3400000 | uint32(64-shift)<<16 | uint32(63-shift)<<10 | uint32(src.ID())<<5 | uint32(dst.ID()))
}

func LSR(dst, src asm.Register, shift uint8) asm.Instruction {
	return encode(0xD340FC00 | uint32(shift)<<16 | uint32(src.ID())<<5 | uint32(dst.ID()))
}

func CMP(src1, src2 asm.Register) asm.Instruction {
	return encode(0xEB00001F | uint32(src2.ID())<<16 | uint32(src1.ID())<<5)
}

func CMPI(src asm.Register, imm uint16) asm.Instruction {
	return encode(0xF1000000 | uint32(imm)<<10 | uint32(src.ID())<<5 | 0x1F)
}

func CMN(src1, src2 asm.Register) asm.Instruction {
	return encode(0xAB00001F | uint32(src2.ID())<<16 | uint32(src1.ID())<<5)
}

func CMNI(src asm.Register, imm uint16) asm.Instruction {
	return encode(0xB1000000 | uint32(imm)<<10 | uint32(src.ID())<<5 | 0x1F)
}

func MOV(dst, src asm.Register) asm.Instruction {
	return encode(0xAA0003E0 | uint32(src.ID())<<16 | uint32(dst.ID()))
}

func MOVZ(dst asm.Register, imm uint16, shift uint8) asm.Instruction {
	return encode(0xD2800000 | uint32(shift/16)<<21 | uint32(imm)<<5 | uint32(dst.ID()))
}

func MOVK(dst asm.Register, imm uint16, shift uint8) asm.Instruction {
	return encode(0xF2800000 | uint32(shift/16)<<21 | uint32(imm)<<5 | uint32(dst.ID()))
}

func LDR(dst, base asm.Register, offset int16) asm.Instruction {
	return encode(0xF9400000 | uint32(offset/8)<<10 | uint32(base.ID())<<5 | uint32(dst.ID()))
}

func STR(src, base asm.Register, offset int16) asm.Instruction {
	return encode(0xF9000000 | uint32(offset/8)<<10 | uint32(base.ID())<<5 | uint32(src.ID()))
}

func FADD(dst, src1, src2 asm.Register) asm.Instruction {
	return encode(0x1E602800 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID()))
}

func FSUB(dst, src1, src2 asm.Register) asm.Instruction {
	return encode(0x1E603800 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID()))
}

func FMUL(dst, src1, src2 asm.Register) asm.Instruction {
	return encode(0x1E600800 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID()))
}

func FDIV(dst, src1, src2 asm.Register) asm.Instruction {
	return encode(0x1E601800 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID()))
}

func FMOV(dst, src asm.Register) asm.Instruction {
	return encode(0x1E604000 | uint32(src.ID())<<5 | uint32(dst.ID()))
}

func FCMP(src1, src2 asm.Register) asm.Instruction {
	return encode(0x1E602000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5)
}

func SCVTF(dst, src asm.Register) asm.Instruction {
	return encode(0x9E620000 | uint32(src.ID())<<5 | uint32(dst.ID()))
}

func FCVTZS(dst, src asm.Register) asm.Instruction {
	return encode(0x9E780000 | uint32(src.ID())<<5 | uint32(dst.ID()))
}

func RET() asm.Instruction {
	return encode(0xD65F03C0)
}

func B(offset int32) asm.Instruction {
	return encode(0x14000000 | uint32(offset/4)&0x3FFFFFF)
}

func BL(offset int32) asm.Instruction {
	return encode(0x94000000 | uint32(offset/4)&0x3FFFFFF)
}

func BR(reg asm.Register) asm.Instruction {
	return encode(0xD61F0000 | uint32(reg.ID())<<5)
}

func BLR(reg asm.Register) asm.Instruction {
	return encode(0xD63F0000 | uint32(reg.ID())<<5)
}

func CBZ(reg asm.Register, offset int32) asm.Instruction {
	return encode(0xB4000000 | uint32(offset/4)&0x7FFFF<<5 | uint32(reg.ID()))
}

func CBNZ(reg asm.Register, offset int32) asm.Instruction {
	return encode(0xB5000000 | uint32(offset/4)&0x7FFFF<<5 | uint32(reg.ID()))
}

func BEQ(offset int32) asm.Instruction { return encodeCond(0x0, offset) }
func BNE(offset int32) asm.Instruction { return encodeCond(0x1, offset) }
func BLT(offset int32) asm.Instruction { return encodeCond(0xB, offset) }
func BGT(offset int32) asm.Instruction { return encodeCond(0xC, offset) }
func BLE(offset int32) asm.Instruction { return encodeCond(0xD, offset) }
func BGE(offset int32) asm.Instruction { return encodeCond(0xA, offset) }

func encode(instr uint32) asm.Instruction {
	return asm.Instruction{byte(instr), byte(instr >> 8), byte(instr >> 16), byte(instr >> 24)}
}

func encodeCond(cond uint32, offset int32) asm.Instruction {
	return encode(0x54000000 | uint32(offset/4)&0x7FFFF<<5 | cond)
}
