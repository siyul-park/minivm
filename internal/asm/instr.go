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
