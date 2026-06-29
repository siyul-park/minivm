package arm64

import (
	"errors"
	"math/bits"

	"github.com/siyul-park/minivm/asm"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	ErrUnsupportedOpcode         = errors.New("unsupported opcode")
	ErrMissingDestinationReg     = errors.New("missing destination register")
	ErrMissingSourceReg          = errors.New("missing source register")
	ErrMissingSourceRegs         = errors.New("missing source registers")
	ErrMissingImmediate          = errors.New("missing immediate")
	ErrMissingShiftImmediate     = errors.New("missing shift immediate")
	ErrMissingMemoryOperand      = errors.New("missing memory operand")
	ErrMissingRegisterOperand    = errors.New("missing register operand")
	ErrMissingBranchOffset       = errors.New("missing branch offset")
	ErrUnexpectedRegisterOperand = errors.New("unexpected register operand")
)

// ---------------------------------------------------------------------------
// Encoder
// ---------------------------------------------------------------------------

type Encoder struct{}

var _ asm.Encoder = (*Encoder)(nil)

func NewEncoder() *Encoder { return &Encoder{} }

// condCode maps a conditional-branch opcode to the 4-bit AArch64 condition code.
var condCode = map[Op]uint32{
	OpBEQ: 0x0, // EQ  Z==1
	OpBNE: 0x1, // NE  Z==0
	OpBCS: 0x2, // CS  C==1
	OpBCC: 0x3, // CC  C==0
	OpBMI: 0x4, // MI  N==1
	OpBPL: 0x5, // PL  N==0
	OpBVS: 0x6, // VS  V==1
	OpBVC: 0x7, // VC  V==0
	OpBHI: 0x8, // HI  C==1 && Z==0
	OpBLS: 0x9, // LS  C==0 || Z==1
	OpBGE: 0xA, // GE  N==V
	OpBLT: 0xB, // LT  N!=V
	OpBGT: 0xC, // GT  Z==0 && N==V
	OpBLE: 0xD, // LE  Z==1 || N!=V
}

// extendOpcodes maps each sign/zero-extend opcode to its SBFM/UBFM base word
// and imms field (the source width minus one).
var extendOpcodes = map[Op]struct{ base, imms uint32 }{
	OpSXTB: {0x93400000, 7},
	OpSXTH: {0x93400000, 15},
	OpSXTW: {0x93400000, 31},
	OpUXTB: {0xD3400000, 7},
	OpUXTH: {0xD3400000, 15},
	OpUXTW: {0xD3400000, 31},
}

// floatUnaryOpcodes maps each scalar float unary opcode to its single- and
// double-precision base words.
var floatUnaryOpcodes = map[Op]struct{ single, double uint32 }{
	OpFABS:   {0x1E20C000, 0x1E60C000},
	OpFNEG:   {0x1E214000, 0x1E614000},
	OpFSQRT:  {0x1E21C000, 0x1E61C000},
	OpFRINTN: {0x1E244000, 0x1E644000}, // round to nearest, ties to even
	OpFRINTM: {0x1E254000, 0x1E654000}, // round toward minus infinity
	OpFRINTP: {0x1E24C000, 0x1E64C000}, // round toward plus infinity
	OpFRINTZ: {0x1E25C000, 0x1E65C000}, // round toward zero
}

// reg3Opcodes maps each uniform 3-register opcode (Rm<<16 | Rn<<5 | Rd) to its
// 64-bit base word. MUL/MNEG encode as MADD/MSUB with Ra=XZR; LSL/LSR/ASR/ROR
// are the register-shift variants (LSLV-family).
var reg3Opcodes = map[Op]uint32{
	OpADD:  0x8B000000,
	OpADDS: 0xAB000000,
	OpSUB:  0xCB000000,
	OpSUBS: 0xEB000000,
	OpMUL:  0x9B007C00,
	OpMNEG: 0x9B00FC00,
	OpSDIV: 0x9AC00C00,
	OpUDIV: 0x9AC00800,
	OpADC:  0x9A000000,
	OpADCS: 0xBA000000,
	OpSBC:  0xDA000000,
	OpSBCS: 0xFA000000,
	OpAND:  0x8A000000,
	OpANDS: 0xEA000000,
	OpORR:  0xAA000000,
	OpEOR:  0xCA000000,
	OpBIC:  0x8A200000,
	OpBICS: 0xEA200000,
	OpEON:  0xCA200000,
	OpORN:  0xAA200000,
	OpLSL:  0x9AC02000,
	OpLSR:  0x9AC02400,
	OpASR:  0x9AC02800,
	OpROR:  0x9AC02C00,
}

// arithImmOpcodes maps each arithmetic-immediate opcode (imm12) to its base word.
var arithImmOpcodes = map[Op]uint32{
	OpADDI:  0x91000000,
	OpADDSI: 0xB1000000,
	OpSUBI:  0xD1000000,
	OpSUBSI: 0xF1000000,
}

// logicalImmOpcodes maps each logical-immediate opcode to its base word.
var logicalImmOpcodes = map[Op]uint32{
	OpANDI:  0x92000000,
	OpANDSI: 0xF2000000,
	OpORRI:  0xB2000000,
	OpEORI:  0xD2000000,
}

// reg2Opcodes maps each uniform 2-register opcode (Rn<<5 | Rd) to its base word.
var reg2Opcodes = map[Op]uint32{
	OpCLZ:   0xDAC01000,
	OpRBIT:  0xDAC00000,
	OpREV16: 0xDAC00400,
	OpREV32: 0xDAC00800,
}

// selectOpcodes maps each conditional-select opcode to its base word.
var selectOpcodes = map[Op]uint32{
	OpCSEL:  0x9A800000,
	OpCSINC: 0x9A800400,
	OpCSINV: 0xDA800000,
	OpCSNEG: 0xDA800400,
}

// moveOpcodes maps each wide-immediate move opcode to its 32- and 64-bit base words.
var moveOpcodes = map[Op]struct{ op32, op64 uint32 }{
	OpMOVZ: {0x52800000, 0xD2800000},
	OpMOVK: {0x72800000, 0xF2800000},
	OpMOVN: {0x12800000, 0x92800000},
}

