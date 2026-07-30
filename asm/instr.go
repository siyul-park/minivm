package asm

import (
	"fmt"
	"strings"
)

// Instruction is the architecture-neutral IR row consumed by the
// assembler. Op is opaque to asm; each architecture defines its own Op
// constants. Four operand slots cover every supported instruction shape;
// unused tails stay nil.
type Instruction struct {
	Op   uint16
	Dst  Operand
	Src1 Operand
	Src2 Operand
	Src3 Operand
}

// OpPseudoUse is a zero-byte pseudo-instruction that extends the live range
// of its virtual-register source operands through its position.
const OpPseudoUse uint16 = 0xFFFF

func (i Instruction) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d", i.Op)
	sep := " "
	for _, op := range [4]Operand{i.Dst, i.Src1, i.Src2, i.Src3} {
		if op == nil {
			continue
		}
		b.WriteString(sep)
		b.WriteString(op.String())
		sep = ", "
	}
	return b.String()
}

// def returns the virtual register the instruction writes.
func (i Instruction) def() (VReg, bool) {
	v, ok := i.Dst.(VRegOperand)
	return v.Reg, ok
}

// uses collects every virtual register the instruction reads: the
// destination's memory base, then each source's register or memory base.
// The count is bounded by the four operand slots, so the result never
// escapes to the heap.
func (i Instruction) uses() (regs [4]VReg, n int) {
	if v, ok := base(i.Dst); ok {
		regs[n] = v
		n++
	}
	for _, op := range [3]Operand{i.Src1, i.Src2, i.Src3} {
		if v, ok := op.(VRegOperand); ok {
			regs[n] = v.Reg
			n++
			continue
		}
		if v, ok := base(op); ok {
			regs[n] = v
			n++
		}
	}
	return regs, n
}

// base returns the virtual register op dereferences as a memory address.
func base(op Operand) (VReg, bool) {
	mem, ok := op.(MemOperand)
	if !ok {
		return VReg{}, false
	}
	v, ok := mem.Base.(VRegOperand)
	return v.Reg, ok
}
