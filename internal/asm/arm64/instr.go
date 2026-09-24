package arm64

import "github.com/siyul-park/minivm/internal/asm"

// Op identifies an ARM64 instruction opcode.
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
	OpSBFX
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
	OpUXTW

	// Move
	OpMOV
	OpMOVW
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

	// Load / Store (32-bit)
	OpLDRSW
	OpSTRW

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
	OpFMIN
	OpFMAX
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

	// SIMD (fixed 8B arrangement)
	OpCNT
	OpADDV

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
	OpFCSEL

	// Branch (unconditional / register)
	OpB
	OpBL
	OpBR
	OpBLR
	OpEXIT
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
	OpUSE
	OpDEF
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

const (
	// Cond* are ARM64 condition codes.
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

// ---------------------------------------------------------------------------
// Arithmetic
// ---------------------------------------------------------------------------

// ADD returns an ARM64 instruction.
func ADD(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpADD, dst, src1, src2) }

// ADDI returns an ARM64 instruction.
func ADDI(dst, src asm.Reg, i uint16) asm.Instruction { return newRegImm(OpADDI, dst, src, int64(i)) }

// ADDS returns an ARM64 instruction.
func ADDS(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpADDS, dst, src1, src2) }

// ADDSI returns an ARM64 instruction.
func ADDSI(dst, src asm.Reg, i uint16) asm.Instruction { return newRegImm(OpADDSI, dst, src, int64(i)) }

// SUB returns an ARM64 instruction.
func SUB(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpSUB, dst, src1, src2) }

// SUBI returns an ARM64 instruction.
func SUBI(dst, src asm.Reg, i uint16) asm.Instruction { return newRegImm(OpSUBI, dst, src, int64(i)) }

// SUBS returns an ARM64 instruction.
func SUBS(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpSUBS, dst, src1, src2) }

// SUBSI returns an ARM64 instruction.
func SUBSI(dst, src asm.Reg, i uint16) asm.Instruction { return newRegImm(OpSUBSI, dst, src, int64(i)) }

// NEG Xd, Xm  →  SUB Xd, XZR, Xm
func NEG(dst, src asm.Reg) asm.Instruction { return newReg2(OpNEG, dst, src) }

// NEGS returns an ARM64 instruction.
func NEGS(dst, src asm.Reg) asm.Instruction { return newReg2(OpNEGS, dst, src) }

// MUL returns an ARM64 instruction.
func MUL(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpMUL, dst, src1, src2) }

// SDIV returns an ARM64 instruction.
func SDIV(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpSDIV, dst, src1, src2) }

// UDIV returns an ARM64 instruction.
func UDIV(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpUDIV, dst, src1, src2) }

// MADD returns an ARM64 instruction.
func MADD(dst, src1, src2, acc asm.Reg) asm.Instruction {
	return newInst(OpMADD, regOperand(dst), regOperand(src1), regOperand(src2), regOperand(acc))
}

// MSUB returns an ARM64 instruction.
func MSUB(dst, src1, src2, acc asm.Reg) asm.Instruction {
	return newInst(OpMSUB, regOperand(dst), regOperand(src1), regOperand(src2), regOperand(acc))
}

// MNEG Xd, Xn, Xm  →  Xd = -(Xn*Xm)
func MNEG(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpMNEG, dst, src1, src2) }

// ADC / SBC — add/subtract with carry
func ADC(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpADC, dst, src1, src2) }

// ADCS returns an ARM64 instruction.
func ADCS(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpADCS, dst, src1, src2) }

// SBC returns an ARM64 instruction.
func SBC(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpSBC, dst, src1, src2) }

// SBCS returns an ARM64 instruction.
func SBCS(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpSBCS, dst, src1, src2) }

// ---------------------------------------------------------------------------
// Bitwise / Shift
// ---------------------------------------------------------------------------

// AND returns an ARM64 instruction.
func AND(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpAND, dst, src1, src2) }