// loadOpcodes maps each unsigned-offset load opcode to its base word and the
// access size that scales the byte offset.
var loadOpcodes = map[Op]struct {
	base  uint32
	scale int64
}{
	OpLDR:   {0xF9400000, 8},
	OpLDRB:  {0x39400000, 1},
	OpLDRSB: {0x39800000, 1},
	OpLDRH:  {0x79400000, 2},
	OpLDRSH: {0x79800000, 2},
	OpLDRSW: {0xB9800000, 4},
}

// storeOpcodes maps each unsigned-offset store opcode to its base word and the
// access size that scales the byte offset.
var storeOpcodes = map[Op]struct {
	base  uint32
	scale int64
}{
	OpSTR:  {0xF9000000, 8},
	OpSTRB: {0x39000000, 1},
	OpSTRH: {0x79000000, 2},
	OpSTRW: {0xB9000000, 4},
}

// cvtOpcodes maps each int↔float convert opcode to its base word and whether
// the integer register is the destination. The final word sets bit 31 for a
// 64-bit integer register and bit 22 for a double-precision float register.
var cvtOpcodes = map[Op]struct {
	base  uint32
	toInt bool
}{
	OpSCVTF:  {0x1E220000, false},
	OpUCVTF:  {0x1E230000, false},
	OpFCVTZS: {0x1E380000, true},
	OpFCVTZU: {0x1E390000, true},
}

// floatBinaryOpcodes maps each 3-register scalar float opcode to its single-
// and double-precision base words.
var floatBinaryOpcodes = map[Op]struct{ single, double uint32 }{
	OpFADD: {0x1E202800, 0x1E602800},
	OpFSUB: {0x1E203800, 0x1E603800},
	OpFMUL: {0x1E200800, 0x1E600800},
	OpFDIV: {0x1E201800, 0x1E601800},
	OpFMIN: {0x1E205800, 0x1E605800},
	OpFMAX: {0x1E204800, 0x1E604800},
}

// floatTernaryOpcodes maps each 4-register scalar float opcode (FMADD-family)
// to its single- and double-precision base words.
var floatTernaryOpcodes = map[Op]struct{ single, double uint32 }{
	OpFMADD:  {0x1F000000, 0x1F400000},
	OpFMSUB:  {0x1F008000, 0x1F408000},
	OpFNMADD: {0x1F200000, 0x1F600000},
	OpFNMSUB: {0x1F208000, 0x1F608000},
}

