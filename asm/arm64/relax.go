package arm64

import "github.com/siyul-park/minivm/asm"

// skipDisp is the fixed forward byte displacement the inverted skip
// branch Relax emits uses to jump over the single unconditional B that
// follows it. Branch offsets are relative to the branch instruction
// itself, so clearing one 4-byte instruction needs +8.
const skipDisp = 8

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
	skip, ok := invertBranch(inst)
	if !ok {
		return nil, false
	}
	if checkBranchOffset(disp, 19) == nil {
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
	case OpBEQ:
		return BNE(skipDisp), true
	case OpBNE:
		return BEQ(skipDisp), true
	case OpBCS:
		return BCC(skipDisp), true
	case OpBCC:
		return BCS(skipDisp), true
	case OpBMI:
		return BPL(skipDisp), true
	case OpBPL:
		return BMI(skipDisp), true
	case OpBVS:
		return BVC(skipDisp), true
	case OpBVC:
		return BVS(skipDisp), true
	case OpBHI:
		return BLS(skipDisp), true
	case OpBLS:
		return BHI(skipDisp), true
	case OpBGE:
		return BLT(skipDisp), true
	case OpBLT:
		return BGE(skipDisp), true
	case OpBGT:
		return BLE(skipDisp), true
	case OpBLE:
		return BGT(skipDisp), true
	default:
		return asm.Instruction{}, false
	}
}
