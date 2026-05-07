package arm64

import (
	"errors"

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
		// ADD (shifted reg, 64-bit, shift=LSL #0)
		return enc(0x8B000000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpADDS:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xAB000000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpSUB:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xCB000000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpSUBS:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xEB000000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpNEG: // NEG Xd, Xm  →  SUB Xd, XZR, Xm
		d, m, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xCB000000 | reg(m)<<16 | 0x1F<<5 | reg(d)), nil

	case OpNEGS: // NEGS Xd, Xm  →  SUBS Xd, XZR, Xm
		d, m, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xEB000000 | reg(m)<<16 | 0x1F<<5 | reg(d)), nil

	case OpMUL: // MUL  →  MADD Ra=XZR
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9B007C00 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpMNEG: // MNEG  →  MSUB Ra=XZR
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9B00FC00 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpMADD: // MADD Xd, Xn, Xm, Xa  — Ra from Src2 field (see inst.go note)
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		// Without a dedicated accumulator operand we default Ra=XZR (==MUL).
		// When the IR is extended with an Acc field, replace 0x1F with acc.ID().
		return enc(0x9B000000 | reg(m)<<16 | 0x1F<<10 | reg(n)<<5 | reg(d)), nil

	case OpMSUB:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9B008000 | reg(m)<<16 | 0x1F<<10 | reg(n)<<5 | reg(d)), nil

	case OpSDIV:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9AC00C00 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpUDIV:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9AC00800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpADC:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9A000000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpADCS:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xBA000000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpSBC:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xDA000000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpSBCS:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xFA000000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Arithmetic — immediate
	// -----------------------------------------------------------------------

	case OpADDI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		// ADD (immediate, 64-bit): sf=1 op=0 S=0  imm12[21:10]
		return enc(0x91000000 | (uint32(imm)&0xFFF)<<10 | reg(n)<<5 | reg(d)), nil

	case OpADDSI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xB1000000 | (uint32(imm)&0xFFF)<<10 | reg(n)<<5 | reg(d)), nil

	case OpSUBI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xD1000000 | (uint32(imm)&0xFFF)<<10 | reg(n)<<5 | reg(d)), nil

	case OpSUBSI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xF1000000 | (uint32(imm)&0xFFF)<<10 | reg(n)<<5 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Bitwise — register
	// -----------------------------------------------------------------------

	case OpAND:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x8A000000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpANDS:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xEA000000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpORR:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xAA000000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpEOR:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xCA000000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpBIC:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		// BIC (shifted reg): sf=1 opc=00 N=1
		return enc(0x8A200000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpBICS:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xEA200000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpEON:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xCA200000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpORN:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xAA200000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpMVN: // MVN Xd, Xm  →  ORN Xd, XZR, Xm
		d, m, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xAA2003E0 | reg(m)<<16 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Bitwise — immediate  (logical immediate encoding, N=1 for 64-bit)
	// -----------------------------------------------------------------------

	case OpANDI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		immr, imms, ok := encodeLogicalImm(uint64(imm), true)
		if !ok {
			return nil, ErrMissingImmediate
		}
		return enc(0x92000000 | 1<<22 | immr<<16 | imms<<10 | reg(n)<<5 | reg(d)), nil

	case OpANDSI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		immr, imms, ok := encodeLogicalImm(uint64(imm), true)
		if !ok {
			return nil, ErrMissingImmediate
		}
		return enc(0xF2000000 | 1<<22 | immr<<16 | imms<<10 | reg(n)<<5 | reg(d)), nil

	case OpORRI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		immr, imms, ok := encodeLogicalImm(uint64(imm), true)
		if !ok {
			return nil, ErrMissingImmediate
		}
		return enc(0xB2000000 | 1<<22 | immr<<16 | imms<<10 | reg(n)<<5 | reg(d)), nil

	case OpEORI:
		d, n, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		immr, imms, ok := encodeLogicalImm(uint64(imm), true)
		if !ok {
			return nil, ErrMissingImmediate
		}
		return enc(0xD2000000 | 1<<22 | immr<<16 | imms<<10 | reg(n)<<5 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Shift — register
	// -----------------------------------------------------------------------

	case OpLSL: // LSLV
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9AC02000 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpLSR: // LSRV
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9AC02400 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpASR: // ASRV
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9AC02800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpROR: // RORV
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9AC02C00 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Shift — immediate  (encoded as UBFM / SBFM / EXTR)
	// -----------------------------------------------------------------------

	case OpLSLI:
		// LSL (imm) Xd, Xn, #shift  →  UBFM Xd, Xn, #(-shift MOD 64), #(63-shift)
		d, n, shift, err := decodeRegShift(inst)
		if err != nil {
			return nil, err
		}
		s := uint32(shift) & 63
		immr := (-s) & 63
		imms := 63 - s
		return enc(0xD3400000 | immr<<16 | imms<<10 | reg(n)<<5 | reg(d)), nil

	case OpLSRI:
		// LSR (imm) Xd, Xn, #shift  →  UBFM Xd, Xn, #shift, #63
		d, n, shift, err := decodeRegShift(inst)
		if err != nil {
			return nil, err
		}
		s := uint32(shift) & 63
		return enc(0xD3400000 | s<<16 | 0x3F<<10 | reg(n)<<5 | reg(d)), nil

	case OpASRI:
		// ASR (imm) Xd, Xn, #shift  →  SBFM Xd, Xn, #shift, #63
		d, n, shift, err := decodeRegShift(inst)
		if err != nil {
			return nil, err
		}
		s := uint32(shift) & 63
		return enc(0x93400000 | s<<16 | 0x3F<<10 | reg(n)<<5 | reg(d)), nil

	case OpRORI:
		// ROR (imm) Xd, Xn, #shift  →  EXTR Xd, Xn, Xn, #shift
		d, n, shift, err := decodeRegShift(inst)
		if err != nil {
			return nil, err
		}
		s := uint32(shift) & 63
		return enc(0x93C00000 | reg(n)<<16 | s<<10 | reg(n)<<5 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Bit manipulation
	// -----------------------------------------------------------------------

	case OpCLZ:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xDAC01000 | reg(n)<<5 | reg(d)), nil

	case OpRBIT:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xDAC00000 | reg(n)<<5 | reg(d)), nil

	case OpREV:
		// REV64 (sf=1)
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xDAC00C00 | reg(n)<<5 | reg(d)), nil

	case OpREV16:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xDAC00400 | reg(n)<<5 | reg(d)), nil

	case OpREV32:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xDAC00800 | reg(n)<<5 | reg(d)), nil

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
		return enc(0xEA000000 | reg(m)<<16 | reg(n)<<5 | 0x1F), nil

	case OpTSTI:
		n, imm, err := decodeCmpImm(inst)
		if err != nil {
			return nil, err
		}
		immr, imms, ok := encodeLogicalImm(uint64(imm), true)
		if !ok {
			return nil, ErrMissingImmediate
		}
		return enc(0xF2000000 | 1<<22 | immr<<16 | imms<<10 | reg(n)<<5 | 0x1F), nil

	// -----------------------------------------------------------------------
	// Compare
	// -----------------------------------------------------------------------

	case OpCMP: // SUBS XZR, Xn, Xm
		n, m, err := decodeCmp(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xEB00001F | reg(m)<<16 | reg(n)<<5), nil

	case OpCMPI: // SUBS XZR, Xn, #imm
		n, imm, err := decodeCmpImm(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xF100001F | (uint32(imm)&0xFFF)<<10 | reg(n)<<5), nil

	case OpCMN: // ADDS XZR, Xn, Xm
		n, m, err := decodeCmp(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xAB00001F | reg(m)<<16 | reg(n)<<5), nil

	case OpCMNI: // ADDS XZR, Xn, #imm
		n, imm, err := decodeCmpImm(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xB100001F | (uint32(imm)&0xFFF)<<10 | reg(n)<<5), nil

	case OpCCMP:
		// CCMP Xn, Xm, #nzcv, cond
		// Minimal encoding: nzcv=0, cond=AL (0xE) — full operand needs extension
		n, m, err := decodeCmp(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xFA400000 | reg(m)<<16 | 0xE<<12 | reg(n)<<5), nil

	case OpCCMPI:
		n, imm, err := decodeCmpImm(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xFA400800 | (uint32(imm)&0x1F)<<16 | 0xE<<12 | reg(n)<<5), nil

	// -----------------------------------------------------------------------
	// Move
	// -----------------------------------------------------------------------

	case OpMOV: // MOV Xd, Xn  →  ORR Xd, XZR, Xn
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xAA0003E0 | reg(n)<<16 | reg(d)), nil

	case OpMOVI: // pseudo: MOVZ + MOVK sequence; emit MOVZ for first 16 bits
		d, imm64, err := decodeDstImm(inst)
		if err != nil {
			return nil, err
		}
		u := uint64(imm64)
		// Emit MOVZ for bits[15:0], shift=0
		return enc(0xD2800000 | (uint32(u)&0xFFFF)<<5 | reg(d)), nil

	case OpMOVZ:
		d, imm, shift, err := decodeMovImm(inst)
		if err != nil {
			return nil, err
		}
		hw := uint32(shift/16) & 3
		return enc(0xD2800000 | hw<<21 | (uint32(imm)&0xFFFF)<<5 | reg(d)), nil

	case OpMOVK:
		d, imm, shift, err := decodeMovImm(inst)
		if err != nil {
			return nil, err
		}
		hw := uint32(shift/16) & 3
		return enc(0xF2800000 | hw<<21 | (uint32(imm)&0xFFFF)<<5 | reg(d)), nil

	case OpMOVN:
		d, imm, shift, err := decodeMovImm(inst)
		if err != nil {
			return nil, err
		}
		hw := uint32(shift/16) & 3
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
		return enc(0x9E620000 | reg(n)<<5 | reg(d)), nil

	case OpUCVTF: // UCVTF Dd, Xn  (unsigned integer → double)
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9E630000 | reg(n)<<5 | reg(d)), nil

	case OpFCVTZS: // FCVTZS Xd, Dn  (double → integer, round toward zero)
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9E780000 | reg(n)<<5 | reg(d)), nil

	case OpFCVTZU: // FCVTZU Xd, Dn  (double → unsigned integer)
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9E790000 | reg(n)<<5 | reg(d)), nil

	case OpFCVT: // FCVT Dd, Sn  (single → double, type=01→00)
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		// FCVT (double←single): ftype=01 opc=00
		return enc(0x1E22C000 | reg(n)<<5 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Float — arithmetic (double precision)
	// -----------------------------------------------------------------------

	case OpFADD:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1E602800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpFSUB:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1E603800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpFMUL:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1E600800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpFDIV:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1E601800 | reg(m)<<16 | reg(n)<<5 | reg(d)), nil

	case OpFMADD: // FMADD Dd, Dn, Dm, Da=D0(placeholder)
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1F400000 | reg(m)<<16 | 0x00<<10 | reg(n)<<5 | reg(d)), nil

	case OpFMSUB:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1F408000 | reg(m)<<16 | 0x00<<10 | reg(n)<<5 | reg(d)), nil

	case OpFNMADD:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1F600000 | reg(m)<<16 | 0x00<<10 | reg(n)<<5 | reg(d)), nil

	case OpFNMSUB:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1F608000 | reg(m)<<16 | 0x00<<10 | reg(n)<<5 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Float — unary
	// -----------------------------------------------------------------------

	case OpFABS:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1E60C000 | reg(n)<<5 | reg(d)), nil

	case OpFNEG:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1E614000 | reg(n)<<5 | reg(d)), nil

	case OpFSQRT:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1E61C000 | reg(n)<<5 | reg(d)), nil

	case OpFRINTN: // round to nearest, ties to even
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1E644000 | reg(n)<<5 | reg(d)), nil

	case OpFRINTM: // round toward minus infinity
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1E654000 | reg(n)<<5 | reg(d)), nil

	case OpFRINTP: // round toward plus infinity
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1E64C000 | reg(n)<<5 | reg(d)), nil

	case OpFRINTZ: // round toward zero
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1E65C000 | reg(n)<<5 | reg(d)), nil

	// -----------------------------------------------------------------------
	// Float — move / compare
	// -----------------------------------------------------------------------

	case OpFMOV:
		d, n, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		dFloat := d.Type() == asm.RegTypeFloat
		nFloat := n.Type() == asm.RegTypeFloat
		switch {
		case dFloat && nFloat: // float → float register copy
			if d.Width() == asm.Width32 {
				return enc(0x1E204000 | reg(n)<<5 | reg(d)), nil // FMOV Sd, Sn
			}
			return enc(0x1E604000 | reg(n)<<5 | reg(d)), nil // FMOV Dd, Dn
		case dFloat && !nFloat: // int → float (bit-copy)
			if d.Width() == asm.Width32 {
				return enc(0x1E270000 | reg(n)<<5 | reg(d)), nil // FMOV Sd, Wn
			}
			return enc(0x9E670000 | reg(n)<<5 | reg(d)), nil // FMOV Dd, Xn
		default: // float → int (bit-copy)
			if n.Width() == asm.Width32 {
				return enc(0x1E260000 | reg(n)<<5 | reg(d)), nil // FMOV Wn, Sd
			}
			return enc(0x9E660000 | reg(n)<<5 | reg(d)), nil // FMOV Xn, Dd
		}

	case OpFCMP: // FCMP Dn, Dm — sets NZCV
		n, m, err := decodeCmp(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1E602000 | reg(m)<<16 | reg(n)<<5), nil

	case OpFCMPE: // FCMPE — also raises Invalid if either operand is NaN
		n, m, err := decodeCmp(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x1E602010 | reg(m)<<16 | reg(n)<<5), nil

	// -----------------------------------------------------------------------
	// Conditional select
	// -----------------------------------------------------------------------

	case OpCSEL: // CSEL Xd, Xn, Xm, cond(=AL)
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9A800000 | reg(m)<<16 | 0xE<<12 | reg(n)<<5 | reg(d)), nil

	case OpCSINC: // CSINC Xd, Xn, Xm, cond
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0x9A800400 | reg(m)<<16 | 0xE<<12 | reg(n)<<5 | reg(d)), nil

	case OpCSINV:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xDA800000 | reg(m)<<16 | 0xE<<12 | reg(n)<<5 | reg(d)), nil

	case OpCSNEG:
		d, n, m, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return enc(0xDA800400 | reg(m)<<16 | 0xE<<12 | reg(n)<<5 | reg(d)), nil

	case OpCSET: // CSET Xd, cond  →  CSINC Xd, XZR, XZR, !cond
		d, condImm, err := decodeDstImm(inst)
		if err != nil {
			return nil, err
		}
		cond := uint32(condImm)&0xF ^ 1 // invert lsb to negate condition
		return enc(0x9A9F07E0 | cond<<12 | reg(d)), nil

	case OpCSETM: // CSETM Xd, cond  →  CSINV Xd, XZR, XZR, !cond
		d, condImm, err := decodeDstImm(inst)
		if err != nil {
			return nil, err
		}
		cond := uint32(condImm)&0xF ^ 1
		return enc(0xDA9F03E0 | cond<<12 | reg(d)), nil

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
		return enc(0xD61F0000 | reg(r)<<5), nil

	case OpBLR:
		r, err := decodeRegOnly(inst)
		if err != nil {
			return nil, err
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
		imm19 := (uint32(offset/4) & 0x7FFFF) << 5
		return enc(0xB4000000 | imm19 | reg(r)), nil

	case OpCBNZ:
		r, offset, err := decodeRegBranch(inst)
		if err != nil {
			return nil, err
		}
		imm19 := (uint32(offset/4) & 0x7FFFF) << 5
		return enc(0xB5000000 | imm19 | reg(r)), nil

	// -----------------------------------------------------------------------
	// Branch — test-and-branch
	// -----------------------------------------------------------------------

	case OpTBZ:
		r, bit, offset, err := decodeTestBranch(inst)
		if err != nil {
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
	if val == 0 || (is64 && val == ^uint64(0)) {
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
		for i := uint(esize); i < 64; i += esize {
			if (val>>i)&mask != elem {
				uniform = false
				break
			}
		}
		if !uniform {
			continue
		}

		// Count leading/trailing zeros/ones within the element.
		ones := popcount64(elem, esize)
		if ones == 0 || ones == esize {
			continue // all-zeros or all-ones element
		}

		// Find rotation: number of trailing zeros before the first 1.
		tz := trailingZeros64(elem, esize)
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

func popcount64(v uint64, bits uint) (n uint) {
	for i := uint(0); i < bits; i++ {
		if v>>i&1 == 1 {
			n++
		}
	}
	return
}

func trailingZeros64(v uint64, bits uint) uint {
	for i := uint(0); i < bits; i++ {
		if v>>i&1 == 1 {
			return i
		}
	}
	return bits
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
