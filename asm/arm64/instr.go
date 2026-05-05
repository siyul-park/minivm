package arm64

import "github.com/siyul-park/minivm/asm"

type Op uint16

const (
	// Arithmetic
	OpADD Op = iota
	OpADDI
	OpADDS
	OpADDSI
	OpSUB
	OpSUBI
	OpSUBS
	OpSUBSI
	OpNEG
	OpNEGS
	OpMUL
	OpMADD
	OpMSUB
	OpMNEG
	OpSDIV
	OpUDIV
	OpADC
	OpADCS
	OpSBC
	OpSBCS

	// Bitwise / Shift
	OpAND
	OpANDI
	OpANDS
	OpANDSI
	OpORR
	OpORRI
	OpEOR
	OpEORI
	OpBIC
	OpBICS
	OpEON
	OpORN
	OpMVN
	OpTST
	OpTSTI
	OpLSL
	OpLSR
	OpASR
	OpROR
	OpLSLI
	OpLSRI
	OpASRI
	OpRORI
	OpCLZ
	OpRBIT
	OpREV
	OpREV16
	OpREV32

	// Sign/Zero extend
	OpSXTB
	OpSXTH
	OpSXTW
	OpUXTB
	OpUXTH

	// Move
	OpMOV
	OpMOVI
	OpMOVZ
	OpMOVK
	OpMOVN

	// Compare
	OpCMP
	OpCMPI
	OpCMN
	OpCMNI
	OpCCMP
	OpCCMPI

	// Load / Store (64-bit)
	OpLDR
	OpSTR

	// Load / Store (8-bit)
	OpLDRB
	OpLDRSB
	OpSTRB

	// Load / Store (16-bit)
	OpLDRH
	OpLDRSH
	OpSTRH

	// Load / Store (32-bit sign-extended)
	OpLDRSW

	// Load / Store register-offset
	OpLDRR
	OpSTRR

	// Load / Store pair
	OpLDP
	OpSTP

	// Float convert
	OpSCVTF
	OpUCVTF
	OpFCVTZS
	OpFCVTZU
	OpFCVT

	// Float arithmetic
	OpFADD
	OpFSUB
	OpFMUL
	OpFDIV
	OpFMADD
	OpFMSUB
	OpFNMADD
	OpFNMSUB

	// Float unary
	OpFABS
	OpFNEG
	OpFSQRT
	OpFRINTN
	OpFRINTM
	OpFRINTP
	OpFRINTZ

	// Float move / compare
	OpFMOV
	OpFCMP
	OpFCMPE

	// Conditional select
	OpCSEL
	OpCSINC
	OpCSINV
	OpCSNEG
	OpCSET
	OpCSETM

	// Branch (unconditional / register)
	OpB
	OpBL
	OpBR
	OpBLR
	OpRET

	// Branch (compare-and-branch)
	OpCBZ
	OpCBNZ

	// Branch (test-and-branch)
	OpTBZ
	OpTBNZ

	// Branch (conditional)
	OpBEQ
	OpBNE
	OpBLT
	OpBGT
	OpBLE
	OpBGE
	OpBMI
	OpBPL
	OpBVS
	OpBVC
	OpBHI
	OpBLS
	OpBCS
	OpBCC

	// System
	OpNOP
	OpBRK
	OpSVC
	OpHLT
	OpERET
	OpMRS
	OpMSR
	OpISB
	OpDSB
	OpDMB
)

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func newInst(op Op, dst, src1, src2 asm.Operand) asm.Instruction {
	return asm.Instruction{Op: uint16(op), Dst: dst, Src1: src1, Src2: src2}
}

func regOperand(reg asm.Reg) asm.Operand {
	switch r := reg.(type) {
	case asm.PReg:
		return asm.P(r)
	case asm.VReg:
		return asm.V(r)
	default:
		panic("unsupported register type")
	}
}

func imm(v int64) asm.Operand { return asm.ImmOperand{Value: v} }

