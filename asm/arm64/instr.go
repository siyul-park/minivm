package arm64

import "github.com/siyul-park/minivm/asm"

type Op uint16

const (
	OpADD Op = iota
	OpADDI
	OpADDS
	OpADDSI
	OpSUB
	OpSUBI
	OpSUBS
	OpSUBSI
	OpMUL
	OpDIV
	OpAND
	OpORR
	OpEOR
	OpLSL
	OpLSR
	OpCMP
	OpCMPI
	OpMOV
	OpMOVZ
	OpMOVK
	OpLDR
	OpSTR
	OpSCVTF
	OpFCVTZS
	OpRET
	OpB
	OpBL
	OpBR
	OpBLR
	OpCBZ
	OpCBNZ
	OpBEQ
	OpBNE
	OpBLT
	OpBGT
	OpBLE
	OpBGE
)

func newInst(op Op, dst, src1, src2 asm.Operand) asm.Instruction {
	return asm.Instruction{Op: uint16(op), Dst: dst, Src1: src1, Src2: src2}
}

func newReg3(op Op, dst, src1, src2 asm.Register) asm.Instruction {
	return newInst(op, asm.RegOperand{Reg: dst}, asm.RegOperand{Reg: src1}, asm.RegOperand{Reg: src2})
}

func newReg2(op Op, dst, src asm.Register) asm.Instruction {
	return newInst(op, asm.RegOperand{Reg: dst}, asm.RegOperand{Reg: src}, nil)
}

func newRegImm(op Op, dst, src asm.Register, imm int64) asm.Instruction {
	return newInst(op, asm.RegOperand{Reg: dst}, asm.RegOperand{Reg: src}, asm.ImmOperand{Value: imm})
}

func newRegMem(op Op, dst asm.Register, base asm.Register, offset int64) asm.Instruction {
	return newInst(op, asm.RegOperand{Reg: dst}, asm.MemOperand{Base: base, Offset: offset}, nil)
}

func newMemReg(op Op, src asm.Register, base asm.Register, offset int64) asm.Instruction {
	return newInst(op, asm.MemOperand{Base: base, Offset: offset}, asm.RegOperand{Reg: src}, nil)
}

func newCmp(op Op, src1, src2 asm.Register) asm.Instruction {
	return newInst(op, nil, asm.RegOperand{Reg: src1}, asm.RegOperand{Reg: src2})
}

func newCmpImm(op Op, src asm.Register, imm int64) asm.Instruction {
	return newInst(op, nil, asm.RegOperand{Reg: src}, asm.ImmOperand{Value: imm})
}

func newBranch(op Op, offset int64) asm.Instruction {
	return newInst(op, nil, nil, asm.ImmOperand{Value: offset})
}

func newRegOnly(op Op, reg asm.Register) asm.Instruction {
	return newInst(op, nil, asm.RegOperand{Reg: reg}, nil)
}

func ADD(dst, src1, src2 asm.Register) asm.Instruction {
	return newReg3(OpADD, dst, src1, src2)
}

func ADDI(dst, src asm.Register, imm uint16) asm.Instruction {
	return newRegImm(OpADDI, dst, src, int64(imm))
}

func ADDS(dst, src1, src2 asm.Register) asm.Instruction {
	return newReg3(OpADDS, dst, src1, src2)
}

func ADDSI(dst, src asm.Register, imm uint16) asm.Instruction {
	return newRegImm(OpADDSI, dst, src, int64(imm))
}

func SUB(dst, src1, src2 asm.Register) asm.Instruction {
	return newReg3(OpSUB, dst, src1, src2)
}

func SUBI(dst, src asm.Register, imm uint16) asm.Instruction {
	return newRegImm(OpSUBI, dst, src, int64(imm))
}

func SUBS(dst, src1, src2 asm.Register) asm.Instruction {
	return newReg3(OpSUBS, dst, src1, src2)
}

func SUBSI(dst, src asm.Register, imm uint16) asm.Instruction {
	return newRegImm(OpSUBSI, dst, src, int64(imm))
}