func (e *Encoder) Encode(inst asm.Instruction) ([]byte, error) {
	op := Op(inst.Op)
	switch op {

	// -----------------------------------------------------------------------
	// Arithmetic / bitwise / shift — register
	// -----------------------------------------------------------------------

	case OpADD, OpADDS, OpSUB, OpSUBS, OpMUL, OpMNEG, OpSDIV, OpUDIV,
		OpADC, OpADCS, OpSBC, OpSBCS, OpAND, OpANDS, OpORR, OpEOR,
		OpBIC, OpBICS, OpEON, OpORN, OpLSL, OpLSR, OpASR, OpROR:
		d, n, m, err := e.decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(reg3Opcodes[op], d, n, m)

	case OpNEG: // NEG Xd, Xm  →  SUB Xd, XZR, Xm
		d, m, err := e.decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xCB000000, d, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | 0x1F<<5 | reg(d)), nil

	case OpNEGS: // NEGS Xd, Xm  →  SUBS Xd, XZR, Xm
		d, m, err := e.decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xEB000000, d, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | 0x1F<<5 | reg(d)), nil

	case OpMADD:
		d, n, m, a, err := e.decodeReg4(inst)
		if err != nil {
			return nil, err
		}
		return encR4(0x9B000000, d, n, m, a)

	case OpMSUB:
		d, n, m, a, err := e.decodeReg4(inst)
		if err != nil {
			return nil, err
		}
		return encR4(0x9B008000, d, n, m, a)

	// -----------------------------------------------------------------------
	// Arithmetic — immediate
	// -----------------------------------------------------------------------

	case OpADDI, OpADDSI, OpSUBI, OpSUBSI:
		d, n, imm, err := e.decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return encRImm12(arithImmOpcodes[op], d, n, imm)

	case OpMVN: // MVN Xd, Xm  →  ORN Xd, XZR, Xm
		d, m, err := e.decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xAA2003E0, d, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Bitwise — immediate  (logical immediate encoding, N=1 for 64-bit)
	// -----------------------------------------------------------------------

	case OpANDI, OpANDSI, OpORRI, OpEORI:
		d, n, imm, err := e.decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		word, err := logicalImmediate(logicalImmOpcodes[op], d, n, imm)
		if err != nil {
			return nil, err
		}
		return enc(word), nil

	// -----------------------------------------------------------------------
	// Shift — immediate  (encoded as UBFM / SBFM / EXTR)
	// -----------------------------------------------------------------------

	case OpLSLI:
		d, n, shift, err := e.decodeRegShift(inst)
		if err != nil {
			return nil, err
		}
		base, mask, err := bitfieldBase(0xD3400000, d, n)
		if err != nil {
			return nil, err
		}
		s := uint32(shift) & mask
		return enc(base | ((-s)&mask)<<16 | (mask-s)<<10 | reg(n)<<5 | reg(d)), nil

	case OpLSRI:
		d, n, shift, err := e.decodeRegShift(inst)
		if err != nil {
			return nil, err
		}
		base, mask, err := bitfieldBase(0xD3400000, d, n)
		if err != nil {
			return nil, err
		}
		s := uint32(shift) & mask
		return enc(base | s<<16 | mask<<10 | reg(n)<<5 | reg(d)), nil

	case OpASRI:
		d, n, shift, err := e.decodeRegShift(inst)
		if err != nil {
			return nil, err
		}
		base, mask, err := bitfieldBase(0x93400000, d, n)
		if err != nil {
			return nil, err
		}
		s := uint32(shift) & mask
		return enc(base | s<<16 | mask<<10 | reg(n)<<5 | reg(d)), nil

	case OpRORI:
		d, n, shift, err := e.decodeRegShift(inst)
		if err != nil {
			return nil, err
		}
		base, mask, err := bitfieldBase(0x93C00000, d, n)
		if err != nil {
			return nil, err
		}
		s := uint32(shift) & mask
		return enc(base | reg(n)<<16 | s<<10 | reg(n)<<5 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Bit manipulation
	// -----------------------------------------------------------------------

	case OpCLZ, OpRBIT, OpREV16, OpREV32:
		d, n, err := e.decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encR2(reg2Opcodes[op], d, n)

	case OpREV:
		d, n, err := e.decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xDAC00C00, d, n)
		if err != nil {
			return nil, err
		}
		if d.Width() == asm.Width32 {
			base = 0x5AC00800
		}
		return enc(base | reg(n)<<5 | reg(d)), nil

	case OpSXTB, OpSXTH, OpSXTW, OpUXTB, OpUXTH, OpUXTW:
		d, n, err := e.decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		ext := extendOpcodes[op]
		return enc(ext.base | ext.imms<<10 | reg(n)<<5 | reg(d)), nil

	// -----------------------------------------------------------------------
	// TST
	// -----------------------------------------------------------------------

	case OpTST: // ANDS XZR, Xn, Xm
		return e.encodeCompareReg(0xEA00001F, inst)

	case OpTSTI:
		n, imm, err := e.decodeCmpImm(inst)
		if err != nil {
			return nil, err
		}
		word, err := logicalImmediate(0xF200001F, n, n, imm)
		if err != nil {
			return nil, err
		}
		return enc(word), nil

	// -----------------------------------------------------------------------
	// Compare
	// -----------------------------------------------------------------------

	case OpCMP: // SUBS XZR, Xn, Xm
		return e.encodeCompareReg(0xEB00001F, inst)

	case OpCMPI: // SUBS XZR, Xn, #imm
		return e.encodeCompareImm(0xF100001F, inst)

	case OpCMN: // ADDS XZR, Xn, Xm
		return e.encodeCompareReg(0xAB00001F, inst)

	case OpCMNI: // ADDS XZR, Xn, #imm
		return e.encodeCompareImm(0xB100001F, inst)

	case OpCCMP:
		n, m, err := e.decodeCmp(inst)
		if err != nil {
			return nil, err
		}
		flags, ok := inst.Src3.(asm.ImmOperand)
		if !ok {
			return nil, ErrMissingImmediate
		}
		base, err := intBase(0xFA400000, n, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | (uint32(flags.Value>>4)&0xF)<<12 | reg(n)<<5 | uint32(flags.Value)&0xF), nil

	case OpCCMPI:
		n, imm, err := e.decodeCmpImm(inst)
		if err != nil {
			return nil, err
		}
		flags, ok := inst.Src3.(asm.ImmOperand)
		if !ok {
			return nil, ErrMissingImmediate
		}
		base, err := intBase(0xFA400800, n)
		if err != nil {
			return nil, err
		}
		return enc(base | (uint32(imm)&0x1F)<<16 | (uint32(flags.Value>>4)&0xF)<<12 | reg(n)<<5 | uint32(flags.Value)&0xF), nil

	// -----------------------------------------------------------------------
	// Move
	// -----------------------------------------------------------------------

	case OpMOV: // MOV Xd, Xn  →  ORR Xd, XZR, Xn
		d, n, err := e.decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xAA0003E0, d, n)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(n)<<16 | reg(d)), nil

	case OpMOVI: // pseudo: MOVZ + MOVK sequence; emit MOVZ for first 16 bits
		d, imm64, err := e.decodeDstImm(inst)
		if err != nil {
			return nil, err
		}
		if err := validMoveImmediate(d, 0); err != nil {
			return nil, err
		}
		u := uint64(imm64)
		if d.Width() == asm.Width32 {
			return enc(0x52800000 | (uint32(u)&0xFFFF)<<5 | reg(d)), nil
		}
		return enc(0xD2800000 | (uint32(u)&0xFFFF)<<5 | reg(d)), nil

	case OpMOVZ, OpMOVK, OpMOVN:
		mv := moveOpcodes[op]
		return e.encodeMovImmediate(mv.op32, mv.op64, inst)

	// -----------------------------------------------------------------------
	// Load / Store  unsigned offset
	// -----------------------------------------------------------------------

	case OpLDR, OpLDRB, OpLDRSB, OpLDRH, OpLDRSH, OpLDRSW:
		ld := loadOpcodes[op]
		return e.encodeLoad(ld.base, ld.scale, inst)

	case OpSTR, OpSTRB, OpSTRH, OpSTRW:
		st := storeOpcodes[op]
		return e.encodeStore(st.base, st.scale, inst)

	// -----------------------------------------------------------------------
	// Load / Store  register-offset  [Xbase, Xoffset]
	// -----------------------------------------------------------------------

	case OpLDRR:
		// LDR Xt, [Xbase, Xm, LSL #3]  — extended register
		d, base, m, err := e.decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xF8607800 | reg(m)<<16 | reg(base)<<5 | reg(d)), nil

	case OpSTRR:
		// STR Xt, [Xbase, Xm, LSL #3]
		// inst encoding: Dst=base, Src1=src, Src2=offsetReg
		d, n, m, err := e.decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xF8207800 | reg(m)<<16 | reg(d)<<5 | reg(n)), nil

	// -----------------------------------------------------------------------
	// Load / Store pair
	// -----------------------------------------------------------------------

	case OpLDP:
		// LDP Xt1, Xt2, [Xbase, #offset]
		// Encoding: Dst=Xt1, Src1=Mem(base,offset), Src2=Xt2
		d1, base, offset, err := e.decodeMemOp(inst)
		if err != nil {
			return nil, err
		}
		d2, ok := inst.Src2.(asm.PRegOperand)
		if !ok {
			return nil, ErrMissingDestinationReg
		}
		simm7 := uint32(offset/8) & 0x7F
		return enc(0xA9400000 | simm7<<15 | reg(d2.Reg)<<10 | reg(base)<<5 | reg(d1)), nil

	case OpSTP:
		// STP Xt1, Xt2, [Xbase, #offset]
		// Encoding: Dst=Mem(base,offset), Src1=Xt1, Src2=Xt2
		src1, base, offset, err := e.decodeStrOp(inst)
		if err != nil {
			return nil, err
		}
		src2, ok := inst.Src2.(asm.PRegOperand)
		if !ok {
			return nil, ErrMissingSourceReg
		}
		simm7 := uint32(offset/8) & 0x7F
		return enc(0xA9000000 | simm7<<15 | reg(src2.Reg)<<10 | reg(base)<<5 | reg(src1)), nil

	// -----------------------------------------------------------------------
	// Float — convert
	// -----------------------------------------------------------------------

	case OpSCVTF, OpUCVTF, OpFCVTZS, OpFCVTZU:
		d, n, err := e.decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		cv := cvtOpcodes[op]
		intReg, floatReg := d, n
		if !cv.toInt {
			intReg, floatReg = n, d
		}
		if intReg.Type() != asm.RegTypeInt || floatReg.Type() != asm.RegTypeFloat ||
			(intReg.Width() != asm.Width32 && intReg.Width() != asm.Width64) ||
			(floatReg.Width() != asm.Width32 && floatReg.Width() != asm.Width64) {
			return nil, asm.ErrInvalidOperand
		}
		word := cv.base
		if intReg.Width() == asm.Width64 {
			word |= 1 << 31 // sf: 64-bit integer register
		}
		if floatReg.Width() == asm.Width64 {
			word |= 1 << 22 // ftype: double precision
		}
		return enc(word | reg(n)<<5 | reg(d)), nil

	case OpFCVT:
		d, n, err := e.decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		if d.Type() != asm.RegTypeFloat || n.Type() != asm.RegTypeFloat {
			return nil, asm.ErrInvalidOperand
		}
		switch {
		case d.Width() == asm.Width64 && n.Width() == asm.Width32:
			return enc(0x1E22C000 | reg(n)<<5 | reg(d)), nil // FCVT Dd, Sn
		case d.Width() == asm.Width32 && n.Width() == asm.Width64:
			return enc(0x1E624000 | reg(n)<<5 | reg(d)), nil // FCVT Sd, Dn
		default:
			return nil, asm.ErrInvalidOperand
		}
		// -----------------------------------------------------------------------
		// Float — arithmetic (double precision)
		// -----------------------------------------------------------------------

	case OpFADD, OpFSUB, OpFMUL, OpFDIV, OpFMIN, OpFMAX:
		fb := floatBinaryOpcodes[op]
		return e.encodeFloatBinary(fb.single, fb.double, inst)

	// -----------------------------------------------------------------------
	// SIMD (fixed 8B arrangement): CNT, ADDV
	// -----------------------------------------------------------------------

	case OpCNT, OpADDV:
		d, n, err := e.decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		base := uint32(0x0E205800) // CNT Vd.8B, Vn.8B
		if op == OpADDV {
			base = 0x0E31B800 // ADDV Bd, Vn.8B
		}
		return enc(base | reg(n)<<5 | reg(d)), nil

	case OpFMADD, OpFMSUB, OpFNMADD, OpFNMSUB:
		ft := floatTernaryOpcodes[op]
		return e.encodeFloatTernary(ft.single, ft.double, inst)

	// -----------------------------------------------------------------------
	// Float — unary
	// -----------------------------------------------------------------------

	case OpFABS, OpFNEG, OpFSQRT, OpFRINTN, OpFRINTM, OpFRINTP, OpFRINTZ:
		d, n, err := e.decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		fu := floatUnaryOpcodes[op]
		return encodeFloatUnary(fu.single, fu.double, d, n)

	// -----------------------------------------------------------------------
	// Float — move / compare
	// -----------------------------------------------------------------------

	case OpFMOV:
		d, n, err := e.decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		if (d.Width() != asm.Width32 && d.Width() != asm.Width64) ||
			(n.Width() != asm.Width32 && n.Width() != asm.Width64) {
			return nil, asm.ErrInvalidOperand
		}
		dFloat := d.Type() == asm.RegTypeFloat
		nFloat := n.Type() == asm.RegTypeFloat
		switch {
		case dFloat && nFloat: // float → float register copy
			if d.Width() != n.Width() {
				return nil, asm.ErrInvalidOperand
			}
			if d.Width() == asm.Width32 {
				return enc(0x1E204000 | reg(n)<<5 | reg(d)), nil // FMOV Sd, Sn
			}
			return enc(0x1E604000 | reg(n)<<5 | reg(d)), nil // FMOV Dd, Dn
		case dFloat && !nFloat: // int → float (bit-copy)
			if n.Type() != asm.RegTypeInt {
				return nil, asm.ErrInvalidOperand
			}
			if d.Width() == asm.Width32 {
				// Accept Width32 or Width64 int source; Width64 uses its low 32 bits.
				if n.Width() != asm.Width32 && n.Width() != asm.Width64 {
					return nil, asm.ErrInvalidOperand
				}
				return enc(0x1E270000 | reg(n)<<5 | reg(d)), nil // FMOV Sd, Wn
			}
			if d.Width() != asm.Width64 || n.Width() != asm.Width64 {
				return nil, asm.ErrInvalidOperand
			}
			return enc(0x9E670000 | reg(n)<<5 | reg(d)), nil // FMOV Dd, Xn
		case !dFloat && nFloat: // float → int (bit-copy)
			if d.Type() != asm.RegTypeInt {
				return nil, asm.ErrInvalidOperand
			}
			if n.Width() == asm.Width32 {
				// Accept Width32 or Width64 int destination; Width64 zero-extends.
				if d.Width() != asm.Width32 && d.Width() != asm.Width64 {
					return nil, asm.ErrInvalidOperand
				}
				return enc(0x1E260000 | reg(n)<<5 | reg(d)), nil // FMOV Wn, Sd (zero-extends to Xn)
			}
			if n.Width() != asm.Width64 || d.Width() != asm.Width64 {
				return nil, asm.ErrInvalidOperand
			}
			return enc(0x9E660000 | reg(n)<<5 | reg(d)), nil // FMOV Xn, Dd
		default:
			return nil, asm.ErrInvalidOperand
		}

	case OpFCMP:
		n, m, err := e.decodeCmp(inst)
		if err != nil {
			return nil, err
		}
		if err := floatMatch(n, m); err != nil {
			return nil, err
		}
		if n.Width() == asm.Width32 {
			return enc(0x1E202000 | reg(m)<<16 | reg(n)<<5), nil // FCMP Sn, Sm
		}
		return enc(0x1E602000 | reg(m)<<16 | reg(n)<<5), nil // FCMP Dn, Dm

	case OpFCMPE:
		n, m, err := e.decodeCmp(inst)
		if err != nil {
			return nil, err
		}
		if err := floatMatch(n, m); err != nil {
			return nil, err
		}
		if n.Width() == asm.Width32 {
			return enc(0x1E202010 | reg(m)<<16 | reg(n)<<5), nil // FCMPE Sn, Sm
		}
		return enc(0x1E602010 | reg(m)<<16 | reg(n)<<5), nil // FCMPE Dn, Dm

	// -----------------------------------------------------------------------
	// Conditional select
	// -----------------------------------------------------------------------

	case OpCSEL, OpCSINC, OpCSINV, OpCSNEG: // CSxx Xd, Xn, Xm, cond
		d, n, m, cond, err := e.decodeSelect(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(selectOpcodes[op], d, n, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | cond<<12 | reg(n)<<5 | reg(d)), nil

	case OpCSET, OpCSETM: // CSET(M) Xd, cond  →  CSINC/CSINV Xd, XZR, XZR, !cond
		d, condImm, err := e.decodeDstImm(inst)
		if err != nil {
			return nil, err
		}
		cond := uint32(condImm)&0xF ^ 1 // invert lsb to negate condition
		word := uint32(0x9A9F07E0)
		if op == OpCSETM {
			word = 0xDA9F03E0
		}
		base, err := intBase(word, d)
		if err != nil {
			return nil, err
		}
		return enc(base | cond<<12 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Branch — unconditional
	// -----------------------------------------------------------------------

	case OpB:
		offset, err := e.decodeBranch(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x14000000 | (uint32(offset/4) & 0x3FFFFFF)), nil

	case OpBL:
		offset, err := e.decodeBranch(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x94000000 | (uint32(offset/4) & 0x3FFFFFF)), nil

	case OpBR:
		r, err := e.decodeRegOnly(inst)
		if err != nil {
			return nil, err
		}
		if r.Type() != asm.RegTypeInt || r.Width() != asm.Width64 {
			return nil, asm.ErrInvalidOperand
		}
		return enc(0xD61F0000 | reg(r)<<5), nil

	case OpBLR:
		r, err := e.decodeRegOnly(inst)
		if err != nil {
			return nil, err
		}
		if r.Type() != asm.RegTypeInt || r.Width() != asm.Width64 {
			return nil, asm.ErrInvalidOperand
		}
		return enc(0xD63F0000 | reg(r)<<5), nil

	case OpRET:
		// RET X30 (default link register)
		return enc(0xD65F03C0), nil

	// -----------------------------------------------------------------------
	// Branch — compare-and-branch
	// -----------------------------------------------------------------------

	case OpCBZ:
		r, offset, err := e.decodeRegBranch(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xB4000000, r)
		if err != nil {
			return nil, err
		}
		imm19 := (uint32(offset/4) & 0x7FFFF) << 5
		return enc(base | imm19 | reg(r)), nil

	case OpCBNZ:
		r, offset, err := e.decodeRegBranch(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xB5000000, r)
		if err != nil {
			return nil, err
		}
		imm19 := (uint32(offset/4) & 0x7FFFF) << 5
		return enc(base | imm19 | reg(r)), nil

	// -----------------------------------------------------------------------
	// Branch — test-and-branch
	// -----------------------------------------------------------------------

	case OpTBZ:
		r, bit, offset, err := e.decodeTestBranch(inst)
		if err != nil {
			return nil, err
		}
		if err := validTestBit(r, bit); err != nil {
			return nil, err
		}
		b5 := (uint32(bit) >> 5) & 1 // b5 lives in bit 31
		b40 := uint32(bit) & 0x1F    // b40 lives in bits[23:19]
		imm14 := (uint32(offset/4) & 0x3FFF) << 5
		return enc(0x36000000 | b5<<31 | b40<<19 | imm14 | reg(r)), nil

	case OpTBNZ:
		r, bit, offset, err := e.decodeTestBranch(inst)
		if err != nil {
			return nil, err
		}
		if err := validTestBit(r, bit); err != nil {
			return nil, err
		}
		b5 := (uint32(bit) >> 5) & 1
		b40 := uint32(bit) & 0x1F
		imm14 := (uint32(offset/4) & 0x3FFF) << 5
		return enc(0x37000000 | b5<<31 | b40<<19 | imm14 | reg(r)), nil

	// -----------------------------------------------------------------------
	// Branch — conditional  (B.cond)
	// -----------------------------------------------------------------------

	case OpBEQ, OpBNE, OpBCS, OpBCC, OpBMI, OpBPL,
		OpBVS, OpBVC, OpBHI, OpBLS, OpBGE, OpBLT, OpBGT, OpBLE:
		offset, err := e.decodeBranch(inst)
		if err != nil {
			return nil, err
		}
		cond := condCode[op]
		imm19 := (uint32(offset/4) & 0x7FFFF) << 5
		return enc(0x54000000 | imm19 | cond), nil

	// -----------------------------------------------------------------------
	// System
	// -----------------------------------------------------------------------

	case OpNOP:
		return enc(0xD503201F), nil

	case OpHLT:
		return enc(0xD4400000), nil // HLT #0

	case OpBRK:
		imm, err := e.decodeBranch(inst) // reuse: imm16 in Src2
		if err != nil {
			return nil, err
		}
		return enc(0xD4200000 | (uint32(imm)&0xFFFF)<<5), nil

	case OpSVC:
		imm, err := e.decodeBranch(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xD4000001 | (uint32(imm)&0xFFFF)<<5), nil

	case OpERET:
		return enc(0xD69F03E0), nil

	case OpMRS:
		// MRS Xt, sysreg  — sysreg encoded in Src1 as ImmOperand
		d, sysreg, err := e.decodeDstImm(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xD5300000 | (uint32(sysreg)&0xFFFF)<<5 | reg(d)), nil

	case OpMSR:
		// MSR sysreg, Xt  — sysreg in Dst(ImmOperand), Xt in Src1
		sysregOp, ok := inst.Dst.(asm.ImmOperand)
		if !ok {
			return nil, ErrMissingImmediate
		}
		srcOp, ok := inst.Src1.(asm.PRegOperand)
		if !ok {
			return nil, ErrMissingSourceReg
		}
		return enc(0xD5100000 | (uint32(sysregOp.Value)&0xFFFF)<<5 | reg(srcOp.Reg)), nil

	case OpISB:
		return enc(0xD5033FDF), nil // ISB SY

	case OpDSB:
		return enc(0xD5033F9F), nil // DSB SY

	case OpDMB:
		return enc(0xD5033BBF), nil // DMB ISH

	default:
		return nil, ErrUnsupportedOpcode
	}
}