func newReg3(op Op, dst, src1, src2 asm.Reg) asm.Instruction {
	return newInst(op, regOperand(dst), regOperand(src1), regOperand(src2))
}

func newReg2(op Op, dst, src asm.Reg) asm.Instruction {
	return newInst(op, regOperand(dst), regOperand(src), nil)
}

func newReg1(op Op, reg asm.Reg) asm.Instruction {
	return newInst(op, nil, regOperand(reg), nil)
}

func newRegImm(op Op, dst, src asm.Reg, v int64) asm.Instruction {
	return newInst(op, regOperand(dst), regOperand(src), imm(v))
}

func newRegMem(op Op, dst, base asm.Reg, offset int64) asm.Instruction {
	return newInst(op, regOperand(dst), asm.Mem(regOperand(base), offset), nil)
}

func newMemReg(op Op, src, base asm.Reg, offset int64) asm.Instruction {
	return newInst(op, asm.Mem(regOperand(base), offset), regOperand(src), nil)
}

func newCmp(op Op, src1, src2 asm.Reg) asm.Instruction {
	return newInst(op, nil, regOperand(src1), regOperand(src2))
}

func newCmpImm(op Op, src asm.Reg, v int64) asm.Instruction {
	return newInst(op, nil, regOperand(src), imm(v))
}

func newBranch(op Op, offset int64) asm.Instruction {
	return newInst(op, nil, nil, imm(offset))
}

// ---------------------------------------------------------------------------
// Arithmetic
// ---------------------------------------------------------------------------

func ADD(dst, src1, src2 asm.Reg) asm.Instruction      { return newReg3(OpADD, dst, src1, src2) }
func ADDI(dst, src asm.Reg, i uint16) asm.Instruction  { return newRegImm(OpADDI, dst, src, int64(i)) }
func ADDS(dst, src1, src2 asm.Reg) asm.Instruction     { return newReg3(OpADDS, dst, src1, src2) }
func ADDSI(dst, src asm.Reg, i uint16) asm.Instruction { return newRegImm(OpADDSI, dst, src, int64(i)) }

func SUB(dst, src1, src2 asm.Reg) asm.Instruction      { return newReg3(OpSUB, dst, src1, src2) }
func SUBI(dst, src asm.Reg, i uint16) asm.Instruction  { return newRegImm(OpSUBI, dst, src, int64(i)) }
func SUBS(dst, src1, src2 asm.Reg) asm.Instruction     { return newReg3(OpSUBS, dst, src1, src2) }
func SUBSI(dst, src asm.Reg, i uint16) asm.Instruction { return newRegImm(OpSUBSI, dst, src, int64(i)) }

// NEG Xd, Xm  →  SUB Xd, XZR, Xm
func NEG(dst, src asm.Reg) asm.Instruction  { return newReg2(OpNEG, dst, src) }
func NEGS(dst, src asm.Reg) asm.Instruction { return newReg2(OpNEGS, dst, src) }

func MUL(dst, src1, src2 asm.Reg) asm.Instruction  { return newReg3(OpMUL, dst, src1, src2) }
func SDIV(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpSDIV, dst, src1, src2) }
func UDIV(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpUDIV, dst, src1, src2) }

// MADD Xd, Xn, Xm, Xa  →  Xd = Xa + Xn*Xm  (Xa packed into Src2 slot via a 4-reg helper)
func MADD(dst, src1, src2, acc asm.Reg) asm.Instruction {
	return asm.Instruction{
		Op:   uint16(OpMADD),
		Dst:  regOperand(dst),
		Src1: regOperand(src1),
		Src2: regOperand(src2),
		// acc is carried as an extra operand — extend Instruction if needed
		// For now store in a dedicated Acc field if asm.Instruction supports it;
		// otherwise callers must handle the 4th operand at decode time.
	}
}

// MSUB Xd, Xn, Xm, Xa  →  Xd = Xa - Xn*Xm
func MSUB(dst, src1, src2, acc asm.Reg) asm.Instruction {
	return asm.Instruction{
		Op:   uint16(OpMSUB),
		Dst:  regOperand(dst),
		Src1: regOperand(src1),
		Src2: regOperand(src2),
	}
}