// ANDI returns an ARM64 instruction.
func ANDI(dst, src asm.Reg, mask uint64) asm.Instruction {
	return newRegImm(OpANDI, dst, src, int64(mask))
}

// ANDS returns an ARM64 instruction.
func ANDS(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpANDS, dst, src1, src2) }

// ANDSI returns an ARM64 instruction.
func ANDSI(dst, src asm.Reg, mask uint64) asm.Instruction {
	return newRegImm(OpANDSI, dst, src, int64(mask))
}

// ORR returns an ARM64 instruction.
func ORR(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpORR, dst, src1, src2) }

// ORRI returns an ARM64 instruction.
func ORRI(dst, src asm.Reg, mask uint64) asm.Instruction {
	return newRegImm(OpORRI, dst, src, int64(mask))
}

// EOR returns an ARM64 instruction.
func EOR(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpEOR, dst, src1, src2) }

// EORI returns an ARM64 instruction.
func EORI(dst, src asm.Reg, mask uint64) asm.Instruction {
	return newRegImm(OpEORI, dst, src, int64(mask))
}

// BIC returns an ARM64 instruction.
func BIC(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpBIC, dst, src1, src2) }

// BICS returns an ARM64 instruction.
func BICS(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpBICS, dst, src1, src2) }

// EON returns an ARM64 instruction.
func EON(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpEON, dst, src1, src2) }

// ORN returns an ARM64 instruction.
func ORN(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpORN, dst, src1, src2) }

// MVN Xd, Xm  →  bitwise NOT
func MVN(dst, src asm.Reg) asm.Instruction { return newReg2(OpMVN, dst, src) }

// TST — AND, discard result, set flags
func TST(src1, src2 asm.Reg) asm.Instruction { return newCmp(OpTST, src1, src2) }

// TSTI returns an ARM64 instruction.
func TSTI(src asm.Reg, mask uint64) asm.Instruction { return newCmpImm(OpTSTI, src, int64(mask)) }

// Shift (register)
// LSL returns an ARM64 instruction.
func LSL(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpLSL, dst, src1, src2) }

// LSR returns an ARM64 instruction.
func LSR(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpLSR, dst, src1, src2) }

// ASR returns an ARM64 instruction.
func ASR(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpASR, dst, src1, src2) }

// ROR returns an ARM64 instruction.
func ROR(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpROR, dst, src1, src2) }

// Shift (immediate)
// LSLI returns an ARM64 instruction.
func LSLI(dst, src asm.Reg, shift uint8) asm.Instruction {
	return newRegImm(OpLSLI, dst, src, int64(shift))
}

// LSRI returns an ARM64 instruction.
func LSRI(dst, src asm.Reg, shift uint8) asm.Instruction {
	return newRegImm(OpLSRI, dst, src, int64(shift))
}

// ASRI returns an ARM64 instruction.
func ASRI(dst, src asm.Reg, shift uint8) asm.Instruction {
	return newRegImm(OpASRI, dst, src, int64(shift))
}

// RORI returns an ARM64 instruction.
func RORI(dst, src asm.Reg, shift uint8) asm.Instruction {
	return newRegImm(OpRORI, dst, src, int64(shift))
}

// SBFX extracts a bitfield and sign-extends it.
func SBFX(dst, src asm.Reg, lsb, width uint8) asm.Instruction {
	return newRegImm2(OpSBFX, dst, src, int64(lsb), int64(width))
}

// Bit-manipulation
// CLZ returns an ARM64 instruction.
func CLZ(dst, src asm.Reg) asm.Instruction { return newReg2(OpCLZ, dst, src) }

// RBIT returns an ARM64 instruction.
func RBIT(dst, src asm.Reg) asm.Instruction { return newReg2(OpRBIT, dst, src) }

// REV returns an ARM64 instruction.
func REV(dst, src asm.Reg) asm.Instruction { return newReg2(OpREV, dst, src) }

