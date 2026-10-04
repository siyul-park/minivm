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

func (i Instruction) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d", i.Op)
	sep := " "
	for _, op := range i.operands() {
		if *op == nil {
			continue
		}
		b.WriteString(sep)
		b.WriteString((*op).String())
		sep = ", "
	}
	return b.String()
}

// operands lists the operand slots in order Dst, Src1, Src2, Src3.
func (i *Instruction) operands() [4]*Operand {
	return [4]*Operand{&i.Dst, &i.Src1, &i.Src2, &i.Src3}
}