// MNEG Xd, Xn, Xm  →  Xd = -(Xn*Xm)
func MNEG(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpMNEG, dst, src1, src2) }

// ADC / SBC — add/subtract with carry
func ADC(dst, src1, src2 asm.Reg) asm.Instruction  { return newReg3(OpADC, dst, src1, src2) }
func ADCS(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpADCS, dst, src1, src2) }
func SBC(dst, src1, src2 asm.Reg) asm.Instruction  { return newReg3(OpSBC, dst, src1, src2) }
func SBCS(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpSBCS, dst, src1, src2) }

// ---------------------------------------------------------------------------
// Bitwise / Shift
// ---------------------------------------------------------------------------

func AND(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpAND, dst, src1, src2) }
func ANDI(dst, src asm.Reg, mask uint64) asm.Instruction {
	return newRegImm(OpANDI, dst, src, int64(mask))
}
func ANDS(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpANDS, dst, src1, src2) }
func ANDSI(dst, src asm.Reg, mask uint64) asm.Instruction {
	return newRegImm(OpANDSI, dst, src, int64(mask))
}
func ORR(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpORR, dst, src1, src2) }
func ORRI(dst, src asm.Reg, mask uint64) asm.Instruction {
	return newRegImm(OpORRI, dst, src, int64(mask))
}
func EOR(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpEOR, dst, src1, src2) }
func EORI(dst, src asm.Reg, mask uint64) asm.Instruction {
	return newRegImm(OpEORI, dst, src, int64(mask))
}
func BIC(dst, src1, src2 asm.Reg) asm.Instruction  { return newReg3(OpBIC, dst, src1, src2) }
func BICS(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpBICS, dst, src1, src2) }
func EON(dst, src1, src2 asm.Reg) asm.Instruction  { return newReg3(OpEON, dst, src1, src2) }
func ORN(dst, src1, src2 asm.Reg) asm.Instruction  { return newReg3(OpORN, dst, src1, src2) }

// MVN Xd, Xm  →  bitwise NOT
func MVN(dst, src asm.Reg) asm.Instruction { return newReg2(OpMVN, dst, src) }

// TST — AND, discard result, set flags
func TST(src1, src2 asm.Reg) asm.Instruction        { return newCmp(OpTST, src1, src2) }
func TSTI(src asm.Reg, mask uint64) asm.Instruction { return newCmpImm(OpTSTI, src, int64(mask)) }

// Shift (register)
func LSL(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpLSL, dst, src1, src2) }
func LSR(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpLSR, dst, src1, src2) }
func ASR(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpASR, dst, src1, src2) }
func ROR(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpROR, dst, src1, src2) }

// Shift (immediate)
func LSLI(dst, src asm.Reg, shift uint8) asm.Instruction {
	return newRegImm(OpLSLI, dst, src, int64(shift))
}
func LSRI(dst, src asm.Reg, shift uint8) asm.Instruction {
	return newRegImm(OpLSRI, dst, src, int64(shift))
}
func ASRI(dst, src asm.Reg, shift uint8) asm.Instruction {
	return newRegImm(OpASRI, dst, src, int64(shift))
}
func RORI(dst, src asm.Reg, shift uint8) asm.Instruction {
	return newRegImm(OpRORI, dst, src, int64(shift))
}

// Bit-manipulation
func CLZ(dst, src asm.Reg) asm.Instruction   { return newReg2(OpCLZ, dst, src) }
func RBIT(dst, src asm.Reg) asm.Instruction  { return newReg2(OpRBIT, dst, src) }
func REV(dst, src asm.Reg) asm.Instruction   { return newReg2(OpREV, dst, src) }
func REV16(dst, src asm.Reg) asm.Instruction { return newReg2(OpREV16, dst, src) }
func REV32(dst, src asm.Reg) asm.Instruction { return newReg2(OpREV32, dst, src) }