// REV16 returns an ARM64 instruction.
func REV16(dst, src asm.Reg) asm.Instruction { return newReg2(OpREV16, dst, src) }

// REV32 returns an ARM64 instruction.
func REV32(dst, src asm.Reg) asm.Instruction { return newReg2(OpREV32, dst, src) }

// SXTB returns an ARM64 instruction.
func SXTB(dst, src asm.Reg) asm.Instruction { return newReg2(OpSXTB, dst, src) }

// SXTH returns an ARM64 instruction.
func SXTH(dst, src asm.Reg) asm.Instruction { return newReg2(OpSXTH, dst, src) }

// SXTW returns an ARM64 instruction.
func SXTW(dst, src asm.Reg) asm.Instruction { return newReg2(OpSXTW, dst, src) }

// UXTB returns an ARM64 instruction.
func UXTB(dst, src asm.Reg) asm.Instruction { return newReg2(OpUXTB, dst, src) }

// UXTH returns an ARM64 instruction.
func UXTH(dst, src asm.Reg) asm.Instruction { return newReg2(OpUXTH, dst, src) }

// UXTW returns an ARM64 instruction.
func UXTW(dst, src asm.Reg) asm.Instruction { return newReg2(OpUXTW, dst, src) }

// ---------------------------------------------------------------------------
// Move
// ---------------------------------------------------------------------------

// MOV returns an ARM64 instruction.
func MOV(dst, src asm.Reg) asm.Instruction { return newReg2(OpMOV, dst, src) }

// MOVW returns an ARM64 instruction.
func MOVW(dst, src asm.Reg) asm.Instruction { return newReg2(OpMOVW, dst, src) }

// MOVI dst, #imm — move 64-bit immediate (pseudo, expanded by assembler)
func MOVI(dst asm.Reg, val int64) asm.Instruction {
	return newInst(OpMOVI, regOperand(dst), imm(val))
}

// Each instruction must be passed to Emit separately.
// LDI returns the instructions needed to load an immediate.
func LDI(dst asm.Reg, val uint64) []asm.Instruction {
	if val == 0 {
		return []asm.Instruction{MOVZ(dst, 0, 0)}
	}

	insts := make([]asm.Instruction, 0, 4)
	for shift := uint8(0); shift <= 48; shift += 16 {
		part := uint16(val >> shift)
		if part == 0 {
			continue
		}
		if len(insts) == 0 {
			insts = append(insts, MOVZ(dst, part, shift))
			continue
		}
		insts = append(insts, MOVK(dst, part, shift))
	}
	return insts
}

// MOVZ dst, #imm, LSL #shift  — zero other bits
func MOVZ(dst asm.Reg, val uint16, shift uint8) asm.Instruction {
	return newInst(OpMOVZ, regOperand(dst), imm(int64(val)), imm(int64(shift)))
}

// MOVK dst, #imm, LSL #shift  — keep other bits
func MOVK(dst asm.Reg, val uint16, shift uint8) asm.Instruction {
	return newInst(OpMOVK, regOperand(dst), imm(int64(val)), imm(int64(shift)), regOperand(dst))
}

// MOVN dst, #imm, LSL #shift  — invert bits after shift
func MOVN(dst asm.Reg, val uint16, shift uint8) asm.Instruction {
	return newInst(OpMOVN, regOperand(dst), imm(int64(val)), imm(int64(shift)))
}

// ---------------------------------------------------------------------------
// Compare
// ---------------------------------------------------------------------------

// CMP returns an ARM64 instruction.
func CMP(src1, src2 asm.Reg) asm.Instruction { return newCmp(OpCMP, src1, src2) }

// CMPI returns an ARM64 instruction.
func CMPI(src asm.Reg, i uint16) asm.Instruction { return newCmpImm(OpCMPI, src, int64(i)) }

// CMN returns an ARM64 instruction.
func CMN(src1, src2 asm.Reg) asm.Instruction { return newCmp(OpCMN, src1, src2) }

