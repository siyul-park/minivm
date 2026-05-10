package asm

import "fmt"

// OpPseudoLabel is a pseudo-instruction that marks a label position.
// It emits zero bytes and is stripped before register allocation.
const OpPseudoLabel uint16 = 0xFFFF

type Instruction struct {
	Op   uint16
	Dst  Operand
	Src1 Operand
	Src2 Operand
	Src3 Operand
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
