package arm64

import "github.com/siyul-park/minivm/internal/asm"

var _ asm.Frame = arch{}

// ints are the allocatable general registers in preference order. Left out:
// X16 and X17, scratch for the exit protocol; X18, the platform register;
// X26, Ctx; X28, Go's g; X29 and X30, the frame; and SP.
var ints = []asm.PReg{
	X0, X1, X2, X3, X4, X5, X6, X7, X8, X9, X10, X11, X12, X13, X14, X15,
	X19, X20, X21, X22, X23, X24, X25, X27,
}

// floats are the allocatable float registers: the whole file, since native
// code owes no callee-saved registers to anyone.
var floats = []asm.PReg{
	D0, D1, D2, D3, D4, D5, D6, D7, D8, D9, D10, D11, D12, D13, D14, D15,
	D16, D17, D18, D19, D20, D21, D22, D23, D24, D25, D26, D27, D28, D29, D30, D31,
}

// Flow implements asm.Frame.
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

// Writes implements asm.Frame: every op writes Dst except the stores,
// compares, and branches, which write nothing, and LDP, whose second
// destination sits in Src2 after the memory operand.
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

// Registers implements asm.Frame.
func (a arch) Registers(typ asm.RegType) []asm.PReg {
	if typ == asm.RegTypeFloat {
		return floats
	}
	return ints
}

// Spill implements asm.Frame.
func (a arch) Spill(r asm.Reg, slot int) asm.Instruction {
	if r.Type() == asm.RegTypeInt && r.Width() == asm.Width32 {
		return STRW(r, SP, int16(8*slot))
	}
	return STR(r, SP, int16(8*slot))
}

// Reload implements asm.Frame.
func (a arch) Reload(r asm.Reg, slot int) asm.Instruction {
	return LDR(r, SP, int16(8*slot))
}