// CMNI returns an ARM64 instruction.
func CMNI(src asm.Reg, i uint16) asm.Instruction { return newCmpImm(OpCMNI, src, int64(i)) }

// CCMP Xn, Xm, #nzcv, cond — conditional compare (register)
// nzcv and cond packed into Src2 as (nzcv | cond<<4)
// CCMP returns an ARM64 instruction.
func CCMP(src1, src2 asm.Reg, nzcv uint8, cond uint8) asm.Instruction {
	flags := int64(nzcv&0xF) | int64(cond&0xF)<<4
	return newInst(OpCCMP, nil, regOperand(src1), regOperand(src2), imm(flags))
}

// CCMPI returns an ARM64 instruction.
func CCMPI(src asm.Reg, val uint8, nzcv uint8, cond uint8) asm.Instruction {
	flags := int64(nzcv&0xF) | int64(cond&0xF)<<4
	return newInst(OpCCMPI, nil, regOperand(src), imm(int64(val)), imm(flags))
}

// ---------------------------------------------------------------------------
// Load / Store
// ---------------------------------------------------------------------------

// 64-bit
// LDR returns an ARM64 instruction.
func LDR(dst, base asm.Reg, offset int16) asm.Instruction {
	return newRegMem(OpLDR, dst, base, int64(offset))
}

// STR returns an ARM64 instruction.
func STR(src, base asm.Reg, offset int16) asm.Instruction {
	return newMemReg(OpSTR, src, base, int64(offset))
}

// 8-bit
// LDRB returns an ARM64 instruction.
func LDRB(dst, base asm.Reg, offset int16) asm.Instruction {
	return newRegMem(OpLDRB, dst, base, int64(offset))
}

// LDRSB returns an ARM64 instruction.
func LDRSB(dst, base asm.Reg, offset int16) asm.Instruction {
	return newRegMem(OpLDRSB, dst, base, int64(offset))
}

// STRB returns an ARM64 instruction.
func STRB(src, base asm.Reg, offset int16) asm.Instruction {
	return newMemReg(OpSTRB, src, base, int64(offset))
}

// 16-bit
// LDRH returns an ARM64 instruction.
func LDRH(dst, base asm.Reg, offset int16) asm.Instruction {
	return newRegMem(OpLDRH, dst, base, int64(offset))
}

// LDRSH returns an ARM64 instruction.
func LDRSH(dst, base asm.Reg, offset int16) asm.Instruction {
	return newRegMem(OpLDRSH, dst, base, int64(offset))
}

// STRH returns an ARM64 instruction.
func STRH(src, base asm.Reg, offset int16) asm.Instruction {
	return newMemReg(OpSTRH, src, base, int64(offset))
}

// 32-bit sign-extended to 64-bit
// LDRSW returns an ARM64 instruction.
func LDRSW(dst, base asm.Reg, offset int16) asm.Instruction {
	return newRegMem(OpLDRSW, dst, base, int64(offset))
}

// STRW returns an ARM64 instruction.
func STRW(src, base asm.Reg, offset int16) asm.Instruction {
	return newMemReg(OpSTRW, src, base, int64(offset))
}

// Register-offset variants: LDR Xt, [Xbase, Xoffset]
// LDRR returns an ARM64 instruction.
func LDRR(dst, base, offsetReg asm.Reg) asm.Instruction {
	return newInst(OpLDRR, regOperand(dst), regOperand(base), regOperand(offsetReg))
}

// STRR returns an ARM64 instruction.
func STRR(src, base, offsetReg asm.Reg) asm.Instruction {
	return newInst(OpSTRR, regOperand(base), regOperand(src), regOperand(offsetReg))
}

// Pair: LDP / STP  —  offset is in units of 8 bytes (64-bit variant)
// LDP returns an ARM64 instruction.
func LDP(dst1, dst2, base asm.Reg, offset int16) asm.Instruction {
	return newInst(OpLDP,
		regOperand(dst1),
		asm.Mem(regOperand(base), int64(offset)),
		regOperand(dst2),
	)
}

