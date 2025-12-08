package arm64

func MOVZ(rd int, imm uint16, shift int) uint32 {
	return 0x52800000 | uint32(imm)<<5 | uint32(shift&3)<<21 | uint32(rd&31)
}

func MOVK(rd int, imm uint16, shift int) uint32 {
	return 0x72800000 | uint32(imm)<<5 | uint32(shift&3)<<21 | uint32(rd&31)
}

func ORRW(rd, rn int) uint32 {
	return 0x2A0003E0 | (uint32(rn)&31)<<16 | (uint32(rd) & 31)
}

func STR(rt, rn int, imm int) uint32 {
	return 0xF9000000 | (uint32(imm&0xFFF) << 10) | (uint32(rn&31) << 5) | uint32(rt&31)
}

func LDR(rt, rn int, imm int) uint32 {
	return uint32(0xF9400000) | (uint32(imm&0xFFF) << 10) | (uint32(rn&31) << 5) | uint32(rt&31)
}

func ADD(rd, rn, rm int) uint32 {
	return encodeRType(0x0B000000, uint32(rd), uint32(rn), uint32(rm))
}

func SUB(rd, rn, rm int) uint32 {
	return encodeRType(0x4B000000, uint32(rd), uint32(rn), uint32(rm))
}

func MUL(rd, rn, rm int) uint32 {
	return encodeRType(0x1B007C00, uint32(rd), uint32(rn), uint32(rm))
}

func ADDI(rd, rn int, imm int) uint32 {
	return 0x11000000 | (uint32(imm&0xFFF) << 10) | (uint32(rn&31) << 5) | uint32(rd&31)
}

func SDIV(rd, rn, rm int) uint32 {
	return encodeRType(0x1AC00C00, uint32(rd), uint32(rn), uint32(rm))
}

func UDIV(rd, rn, rm int) uint32 {
	return encodeRType(0x1AC00800, uint32(rd), uint32(rn), uint32(rm))
}

func LSL(rd, rn, rm int) uint32 {
	return encodeRType(0x1AC00000, uint32(rd), uint32(rn), uint32(rm))
}

func LSR(rd, rn, rm int) uint32 {
	return encodeRType(0x1AC00020, uint32(rd), uint32(rn), uint32(rm))
}

func ASR(rd, rn, rm int) uint32 {
	return encodeRType(0x1AC00040, uint32(rd), uint32(rn), uint32(rm))
}

func RET() uint32 {
	return 0xD65F03C0
}

func encodeRType(op uint32, rd, rn, rm uint32) uint32 {
	return op | (rm << 16) | (rn << 5) | rd
}
