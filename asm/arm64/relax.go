package arm64

import "github.com/siyul-park/minivm/asm"

// skipDisp is the fixed forward byte displacement the inverted skip
// branch Relax emits uses to jump over the single unconditional B that
// follows it. Branch offsets are relative to the branch instruction
// itself, so clearing one 4-byte instruction needs +8.
const skipDisp = 8

// invertOp maps a branch opcode to its inverted sense. Inverting the
// condition lets Relax skip over an unconditional B to the original target.
var invertOp = map[Op]Op{
	OpCBZ: OpCBNZ, OpCBNZ: OpCBZ,
	OpBEQ: OpBNE, OpBNE: OpBEQ,
	OpBCS: OpBCC, OpBCC: OpBCS,
	OpBMI: OpBPL, OpBPL: OpBMI,
	OpBVS: OpBVC, OpBVC: OpBVS,
	OpBHI: OpBLS, OpBLS: OpBHI,
	OpBGE: OpBLT, OpBLT: OpBGE,
	OpBGT: OpBLE, OpBLE: OpBGT,
}

// Relax implements asm.Relaxer for arm64. For B.cond and CBZ/CBNZ label
// branches whose imm19 field (+-1MB) does not fit disp, it rewrites the
// branch into an inverted-condition branch that skips over an
// unconditional B to the original target:
//
//	B.<inv-cond> +8   ; skip the B below when the original condition is false
//	B   <target>      ; imm26, +-128MB
//
// Each replacement is constructed to already be in range, so a branch
// relaxes at most once.
func (a arch) Relax(inst asm.Instruction, disp int64) ([]asm.Instruction, bool) {
	op := Op(inst.Op)
	inv, ok := invertOp[op]
	if !ok {
		return nil, false
	}
	if checkBranchOffset(op, disp, 19) == nil {
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
	if checkBranchOffset(OpB, bDisp, 26) != nil {
		return nil, false
	}

	skip := asm.Instruction{Op: uint16(inv), Src2: asm.Imm(skipDisp)}
	if op == OpCBZ || op == OpCBNZ {
		skip.Src1 = inst.Src1
	}
	return []asm.Instruction{skip, BLabel(lbl.ID)}, true
}
