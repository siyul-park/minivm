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

func (e *Encoder) Encode(inst asm.Instruction) ([]byte, error) {
	op := Op(inst.Op)
	switch op {

	// -----------------------------------------------------------------------
	// Arithmetic — register
	// -----------------------------------------------------------------------

	case OpADD:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0x8B000000, d, n, m)

	case OpADDS:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0xAB000000, d, n, m)

	case OpSUB:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0xCB000000, d, n, m)

	case OpSUBS:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0xEB000000, d, n, m)

	case OpNEG: // NEG Xd, Xm  →  SUB Xd, XZR, Xm
		d, m, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xCB000000, d, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | 0x1F<<5 | reg(d)), nil

	case OpNEGS: // NEGS Xd, Xm  →  SUBS Xd, XZR, Xm
		d, m, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xEB000000, d, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | 0x1F<<5 | reg(d)), nil

	case OpMUL: // MUL  →  MADD Ra=XZR
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0x9B007C00, d, n, m)

	case OpMNEG: // MNEG  →  MSUB Ra=XZR
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0x9B00FC00, d, n, m)

	case OpMADD:
		d, n, m, a, err := decodeReg4(inst)
		if err != nil {
			return nil, err
		}
		return encR4(0x9B000000, d, n, m, a)

	case OpMSUB:
		d, n, m, a, err := decodeReg4(inst)
		if err != nil {
			return nil, err
		}
		return encR4(0x9B008000, d, n, m, a)

	case OpSDIV:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0x9AC00C00, d, n, m)

	case OpUDIV:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0x9AC00800, d, n, m)

	case OpADC:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0x9A000000, d, n, m)

	case OpADCS:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0xBA000000, d, n, m)

	case OpSBC:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0xDA000000, d, n, m)

	case OpSBCS:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0xFA000000, d, n, m)

	// -----------------------------------------------------------------------
	// Arithmetic — immediate
	// -----------------------------------------------------------------------

	case OpADDI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return encRImm12(0x91000000, d, n, imm)

	case OpADDSI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return encRImm12(0xB1000000, d, n, imm)

	case OpSUBI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return encRImm12(0xD1000000, d, n, imm)

	case OpSUBSI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return encRImm12(0xF1000000, d, n, imm)

	// -----------------------------------------------------------------------
	// Bitwise — register
	// -----------------------------------------------------------------------

	case OpAND:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0x8A000000, d, n, m)

	case OpANDS:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0xEA000000, d, n, m)

	case OpORR:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0xAA000000, d, n, m)

	case OpEOR:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0xCA000000, d, n, m)

	case OpBIC:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0x8A200000, d, n, m)

	case OpBICS:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0xEA200000, d, n, m)

	case OpEON:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0xCA200000, d, n, m)

	case OpORN:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0xAA200000, d, n, m)

	case OpMVN: // MVN Xd, Xm  →  ORN Xd, XZR, Xm
		d, m, err := decodeReg2(inst)
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

	case OpANDI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		word, err := logicalImmediate(0x92000000, d, n, imm)
		if err != nil {
			return nil, err
		}
		return enc(word), nil

	case OpANDSI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		word, err := logicalImmediate(0xF2000000, d, n, imm)
		if err != nil {
			return nil, err
		}
		return enc(word), nil

	case OpORRI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		word, err := logicalImmediate(0xB2000000, d, n, imm)
		if err != nil {
			return nil, err
		}
		return enc(word), nil

	case OpEORI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		word, err := logicalImmediate(0xD2000000, d, n, imm)
		if err != nil {
			return nil, err
		}
		return enc(word), nil

	// -----------------------------------------------------------------------
	// Shift — register
	// -----------------------------------------------------------------------

	case OpLSL: // LSLV
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0x9AC02000, d, n, m)

	case OpLSR: // LSRV
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0x9AC02400, d, n, m)

	case OpASR: // ASRV
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0x9AC02800, d, n, m)

	case OpROR: // RORV
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encR3(0x9AC02C00, d, n, m)

	// -----------------------------------------------------------------------
	// Shift — immediate  (encoded as UBFM / SBFM / EXTR)
	// -----------------------------------------------------------------------

	case OpLSLI:
		d, n, shift, err := decodeRegShift(inst)
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
		d, n, shift, err := decodeRegShift(inst)
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
		d, n, shift, err := decodeRegShift(inst)
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
		d, n, shift, err := decodeRegShift(inst)
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

	case OpCLZ:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encR2(0xDAC01000, d, n)

	case OpRBIT:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encR2(0xDAC00000, d, n)

	case OpREV:
		d, n, err := decodeReg2(inst)
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

	case OpREV16:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encR2(0xDAC00400, d, n)

	case OpREV32:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encR2(0xDAC00800, d, n)

	case OpSXTB:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}

		return enc(0x93400000 | 0<<16 | 7<<10 | reg(n)<<5 | reg(d)), nil

	case OpSXTH:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}

		return enc(0x93400000 | 0<<16 | 15<<10 | reg(n)<<5 | reg(d)), nil

	case OpSXTW:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}

		return enc(0x93400000 | 0<<16 | 31<<10 | reg(n)<<5 | reg(d)), nil

	case OpUXTB:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}

		return enc(0xD3400000 | 0<<16 | 7<<10 | reg(n)<<5 | reg(d)), nil

	case OpUXTH:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xD3400000 | 0<<16 | 15<<10 | reg(n)<<5 | reg(d)), nil

	case OpUXTW:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xD3400000 | 0<<16 | 31<<10 | reg(n)<<5 | reg(d)), nil

	// -----------------------------------------------------------------------
	// TST
	// -----------------------------------------------------------------------

	case OpTST: // ANDS XZR, Xn, Xm
		n, m, err := decodeCmp(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xEA00001F, n, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | reg(n)<<5), nil

	case OpTSTI:
		n, imm, err := decodeCmpImm(inst)
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
		n, m, err := decodeCmp(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xEB00001F, n, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | reg(n)<<5), nil

	case OpCMPI: // SUBS XZR, Xn, #imm
		n, imm, err := decodeCmpImm(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xF100001F, n)
		if err != nil {
			return nil, err
		}
		return enc(base | (uint32(imm)&0xFFF)<<10 | reg(n)<<5), nil

	case OpCMN: // ADDS XZR, Xn, Xm
		n, m, err := decodeCmp(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xAB00001F, n, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | reg(n)<<5), nil

	case OpCMNI: // ADDS XZR, Xn, #imm
		n, imm, err := decodeCmpImm(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xB100001F, n)
		if err != nil {
			return nil, err
		}
		return enc(base | (uint32(imm)&0xFFF)<<10 | reg(n)<<5), nil

	case OpCCMP:
		n, m, err := decodeCmp(inst)
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
		n, imm, err := decodeCmpImm(inst)
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
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xAA0003E0, d, n)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(n)<<16 | reg(d)), nil

	case OpMOVI: // pseudo: MOVZ + MOVK sequence; emit MOVZ for first 16 bits
		d, imm64, err := decodeDstImm(inst)
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

	case OpMOVZ:
		d, imm, shift, err := decodeMovImm(inst)
		if err != nil {
			return nil, err
		}
		if err := validMoveImmediate(d, shift); err != nil {
			return nil, err
		}
		hw := uint32(shift/16) & 3
		if d.Width() == asm.Width32 {
			return enc(0x52800000 | hw<<21 | (uint32(imm)&0xFFFF)<<5 | reg(d)), nil
		}
		return enc(0xD2800000 | hw<<21 | (uint32(imm)&0xFFFF)<<5 | reg(d)), nil

	case OpMOVK:
		d, imm, shift, err := decodeMovImm(inst)
		if err != nil {
			return nil, err
		}
		if err := validMoveImmediate(d, shift); err != nil {
			return nil, err
		}
		hw := uint32(shift/16) & 3
		if d.Width() == asm.Width32 {
			return enc(0x72800000 | hw<<21 | (uint32(imm)&0xFFFF)<<5 | reg(d)), nil
		}
		return enc(0xF2800000 | hw<<21 | (uint32(imm)&0xFFFF)<<5 | reg(d)), nil

	case OpMOVN:
		d, imm, shift, err := decodeMovImm(inst)
		if err != nil {
			return nil, err
		}
		if err := validMoveImmediate(d, shift); err != nil {
			return nil, err
		}
		hw := uint32(shift/16) & 3
		if d.Width() == asm.Width32 {
			return enc(0x12800000 | hw<<21 | (uint32(imm)&0xFFFF)<<5 | reg(d)), nil
		}
		return enc(0x92800000 | hw<<21 | (uint32(imm)&0xFFFF)<<5 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Load / Store  64-bit
	// -----------------------------------------------------------------------

	case OpLDR:
		// LDR Xt, [Xn, #pimm*8]  — unsigned offset
		d, base, offset, err := decodeMemOp(inst)
		if err != nil {
			return nil, err
		}
		pimm := uint32(offset/8) & 0xFFF
		return enc(0xF9400000 | pimm<<10 | reg(base)<<5 | reg(d)), nil

	case OpSTR:
		src, base, offset, err := decodeStrOp(inst)
		if err != nil {
			return nil, err
		}
		pimm := uint32(offset/8) & 0xFFF
		return enc(0xF9000000 | pimm<<10 | reg(base)<<5 | reg(src)), nil

	// -----------------------------------------------------------------------
	// Load / Store  8-bit
	// -----------------------------------------------------------------------

	case OpLDRB:
		d, base, offset, err := decodeMemOp(inst)
		if err != nil {
			return nil, err
		}
		pimm := uint32(offset) & 0xFFF
		return enc(0x39400000 | pimm<<10 | reg(base)<<5 | reg(d)), nil

	case OpLDRSB: // sign-extends to 64-bit
		d, base, offset, err := decodeMemOp(inst)
		if err != nil {
			return nil, err
		}
		pimm := uint32(offset) & 0xFFF
		return enc(0x39800000 | pimm<<10 | reg(base)<<5 | reg(d)), nil

	case OpSTRB:
		src, base, offset, err := decodeStrOp(inst)
		if err != nil {
			return nil, err
		}
		pimm := uint32(offset) & 0xFFF
		return enc(0x39000000 | pimm<<10 | reg(base)<<5 | reg(src)), nil

	// -----------------------------------------------------------------------
	// Load / Store  16-bit
	// -----------------------------------------------------------------------

	case OpLDRH:
		d, base, offset, err := decodeMemOp(inst)
		if err != nil {
			return nil, err
		}
		pimm := uint32(offset/2) & 0xFFF
		return enc(0x79400000 | pimm<<10 | reg(base)<<5 | reg(d)), nil

	case OpLDRSH: // sign-extends to 64-bit
		d, base, offset, err := decodeMemOp(inst)
		if err != nil {
			return nil, err
		}
		pimm := uint32(offset/2) & 0xFFF
		return enc(0x79800000 | pimm<<10 | reg(base)<<5 | reg(d)), nil

	case OpSTRH:
		src, base, offset, err := decodeStrOp(inst)
		if err != nil {
			return nil, err
		}
		pimm := uint32(offset/2) & 0xFFF
		return enc(0x79000000 | pimm<<10 | reg(base)<<5 | reg(src)), nil

	// -----------------------------------------------------------------------
	// Load / Store  32-bit sign-extended
	// -----------------------------------------------------------------------

	case OpLDRSW:
		d, base, offset, err := decodeMemOp(inst)
		if err != nil {
			return nil, err
		}
		pimm := uint32(offset/4) & 0xFFF
		return enc(0xB9800000 | pimm<<10 | reg(base)<<5 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Load / Store  register-offset  [Xbase, Xoffset]
	// -----------------------------------------------------------------------

	case OpLDRR:
		// LDR Xt, [Xbase, Xm, LSL #3]  — extended register
		d, base, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xF8606800 | reg(m)<<16 | reg(base)<<5 | reg(d)), nil

	case OpSTRR:
		// STR Xt, [Xbase, Xm, LSL #3]
		// inst encoding: Dst=base, Src1=src, Src2=offsetReg
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xF8206800 | reg(m)<<16 | reg(d)<<5 | reg(n)), nil

	// -----------------------------------------------------------------------
	// Load / Store pair
	// -----------------------------------------------------------------------

	case OpLDP:
		// LDP Xt1, Xt2, [Xbase, #offset]
		// Encoding: Dst=Xt1, Src1=Mem(base,offset), Src2=Xt2
		d1, base, offset, err := decodeMemOp(inst)
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
		src1, base, offset, err := decodeStrOp(inst)
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

	case OpSCVTF: // SCVTF Dd, Xn  (integer → double)
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		if d.Type() != asm.RegTypeFloat || n.Type() != asm.RegTypeInt {
			return nil, asm.ErrInvalidOperand
		}
		switch {
		case d.Width() == asm.Width32 && n.Width() == asm.Width32:
			return enc(0x1E220000 | reg(n)<<5 | reg(d)), nil // SCVTF Sd, Wn
		case d.Width() == asm.Width32 && n.Width() == asm.Width64:
			return enc(0x9E220000 | reg(n)<<5 | reg(d)), nil // SCVTF Sd, Xn
		case d.Width() == asm.Width64 && n.Width() == asm.Width32:
			return enc(0x1E620000 | reg(n)<<5 | reg(d)), nil // SCVTF Dd, Wn
		case d.Width() == asm.Width64 && n.Width() == asm.Width64:
			return enc(0x9E620000 | reg(n)<<5 | reg(d)), nil // SCVTF Dd, Xn
		default:
			return nil, asm.ErrInvalidOperand
		}

	case OpUCVTF: // UCVTF Dd, Xn  (unsigned integer → double)
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		if d.Type() != asm.RegTypeFloat || n.Type() != asm.RegTypeInt {
			return nil, asm.ErrInvalidOperand
		}
		switch {
		case d.Width() == asm.Width32 && n.Width() == asm.Width32:
			return enc(0x1E230000 | reg(n)<<5 | reg(d)), nil // UCVTF Sd, Wn
		case d.Width() == asm.Width32 && n.Width() == asm.Width64:
			return enc(0x9E230000 | reg(n)<<5 | reg(d)), nil // UCVTF Sd, Xn
		case d.Width() == asm.Width64 && n.Width() == asm.Width32:
			return enc(0x1E630000 | reg(n)<<5 | reg(d)), nil // UCVTF Dd, Wn
		case d.Width() == asm.Width64 && n.Width() == asm.Width64:
			return enc(0x9E630000 | reg(n)<<5 | reg(d)), nil // UCVTF Dd, Xn
		default:
			return nil, asm.ErrInvalidOperand
		}

	case OpFCVTZS:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		if d.Type() != asm.RegTypeInt || n.Type() != asm.RegTypeFloat {
			return nil, asm.ErrInvalidOperand
		}
		switch {
		case d.Width() == asm.Width32 && n.Width() == asm.Width32:
			return enc(0x1E380000 | reg(n)<<5 | reg(d)), nil // FCVTZS Wd, Sn
		case d.Width() == asm.Width32 && n.Width() == asm.Width64:
			return enc(0x1E780000 | reg(n)<<5 | reg(d)), nil // FCVTZS Wd, Dn
		case d.Width() == asm.Width64 && n.Width() == asm.Width32:
			return enc(0x9E380000 | reg(n)<<5 | reg(d)), nil // FCVTZS Xd, Sn
		case d.Width() == asm.Width64 && n.Width() == asm.Width64:
			return enc(0x9E780000 | reg(n)<<5 | reg(d)), nil // FCVTZS Xd, Dn
		default:
			return nil, asm.ErrInvalidOperand
		}

	case OpFCVTZU:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		if d.Type() != asm.RegTypeInt || n.Type() != asm.RegTypeFloat {
			return nil, asm.ErrInvalidOperand
		}
		switch {
		case d.Width() == asm.Width32 && n.Width() == asm.Width32:
			return enc(0x1E390000 | reg(n)<<5 | reg(d)), nil // FCVTZU Wd, Sn
		case d.Width() == asm.Width32 && n.Width() == asm.Width64:
			return enc(0x1E790000 | reg(n)<<5 | reg(d)), nil // FCVTZU Wd, Dn
		case d.Width() == asm.Width64 && n.Width() == asm.Width32:
			return enc(0x9E390000 | reg(n)<<5 | reg(d)), nil // FCVTZU Xd, Sn
		case d.Width() == asm.Width64 && n.Width() == asm.Width64:
			return enc(0x9E790000 | reg(n)<<5 | reg(d)), nil // FCVTZU Xd, Dn
		default:
			return nil, asm.ErrInvalidOperand
		}

	case OpFCVT:
		d, n, err := decodeReg2(inst)
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

	case OpFADD:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		if err := floatMatch(d, n, m); err != nil {
			return nil, err
		}
		if d.Width() == asm.Width32 {
			return enc(0x1E202800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil // FADD Sd, Sn, Sm
		}
		return enc(0x1E602800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil // FADD Dd, Dn, Dm

	case OpFSUB:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		if err := floatMatch(d, n, m); err != nil {
			return nil, err
		}
		if d.Width() == asm.Width32 {
			return enc(0x1E203800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil // FSUB Sd, Sn, Sm
		}
		return enc(0x1E603800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil // FSUB Dd, Dn, Dm

	case OpFMUL:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		if err := floatMatch(d, n, m); err != nil {
			return nil, err
		}
		if d.Width() == asm.Width32 {
			return enc(0x1E200800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil // FMUL Sd, Sn, Sm
		}
		return enc(0x1E600800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil // FMUL Dd, Dn, Dm

	case OpFDIV:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		if err := floatMatch(d, n, m); err != nil {
			return nil, err
		}
		if d.Width() == asm.Width32 {
			return enc(0x1E201800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil // FDIV Sd, Sn, Sm
		}
		return enc(0x1E601800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil // FDIV Dd, Dn, Dm

	case OpFMADD:
		d, n, m, a, err := decodeReg4(inst)
		if err != nil {
			return nil, err
		}
		if err := floatMatch(d, n, m, a); err != nil {
			return nil, err
		}
		if d.Width() == asm.Width32 {
			return enc(0x1F000000 | reg(m)<<16 | reg(a)<<10 | reg(n)<<5 | reg(d)), nil // FMADD Sd, Sn, Sm, Sa
		}
		return enc(0x1F400000 | reg(m)<<16 | reg(a)<<10 | reg(n)<<5 | reg(d)), nil // FMADD Dd, Dn, Dm, Da

	case OpFMSUB:
		d, n, m, a, err := decodeReg4(inst)
		if err != nil {
			return nil, err
		}
		if err := floatMatch(d, n, m, a); err != nil {
			return nil, err
		}
		if d.Width() == asm.Width32 {
			return enc(0x1F008000 | reg(m)<<16 | reg(a)<<10 | reg(n)<<5 | reg(d)), nil // FMSUB Sd, Sn, Sm, Sa
		}
		return enc(0x1F408000 | reg(m)<<16 | reg(a)<<10 | reg(n)<<5 | reg(d)), nil // FMSUB Dd, Dn, Dm, Da

	case OpFNMADD:
		d, n, m, a, err := decodeReg4(inst)
		if err != nil {
			return nil, err
		}
		if err := floatMatch(d, n, m, a); err != nil {
			return nil, err
		}
		if d.Width() == asm.Width32 {
			return enc(0x1F200000 | reg(m)<<16 | reg(a)<<10 | reg(n)<<5 | reg(d)), nil // FNMADD Sd, Sn, Sm, Sa
		}
		return enc(0x1F600000 | reg(m)<<16 | reg(a)<<10 | reg(n)<<5 | reg(d)), nil // FNMADD Dd, Dn, Dm, Da

	case OpFNMSUB:
		d, n, m, a, err := decodeReg4(inst)
		if err != nil {
			return nil, err
		}
		if err := floatMatch(d, n, m, a); err != nil {
			return nil, err
		}
		if d.Width() == asm.Width32 {
			return enc(0x1F208000 | reg(m)<<16 | reg(a)<<10 | reg(n)<<5 | reg(d)), nil // FNMSUB Sd, Sn, Sm, Sa
		}
		return enc(0x1F608000 | reg(m)<<16 | reg(a)<<10 | reg(n)<<5 | reg(d)), nil // FNMSUB Dd, Dn, Dm, Da

	// -----------------------------------------------------------------------
	// Float — unary
	// -----------------------------------------------------------------------

	case OpFABS:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encodeFloatUnary(0x1E20C000, 0x1E60C000, d, n)

	case OpFNEG:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encodeFloatUnary(0x1E214000, 0x1E614000, d, n)

	case OpFSQRT:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encodeFloatUnary(0x1E21C000, 0x1E61C000, d, n)

	case OpFRINTN: // round to nearest, ties to even
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encodeFloatUnary(0x1E244000, 0x1E644000, d, n)

	case OpFRINTM: // round toward minus infinity
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encodeFloatUnary(0x1E254000, 0x1E654000, d, n)

	case OpFRINTP: // round toward plus infinity
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encodeFloatUnary(0x1E24C000, 0x1E64C000, d, n)

	case OpFRINTZ: // round toward zero
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encodeFloatUnary(0x1E25C000, 0x1E65C000, d, n)

	// -----------------------------------------------------------------------
	// Float — move / compare
	// -----------------------------------------------------------------------

	case OpFMOV:
		d, n, err := decodeReg2(inst)
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
			if n.Type() != asm.RegTypeInt || d.Width() != n.Width() {
				return nil, asm.ErrInvalidOperand
			}
			if d.Width() == asm.Width32 {
				return enc(0x1E270000 | reg(n)<<5 | reg(d)), nil // FMOV Sd, Wn
			}
			return enc(0x9E670000 | reg(n)<<5 | reg(d)), nil // FMOV Dd, Xn
		case !dFloat && nFloat: // float → int (bit-copy)
			if d.Type() != asm.RegTypeInt || d.Width() != n.Width() {
				return nil, asm.ErrInvalidOperand
			}
			if n.Width() == asm.Width32 {
				return enc(0x1E260000 | reg(n)<<5 | reg(d)), nil // FMOV Wn, Sd
			}
			return enc(0x9E660000 | reg(n)<<5 | reg(d)), nil // FMOV Xn, Dd
		default:
			return nil, asm.ErrInvalidOperand
		}

	case OpFCMP:
		n, m, err := decodeCmp(inst)
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
		n, m, err := decodeCmp(inst)
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

	case OpCSEL: // CSEL Xd, Xn, Xm, cond
		d, n, m, cond, err := decodeSelect(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0x9A800000, d, n, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | cond<<12 | reg(n)<<5 | reg(d)), nil

	case OpCSINC: // CSINC Xd, Xn, Xm, cond
		d, n, m, cond, err := decodeSelect(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0x9A800400, d, n, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | cond<<12 | reg(n)<<5 | reg(d)), nil

	case OpCSINV:
		d, n, m, cond, err := decodeSelect(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xDA800000, d, n, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | cond<<12 | reg(n)<<5 | reg(d)), nil

	case OpCSNEG:
		d, n, m, cond, err := decodeSelect(inst)
		if err != nil {
			return nil, err
		}
		base, err := intBase(0xDA800400, d, n, m)
		if err != nil {
			return nil, err
		}
		return enc(base | reg(m)<<16 | cond<<12 | reg(n)<<5 | reg(d)), nil

	case OpCSET: // CSET Xd, cond  →  CSINC Xd, XZR, XZR, !cond
		d, condImm, err := decodeDstImm(inst)
		if err != nil {
			return nil, err
		}
		cond := uint32(condImm)&0xF ^ 1 // invert lsb to negate condition
		base, err := intBase(0x9A9F07E0, d)
		if err != nil {
			return nil, err
		}
		return enc(base | cond<<12 | reg(d)), nil

	case OpCSETM: // CSETM Xd, cond  →  CSINV Xd, XZR, XZR, !cond
		d, condImm, err := decodeDstImm(inst)
		if err != nil {
			return nil, err
		}
		cond := uint32(condImm)&0xF ^ 1
		base, err := intBase(0xDA9F03E0, d)
		if err != nil {
			return nil, err
		}
		return enc(base | cond<<12 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Branch — unconditional
	// -----------------------------------------------------------------------

	case OpB:
		offset, err := decodeBranch(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x14000000 | (uint32(offset/4) & 0x3FFFFFF)), nil

	case OpBL:
		offset, err := decodeBranch(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x94000000 | (uint32(offset/4) & 0x3FFFFFF)), nil

	case OpBR:
		r, err := decodeRegOnly(inst)
		if err != nil {
			return nil, err
		}
		if r.Type() != asm.RegTypeInt || r.Width() != asm.Width64 {
			return nil, asm.ErrInvalidOperand
		}
		return enc(0xD61F0000 | reg(r)<<5), nil

	case OpBLR:
		r, err := decodeRegOnly(inst)
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
		r, offset, err := decodeRegBranch(inst)
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
		r, offset, err := decodeRegBranch(inst)
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
		r, bit, offset, err := decodeTestBranch(inst)
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
		r, bit, offset, err := decodeTestBranch(inst)
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
		offset, err := decodeBranch(inst)
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
		imm, err := decodeBranch(inst) // reuse: imm16 in Src2
		if err != nil {
			return nil, err
		}
		return enc(0xD4200000 | (uint32(imm)&0xFFFF)<<5), nil

	case OpSVC:
		imm, err := decodeBranch(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xD4000001 | (uint32(imm)&0xFFFF)<<5), nil

	case OpERET:
		return enc(0xD69F03E0), nil

	case OpMRS:
		// MRS Xt, sysreg  — sysreg encoded in Src1 as ImmOperand
		d, sysreg, err := decodeDstImm(inst)
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

func decodeReg4(inst asm.Instruction) (dst, src1, src2, src3 asm.PReg, err error) {
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

func decodeReg3(inst asm.Instruction) (dst, src1, src2 asm.PReg, err error) {
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

func decodeReg2(inst asm.Instruction) (dst, src asm.PReg, err error) {
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

func decodeRegImm(inst asm.Instruction) (dst, src asm.PReg, imm int64, err error) {
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
func decodeRegShift(inst asm.Instruction) (dst, src asm.PReg, shift int64, err error) {
	return decodeRegImm(inst)
}

func decodeCmp(inst asm.Instruction) (src1, src2 asm.PReg, err error) {
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

func decodeCmpImm(inst asm.Instruction) (src asm.PReg, imm int64, err error) {
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

func decodeSelect(inst asm.Instruction) (dst, src1, src2 asm.PReg, cond uint32, err error) {
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

func decodeMovImm(inst asm.Instruction) (dst asm.PReg, imm, shift int64, err error) {
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
func decodeDstImm(inst asm.Instruction) (dst asm.PReg, imm int64, err error) {
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

func decodeMemOp(inst asm.Instruction) (dst, base asm.PReg, offset int64, err error) {
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

func decodeStrOp(inst asm.Instruction) (src, base asm.PReg, offset int64, err error) {
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

func decodeRegOnly(inst asm.Instruction) (asm.PReg, error) {
	op, ok := inst.Src1.(asm.PRegOperand)
	if !ok {
		return asm.PReg{}, ErrMissingRegisterOperand
	}
	return op.Reg, nil
}

func decodeBranch(inst asm.Instruction) (int64, error) {
	immOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return 0, ErrMissingBranchOffset
	}
	return immOp.Value, nil
}

func decodeRegBranch(inst asm.Instruction) (r asm.PReg, offset int64, err error) {
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
func decodeTestBranch(inst asm.Instruction) (r asm.PReg, bit uint8, offset int64, err error) {
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