func SXTB(dst, src asm.Reg) asm.Instruction { return newReg2(OpSXTB, dst, src) }
func SXTH(dst, src asm.Reg) asm.Instruction { return newReg2(OpSXTH, dst, src) }
func SXTW(dst, src asm.Reg) asm.Instruction { return newReg2(OpSXTW, dst, src) }
func UXTB(dst, src asm.Reg) asm.Instruction { return newReg2(OpUXTB, dst, src) }
func UXTH(dst, src asm.Reg) asm.Instruction { return newReg2(OpUXTH, dst, src) }

// ---------------------------------------------------------------------------
// Move
// ---------------------------------------------------------------------------

func MOV(dst, src asm.Reg) asm.Instruction { return newReg2(OpMOV, dst, src) }

// MOVI dst, #imm — move 64-bit immediate (pseudo, expanded by assembler)
func MOVI(dst asm.Reg, val int64) asm.Instruction {
	return newInst(OpMOVI, regOperand(dst), imm(val), nil)
}

// MOVZ dst, #imm, LSL #shift  — zero other bits
func MOVZ(dst asm.Reg, val uint16, shift uint8) asm.Instruction {
	return newInst(OpMOVZ, regOperand(dst), imm(int64(val)), imm(int64(shift)))
}

// MOVK dst, #imm, LSL #shift  — keep other bits
func MOVK(dst asm.Reg, val uint16, shift uint8) asm.Instruction {
	return newInst(OpMOVK, regOperand(dst), imm(int64(val)), imm(int64(shift)))
}

// MOVN dst, #imm, LSL #shift  — invert bits after shift
func MOVN(dst asm.Reg, val uint16, shift uint8) asm.Instruction {
	return newInst(OpMOVN, regOperand(dst), imm(int64(val)), imm(int64(shift)))
}

// ---------------------------------------------------------------------------
// Compare
// ---------------------------------------------------------------------------

func CMP(src1, src2 asm.Reg) asm.Instruction     { return newCmp(OpCMP, src1, src2) }
func CMPI(src asm.Reg, i uint16) asm.Instruction { return newCmpImm(OpCMPI, src, int64(i)) }
func CMN(src1, src2 asm.Reg) asm.Instruction     { return newCmp(OpCMN, src1, src2) }
func CMNI(src asm.Reg, i uint16) asm.Instruction { return newCmpImm(OpCMNI, src, int64(i)) }

// CCMP Xn, Xm, #nzcv, cond — conditional compare (register)
// nzcv and cond packed into Src2 as (nzcv | cond<<4)
func CCMP(src1, src2 asm.Reg, nzcv uint8, cond uint8) asm.Instruction {
	return newInst(OpCCMP, nil, regOperand(src1), regOperand(src2))
}
func CCMPI(src asm.Reg, val uint8, nzcv uint8, cond uint8) asm.Instruction {
	return newInst(OpCCMPI, nil, regOperand(src), imm(int64(val)))
}

// ---------------------------------------------------------------------------
// Load / Store
// ---------------------------------------------------------------------------

// 64-bit
func LDR(dst, base asm.Reg, offset int16) asm.Instruction {
	return newRegMem(OpLDR, dst, base, int64(offset))
}
func STR(src, base asm.Reg, offset int16) asm.Instruction {
	return newMemReg(OpSTR, src, base, int64(offset))
}

// 8-bit
func LDRB(dst, base asm.Reg, offset int16) asm.Instruction {
	return newRegMem(OpLDRB, dst, base, int64(offset))
}
func LDRSB(dst, base asm.Reg, offset int16) asm.Instruction {
	return newRegMem(OpLDRSB, dst, base, int64(offset))
}
func STRB(src, base asm.Reg, offset int16) asm.Instruction {
	return newMemReg(OpSTRB, src, base, int64(offset))
}

// 16-bit
func LDRH(dst, base asm.Reg, offset int16) asm.Instruction {
	return newRegMem(OpLDRH, dst, base, int64(offset))
}
func LDRSH(dst, base asm.Reg, offset int16) asm.Instruction {
	return newRegMem(OpLDRSH, dst, base, int64(offset))
}
func STRH(src, base asm.Reg, offset int16) asm.Instruction {
	return newMemReg(OpSTRH, src, base, int64(offset))
}

