package arm64

import (
	"errors"

	"github.com/siyul-park/minivm/asm"
)

type Encoder struct{}

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

var _ asm.Encoder = (*Encoder)(nil)

func NewEncoder() *Encoder {
	return &Encoder{}
}

func (*Encoder) Encode(inst asm.Instruction) ([]byte, error) {
	switch Op(inst.Op) {
	case OpADD:
		dst, src1, src2, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encode(0x8B000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID())), nil
	case OpADDI:
		dst, src, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return encode(0x91000000 | uint32(imm)<<10 | uint32(src.ID())<<5 | uint32(dst.ID())), nil
	case OpADDS:
		dst, src1, src2, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xAB000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID())), nil
	case OpADDSI:
		dst, src, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xB1000000 | uint32(imm)<<10 | uint32(src.ID())<<5 | uint32(dst.ID())), nil
	case OpSUB:
		dst, src1, src2, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xCB000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID())), nil
	case OpSUBI:
		dst, src, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xD1000000 | uint32(imm)<<10 | uint32(src.ID())<<5 | uint32(dst.ID())), nil
	case OpSUBS:
		dst, src1, src2, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xEB000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID())), nil
	case OpSUBSI:
		dst, src, imm, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xF1000000 | uint32(imm)<<10 | uint32(src.ID())<<5 | uint32(dst.ID())), nil
	case OpMUL:
		dst, src1, src2, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encode(0x9B007C00 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID())), nil
	case OpDIV:
		dst, src1, src2, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encode(0x9AC00C00 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID())), nil
	case OpAND:
		dst, src1, src2, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encode(0x8A000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID())), nil
	case OpORR:
		dst, src1, src2, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xAA000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID())), nil
	case OpEOR:
		dst, src1, src2, err := decodeReg3(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xCA000000 | uint32(src2.ID())<<16 | uint32(src1.ID())<<5 | uint32(dst.ID())), nil
	case OpLSL:
		dst, src, shift, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xD3400000 | uint32(64-shift)<<16 | uint32(63-shift)<<10 | uint32(src.ID())<<5 | uint32(dst.ID())), nil
	case OpLSR:
		dst, src, shift, err := decodeRegImm(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xD340FC00 | uint32(shift)<<16 | uint32(src.ID())<<5 | uint32(dst.ID())), nil
	case OpCMP:
		src1, src2, err := decodeCmp(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xEB00001F | uint32(src2.ID())<<16 | uint32(src1.ID())<<5), nil
	case OpCMPI:
		src, imm, err := decodeCmpImm(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xF1000000 | uint32(imm)<<10 | uint32(src.ID())<<5 | 0x1F), nil
	case OpMOV:
		dst, src, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xAA0003E0 | uint32(src.ID())<<16 | uint32(dst.ID())), nil
	case OpMOVZ:
		dst, imm, shift, err := decodeMovImm(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xD2800000 | uint32(shift/16)<<21 | uint32(imm)<<5 | uint32(dst.ID())), nil
	case OpMOVK:
		dst, imm, shift, err := decodeMovImm(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xF2800000 | uint32(shift/16)<<21 | uint32(imm)<<5 | uint32(dst.ID())), nil
	case OpLDR:
		dst, base, offset, err := decodeMemOp(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xF9400000 | uint32(offset/8)<<10 | uint32(base.ID())<<5 | uint32(dst.ID())), nil
	case OpSTR:
		src, base, offset, err := decodeStrOp(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xF9000000 | uint32(offset/8)<<10 | uint32(base.ID())<<5 | uint32(src.ID())), nil
	case OpSCVTF:
		dst, src, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encode(0x9E620000 | uint32(src.ID())<<5 | uint32(dst.ID())), nil
	case OpFCVTZS:
		dst, src, err := decodeReg2(inst)
		if err != nil {
			return nil, err
		}
		return encode(0x9E780000 | uint32(src.ID())<<5 | uint32(dst.ID())), nil
	case OpRET:
		return encode(0xD65F03C0), nil
	case OpB:
		offset, err := decodeBranch(inst)
		if err != nil {
			return nil, err
		}
		return encode(0x14000000 | uint32(offset/4)&0x3FFFFFF), nil
	case OpBL:
		offset, err := decodeBranch(inst)
		if err != nil {
			return nil, err
		}
		return encode(0x94000000 | uint32(offset/4)&0x3FFFFFF), nil
	case OpBR:
		reg, err := decodeRegOnly(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xD61F0000 | uint32(reg.ID())<<5), nil
	case OpBLR:
		reg, err := decodeRegOnly(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xD63F0000 | uint32(reg.ID())<<5), nil
	case OpCBZ:
		reg, offset, err := decodeRegBranch(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xB4000000 | uint32(offset/4)&0x7FFFF<<5 | uint32(reg.ID())), nil
	case OpCBNZ:
		reg, offset, err := decodeRegBranch(inst)
		if err != nil {
			return nil, err
		}
		return encode(0xB5000000 | uint32(offset/4)&0x7FFFF<<5 | uint32(reg.ID())), nil
	case OpBEQ, OpBNE, OpBLT, OpBGT, OpBLE, OpBGE:
		cond, offset, err := decodeCondBranch(inst)
		if err != nil {
			return nil, err
		}
		return encode(0x54000000 | uint32(offset/4)&0x7FFFF<<5 | uint32(cond)), nil
	default:
		return nil, ErrUnsupportedOpcode
	}
}

