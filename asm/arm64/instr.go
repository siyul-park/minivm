package arm64

import "encoding/binary"

func MOVZ(rd int, imm uint16, shift int) []byte {
	op := uint32(0x52800000) | uint32(imm)<<5 | uint32(shift&3)<<21 | uint32(rd&31)
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], op)
	return buf[:]
}

func MOVK(rd int, imm uint16, shift int) []byte {
	op := uint32(0x72800000) | uint32(imm)<<5 | uint32(shift&3)<<21 | uint32(rd&31)
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], op)
	return buf[:]
}

func ORRW(rd, rn int) []byte {
	op := uint32(0x2A0003E0) | (uint32(rn)&31)<<16 | (uint32(rd) & 31)
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], op)
	return buf[:]
}

func ADD(rd, rn, rm int) []byte {
	op := encodeRType(0x0B000000, uint32(rd), uint32(rn), uint32(rm))
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], op)
	return buf[:]
}

func SUB(rd, rn, rm int) []byte {
	op := encodeRType(0x4B000000, uint32(rd), uint32(rn), uint32(rm))
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], op)
	return buf[:]
}

func MUL(rd, rn, rm int) []byte {
	op := encodeRType(0x1B007C00, uint32(rd), uint32(rn), uint32(rm))
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], op)
	return buf[:]
}

func SDIV(rd, rn, rm int) []byte {
	op := encodeRType(0x1AC00C00, uint32(rd), uint32(rn), uint32(rm))
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], op)
	return buf[:]
}

func UDIV(rd, rn, rm int) []byte {
	op := encodeRType(0x1AC00800, uint32(rd), uint32(rn), uint32(rm))
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], op)
	return buf[:]
}

func LSL(rd, rn, rm int) []byte {
	op := encodeRType(0x1AC00000, uint32(rd), uint32(rn), uint32(rm))
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], op)
	return buf[:]
}

func LSR(rd, rn, rm int) []byte {
	op := encodeRType(0x1AC00020, uint32(rd), uint32(rn), uint32(rm))
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], op)
	return buf[:]
}

func ASR(rd, rn, rm int) []byte {
	op := encodeRType(0x1AC00040, uint32(rd), uint32(rn), uint32(rm))
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], op)
	return buf[:]
}

func RET() []byte {
	return []byte{0xC0, 0x03, 0x5F, 0xD6}
}

func encodeRType(op uint32, rd, rn, rm uint32) uint32 {
	return op | (rm << 16) | (rn << 5) | rd
}
