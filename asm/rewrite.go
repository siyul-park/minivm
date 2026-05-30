package asm

// rewrite returns a copy of insts with every VReg / MemOperand-base
// rewritten to its assigned PReg. Width defaults to the operand's
// declared width; undefined widths fall back to the per-vreg widths map.
func rewrite(insts []Instruction, phys map[int32]PReg, widths map[int32]RegWidth) []Instruction {
	out := make([]Instruction, len(insts))
	for i, inst := range insts {
		out[i] = Instruction{
			Op:   inst.Op,
			Dst:  rewriteOp(inst.Dst, phys, widths),
			Src1: rewriteOp(inst.Src1, phys, widths),
			Src2: rewriteOp(inst.Src2, phys, widths),
			Src3: rewriteOp(inst.Src3, phys, widths),
		}
	}
	return out
}

func rewriteOp(op Operand, phys map[int32]PReg, widths map[int32]RegWidth) Operand {
	switch v := op.(type) {
	case VRegOperand:
		if pr, ok := resolveVReg(v.Reg, phys, widths); ok {
			return P(pr)
		}
	case MemOperand:
		base, isVReg := v.Base.(VRegOperand)
		if !isVReg {
			break
		}
		if pr, ok := resolveVReg(base.Reg, phys, widths); ok {
			return Mem(P(pr), v.Offset)
		}
	}
	return op
}

func resolveVReg(v VReg, phys map[int32]PReg, widths map[int32]RegWidth) (PReg, bool) {
	pr, ok := phys[v.ID()]
	if !ok {
		return PReg{}, false
	}
	w := v.Width()
	if w == WidthUndefined {
		w = widths[v.ID()]
	}
	return NewPReg(pr.ID(), pr.Type(), w), true
}