// ---------------------------------------------------------------------------
// Per-family encoders
// ---------------------------------------------------------------------------

// encodeMovImmediate emits a MOVZ/MOVK/MOVN-style wide-immediate move, picking
// op32 or op64 by destination width.
func (e *Encoder) encodeMovImmediate(op32, op64 uint32, inst asm.Instruction) ([]byte, error) {
	d, imm, shift, err := e.decodeMovImm(inst)
	if err != nil {
		return nil, err
	}
	if err := validMoveImmediate(d, shift); err != nil {
		return nil, err
	}
	hw := uint32(shift/16) & 3
	base := op64
	if d.Width() == asm.Width32 {
		base = op32
	}
	return enc(base | hw<<21 | (uint32(imm)&0xFFFF)<<5 | reg(d)), nil
}

// encodeCompareReg emits a register-form compare/test (CMP/CMN/TST → SUBS/ADDS/
// ANDS with XZR destination).
func (e *Encoder) encodeCompareReg(op uint32, inst asm.Instruction) ([]byte, error) {
	n, m, err := e.decodeCmp(inst)
	if err != nil {
		return nil, err
	}
	base, err := intBase(op, n, m)
	if err != nil {
		return nil, err
	}
	return enc(base | reg(m)<<16 | reg(n)<<5), nil
}

