package arm64

import "github.com/siyul-park/minivm/asm"

// skipDisp is the fixed forward byte displacement the inverted skip
// branch Relax emits uses to jump over the single unconditional B that
// follows it. Branch offsets are relative to the branch instruction
// itself, so clearing one 4-byte instruction needs +8.
const skipDisp = 8

// invCond maps each AArch64 conditional-branch opcode to its logical
// inverse: EQ<->NE, CS<->CC, MI<->PL, VS<->VC, HI<->LS, GE<->LT, GT<->LE.
var invCond = map[Op]Op{
	OpBEQ: OpBNE, OpBNE: OpBEQ,
	OpBCS: OpBCC, OpBCC: OpBCS,
	OpBMI: OpBPL, OpBPL: OpBMI,
	OpBVS: OpBVC, OpBVC: OpBVS,
	OpBHI: OpBLS, OpBLS: OpBHI,
	OpBGE: OpBLT, OpBLT: OpBGE,
	OpBGT: OpBLE, OpBLE: OpBGT,
}

// relaxRangeBits maps each label-branch opcode Relax handles to the bit
// width of its PC-relative immediate field, matching the widths
// encoder.go's checkBranchOffset validates for the same opcodes. TBZ/TBNZ
// are absent: instr.go never exposes a label-carrying constructor for
// them (TBZ/TBNZ always take a caller-computed immediate offset), so they
// never reach Relax with a LabelOperand and need no entry here.
var relaxRangeBits = map[Op]uint{
	OpBEQ: 19, OpBNE: 19, OpBCS: 19, OpBCC: 19, OpBMI: 19, OpBPL: 19,
	OpBVS: 19, OpBVC: 19, OpBHI: 19, OpBLS: 19, OpBGE: 19, OpBLT: 19,
	OpBGT: 19, OpBLE: 19,
	OpCBZ: 19, OpCBNZ: 19,
}

// Relax implements asm.Relaxer for arm64. For B.cond and CBZ/CBNZ label
// branches whose imm19 field (+-1MB) does not fit disp, it rewrites the
// branch into an inverted-condition branch that skips over an
// unconditional B to the original target:
//
//	B.<inv-cond> +8   ; skip the B below when the original condition is false
//	B   <target>      ; imm26, +-128MB
//
// Both replacement instructions are constructed to already be in range
// (the skip branch's displacement is a fixed 8 bytes; the B's range is
// checked before committing), so a branch relaxes at most once.
func (a arch) Relax(inst asm.Instruction, disp int64) ([]asm.Instruction, bool) {
	bits, ok := relaxRangeBits[Op(inst.Op)]
	if !ok {
		return nil, false
	}
	if checkBranchOffset(disp, bits) == nil {
		return nil, false
	}

	lbl, ok := inst.Src2.(asm.LabelOperand)
	if !ok {
		return nil, false
	}

	// Forward targets move by the same four bytes as the inserted B, so their
	// displacement is unchanged. Backward targets stay fixed while the B moves
	// four bytes forward.
	bDisp := disp
	if disp < 0 {
		bDisp -= 4
	}
	if checkBranchOffset(bDisp, 26) != nil {
		return nil, false
	}

	skip, ok := invertBranch(inst)
	if !ok {
		return nil, false
	}
	return []asm.Instruction{skip, BLabel(lbl.ID)}, true
}

// invertBranch builds the inverted-condition skip branch that precedes
// the unconditional B Relax emits, preserving inst's register operand
// (for CBZ/CBNZ) and inverting its condition/comparison sense so control
// falls through to the B only when the original branch would have been
// taken.
func invertBranch(inst asm.Instruction) (asm.Instruction, bool) {
	switch Op(inst.Op) {
	case OpCBZ:
		return asm.Instruction{Op: uint16(OpCBNZ), Src1: inst.Src1, Src2: asm.Imm(skipDisp)}, true
	case OpCBNZ:
		return asm.Instruction{Op: uint16(OpCBZ), Src1: inst.Src1, Src2: asm.Imm(skipDisp)}, true
	default:
		inv, ok := invCond[Op(inst.Op)]
		if !ok {
			return asm.Instruction{}, false
		}
		return asm.Instruction{Op: uint16(inv), Src2: asm.Imm(skipDisp)}, true
	}
}