func decodeReg3(inst asm.Instruction) (dst, src1, src2 asm.Register, err error) {
	dstOp, ok := inst.Dst.(asm.RegOperand)
	if !ok {
		return asm.Register{}, asm.Register{}, asm.Register{}, ErrMissingDestinationReg
	}
	src1Op, ok1 := inst.Src1.(asm.RegOperand)
	src2Op, ok2 := inst.Src2.(asm.RegOperand)
	if !ok1 || !ok2 {
		return asm.Register{}, asm.Register{}, asm.Register{}, ErrMissingSourceRegs
	}
	return dstOp.Reg, src1Op.Reg, src2Op.Reg, nil
}

func decodeReg2(inst asm.Instruction) (dst, src asm.Register, err error) {
	dstOp, ok := inst.Dst.(asm.RegOperand)
	if !ok {
		return asm.Register{}, asm.Register{}, ErrMissingDestinationReg
	}
	srcOp, ok := inst.Src1.(asm.RegOperand)
	if !ok {
		return asm.Register{}, asm.Register{}, ErrMissingSourceReg
	}
	return dstOp.Reg, srcOp.Reg, nil
}

func decodeRegImm(inst asm.Instruction) (dst, src asm.Register, imm int64, err error) {
	dstOp, ok := inst.Dst.(asm.RegOperand)
	if !ok {
		return asm.Register{}, asm.Register{}, 0, ErrMissingDestinationReg
	}
	srcOp, ok := inst.Src1.(asm.RegOperand)
	if !ok {
		return asm.Register{}, asm.Register{}, 0, ErrMissingSourceReg
	}
	immOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return asm.Register{}, asm.Register{}, 0, ErrMissingImmediate
	}
	return dstOp.Reg, srcOp.Reg, immOp.Value, nil
}

func decodeCmp(inst asm.Instruction) (src1, src2 asm.Register, err error) {
	src1Op, ok := inst.Src1.(asm.RegOperand)
	if !ok {
		return asm.Register{}, asm.Register{}, ErrMissingSourceReg
	}
	src2Op, ok := inst.Src2.(asm.RegOperand)
	if !ok {
		return asm.Register{}, asm.Register{}, ErrMissingSourceReg
	}
	return src1Op.Reg, src2Op.Reg, nil
}

func decodeCmpImm(inst asm.Instruction) (src asm.Register, imm int64, err error) {
	srcOp, ok := inst.Src1.(asm.RegOperand)
	if !ok {
		return asm.Register{}, 0, ErrMissingSourceReg
	}
	immOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return asm.Register{}, 0, ErrMissingImmediate
	}
	return srcOp.Reg, immOp.Value, nil
}

func decodeMovImm(inst asm.Instruction) (dst asm.Register, imm int64, shift int64, err error) {
	dstOp, ok := inst.Dst.(asm.RegOperand)
	if !ok {
		return asm.Register{}, 0, 0, ErrMissingDestinationReg
	}
	immOp, ok := inst.Src1.(asm.ImmOperand)
	if !ok {
		return asm.Register{}, 0, 0, ErrMissingImmediate
	}
	shiftOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return asm.Register{}, 0, 0, ErrMissingShiftImmediate
	}
	return dstOp.Reg, immOp.Value, shiftOp.Value, nil
}

func decodeMemOp(inst asm.Instruction) (dst, base asm.Register, offset int64, err error) {
	dstOp, ok := inst.Dst.(asm.RegOperand)
	if !ok {
		return asm.Register{}, asm.Register{}, 0, ErrMissingDestinationReg
	}
	baseOp, ok := inst.Src1.(asm.MemOperand)
	if !ok {
		return asm.Register{}, asm.Register{}, 0, ErrMissingMemoryOperand
	}
	return dstOp.Reg, baseOp.Base, baseOp.Offset, nil
}

func decodeStrOp(inst asm.Instruction) (src, base asm.Register, offset int64, err error) {
	srcOp, ok := inst.Src1.(asm.RegOperand)
	if !ok {
		return asm.Register{}, asm.Register{}, 0, ErrMissingSourceReg
	}
	baseOp, ok := inst.Dst.(asm.MemOperand)
	if !ok {
		return asm.Register{}, asm.Register{}, 0, ErrMissingMemoryOperand
	}
	return srcOp.Reg, baseOp.Base, baseOp.Offset, nil
}

func decodeRegOnly(inst asm.Instruction) (reg asm.Register, err error) {
	reOp, ok := inst.Src1.(asm.RegOperand)
	if !ok {
		return asm.Register{}, ErrMissingRegisterOperand
	}
	return reOp.Reg, nil
}

func decodeBranch(inst asm.Instruction) (int64, error) {
	immOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return 0, ErrMissingBranchOffset
	}
	return immOp.Value, nil
}

func decodeRegBranch(inst asm.Instruction) (reg asm.Register, offset int64, err error) {
	reOp, ok := inst.Src1.(asm.RegOperand)
	if !ok {
		return asm.Register{}, 0, ErrMissingRegisterOperand
	}
	immOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return asm.Register{}, 0, ErrMissingBranchOffset
	}
	return reOp.Reg, immOp.Value, nil
}

func decodeCondBranch(inst asm.Instruction) (cond int64, offset int64, err error) {
	immOp, ok := inst.Src2.(asm.ImmOperand)
	if !ok {
		return 0, 0, ErrMissingBranchOffset
	}
	_, ok = inst.Src1.(asm.RegOperand)
	if ok {
		return 0, 0, ErrUnexpectedRegisterOperand
	}
	return 0, immOp.Value, nil
}

func encode(instr uint32) []byte {
	return []byte{byte(instr), byte(instr >> 8), byte(instr >> 16), byte(instr >> 24)}
}