func MUL(dst, src1, src2 asm.Register) asm.Instruction {
	return newReg3(OpMUL, dst, src1, src2)
}

func DIV(dst, src1, src2 asm.Register) asm.Instruction {
	return newReg3(OpDIV, dst, src1, src2)
}

func AND(dst, src1, src2 asm.Register) asm.Instruction {
	return newReg3(OpAND, dst, src1, src2)
}

func ORR(dst, src1, src2 asm.Register) asm.Instruction {
	return newReg3(OpORR, dst, src1, src2)
}

func EOR(dst, src1, src2 asm.Register) asm.Instruction {
	return newReg3(OpEOR, dst, src1, src2)
}

func LSL(dst, src asm.Register, shift uint8) asm.Instruction {
	return newRegImm(OpLSL, dst, src, int64(shift))
}

func LSR(dst, src asm.Register, shift uint8) asm.Instruction {
	return newRegImm(OpLSR, dst, src, int64(shift))
}

func CMP(src1, src2 asm.Register) asm.Instruction {
	return newCmp(OpCMP, src1, src2)
}

func CMPI(src asm.Register, imm uint16) asm.Instruction {
	return newCmpImm(OpCMPI, src, int64(imm))
}

func MOV(dst, src asm.Register) asm.Instruction {
	return newReg2(OpMOV, dst, src)
}

func MOVZ(dst asm.Register, imm uint16, shift uint8) asm.Instruction {
	return newInst(OpMOVZ, asm.RegOperand{Reg: dst}, asm.ImmOperand{Value: int64(imm)}, asm.ImmOperand{Value: int64(shift)})
}

func MOVK(dst asm.Register, imm uint16, shift uint8) asm.Instruction {
	return newInst(OpMOVK, asm.RegOperand{Reg: dst}, asm.ImmOperand{Value: int64(imm)}, asm.ImmOperand{Value: int64(shift)})
}

func LDR(dst, base asm.Register, offset int16) asm.Instruction {
	return newRegMem(OpLDR, dst, base, int64(offset))
}

func STR(src, base asm.Register, offset int16) asm.Instruction {
	return newMemReg(OpSTR, src, base, int64(offset))
}

func SCVTF(dst, src asm.Register) asm.Instruction {
	return newReg2(OpSCVTF, dst, src)
}

func FCVTZS(dst, src asm.Register) asm.Instruction {
	return newReg2(OpFCVTZS, dst, src)
}

func RET() asm.Instruction {
	return newInst(OpRET, nil, nil, nil)
}

func B(offset int32) asm.Instruction {
	return newBranch(OpB, int64(offset))
}

func BL(offset int32) asm.Instruction {
	return newBranch(OpBL, int64(offset))
}

func BR(reg asm.Register) asm.Instruction {
	return newRegOnly(OpBR, reg)
}

func BLR(reg asm.Register) asm.Instruction {
	return newRegOnly(OpBLR, reg)
}

func CBZ(reg asm.Register, offset int32) asm.Instruction {
	return newInst(OpCBZ, nil, asm.RegOperand{Reg: reg}, asm.ImmOperand{Value: int64(offset)})
}

func CBNZ(reg asm.Register, offset int32) asm.Instruction {
	return newInst(OpCBNZ, nil, asm.RegOperand{Reg: reg}, asm.ImmOperand{Value: int64(offset)})
}

func BEQ(offset int32) asm.Instruction { return newBranch(OpBEQ, int64(offset)) }
func BNE(offset int32) asm.Instruction { return newBranch(OpBNE, int64(offset)) }
func BLT(offset int32) asm.Instruction { return newBranch(OpBLT, int64(offset)) }
func BGT(offset int32) asm.Instruction { return newBranch(OpBGT, int64(offset)) }
func BLE(offset int32) asm.Instruction { return newBranch(OpBLE, int64(offset)) }
func BGE(offset int32) asm.Instruction { return newBranch(OpBGE, int64(offset)) }