// STP returns an ARM64 instruction.
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

// SCVTF returns an ARM64 instruction.
func SCVTF(dst, src asm.Reg) asm.Instruction { return newReg2(OpSCVTF, dst, src) }

// UCVTF returns an ARM64 instruction.
func UCVTF(dst, src asm.Reg) asm.Instruction { return newReg2(OpUCVTF, dst, src) }

// FCVTZS returns an ARM64 instruction.
func FCVTZS(dst, src asm.Reg) asm.Instruction { return newReg2(OpFCVTZS, dst, src) }

// FCVTZU returns an ARM64 instruction.
func FCVTZU(dst, src asm.Reg) asm.Instruction { return newReg2(OpFCVTZU, dst, src) }

// FCVT — convert between float precisions (single↔double)
func FCVT(dst, src asm.Reg) asm.Instruction { return newReg2(OpFCVT, dst, src) }

// ---------------------------------------------------------------------------
// Float-point arithmetic
// ---------------------------------------------------------------------------

// FADD returns an ARM64 instruction.
func FADD(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpFADD, dst, src1, src2) }

// FSUB returns an ARM64 instruction.
func FSUB(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpFSUB, dst, src1, src2) }

// FMUL returns an ARM64 instruction.
func FMUL(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpFMUL, dst, src1, src2) }

// FDIV returns an ARM64 instruction.
func FDIV(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpFDIV, dst, src1, src2) }

// FMIN returns an ARM64 instruction.
func FMIN(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpFMIN, dst, src1, src2) }

// FMAX returns an ARM64 instruction.
func FMAX(dst, src1, src2 asm.Reg) asm.Instruction { return newReg3(OpFMAX, dst, src1, src2) }

// FMADD Dd, Dn, Dm, Da  →  Dd = Da + Dn*Dm
func FMADD(dst, src1, src2, acc asm.Reg) asm.Instruction {
	return newInst(OpFMADD, regOperand(dst), regOperand(src1), regOperand(src2), regOperand(acc))
}

// FMSUB returns an ARM64 instruction.
func FMSUB(dst, src1, src2, acc asm.Reg) asm.Instruction {
	return newInst(OpFMSUB, regOperand(dst), regOperand(src1), regOperand(src2), regOperand(acc))
}

// FNMADD returns an ARM64 instruction.
func FNMADD(dst, src1, src2, acc asm.Reg) asm.Instruction {
	return newInst(OpFNMADD, regOperand(dst), regOperand(src1), regOperand(src2), regOperand(acc))
}

// FNMSUB returns an ARM64 instruction.
func FNMSUB(dst, src1, src2, acc asm.Reg) asm.Instruction {
	return newInst(OpFNMSUB, regOperand(dst), regOperand(src1), regOperand(src2), regOperand(acc))
}

// ---------------------------------------------------------------------------
// Float-point unary
// ---------------------------------------------------------------------------

// FABS returns an ARM64 instruction.
func FABS(dst, src asm.Reg) asm.Instruction { return newReg2(OpFABS, dst, src) }

// FNEG returns an ARM64 instruction.
func FNEG(dst, src asm.Reg) asm.Instruction { return newReg2(OpFNEG, dst, src) }

// FSQRT returns an ARM64 instruction.
func FSQRT(dst, src asm.Reg) asm.Instruction { return newReg2(OpFSQRT, dst, src) }

// FRINTN returns an ARM64 instruction.
func FRINTN(dst, src asm.Reg) asm.Instruction { return newReg2(OpFRINTN, dst, src) }

// FRINTM returns an ARM64 instruction.
func FRINTM(dst, src asm.Reg) asm.Instruction { return newReg2(OpFRINTM, dst, src) }

// FRINTP returns an ARM64 instruction.
func FRINTP(dst, src asm.Reg) asm.Instruction { return newReg2(OpFRINTP, dst, src) }

