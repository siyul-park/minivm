package asm

import "fmt"

// Instruction is the architecture-neutral IR row consumed by the
// assembler. Op is opaque to asm/; each architecture defines its own
// Op constants. Up to four operand slots cover every supported
// instruction shape; unused tails stay nil.
type Instruction struct {
	Op   uint16
	Dst  Operand
	Src1 Operand
	Src2 Operand
	Src3 Operand
}

// OpPseudoUse is a zero-byte pseudo-instruction that extends the live range
// of virtual-register source operands through its position.
const OpPseudoUse uint16 = 0xFFFE

// OpPseudoLabel is a pseudo-instruction that marks a label position and
// emits zero bytes during encoding.
const OpPseudoLabel uint16 = 0xFFFF

// Def returns the destination vreg if Dst is a virtual-register operand.
// Pure operand inspection — no side effects.
func (i Instruction) Def() (VReg, bool) {
	if v, ok := i.Dst.(VRegOperand); ok {
		return v.Reg, true
	}
	return VReg{}, false
}

// Uses returns every vreg the instruction reads, including memory-base
// references in any operand slot. Order is Dst-base, Src1, Src2, Src3.
func (i Instruction) Uses() []VReg {
	memBase := func(op Operand) (VReg, bool) {
		mem, ok := op.(MemOperand)
		if !ok {
			return VReg{}, false
		}
		v, ok := mem.Base.(VRegOperand)
		if !ok {
			return VReg{}, false
		}
		return v.Reg, true
	}
	var regs []VReg
	if base, ok := memBase(i.Dst); ok {
		regs = append(regs, base)
	}
	for _, op := range []Operand{i.Src1, i.Src2, i.Src3} {
		if v, ok := op.(VRegOperand); ok {
			regs = append(regs, v.Reg)
			continue
		}
		if base, ok := memBase(op); ok {
			regs = append(regs, base)
		}
	}
	return regs
}

func (i Instruction) String() string {
	switch {
	case i.Dst != nil && i.Src1 != nil && i.Src2 != nil:
		return fmt.Sprintf("%v %v, %v, %v", i.Op, i.Dst, i.Src1, i.Src2)
	case i.Dst != nil && i.Src1 != nil:
		return fmt.Sprintf("%v %v, %v", i.Op, i.Dst, i.Src1)
	case i.Dst != nil:
		return fmt.Sprintf("%v %v", i.Op, i.Dst)
	default:
		return fmt.Sprintf("%v", i.Op)
	}
}