// encodeCompareImm emits an immediate-form compare (CMPI/CMNI).
func (e *Encoder) encodeCompareImm(op uint32, inst asm.Instruction) ([]byte, error) {
	n, imm, err := e.decodeCmpImm(inst)
	if err != nil {
		return nil, err
	}
	base, err := intBase(op, n)
	if err != nil {
		return nil, err
	}
	return enc(base | (uint32(imm)&0xFFF)<<10 | reg(n)<<5), nil
}

// encodeLoad emits an unsigned-offset load, scaling the byte offset by the
// access size.
func (e *Encoder) encodeLoad(op uint32, scale int64, inst asm.Instruction) ([]byte, error) {
	dst, base, offset, err := e.decodeMemOp(inst)
	if err != nil {
		return nil, err
	}
	pimm := uint32(offset/scale) & 0xFFF
	return enc(op | pimm<<10 | reg(base)<<5 | reg(dst)), nil
}

// encodeStore emits an unsigned-offset store, scaling the byte offset by the
// access size.
func (e *Encoder) encodeStore(op uint32, scale int64, inst asm.Instruction) ([]byte, error) {
	src, base, offset, err := e.decodeStrOp(inst)
	if err != nil {
		return nil, err
	}
	pimm := uint32(offset/scale) & 0xFFF
	return enc(op | pimm<<10 | reg(base)<<5 | reg(src)), nil
}