// FRINTZ returns an ARM64 instruction.
func FRINTZ(dst, src asm.Reg) asm.Instruction { return newReg2(OpFRINTZ, dst, src) }

// CNT Vd.8B, Vn.8B  →  per-byte population count.
// ADDV Bd, Vn.8B    →  sum the 8 byte lanes into the low byte of Vd.
// Both take SIMD V registers (fixed 8-byte arrangement).
// CNT returns an ARM64 instruction.
func CNT(dst, src asm.Reg) asm.Instruction { return newReg2(OpCNT, dst, src) }

// ADDV returns an ARM64 instruction.
func ADDV(dst, src asm.Reg) asm.Instruction { return newReg2(OpADDV, dst, src) }

// ---------------------------------------------------------------------------
// Float-point move / compare
// ---------------------------------------------------------------------------

// FMOV returns an ARM64 instruction.
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
	return newInst(OpCSEL, regOperand(dst), regOperand(trueReg), asm.ImmOperand{Value: int64(cond)}, regOperand(falseReg))
}

// CSINC returns an ARM64 instruction.
func CSINC(dst, trueReg, falseReg asm.Reg, cond uint8) asm.Instruction {
	return newInst(OpCSINC, regOperand(dst), regOperand(trueReg), imm(int64(cond)), regOperand(falseReg))
}

// CSINV returns an ARM64 instruction.
func CSINV(dst, trueReg, falseReg asm.Reg, cond uint8) asm.Instruction {
	return newInst(OpCSINV, regOperand(dst), regOperand(trueReg), imm(int64(cond)), regOperand(falseReg))
}

// CSNEG returns an ARM64 instruction.
func CSNEG(dst, trueReg, falseReg asm.Reg, cond uint8) asm.Instruction {
	return newInst(OpCSNEG, regOperand(dst), regOperand(trueReg), imm(int64(cond)), regOperand(falseReg))
}

// CSET Xd, cond  — Xd = cond ? 1 : 0
func CSET(dst asm.Reg, cond uint8) asm.Instruction {
	return newInst(OpCSET, regOperand(dst), imm(int64(cond)))
}

// CSETM Xd, cond  — Xd = cond ? -1 : 0
func CSETM(dst asm.Reg, cond uint8) asm.Instruction {
	return newInst(OpCSETM, regOperand(dst), imm(int64(cond)))
}

// FCSEL selects between floating-point registers using the condition flags.
func FCSEL(dst, trueReg, falseReg asm.Reg, cond uint8) asm.Instruction {
	return newInst(OpFCSEL, regOperand(dst), regOperand(trueReg), imm(int64(cond)), regOperand(falseReg))
}

// ---------------------------------------------------------------------------
// Branch (unconditional / register)
// ---------------------------------------------------------------------------

// B returns an ARM64 instruction.
func B(offset int32) asm.Instruction { return newBranch(OpB, int64(offset)) }

// BL returns an ARM64 instruction.
func BL(offset int32) asm.Instruction { return newBranch(OpBL, int64(offset)) }

// BR returns an ARM64 instruction.
func BR(reg asm.Reg) asm.Instruction { return newReg1(OpBR, reg) }

// BLR returns an ARM64 instruction.
func BLR(reg asm.Reg) asm.Instruction { return newReg1(OpBLR, reg) }

// EXIT calls the exit stub, which the runtime guarantees preserves every
// allocatable register; it encodes exactly as BLR but never clobbers or
// forces a spill.
// EXIT returns an ARM64 instruction.
func EXIT(reg asm.Reg) asm.Instruction { return newReg1(OpEXIT, reg) }

// RET returns an ARM64 instruction.
func RET() asm.Instruction { return newInst(OpRET, nil) }

// Build resolves label branches after every label is bound.
// BLabel returns an ARM64 instruction.
func BLabel(id asm.Label) asm.Instruction {
	return asm.Instruction{Op: uint16(OpB), Src2: asm.LabelOperand{ID: id}}
}