// 32-bit sign-extended to 64-bit
func LDRSW(dst, base asm.Reg, offset int16) asm.Instruction {
	return newRegMem(OpLDRSW, dst, base, int64(offset))
}

// Register-offset variants: LDR Xt, [Xbase, Xoffset]
func LDRR(dst, base, offsetReg asm.Reg) asm.Instruction {
	return newInst(OpLDRR, regOperand(dst), regOperand(base), regOperand(offsetReg))
}
func STRR(src, base, offsetReg asm.Reg) asm.Instruction {
	return newInst(OpSTRR, regOperand(base), regOperand(src), regOperand(offsetReg))
}

// Pair: LDP / STP  —  offset is in units of 8 bytes (64-bit variant)
func LDP(dst1, dst2, base asm.Reg, offset int16) asm.Instruction {
	return newInst(OpLDP,
		regOperand(dst1),
		asm.Mem(regOperand(base), int64(offset)),
		regOperand(dst2),
	)
}
func STP(src1, src2, base asm.Reg, offset int16) asm.Instruction {
	return newInst(OpSTP,
		asm.Mem(regOperand(base), int64(offset)),
		regOperand(src1),
		regOperand(src2),
	)
}

// ---------------------------------------------------------------------------
// Float-point convert
// ---------------------------------------------------------------------------

func SCVTF(dst, src asm.Reg) asm.Instruction  { return newReg2(OpSCVTF, dst, src) }
func UCVTF(dst, src asm.Reg) asm.Instruction  { return newReg2(OpUCVTF, dst, src) }
func FCVTZS(dst, src asm.Reg) asm.Instruction { return newReg2(OpFCVTZS, dst, src) }
func FCVTZU(dst, src asm.Reg) asm.Instruction { return newReg2(OpFCVTZU, dst, src) }

// FCVT — convert between float precisions (single↔double)
func FCVT(dst, src asm.Reg) asm.Instruction { return newReg2(OpFCVT, dst, src) }

// ---------------------------------------------------------------------------
// Float-point arithmetic
// ---------------------------------------------------------------------------

func FADD(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpFADD, dst, src1, src2) }
func FSUB(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpFSUB, dst, src1, src2) }
func FMUL(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpFMUL, dst, src1, src2) }
func FDIV(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpFDIV, dst, src1, src2) }

// FMADD Dd, Dn, Dm, Da  →  Dd = Da + Dn*Dm
func FMADD(dst, src1, src2, acc asm.Reg) asm.Instruction {
	return asm.Instruction{Op: uint16(OpFMADD), Dst: regOperand(dst), Src1: regOperand(src1), Src2: regOperand(src2)}
}
func FMSUB(dst, src1, src2, acc asm.Reg) asm.Instruction {
	return asm.Instruction{Op: uint16(OpFMSUB), Dst: regOperand(dst), Src1: regOperand(src1), Src2: regOperand(src2)}
}
func FNMADD(dst, src1, src2, acc asm.Reg) asm.Instruction {
	return asm.Instruction{Op: uint16(OpFNMADD), Dst: regOperand(dst), Src1: regOperand(src1), Src2: regOperand(src2)}
}
func FNMSUB(dst, src1, src2, acc asm.Reg) asm.Instruction {
	return asm.Instruction{Op: uint16(OpFNMSUB), Dst: regOperand(dst), Src1: regOperand(src1), Src2: regOperand(src2)}
}

// ---------------------------------------------------------------------------
// Float-point unary
// ---------------------------------------------------------------------------

