package arm64

import (
	"errors"
	"fmt"
	"math/bits"

	"github.com/siyul-park/minivm/internal/asm"
)

// Encoder emits ARM64 machine encodings.
type Encoder struct{}

// operands reads an instruction's operands by kind, keeping the first
// operand that was missing or of the wrong kind as err.
type operands struct{ err error }

// Stable ARM64 encoder errors.
var (
	ErrUnsupportedOpcode      = errors.New("unsupported opcode")
	ErrMissingDestinationReg  = errors.New("missing destination register")
	ErrMissingSourceReg       = errors.New("missing source register")
	ErrMissingSourceRegs      = errors.New("missing source registers")
	ErrMissingImmediate       = errors.New("missing immediate")
	ErrMissingShiftImmediate  = errors.New("missing shift immediate")
	ErrMissingMemoryOperand   = errors.New("missing memory operand")
	ErrMissingRegisterOperand = errors.New("missing register operand")
	ErrMissingBranchOffset    = errors.New("missing branch offset")
)

var _ asm.Encoder = Encoder{}

// condCode maps a conditional-branch opcode to the 4-bit AArch64 condition code.
var condCode = map[Op]uint32{
	OpBEQ: 0x0,
	OpBNE: 0x1,
	OpBCS: 0x2,
	OpBCC: 0x3,
	OpBMI: 0x4,
	OpBPL: 0x5,
	OpBVS: 0x6,
	OpBVC: 0x7,
	OpBHI: 0x8,
	OpBLS: 0x9,
	OpBGE: 0xA,
	OpBLT: 0xB,
	OpBGT: 0xC,
	OpBLE: 0xD,
}

// extendOpcodes maps each sign/zero-extend opcode to its 32- and 64-bit
// SBFM/UBFM base words, picked by the destination's declared width, and the
// imms field (the source width minus one). SXTW and UXTW widen a 32-bit
// source into a 64-bit destination by definition - there is no same-width
// form - so their two base words are identical and dst is expected to stay
// Width64 wherever they are used.
var extendOpcodes = map[Op]struct{ op32, op64, imms uint32 }{
	OpSXTB: {0x13000000, 0x93400000, 7},
	OpSXTH: {0x13000000, 0x93400000, 15},
	OpSXTW: {0x93400000, 0x93400000, 31},
	OpUXTB: {0x53000000, 0xD3400000, 7},
	OpUXTH: {0x53000000, 0xD3400000, 15},
	OpUXTW: {0xD3400000, 0xD3400000, 31},
}