// encodeFloatBinary emits a 3-register scalar float op (FADD/FSUB/FMUL/FDIV),
// picking op32 or op64 by destination width.
func (e *Encoder) encodeFloatBinary(op32, op64 uint32, inst asm.Instruction) ([]byte, error) {
	d, n, m, err := e.decodeReg3(inst)
	if err != nil {
		return nil, err
	}
	if err := floatMatch(d, n, m); err != nil {
		return nil, err
	}
	base := op64
	if d.Width() == asm.Width32 {
		base = op32
	}
	return enc(base | reg(m)<<16 | reg(n)<<5 | reg(d)), nil
}

// encodeFloatTernary emits a 4-register scalar float op (FMADD-family), picking
// op32 or op64 by destination width.
func (e *Encoder) encodeFloatTernary(op32, op64 uint32, inst asm.Instruction) ([]byte, error) {
	d, n, m, a, err := e.decodeReg4(inst)
	if err != nil {
		return nil, err
	}
	if err := floatMatch(d, n, m, a); err != nil {
		return nil, err
	}
	base := op64
	if d.Width() == asm.Width32 {
		base = op32
	}
	return enc(base | reg(m)<<16 | reg(a)<<10 | reg(n)<<5 | reg(d)), nil
}

// ---------------------------------------------------------------------------
// Operand decoders
// ---------------------------------------------------------------------------

// reg extracts the 5-bit register ID from a PReg.
func reg(r asm.PReg) uint32 { return uint32(r.ID()) & 0x1F }

// encR3 emits a standard 3-register instruction (Rm<<16 | Rn<<5 | Rd).
func encR3(base uint32, d, n, m asm.PReg) ([]byte, error) {
	b, err := intBase(base, d, n, m)
	if err != nil {
		return nil, err
	}
	return enc(b | reg(m)<<16 | reg(n)<<5 | reg(d)), nil
}

// encR2 emits a standard 2-register instruction (Rn<<5 | Rd).
func encR2(base uint32, d, n asm.PReg) ([]byte, error) {
	b, err := intBase(base, d, n)
	if err != nil {
		return nil, err
	}
	return enc(b | reg(n)<<5 | reg(d)), nil
}

// encR4 emits a standard 4-register instruction (Rm<<16 | Ra<<10 | Rn<<5 | Rd).
func encR4(base uint32, d, n, m, a asm.PReg) ([]byte, error) {
	b, err := intBase(base, d, n, m, a)
	if err != nil {
		return nil, err
	}
	return enc(b | reg(m)<<16 | reg(a)<<10 | reg(n)<<5 | reg(d)), nil
}

// encRImm12 emits an arithmetic-immediate (imm12<<10 | Rn<<5 | Rd).
func encRImm12(base uint32, d, n asm.PReg, imm int64) ([]byte, error) {
	b, err := intBase(base, d, n)
	if err != nil {
		return nil, err
	}
	return enc(b | (uint32(imm)&0xFFF)<<10 | reg(n)<<5 | reg(d)), nil
}

// sameKind verifies that every reg has the given type and a uniform 32- or
// 64-bit width. Returns the shared width.
func sameKind(typ asm.RegType, regs ...asm.PReg) (asm.RegWidth, error) {
	if len(regs) == 0 {
		return 0, asm.ErrInvalidOperand
	}
	width := regs[0].Width()
	if regs[0].Type() != typ || (width != asm.Width32 && width != asm.Width64) {
		return 0, asm.ErrInvalidOperand
	}
	for _, r := range regs[1:] {
		if r.Type() != typ || r.Width() != width {
			return 0, asm.ErrInvalidOperand
		}
	}
	return width, nil
}