// BLLabel returns an ARM64 instruction.
func BLLabel(id asm.Label) asm.Instruction {
	return asm.Instruction{Op: uint16(OpBL), Src2: asm.LabelOperand{ID: id}}
}

// condOp must be one of OpBEQ, OpBNE, OpBLT, OpBGT, OpBLE, OpBGE, …
// BCondLabel returns an ARM64 instruction.
func BCondLabel(condOp Op, id asm.Label) asm.Instruction {
	return asm.Instruction{Op: uint16(condOp), Src2: asm.LabelOperand{ID: id}}
}

// ---------------------------------------------------------------------------
// Branch (compare-and-branch)
// ---------------------------------------------------------------------------

// CBZ returns an ARM64 instruction.
func CBZ(reg asm.Reg, offset int32) asm.Instruction {
	return newInst(OpCBZ, nil, regOperand(reg), imm(int64(offset)))
}

// CBNZ returns an ARM64 instruction.
func CBNZ(reg asm.Reg, offset int32) asm.Instruction {
	return newInst(OpCBNZ, nil, regOperand(reg), imm(int64(offset)))
}

// CBZLabel returns an ARM64 instruction.
func CBZLabel(reg asm.Reg, id asm.Label) asm.Instruction {
	return asm.Instruction{Op: uint16(OpCBZ), Src1: regOperand(reg), Src2: asm.LabelOperand{ID: id}}
}