// floatUnaryOpcodes maps each scalar float unary opcode to its single- and
// double-precision base words.
var floatUnaryOpcodes = map[Op]struct{ single, double uint32 }{
	OpFABS:   {0x1E20C000, 0x1E60C000},
	OpFNEG:   {0x1E214000, 0x1E614000},
	OpFSQRT:  {0x1E21C000, 0x1E61C000},
	OpFRINTN: {0x1E244000, 0x1E644000},
	OpFRINTM: {0x1E254000, 0x1E654000},
	OpFRINTP: {0x1E24C000, 0x1E64C000},
	OpFRINTZ: {0x1E25C000, 0x1E65C000},
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

// loadOpcodes records width-specific encodings and memory strides.
// Byte/halfword loads ignore destination width; sign-extending loads use
// the matching destination-width encoding.
var loadOpcodes = map[Op]struct {
	op32, op64       uint32
	scale32, scale64 int64
}{
	OpLDR:   {0xB9400000, 0xF9400000, 4, 8},
	OpLDRB:  {0x39400000, 0x39400000, 1, 1},
	OpLDRSB: {0x39C00000, 0x39800000, 1, 1},
	OpLDRH:  {0x79400000, 0x79400000, 2, 2},
	OpLDRSH: {0x79C00000, 0x79800000, 2, 2},
	OpLDRSW: {0xB9800000, 0xB9800000, 4, 4},
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

// Encode converts one architecture-neutral instruction to ARM64 machine code.
func (Encoder) Encode(inst asm.Instruction) ([]byte, error) {
	op := Op(inst.Op)
	switch op {

	case OpADD, OpADDS, OpSUB, OpSUBS, OpMUL, OpMNEG, OpSDIV, OpUDIV,
		OpADC, OpADCS, OpSBC, OpSBCS, OpAND, OpANDS, OpORR, OpEOR,
		OpBIC, OpBICS, OpEON, OpORN, OpLSL, OpLSR, OpASR, OpROR:
		var o operands
		d, n, m := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceRegs), o.reg(inst.Src2, ErrMissingSourceRegs)
		if o.err != nil {
			return nil, o.err
		}
		return encR3(reg3Opcodes[op], d, n, m)

	case OpNEG, OpNEGS:
		var o operands
		d, m := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		word := uint32(0xCB000000)
		if op == OpNEGS {
			word = 0xEB000000
		}
		base, err := intBase(word, d, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | 0x1F<<5 | reg(d)), nil

	case OpMADD:
		var o operands
		d, n, m, a := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceRegs), o.reg(inst.Src2, ErrMissingSourceRegs), o.reg(inst.Src3, ErrMissingSourceRegs)
		if o.err != nil {
			return nil, o.err
		}
		return encR4(0x9B000000, d, n, m, a)

	case OpMSUB:
		var o operands
		d, n, m, a := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceRegs), o.reg(inst.Src2, ErrMissingSourceRegs), o.reg(inst.Src3, ErrMissingSourceRegs)
		if o.err != nil {
			return nil, o.err
		}
		return encR4(0x9B008000, d, n, m, a)

	case OpADDI, OpADDSI, OpSUBI, OpSUBSI:
		var o operands
		d, n, imm := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg), o.imm(inst.Src2, ErrMissingImmediate)
		if o.err != nil {
			return nil, o.err
		}
		return encRImm12(arithImmOpcodes[op], d, n, imm)

	case OpMVN:
		var o operands
		d, m := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		base, err := intBase(0xAA2003E0, d, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | reg(d)), nil

	case OpANDI, OpANDSI, OpORRI, OpEORI:
		var o operands
		d, n, imm := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg), o.imm(inst.Src2, ErrMissingImmediate)
		if o.err != nil {
			return nil, o.err
		}
		word, err := logicalImmediate(logicalImmOpcodes[op], d, n, imm)
		if err != nil {
			return nil, err
		}
		return enc(word), nil

	case OpLSLI:
		var o operands
		d, n, shift := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg), o.imm(inst.Src2, ErrMissingImmediate)
		if o.err != nil {
			return nil, o.err
		}
		base, mask, err := bitfieldBase(0xD3400000, d, n)
		if err != nil {
			return nil, err
		}
		s := uint32(shift) & mask
		return enc(base | ((-s)&mask)<<16 | (mask-s)<<10 | reg(n)<<5 | reg(d)), nil

	case OpLSRI, OpASRI:
		var o operands
		d, n, shift := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg), o.imm(inst.Src2, ErrMissingImmediate)
		if o.err != nil {
			return nil, o.err
		}
		word := uint32(0xD3400000)
		if op == OpASRI {
			word = 0x93400000
		}
		base, mask, err := bitfieldBase(word, d, n)
		if err != nil {
			return nil, err
		}
		s := uint32(shift) & mask
		return enc(base | s<<16 | mask<<10 | reg(n)<<5 | reg(d)), nil

	case OpRORI:
		var o operands
		d, n, shift := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg), o.imm(inst.Src2, ErrMissingImmediate)
		if o.err != nil {
			return nil, o.err
		}
		base, mask, err := bitfieldBase(0x93C00000, d, n)
		if err != nil {
			return nil, err
		}
		s := uint32(shift) & mask
		return enc(base | reg(n)<<16 | s<<10 | reg(n)<<5 | reg(d)), nil

	case OpSBFX:
		var o operands
		d, n, lsb, width := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg), o.imm(inst.Src2, ErrMissingImmediate), o.imm(inst.Src3, ErrMissingImmediate)
		if o.err != nil {
			return nil, o.err
		}
		base, mask, err := bitfieldBase(0x93400000, d, n)
		if err != nil {
			return nil, err
		}
		l := uint32(lsb) & mask
		imms := (l + uint32(width) - 1) & mask
		return enc(base | l<<16 | imms<<10 | reg(n)<<5 | reg(d)), nil

	case OpCLZ, OpRBIT, OpREV16, OpREV32:
		var o operands
		d, n := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		return encR2(reg2Opcodes[op], d, n)

	case OpREV:
		var o operands
		d, n := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
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
		var o operands
		d, n := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		ext := extendOpcodes[op]
		base := ext.op64
		if d.Width() == asm.Width32 {
			base = ext.op32
		}
		return enc(base | ext.imms<<10 | reg(n)<<5 | reg(d)), nil

	case OpTST:
		return encodeCompareReg(0xEA00001F, inst)

	case OpTSTI:
		var o operands
		n, imm := o.reg(inst.Src1, ErrMissingSourceReg), o.imm(inst.Src2, ErrMissingImmediate)
		if o.err != nil {
			return nil, o.err
		}
		word, err := logicalImmediate(0xF200001F, n, n, imm)
		if err != nil {
			return nil, err
		}
		return enc(word), nil

	case OpCMP:
		return encodeCompareReg(0xEB00001F, inst)

	case OpCMPI:
		return encodeCompareImm(0xF100001F, inst)

	case OpCMN:
		return encodeCompareReg(0xAB00001F, inst)

	case OpCMNI:
		return encodeCompareImm(0xB100001F, inst)

	case OpCCMP:
		var o operands
		n, m, flags := o.reg(inst.Src1, ErrMissingSourceReg), o.reg(inst.Src2, ErrMissingSourceReg), o.imm(inst.Src3, ErrMissingImmediate)
		if o.err != nil {
			return nil, o.err
		}
		base, err := intBase(0xFA400000, n, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | (uint32(flags>>4)&0xF)<<12 | reg(n)<<5 | uint32(flags)&0xF), nil

	case OpCCMPI:
		var o operands
		n, imm, flags := o.reg(inst.Src1, ErrMissingSourceReg), o.imm(inst.Src2, ErrMissingImmediate), o.imm(inst.Src3, ErrMissingImmediate)
		if o.err != nil {
			return nil, o.err
		}
		base, err := intBase(0xFA400800, n)
		if err != nil {
			return nil, err
		}
		return enc(base | (uint32(imm)&0x1F)<<16 | (uint32(flags>>4)&0xF)<<12 | reg(n)<<5 | uint32(flags)&0xF), nil

	case OpMOVW:
		var o operands
		d, n := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		if d.Type() != asm.RegTypeInt || d.Width() != asm.Width32 || n.Type() != asm.RegTypeInt || n.Width() != asm.Width64 {
			return nil, asm.ErrInvalidOperand
		}
		return enc(0x2A0003E0 | reg(n)<<16 | reg(d)), nil

	case OpMOV:
		var o operands
		d, n := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		base, err := intBase(0xAA0003E0, d, n)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(n)<<16 | reg(d)), nil

	case OpMOVI:
		var o operands
		d, imm64 := o.reg(inst.Dst, ErrMissingDestinationReg), o.imm(inst.Src1, ErrMissingImmediate)
		if o.err != nil {
			return nil, o.err
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
		return encodeMovImmediate(mv.op32, mv.op64, inst)

	case OpLDR, OpLDRB, OpLDRSB, OpLDRH, OpLDRSH, OpLDRSW:
		if op == OpLDR && isFloat(inst.Dst) {
			return encodeLoad(0xBD400000, 0xFD400000, 4, 8, inst)
		}
		ld := loadOpcodes[op]
		return encodeLoad(ld.op32, ld.op64, ld.scale32, ld.scale64, inst)

	case OpSTR, OpSTRB, OpSTRH, OpSTRW:
		if op == OpSTR && isFloat(inst.Src1) {
			return encodeStore(0xFD000000, 8, inst)
		}
		st := storeOpcodes[op]
		return encodeStore(st.base, st.scale, inst)

	case OpLDRR, OpLDRBR, OpLDRSBR:
		var o operands
		d, base, m := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceRegs), o.reg(inst.Src2, ErrMissingSourceRegs)
		if o.err != nil {
			return nil, o.err
		}
		return enc(extend(indexedLoad(op, d), m) | reg(m)<<16 | reg(base)<<5 | reg(d)), nil

	case OpSTRR, OpSTRBR, OpSTRWR:

		var o operands
		base, src, m := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceRegs), o.reg(inst.Src2, ErrMissingSourceRegs)
		if o.err != nil {
			return nil, o.err
		}
		return enc(extend(indexedStore(op, src), m) | reg(m)<<16 | reg(base)<<5 | reg(src)), nil

	case OpLDP:

		var o operands
		d1 := o.reg(inst.Dst, ErrMissingDestinationReg)
		base, offset := o.mem(inst.Src1)
		d2 := o.reg(inst.Src2, ErrMissingDestinationReg)
		if o.err != nil {
			return nil, o.err
		}
		simm7 := uint32(offset/8) & 0x7F
		return enc(0xA9400000 | simm7<<15 | reg(d2)<<10 | reg(base)<<5 | reg(d1)), nil

	case OpSTP:

		var o operands
		src1 := o.reg(inst.Src1, ErrMissingSourceReg)
		base, offset := o.mem(inst.Dst)
		src2 := o.reg(inst.Src2, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		simm7 := uint32(offset/8) & 0x7F
		return enc(0xA9000000 | simm7<<15 | reg(src2)<<10 | reg(base)<<5 | reg(src1)), nil

	case OpSCVTF, OpUCVTF, OpFCVTZS, OpFCVTZU:
		var o operands
		d, n := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
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
			word |= 1 << 31
		}
		if floatReg.Width() == asm.Width64 {
			word |= 1 << 22
		}
		return enc(word | reg(n)<<5 | reg(d)), nil

	case OpFCVT:
		var o operands
		d, n := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		if d.Type() != asm.RegTypeFloat || n.Type() != asm.RegTypeFloat {
			return nil, asm.ErrInvalidOperand
		}
		switch {
		case d.Width() == asm.Width64 && n.Width() == asm.Width32:
			return enc(0x1E22C000 | reg(n)<<5 | reg(d)), nil
		case d.Width() == asm.Width32 && n.Width() == asm.Width64:
			return enc(0x1E624000 | reg(n)<<5 | reg(d)), nil
		default:
			return nil, asm.ErrInvalidOperand
		}

	case OpFADD, OpFSUB, OpFMUL, OpFDIV, OpFMIN, OpFMAX:
		fb := floatBinaryOpcodes[op]
		return encodeFloatBinary(fb.single, fb.double, inst)

	case OpCNT, OpADDV:
		var o operands
		d, n := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		base := uint32(0x0E205800)
		if op == OpADDV {
			base = 0x0E31B800
		}
		return enc(base | reg(n)<<5 | reg(d)), nil

	case OpFMADD, OpFMSUB, OpFNMADD, OpFNMSUB:
		ft := floatTernaryOpcodes[op]
		return encodeFloatTernary(ft.single, ft.double, inst)

	case OpFABS, OpFNEG, OpFSQRT, OpFRINTN, OpFRINTM, OpFRINTP, OpFRINTZ:
		var o operands
		d, n := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		if err := floatMatch(d, n); err != nil {
			return nil, err
		}
		fu := floatUnaryOpcodes[op]
		base := fu.double
		if d.Width() == asm.Width32 {
			base = fu.single
		}
		return enc(base | reg(n)<<5 | reg(d)), nil

	case OpFMOV:
		var o operands
		d, n := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		if (d.Width() != asm.Width32 && d.Width() != asm.Width64) ||
			(n.Width() != asm.Width32 && n.Width() != asm.Width64) {
			return nil, asm.ErrInvalidOperand
		}
		dFloat := d.Type() == asm.RegTypeFloat
		nFloat := n.Type() == asm.RegTypeFloat
		switch {
		case dFloat && nFloat:
			if d.Width() != n.Width() {
				return nil, asm.ErrInvalidOperand
			}
			if d.Width() == asm.Width32 {
				return enc(0x1E204000 | reg(n)<<5 | reg(d)), nil
			}
			return enc(0x1E604000 | reg(n)<<5 | reg(d)), nil
		case dFloat && !nFloat:
			if n.Type() != asm.RegTypeInt {
				return nil, asm.ErrInvalidOperand
			}
			if d.Width() == asm.Width32 {

				if n.Width() != asm.Width32 && n.Width() != asm.Width64 {
					return nil, asm.ErrInvalidOperand
				}
				return enc(0x1E270000 | reg(n)<<5 | reg(d)), nil
			}
			if d.Width() != asm.Width64 || n.Width() != asm.Width64 {
				return nil, asm.ErrInvalidOperand
			}
			return enc(0x9E670000 | reg(n)<<5 | reg(d)), nil
		case !dFloat && nFloat:
			if d.Type() != asm.RegTypeInt {
				return nil, asm.ErrInvalidOperand
			}
			if n.Width() == asm.Width32 {

				if d.Width() != asm.Width32 && d.Width() != asm.Width64 {
					return nil, asm.ErrInvalidOperand
				}
				return enc(0x1E260000 | reg(n)<<5 | reg(d)), nil
			}
			if n.Width() != asm.Width64 || d.Width() != asm.Width64 {
				return nil, asm.ErrInvalidOperand
			}
			return enc(0x9E660000 | reg(n)<<5 | reg(d)), nil
		default:
			return nil, asm.ErrInvalidOperand
		}

	case OpFCMP, OpFCMPE:
		var o operands
		n, m := o.reg(inst.Src1, ErrMissingSourceReg), o.reg(inst.Src2, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		if err := floatMatch(n, m); err != nil {
			return nil, err
		}
		word := uint32(0x1E602000)
		if n.Width() == asm.Width32 {
			word = 0x1E202000
		}
		if op == OpFCMPE {
			word |= 0x10
		}
		return enc(word | reg(m)<<16 | reg(n)<<5), nil

	case OpCSEL, OpCSINC, OpCSINV, OpCSNEG:
		var o operands
		d, n, condImm := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg), o.imm(inst.Src2, ErrMissingImmediate)
		m := o.reg(inst.Src3, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		cond := uint32(condImm) & 0xF
		base, err := intBase(selectOpcodes[op], d, n, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | cond<<12 | reg(n)<<5 | reg(d)), nil

	case OpFCSEL:
		var o operands
		d, n, condImm := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceReg), o.imm(inst.Src2, ErrMissingImmediate)
		m := o.reg(inst.Src3, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		cond := uint32(condImm) & 0xF
		if d.Type() != asm.RegTypeFloat || n.Type() != asm.RegTypeFloat || m.Type() != asm.RegTypeFloat ||
			d.Width() != n.Width() || d.Width() != m.Width() {
			return nil, asm.ErrInvalidOperand
		}
		base := uint32(0x1E600C00)
		if d.Width() == asm.Width32 {
			base = 0x1E200C00
		}
		return enc(base | reg(m)<<16 | cond<<12 | reg(n)<<5 | reg(d)), nil

	case OpCSET, OpCSETM:
		var o operands
		d, condImm := o.reg(inst.Dst, ErrMissingDestinationReg), o.imm(inst.Src1, ErrMissingImmediate)
		if o.err != nil {
			return nil, o.err
		}
		cond := uint32(condImm)&0xF ^ 1
		word := uint32(0x9A9F07E0)
		if op == OpCSETM {
			word = 0xDA9F03E0
		}
		base, err := intBase(word, d)
		if err != nil {
			return nil, err
		}
		return enc(base | cond<<12 | reg(d)), nil

	case OpB, OpBL:
		var o operands
		offset := o.imm(inst.Src2, ErrMissingBranchOffset)
		if o.err != nil {
			return nil, o.err
		}
		if err := checkBranchOffset(op, offset, 26); err != nil {
			return nil, err
		}
		word := uint32(0x14000000)
		if op == OpBL {
			word = 0x94000000
		}
		return enc(word | (uint32(offset/4) & 0x3FFFFFF)), nil

	case OpBR, OpBLR, OpEXIT:
		var o operands
		r := o.reg(inst.Src1, ErrMissingRegisterOperand)
		if o.err != nil {
			return nil, o.err
		}
		if r.Type() != asm.RegTypeInt || r.Width() != asm.Width64 {
			return nil, asm.ErrInvalidOperand
		}
		word := uint32(0xD63F0000)
		if op == OpBR {
			word = 0xD61F0000
		}
		return enc(word | reg(r)<<5), nil

	case OpRET:

		return enc(0xD65F03C0), nil

	case OpCBZ, OpCBNZ:
		var o operands
		r, offset := o.reg(inst.Src1, ErrMissingRegisterOperand), o.imm(inst.Src2, ErrMissingBranchOffset)
		if o.err != nil {
			return nil, o.err
		}
		word := uint32(0xB4000000)
		if op == OpCBNZ {
			word = 0xB5000000
		}
		base, err := intBase(word, r)
		if err != nil {
			return nil, err
		}
		if err := checkBranchOffset(op, offset, 19); err != nil {
			return nil, err
		}
		imm19 := (uint32(offset/4) & 0x7FFFF) << 5
		return enc(base | imm19 | reg(r)), nil

	case OpTBZ, OpTBNZ:
		var o operands
		r, packed := o.reg(inst.Src1, ErrMissingRegisterOperand), o.imm(inst.Src2, ErrMissingBranchOffset)
		if o.err != nil {
			return nil, o.err
		}
		bit, offset := uint8(packed&0xFF), packed>>8
		if err := validTestBit(r, bit); err != nil {
			return nil, err
		}
		if err := checkBranchOffset(op, offset, 14); err != nil {
			return nil, err
		}
		word := uint32(0x36000000)
		if op == OpTBNZ {
			word = 0x37000000
		}
		b5 := (uint32(bit) >> 5) & 1
		b40 := uint32(bit) & 0x1F
		imm14 := (uint32(offset/4) & 0x3FFF) << 5
		return enc(word | b5<<31 | b40<<19 | imm14 | reg(r)), nil

	case OpBEQ, OpBNE, OpBCS, OpBCC, OpBMI, OpBPL,
		OpBVS, OpBVC, OpBHI, OpBLS, OpBGE, OpBLT, OpBGT, OpBLE:
		var o operands
		offset := o.imm(inst.Src2, ErrMissingBranchOffset)
		if o.err != nil {
			return nil, o.err
		}
		if err := checkBranchOffset(op, offset, 19); err != nil {
			return nil, err
		}
		cond := condCode[op]
		imm19 := (uint32(offset/4) & 0x7FFFF) << 5
		return enc(0x54000000 | imm19 | cond), nil

	case OpNOP:
		return enc(0xD503201F), nil

	case OpUSE, OpDEF:
		return nil, nil

	case OpHLT:
		return enc(0xD4400000), nil

	case OpBRK, OpSVC:
		var o operands
		imm := o.imm(inst.Src2, ErrMissingBranchOffset)
		if o.err != nil {
			return nil, o.err
		}
		word := uint32(0xD4200000)
		if op == OpSVC {
			word = 0xD4000001
		}
		return enc(word | (uint32(imm)&0xFFFF)<<5), nil

	case OpERET:
		return enc(0xD69F03E0), nil

	case OpMRS:

		var o operands
		d, sysreg := o.reg(inst.Dst, ErrMissingDestinationReg), o.imm(inst.Src1, ErrMissingImmediate)
		if o.err != nil {
			return nil, o.err
		}
		return enc(0xD5300000 | (uint32(sysreg)&0xFFFF)<<5 | reg(d)), nil

	case OpMSR:

		var o operands
		sysreg, src := o.imm(inst.Dst, ErrMissingImmediate), o.reg(inst.Src1, ErrMissingSourceReg)
		if o.err != nil {
			return nil, o.err
		}
		return enc(0xD5100000 | (uint32(sysreg)&0xFFFF)<<5 | reg(src)), nil

	case OpISB:
		return enc(0xD5033FDF), nil

	case OpDSB:
		return enc(0xD5033F9F), nil

	case OpDMB:
		return enc(0xD5033BBF), nil

	default:
		return nil, ErrUnsupportedOpcode
	}
}