func intBase(base uint32, regs ...asm.PReg) (uint32, error) {
	width, err := sameKind(asm.RegTypeInt, regs...)
	if err != nil {
		return 0, err
	}
	if width == asm.Width32 {
		base &^= 1 << 31
	}
	return base, nil
}

func logicalImmediate(base uint32, dst, src asm.PReg, imm int64) (uint32, error) {
	base, err := intBase(base, dst, src)
	if err != nil {
		return 0, err
	}
	is64 := dst.Width() == asm.Width64
	immr, imms, ok := encodeLogicalImm(uint64(imm), is64)
	if !ok {
		return 0, ErrMissingImmediate
	}
	if is64 {
		base |= 1 << 22
	} else {
		base &^= 1 << 22
	}
	return base | immr<<16 | imms<<10 | reg(src)<<5 | reg(dst), nil
}

func bitfieldBase(base uint32, dst, src asm.PReg) (uint32, uint32, error) {
	base, err := intBase(base, dst, src)
	if err != nil {
		return 0, 0, err
	}
	if dst.Width() == asm.Width32 {
		return base &^ (1 << 22), 31, nil
	}
	return base, 63, nil
}

func validMoveImmediate(dst asm.PReg, shift int64) error {
	if _, err := intBase(0, dst); err != nil {
		return err
	}
	if shift < 0 || shift%16 != 0 || shift > 48 || (dst.Width() == asm.Width32 && shift > 16) {
		return asm.ErrInvalidOperand
	}
	return nil
}

func validTestBit(src asm.PReg, bit uint8) error {
	if _, err := intBase(0, src); err != nil {
		return err
	}
	if bit >= 64 || (src.Width() == asm.Width32 && bit >= 32) {
		return asm.ErrInvalidOperand
	}
	return nil
}

func floatMatch(regs ...asm.PReg) error {
	_, err := sameKind(asm.RegTypeFloat, regs...)
	return err
}

func encodeFloatUnary(single, double uint32, dst, src asm.PReg) ([]byte, error) {
	if err := floatMatch(dst, src); err != nil {
		return nil, err
	}
	if dst.Width() == asm.Width32 {
		return enc(single | reg(src)<<5 | reg(dst)), nil
	}
	return enc(double | reg(src)<<5 | reg(dst)), nil
}

func (e *Encoder) decodeReg4(inst asm.Instruction) (dst, src1, src2, src3 asm.PReg, err error) {
	dstOp, ok := inst.Dst.(asm.PRegOperand)
	if !ok {
		err = ErrMissingDestinationReg
		return
	}
	s1, ok1 := inst.Src1.(asm.PRegOperand)
	s2, ok2 := inst.Src2.(asm.PRegOperand)
	s3, ok3 := inst.Src3.(asm.PRegOperand)
	if !ok1 || !ok2 || !ok3 {
		err = ErrMissingSourceRegs
		return
	}
	return dstOp.Reg, s1.Reg, s2.Reg, s3.Reg, nil
}

func (e *Encoder) decodeReg3(inst asm.Instruction) (dst, src1, src2 asm.PReg, err error) {
	dstOp, ok := inst.Dst.(asm.PRegOperand)
	if !ok {
		err = ErrMissingDestinationReg
		return
	}
	s1, ok1 := inst.Src1.(asm.PRegOperand)
	s2, ok2 := inst.Src2.(asm.PRegOperand)
	if !ok1 || !ok2 {
		err = ErrMissingSourceRegs
		return
	}
	return dstOp.Reg, s1.Reg, s2.Reg, nil
}

func (e *Encoder) decodeReg2(inst asm.Instruction) (dst, src asm.PReg, err error) {
	dstOp, ok := inst.Dst.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, ErrMissingDestinationReg
	}
	srcOp, ok := inst.Src1.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, ErrMissingSourceReg
	}
	return dstOp.Reg, srcOp.Reg, nil
}

func (e *Encoder) decodeRegImm(inst asm.Instruction) (dst, src asm.PReg, imm int64, err error) {
	dstOp, ok := inst.Dst.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, 0, ErrMissingDestinationReg
	}
	srcOp, ok := inst.Src1.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, 0, ErrMissingSourceReg
	}
	immOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, 0, ErrMissingImmediate
	}
	return dstOp.Reg, srcOp.Reg, immOp.Value, nil
}

// decodeRegShift decodes (dst, src, shift_amount) for immediate-shift instructions.
func (e *Encoder) decodeRegShift(inst asm.Instruction) (dst, src asm.PReg, shift int64, err error) {
	return e.decodeRegImm(inst)
}

func (e *Encoder) decodeCmp(inst asm.Instruction) (src1, src2 asm.PReg, err error) {
	s1, ok := inst.Src1.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, ErrMissingSourceReg
	}
	s2, ok := inst.Src2.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, ErrMissingSourceReg
	}
	return s1.Reg, s2.Reg, nil
}

func (e *Encoder) decodeCmpImm(inst asm.Instruction) (src asm.PReg, imm int64, err error) {
	srcOp, ok := inst.Src1.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, 0, ErrMissingSourceReg
	}
	immOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return asm.PReg{}, 0, ErrMissingImmediate
	}
	return srcOp.Reg, immOp.Value, nil
}

func (e *Encoder) decodeSelect(inst asm.Instruction) (dst, src1, src2 asm.PReg, cond uint32, err error) {
	dstOp, ok := inst.Dst.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, asm.PReg{}, 0, ErrMissingDestinationReg
	}
	s1, ok := inst.Src1.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, asm.PReg{}, 0, ErrMissingSourceReg
	}
	condOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, asm.PReg{}, 0, ErrMissingImmediate
	}
	s2, ok := inst.Src3.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, asm.PReg{}, 0, ErrMissingSourceReg
	}
	return dstOp.Reg, s1.Reg, s2.Reg, uint32(condOp.Value) & 0xF, nil
}

func (e *Encoder) decodeMovImm(inst asm.Instruction) (dst asm.PReg, imm, shift int64, err error) {
	dstOp, ok := inst.Dst.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, 0, 0, ErrMissingDestinationReg
	}
	immOp, ok := inst.Src1.(asm.ImmOperand)
	if !ok {
		return asm.PReg{}, 0, 0, ErrMissingImmediate
	}
	shiftOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return asm.PReg{}, 0, 0, ErrMissingShiftImmediate
	}
	return dstOp.Reg, immOp.Value, shiftOp.Value, nil
}