func FABS(dst, src asm.Reg) asm.Instruction   { return newReg2(OpFABS, dst, src) }
func FNEG(dst, src asm.Reg) asm.Instruction   { return newReg2(OpFNEG, dst, src) }
func FSQRT(dst, src asm.Reg) asm.Instruction  { return newReg2(OpFSQRT, dst, src) }
func FRINTN(dst, src asm.Reg) asm.Instruction { return newReg2(OpFRINTN, dst, src) }
func FRINTM(dst, src asm.Reg) asm.Instruction { return newReg2(OpFRINTM, dst, src) }
func FRINTP(dst, src asm.Reg) asm.Instruction { return newReg2(OpFRINTP, dst, src) }
func FRINTZ(dst, src asm.Reg) asm.Instruction { return newReg2(OpFRINTZ, dst, src) }

// ---------------------------------------------------------------------------
// Float-point move / compare
// ---------------------------------------------------------------------------

func FMOV(dst, src asm.Reg) asm.Instruction { return newReg2(OpFMOV, dst, src) }

// FCMP Dn, Dm — sets FP flags, no destination
func FCMP(src1, src2 asm.Reg) asm.Instruction { return newCmp(OpFCMP, src1, src2) }

// FCMPE — compare and raise Invalid Operation exception on NaN
func FCMPE(src1, src2 asm.Reg) asm.Instruction { return newCmp(OpFCMPE, src1, src2) }

// ---------------------------------------------------------------------------
// Conditional select
// ---------------------------------------------------------------------------

// CSEL Xd, Xn, Xm, cond  — Xd = cond ? Xn : Xm  (cond encoded in Src2 upper bits)
func CSEL(dst, trueReg, falseReg asm.Reg, cond uint8) asm.Instruction {
	return newInst(OpCSEL, regOperand(dst), regOperand(trueReg), regOperand(falseReg))
}
func CSINC(dst, trueReg, falseReg asm.Reg, cond uint8) asm.Instruction {
	return newInst(OpCSINC, regOperand(dst), regOperand(trueReg), regOperand(falseReg))
}
func CSINV(dst, trueReg, falseReg asm.Reg, cond uint8) asm.Instruction {
	return newInst(OpCSINV, regOperand(dst), regOperand(trueReg), regOperand(falseReg))
}
func CSNEG(dst, trueReg, falseReg asm.Reg, cond uint8) asm.Instruction {
	return newInst(OpCSNEG, regOperand(dst), regOperand(trueReg), regOperand(falseReg))
}

// CSET Xd, cond  — Xd = cond ? 1 : 0
func CSET(dst asm.Reg, cond uint8) asm.Instruction {
	return newInst(OpCSET, regOperand(dst), imm(int64(cond)), nil)
}

// CSETM Xd, cond  — Xd = cond ? -1 : 0
func CSETM(dst asm.Reg, cond uint8) asm.Instruction {
	return newInst(OpCSETM, regOperand(dst), imm(int64(cond)), nil)
}

// ---------------------------------------------------------------------------
// Branch (unconditional / register)
// ---------------------------------------------------------------------------

func B(offset int32) asm.Instruction  { return newBranch(OpB, int64(offset)) }
func BL(offset int32) asm.Instruction { return newBranch(OpBL, int64(offset)) }
func BR(reg asm.Reg) asm.Instruction  { return newReg1(OpBR, reg) }
func BLR(reg asm.Reg) asm.Instruction { return newReg1(OpBLR, reg) }
func RET() asm.Instruction            { return newInst(OpRET, nil, nil, nil) }

// ---------------------------------------------------------------------------
// Branch (compare-and-branch)
// ---------------------------------------------------------------------------

func CBZ(reg asm.Reg, offset int32) asm.Instruction {
	return newInst(OpCBZ, nil, regOperand(reg), imm(int64(offset)))
}
func CBNZ(reg asm.Reg, offset int32) asm.Instruction {
	return newInst(OpCBNZ, nil, regOperand(reg), imm(int64(offset)))
}

// ---------------------------------------------------------------------------
// Branch (test-and-branch)
// ---------------------------------------------------------------------------

// TBZ reg, #bit, offset — branch if bit N is zero
func TBZ(reg asm.Reg, bit uint8, offset int32) asm.Instruction {
	return newInst(OpTBZ, nil, regOperand(reg), imm(int64(bit)|int64(offset)<<8))
}