// encodeMovImmediate emits a MOVZ/MOVK/MOVN-style wide-immediate move, picking
// op32 or op64 by destination width.
func encodeMovImmediate(op32, op64 uint32, inst asm.Instruction) ([]byte, error) {
	var o operands
	d, imm, shift := o.reg(inst.Dst, ErrMissingDestinationReg), o.imm(inst.Src1, ErrMissingImmediate), o.imm(inst.Src2, ErrMissingShiftImmediate)
	if o.err != nil {
		return nil, o.err
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
func encodeCompareReg(op uint32, inst asm.Instruction) ([]byte, error) {
	var o operands
	n, m := o.reg(inst.Src1, ErrMissingSourceReg), o.reg(inst.Src2, ErrMissingSourceReg)
	if o.err != nil {
		return nil, o.err
	}
	base, err := intBase(op, n, m)
	if err != nil {
		return nil, err
	}
	return enc(base | reg(m)<<16 | reg(n)<<5), nil
}

// encodeCompareImm emits an immediate-form compare (CMPI/CMNI).
func encodeCompareImm(op uint32, inst asm.Instruction) ([]byte, error) {
	var o operands
	n, imm := o.reg(inst.Src1, ErrMissingSourceReg), o.imm(inst.Src2, ErrMissingImmediate)
	if o.err != nil {
		return nil, o.err
	}
	if err := imm12(imm, 1); err != nil {
		return nil, err
	}
	base, err := intBase(op, n)
	if err != nil {
		return nil, err
	}
	return enc(base | (uint32(imm)&0xFFF)<<10 | reg(n)<<5), nil
}

func (o *operands) imm(op asm.Operand, missing error) int64 {
	i, ok := op.(asm.ImmOperand)
	if !ok {
		o.fail(missing)
	}
	return i.Value
}

// encodeLoad emits an unsigned-offset load, picking the 32- or 64-bit form
// by the destination's declared width and scaling the byte offset by that
// form's own access size.
func encodeLoad(op32, op64 uint32, scale32, scale64 int64, inst asm.Instruction) ([]byte, error) {
	var o operands
	dst := o.reg(inst.Dst, ErrMissingDestinationReg)
	base, offset := o.mem(inst.Src1)
	if o.err != nil {
		return nil, o.err
	}
	op, scale := op64, scale64
	if dst.Width() == asm.Width32 {
		op, scale = op32, scale32
	}
	if err := imm12(offset, scale); err != nil {
		return nil, err
	}
	pimm := uint32(offset/scale) & 0xFFF
	return enc(op | pimm<<10 | reg(base)<<5 | reg(dst)), nil
}

// encodeStore emits an unsigned-offset store, scaling the byte offset by the
// access size. A Width32 float source stores a single word: STR St is STR Dt
// with size 10 instead of 11, and the offset scales by 4.
func encodeStore(op uint32, scale int64, inst asm.Instruction) ([]byte, error) {
	var o operands
	src := o.reg(inst.Src1, ErrMissingSourceReg)
	base, offset := o.mem(inst.Dst)
	if o.err != nil {
		return nil, o.err
	}
	if src.Type() == asm.RegTypeFloat && src.Width() == asm.Width32 {
		op, scale = op&^(1<<30), 4
	}
	if err := imm12(offset, scale); err != nil {
		return nil, err
	}
	pimm := uint32(offset/scale) & 0xFFF
	return enc(op | pimm<<10 | reg(base)<<5 | reg(src)), nil
}

func (o *operands) mem(op asm.Operand) (base asm.PReg, offset int64) {
	m, ok := op.(asm.MemOperand)
	if !ok {
		o.fail(ErrMissingMemoryOperand)
		return 0, 0
	}
	return o.reg(m.Base, ErrMissingMemoryOperand), m.Offset
}

// indexedLoad is the register-offset load word op names for dst, its index
// scaled by the access size: LDRR loads as LDR does, by dst's bank and width.
func indexedLoad(op Op, dst asm.PReg) uint32 {
	switch {
	case op == OpLDRBR:
		return 0x38607800
	case op == OpLDRSBR && dst.Width() == asm.Width64:
		return 0x38A07800
	case op == OpLDRSBR:
		return 0x38E07800
	}
	return indexedWord(0xB8607800, dst)
}

// indexedStore is the register-offset store word op names for src: STRR
// stores as STR does, 8 bytes from an integer register.
func indexedStore(op Op, src asm.PReg) uint32 {
	switch {
	case op == OpSTRBR:
		return 0x38207800
	case op == OpSTRWR:
		return 0xB8207800
	case src.Type() == asm.RegTypeInt:
		return 0xF8207800
	}
	return indexedWord(0xB8207800, src)
}

// extend switches word's index extend from LSL to SXTW for a 32-bit index:
// option bits 15:13 go from 011 to 110.
func extend(word uint32, index asm.PReg) uint32 {
	if index.Width() == asm.Width32 {
		word = word&^(7<<13) | 6<<13
	}
	return word
}

// isFloat reports whether op names a float register.
func isFloat(op asm.Operand) bool {
	r, ok := op.(asm.PRegOperand)
	return ok && r.Reg.Type() == asm.RegTypeFloat
}

// encodeFloatBinary emits a 3-register scalar float op (FADD/FSUB/FMUL/FDIV),
// picking op32 or op64 by destination width.
func encodeFloatBinary(op32, op64 uint32, inst asm.Instruction) ([]byte, error) {
	var o operands
	d, n, m := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceRegs), o.reg(inst.Src2, ErrMissingSourceRegs)
	if o.err != nil {
		return nil, o.err
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
func encodeFloatTernary(op32, op64 uint32, inst asm.Instruction) ([]byte, error) {
	var o operands
	d, n, m, a := o.reg(inst.Dst, ErrMissingDestinationReg), o.reg(inst.Src1, ErrMissingSourceRegs), o.reg(inst.Src2, ErrMissingSourceRegs), o.reg(inst.Src3, ErrMissingSourceRegs)
	if o.err != nil {
		return nil, o.err
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

func (o *operands) reg(op asm.Operand, missing error) asm.PReg {
	r, ok := op.(asm.PRegOperand)
	if !ok {
		o.fail(missing)
	}
	return r.Reg
}

func (o *operands) fail(err error) {
	if o.err == nil {
		o.err = err
	}
}

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
	if err := imm12(imm, 1); err != nil {
		return nil, err
	}
	b, err := intBase(base, d, n)
	if err != nil {
		return nil, err
	}
	return enc(b | (uint32(imm)&0xFFF)<<10 | reg(n)<<5 | reg(d)), nil
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

func validTestBit(src asm.PReg, bit uint8) error {
	if _, err := intBase(0, src); err != nil {
		return err
	}
	if bit >= 64 || (src.Width() == asm.Width32 && bit >= 32) {
		return asm.ErrInvalidOperand
	}
	return nil
}

// indexedWord sets word, a 4-byte integer access, to r's size and bank: bit
// 30 widens it to 8 bytes, bit 26 makes it a float access.
func indexedWord(word uint32, r asm.PReg) uint32 {
	if r.Width() == asm.Width64 {
		word |= 1 << 30
	}
	if r.Type() == asm.RegTypeFloat {
		word |= 1 << 26
	}
	return word
}

// imm12 rejects a value that is not a non-negative multiple of scale whose
// quotient fits an unsigned 12-bit immediate field.
func imm12(v, scale int64) error {
	if v < 0 || v%scale != 0 || v/scale > 0xFFF {
		return asm.ErrInvalidOperand
	}
	return nil
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

// reg extracts the 5-bit register ID from a PReg.
func reg(r asm.PReg) uint32 {
	return uint32(r.ID()) & 0x1F
}

func floatMatch(regs ...asm.PReg) error {
	_, err := sameKind(asm.RegTypeFloat, regs...)
	return err
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

// checkBranchOffset validates that offset is 4-byte aligned and its
// instruction-count (offset/4) fits the signed bits-wide immediate field
// of a PC-relative branch encoding. Without it an out-of-range offset
// silently truncates to the wrong target. op and bits are folded into the
// returned error so a caller can tell which branch family and field width
// rejected the offset.
func checkBranchOffset(op Op, offset int64, bits uint) error {
	if offset%4 != 0 {
		return fmt.Errorf("%w: op=%v offset=%d bits=%d", asm.ErrBranchOutOfRange, op, offset, bits)
	}
	units := offset / 4
	half := int64(1) << (bits - 1)
	if units < -half || units >= half {
		return fmt.Errorf("%w: op=%v offset=%d bits=%d", asm.ErrBranchOutOfRange, op, offset, bits)
	}
	return nil
}

// encodeLogicalImm packs val as an ARM64 logical immediate: a rotated run of
// ones repeated across the register in elements of 2..64 bits. It returns the
// immr and imms fields, or ok=false when val has no such form.
func encodeLogicalImm(val uint64, is64 bool) (immr, imms uint32, ok bool) {
	width := uint(64)
	if !is64 {
		width = 32
		val &= uint64(^uint32(0))
	}
	if val == 0 || (is64 && val == ^uint64(0)) || (!is64 && val == uint64(^uint32(0))) {
		return 0, 0, false
	}

	for _, esize := range []uint{2, 4, 8, 16, 32, 64} {
		if !is64 && esize == 64 {
			continue
		}
		mask := uint64((1 << esize) - 1)
		elem := val & mask
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

		ones := uint(bits.OnesCount64(elem))
		if ones == 0 || ones == esize {
			continue
		}

		tz := uint(bits.TrailingZeros64(elem))
		ro := (esize - tz) & (esize - 1)
		if rotateMask(esize, ones, ro) != elem {
			continue
		}

		immrVal := uint32(ro) & 0x3F
		immsVal := ((^uint32(esize-1)&0x3F)<<1)&0x3E | uint32(ones-1)
		return immrVal, immsVal, true
	}
	return 0, 0, false
}

// rotateMask builds the canonical element: `ones` consecutive 1s at the lsb,
// rotated right by `rot` within an `esize`-bit field, matching ARM64's
// logical-immediate decode (DecodeBitMasks: wmask = ROR(welem, immr)).
func rotateMask(esize, ones, rot uint) uint64 {
	mask := uint64((1 << ones) - 1)
	rot &= esize - 1
	if rot == 0 {
		return mask
	}
	hi := mask >> rot
	lo := mask << (esize - rot)
	result := (lo | hi) & ((1 << esize) - 1)
	return result
}

func enc(w uint32) []byte {
	return []byte{byte(w), byte(w >> 8), byte(w >> 16), byte(w >> 24)}
}