// CBNZLabel returns an ARM64 instruction.
func CBNZLabel(reg asm.Reg, id asm.Label) asm.Instruction {
	return asm.Instruction{Op: uint16(OpCBNZ), Src1: regOperand(reg), Src2: asm.LabelOperand{ID: id}}
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

// BEQ returns an ARM64 instruction.
func BEQ(offset int32) asm.Instruction { return newBranch(OpBEQ, int64(offset)) }

// BNE returns an ARM64 instruction.
func BNE(offset int32) asm.Instruction { return newBranch(OpBNE, int64(offset)) }

// BLT returns an ARM64 instruction.
func BLT(offset int32) asm.Instruction { return newBranch(OpBLT, int64(offset)) }

// BGT returns an ARM64 instruction.
func BGT(offset int32) asm.Instruction { return newBranch(OpBGT, int64(offset)) }

// BLE returns an ARM64 instruction.
func BLE(offset int32) asm.Instruction { return newBranch(OpBLE, int64(offset)) }

// BGE returns an ARM64 instruction.
func BGE(offset int32) asm.Instruction { return newBranch(OpBGE, int64(offset)) }

// BMI returns an ARM64 instruction.
func BMI(offset int32) asm.Instruction { return newBranch(OpBMI, int64(offset)) } // Minus / negative
// BPL returns an ARM64 instruction.
func BPL(offset int32) asm.Instruction { return newBranch(OpBPL, int64(offset)) } // Plus / non-negative
// BVS returns an ARM64 instruction.
func BVS(offset int32) asm.Instruction { return newBranch(OpBVS, int64(offset)) } // Overflow set
// BVC returns an ARM64 instruction.
func BVC(offset int32) asm.Instruction { return newBranch(OpBVC, int64(offset)) } // Overflow clear
// BHI returns an ARM64 instruction.
func BHI(offset int32) asm.Instruction { return newBranch(OpBHI, int64(offset)) } // Unsigned higher
// BLS returns an ARM64 instruction.
func BLS(offset int32) asm.Instruction { return newBranch(OpBLS, int64(offset)) } // Unsigned lower or same
// BCS returns an ARM64 instruction.
func BCS(offset int32) asm.Instruction { return newBranch(OpBCS, int64(offset)) } // Carry set  (BHS)
// BCC returns an ARM64 instruction.
func BCC(offset int32) asm.Instruction { return newBranch(OpBCC, int64(offset)) } // Carry clear (BLO)

// ---------------------------------------------------------------------------
// System
// ---------------------------------------------------------------------------

// NOP returns an ARM64 instruction.
func NOP() asm.Instruction { return newInst(OpNOP, nil) }

// HLT returns an ARM64 instruction.
func HLT() asm.Instruction { return newInst(OpHLT, nil) }

// USE reads src and encodes nothing: it keeps a value live up to its row.
func USE(src asm.Reg) asm.Instruction { return newReg1(OpUSE, src) }

// DEF marks reg written here and encodes nothing: the dual of USE, for a
// physical result register (X0/X1) a call convention defines rather than an
// instruction operand, such as BL/BLR's register-convention result.
func DEF(reg asm.Reg) asm.Instruction { return newInst(OpDEF, regOperand(reg)) }

// BRK #imm — software breakpoint
func BRK(imm16 uint16) asm.Instruction { return newInst(OpBRK, nil, nil, imm(int64(imm16))) }

// SVC #imm — supervisor call
func SVC(imm16 uint16) asm.Instruction { return newInst(OpSVC, nil, nil, imm(int64(imm16))) }

// ERET — exception return
func ERET() asm.Instruction { return newInst(OpERET, nil) }

// MRS Xt, sysreg — move system register to GP register
func MRS(dst asm.Reg, sysreg uint16) asm.Instruction {
	return newInst(OpMRS, regOperand(dst), imm(int64(sysreg)))
}

// MSR sysreg, Xt — move GP register to system register
func MSR(sysreg uint16, src asm.Reg) asm.Instruction {
	return newInst(OpMSR, imm(int64(sysreg)), regOperand(src))
}

// ISB returns an ARM64 instruction.
func ISB() asm.Instruction { return newInst(OpISB, nil) }

// DSB returns an ARM64 instruction.
func DSB() asm.Instruction { return newInst(OpDSB, nil) }

// DMB returns an ARM64 instruction.
func DMB() asm.Instruction { return newInst(OpDMB, nil) }

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func newReg3(op Op, dst, src1, src2 asm.Reg) asm.Instruction {
	return newInst(op, regOperand(dst), regOperand(src1), regOperand(src2))
}

func newReg2(op Op, dst, src asm.Reg) asm.Instruction {
	return newInst(op, regOperand(dst), regOperand(src))
}

func newReg1(op Op, reg asm.Reg) asm.Instruction {
	return newInst(op, nil, regOperand(reg))
}

func newRegImm(op Op, dst, src asm.Reg, v int64) asm.Instruction {
	return newInst(op, regOperand(dst), regOperand(src), imm(v))
}

func newRegImm2(op Op, dst, src asm.Reg, v1, v2 int64) asm.Instruction {
	return newInst(op, regOperand(dst), regOperand(src), imm(v1), imm(v2))
}

func newRegMem(op Op, dst, base asm.Reg, offset int64) asm.Instruction {
	return newInst(op, regOperand(dst), asm.Mem(regOperand(base), offset))
}

func newMemReg(op Op, src, base asm.Reg, offset int64) asm.Instruction {
	return newInst(op, asm.Mem(regOperand(base), offset), regOperand(src))
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

func newInst(op Op, dst asm.Operand, srcs ...asm.Operand) asm.Instruction {
	var src1, src2, src3 asm.Operand
	if len(srcs) > 0 {
		src1 = srcs[0]
	}
	if len(srcs) > 1 {
		src2 = srcs[1]
	}
	if len(srcs) > 2 {
		src3 = srcs[2]
	}
	return asm.Instruction{Op: uint16(op), Dst: dst, Src1: src1, Src2: src2, Src3: src3}
}

func regOperand(reg asm.Reg) asm.Operand {
	switch r := reg.(type) {
	case asm.PReg:
		return asm.Physical(r)
	case asm.VReg:
		return asm.Virtual(r)
	default:
		panic("unsupported register type")
	}
}

func imm(v int64) asm.Operand { return asm.ImmOperand{Value: v} }