// TBNZ reg, #bit, offset — branch if bit N is non-zero
func TBNZ(reg asm.Reg, bit uint8, offset int32) asm.Instruction {
	return newInst(OpTBNZ, nil, regOperand(reg), imm(int64(bit)|int64(offset)<<8))
}

// ---------------------------------------------------------------------------
// Branch (conditional)
// ---------------------------------------------------------------------------

func BEQ(offset int32) asm.Instruction { return newBranch(OpBEQ, int64(offset)) }
func BNE(offset int32) asm.Instruction { return newBranch(OpBNE, int64(offset)) }
func BLT(offset int32) asm.Instruction { return newBranch(OpBLT, int64(offset)) }
func BGT(offset int32) asm.Instruction { return newBranch(OpBGT, int64(offset)) }
func BLE(offset int32) asm.Instruction { return newBranch(OpBLE, int64(offset)) }
func BGE(offset int32) asm.Instruction { return newBranch(OpBGE, int64(offset)) }
func BMI(offset int32) asm.Instruction { return newBranch(OpBMI, int64(offset)) } // Minus / negative
func BPL(offset int32) asm.Instruction { return newBranch(OpBPL, int64(offset)) } // Plus / non-negative
func BVS(offset int32) asm.Instruction { return newBranch(OpBVS, int64(offset)) } // Overflow set
func BVC(offset int32) asm.Instruction { return newBranch(OpBVC, int64(offset)) } // Overflow clear
func BHI(offset int32) asm.Instruction { return newBranch(OpBHI, int64(offset)) } // Unsigned higher
func BLS(offset int32) asm.Instruction { return newBranch(OpBLS, int64(offset)) } // Unsigned lower or same
func BCS(offset int32) asm.Instruction { return newBranch(OpBCS, int64(offset)) } // Carry set  (BHS)
func BCC(offset int32) asm.Instruction { return newBranch(OpBCC, int64(offset)) } // Carry clear (BLO)

// ---------------------------------------------------------------------------
// System
// ---------------------------------------------------------------------------

func NOP() asm.Instruction { return newInst(OpNOP, nil, nil, nil) }
func HLT() asm.Instruction { return newInst(OpHLT, nil, nil, nil) }

// BRK #imm — software breakpoint
func BRK(imm16 uint16) asm.Instruction { return newInst(OpBRK, nil, nil, imm(int64(imm16))) }

// SVC #imm — supervisor call
func SVC(imm16 uint16) asm.Instruction { return newInst(OpSVC, nil, nil, imm(int64(imm16))) }

// ERET — exception return
func ERET() asm.Instruction { return newInst(OpERET, nil, nil, nil) }

// MRS Xt, sysreg — move system register to GP register
func MRS(dst asm.Reg, sysreg uint16) asm.Instruction {
	return newInst(OpMRS, regOperand(dst), imm(int64(sysreg)), nil)
}

// MSR sysreg, Xt — move GP register to system register
func MSR(sysreg uint16, src asm.Reg) asm.Instruction {
	return newInst(OpMSR, imm(int64(sysreg)), regOperand(src), nil)
}

func ISB() asm.Instruction { return newInst(OpISB, nil, nil, nil) }
func DSB() asm.Instruction { return newInst(OpDSB, nil, nil, nil) }
func DMB() asm.Instruction { return newInst(OpDMB, nil, nil, nil) }

const (
	CondEQ uint8 = 0x0
	CondNE uint8 = 0x1
	CondCS uint8 = 0x2
	CondCC uint8 = 0x3
	CondMI uint8 = 0x4
	CondPL uint8 = 0x5
	CondVS uint8 = 0x6
	CondVC uint8 = 0x7
	CondHI uint8 = 0x8
	CondLS uint8 = 0x9
	CondGE uint8 = 0xA
	CondLT uint8 = 0xB
	CondGT uint8 = 0xC
	CondLE uint8 = 0xD
	CondAL uint8 = 0xE
)