// decodeDstImm decodes instructions where Dst is a register and Src1 is an immediate.
func (e *Encoder) decodeDstImm(inst asm.Instruction) (dst asm.PReg, imm int64, err error) {
	dstOp, ok := inst.Dst.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, 0, ErrMissingDestinationReg
	}
	immOp, ok := inst.Src1.(asm.ImmOperand)
	if !ok {
		return asm.PReg{}, 0, ErrMissingImmediate
	}
	return dstOp.Reg, immOp.Value, nil
}

func (e *Encoder) decodeMemOp(inst asm.Instruction) (dst, base asm.PReg, offset int64, err error) {
	dstOp, ok := inst.Dst.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, 0, ErrMissingDestinationReg
	}
	memOp, ok := inst.Src1.(asm.MemOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, 0, ErrMissingMemoryOperand
	}
	baseReg, ok := memOp.Base.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, 0, ErrMissingMemoryOperand
	}
	return dstOp.Reg, baseReg.Reg, memOp.Offset, nil
}

func (e *Encoder) decodeStrOp(inst asm.Instruction) (src, base asm.PReg, offset int64, err error) {
	srcOp, ok := inst.Src1.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, 0, ErrMissingSourceReg
	}
	memOp, ok := inst.Dst.(asm.MemOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, 0, ErrMissingMemoryOperand
	}
	baseReg, ok := memOp.Base.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, asm.PReg{}, 0, ErrMissingMemoryOperand
	}
	return srcOp.Reg, baseReg.Reg, memOp.Offset, nil
}

func (e *Encoder) decodeRegOnly(inst asm.Instruction) (asm.PReg, error) {
	op, ok := inst.Src1.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, ErrMissingRegisterOperand
	}
	return op.Reg, nil
}

func (e *Encoder) decodeBranch(inst asm.Instruction) (int64, error) {
	immOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return 0, ErrMissingBranchOffset
	}
	return immOp.Value, nil
}

func (e *Encoder) decodeRegBranch(inst asm.Instruction) (r asm.PReg, offset int64, err error) {
	rOp, ok := inst.Src1.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, 0, ErrMissingRegisterOperand
	}
	immOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return asm.PReg{}, 0, ErrMissingBranchOffset
	}
	return rOp.Reg, immOp.Value, nil
}

// decodeTestBranch unpacks the packed (bit | offset<<8) encoding used by TBZ/TBNZ.
func (e *Encoder) decodeTestBranch(inst asm.Instruction) (r asm.PReg, bit uint8, offset int64, err error) {
	rOp, ok := inst.Src1.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, 0, 0, ErrMissingRegisterOperand
	}
	immOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return asm.PReg{}, 0, 0, ErrMissingBranchOffset
	}
	packed := immOp.Value
	return rOp.Reg, uint8(packed & 0xFF), packed >> 8, nil
}

// ---------------------------------------------------------------------------
// Logical immediate encoder
//
// AArch64 logical immediates must describe a pattern of the form:
//   a sequence of N ones rotated by R within a repeated element of size E,
//   where E ∈ {2,4,8,16,32,64} and the value is neither all-zeros nor all-ones.
//
// Returns (immr, imms, ok) packed as 6-bit fields ready for the instruction word.
// ---------------------------------------------------------------------------

func encodeLogicalImm(val uint64, is64 bool) (immr, imms uint32, ok bool) {
	width := uint(64)
	if !is64 {
		width = 32
		val &= uint64(^uint32(0))
	}
	if val == 0 || (is64 && val == ^uint64(0)) || (!is64 && val == uint64(^uint32(0))) {
		return 0, 0, false
	}

	// Try each element size from smallest to largest.
	for _, esize := range []uint{2, 4, 8, 16, 32, 64} {
		if !is64 && esize == 64 {
			continue
		}
		// Replicate pattern into esize-bit elements and check uniformity.
		mask := uint64((1 << esize) - 1)
		elem := val & mask
		// Check all elements are identical.
		uniform := true
		for i := uint(esize); i < width; i += esize {
			if (val>>i)&mask != elem {
				uniform = false
				break
			}
		}
		if !uniform {
			continue
		}

		// Count leading/trailing zeros/ones within the element.
		ones := uint(bits.OnesCount64(elem))
		if ones == 0 || ones == esize {
			continue // all-zeros or all-ones element
		}

		// Find rotation: number of trailing zeros before the first 1.
		tz := uint(bits.TrailingZeros64(elem))
		ro := (esize - tz) & (esize - 1)

		// Reconstruct and verify.
		canonical := rotateMask(esize, ones, ro)
		if canonical != elem {
			continue
		}

		// Encode N, immr, imms.
		// N  = 1 if esize==64, else 0
		// immr = rotation amount (6 bits)
		// imms encodes element size and number of ones:
		//   imms = NOT(esize) | (ones-1)  — the upper bits encode the element size
		var N uint32
		if esize == 64 {
			N = 1
		}
		immrVal := uint32(ro) & 0x3F
		// imms[5:0]: 0b0xxxxx where x encodes (esize, ones)
		// Standard encoding: imms = ~(esize) truncated to 6 bits, then OR ones-1
		immsVal := (^uint32(esize)&0x3F)&^(uint32(esize)-1) | uint32(ones-1)
		_ = N // N is packed into bit 22 of the instruction by the caller as the "N" field
		// For 64-bit logical immediates N=1 is handled by the caller OR'ing 1<<22.
		return immrVal, immsVal, true
	}
	return 0, 0, false
}

// rotateMask builds the canonical element: `ones` consecutive 1s rotated left by `rot`
// within an `esize`-bit field.
func rotateMask(esize, ones, rot uint) uint64 {
	mask := uint64((1 << ones) - 1) // ones consecutive 1s at lsb
	// rotate left by rot within esize bits
	rot &= esize - 1
	lo := mask << rot
	hi := mask >> (esize - rot)
	result := (lo | hi) & ((1 << esize) - 1)
	return result
}

// ---------------------------------------------------------------------------
// Little-endian word → byte slice
// ---------------------------------------------------------------------------

func enc(w uint32) []byte {
	return []byte{byte(w), byte(w >> 8), byte(w >> 16), byte(w >> 24)}
}
