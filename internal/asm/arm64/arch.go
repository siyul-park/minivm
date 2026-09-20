package arm64

import (
	"slices"

	"github.com/siyul-park/minivm/internal/asm"
)

type arch struct {
	encoder *Encoder
}

// skipDisp skips the single 4-byte B after the inverted conditional branch.
const skipDisp = 8

// ints are the allocatable general registers. The omitted registers are
// reserved by the native runtime or the Go ABI.
var ints = []asm.PReg{
	X0, X1, X2, X3, X4, X5, X6, X7, X8, X9, X10, X11, X12, X13, X14, X15,
	X19, X20, X21, X22, X23, X24, X25, X27,
}

// floats are all scalar floating-point registers.
var floats = []asm.PReg{
	D0, D1, D2, D3, D4, D5, D6, D7, D8, D9, D10, D11, D12, D13, D14, D15,
	D16, D17, D18, D19, D20, D21, D22, D23, D24, D25, D26, D27, D28, D29, D30, D31,
}

// invertOp maps a branch opcode to its inverted sense.
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

var _ asm.Arch = arch{}
var _ asm.Frame = arch{}
var _ asm.Relaxer = arch{}

// New returns the ARM64 assembler architecture.
func New() arch {
	return arch{encoder: NewEncoder()}
}

// Encoder returns the target instruction encoder.
func (a arch) Encoder() asm.Encoder {
	return a.encoder
}

// Flow reports how control leaves an instruction.
func (a arch) Flow(inst asm.Instruction) asm.Flow {
	switch Op(inst.Op) {
	case OpB:
		return asm.FlowJump
	case OpBL, OpBLR:
		return asm.FlowCall
	case OpBR, OpRET, OpBRK, OpHLT, OpERET:
		return asm.FlowEnd
	case OpCBZ, OpCBNZ, OpTBZ, OpTBNZ,
		OpBEQ, OpBNE, OpBLT, OpBGT, OpBLE, OpBGE, OpBMI, OpBPL, OpBVS, OpBVC, OpBHI, OpBLS, OpBCS, OpBCC:
		return asm.FlowBranch
	default:
		return asm.FlowNext
	}
}

// Writes reports which instruction operands are written.
func (a arch) Writes(inst asm.Instruction) [4]bool {
	switch Op(inst.Op) {
	case OpSTR, OpSTRB, OpSTRH, OpSTRW, OpSTRR, OpSTP,
		OpCMP, OpCMPI, OpCMN, OpCMNI, OpTST, OpTSTI, OpCCMP, OpCCMPI, OpFCMP, OpFCMPE:
		return [4]bool{}
	case OpLDP:
		return [4]bool{true, false, true}
	default:
		return [4]bool{inst.Dst != nil && a.Flow(inst) == asm.FlowNext}
	}
}

// Registers lists the allocatable registers in preference order.
func (a arch) Registers(typ asm.RegType) []asm.PReg {
	if typ == asm.RegTypeFloat {
		return slices.Clone(floats)
	}
	return slices.Clone(ints)
}

// Spill stores r into spill slot n.
func (a arch) Spill(r asm.Reg, slot int) asm.Instruction {
	if r.Type() == asm.RegTypeInt && r.Width() == asm.Width32 {
		return STRW(r, SP, int16(8*slot))
	}
	return STR(r, SP, int16(8*slot))
}

// Reload loads r from spill slot n.
func (a arch) Reload(r asm.Reg, slot int) asm.Instruction {
	return LDR(r, SP, int16(8*slot))
}

// Relax implements asm.Relaxer for ARM64 conditional label branches.
// Out-of-range branches become an inverted skip plus an unconditional B.
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

	// Inserting B leaves forward displacement unchanged; a backward target
	// moves four bytes farther from the conditional branch.
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
