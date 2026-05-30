package asm

import "fmt"

// Operand is a parameter slot on an Instruction. Each kind carries its
// payload directly so encoders can pattern-match without further unwrapping.
type Operand interface {
	operand()
	String() string
}

type VRegOperand struct {
	Reg VReg
}

func (VRegOperand) operand() {}

func (o VRegOperand) String() string {
	return o.Reg.String()
}

type PRegOperand struct {
	Reg PReg
}

func (PRegOperand) operand() {}

func (o PRegOperand) String() string {
	return o.Reg.String()
}

func V(r VReg) VRegOperand {
	return VRegOperand{Reg: r}
}

func P(r PReg) PRegOperand {
	return PRegOperand{Reg: r}
}

type ImmOperand struct {
	Value int64
}

func (ImmOperand) operand() {}

func Imm(v int64) ImmOperand {
	return ImmOperand{Value: v}
}

func (o ImmOperand) String() string {
	return fmt.Sprintf("#%d", o.Value)
}

// LabelOperand references a Label by id. Inside the same Code, the offset is
// resolved during Build. References to labels bound in other Codes survive
// in Code.Relocs until Link patches them.
type LabelOperand struct {
	ID Label
}

func (LabelOperand) operand() {}

func (o LabelOperand) String() string {
	return fmt.Sprintf("label%d", o.ID)
}

func LabelOp(id Label) LabelOperand {
	return LabelOperand{ID: id}
}

type MemOperand struct {
	Base   Operand
	Offset int64
}

func (MemOperand) operand() {}

func Mem(base Operand, offset int64) MemOperand {
	return MemOperand{Base: base, Offset: offset}
}

func (o MemOperand) String() string {
	if o.Offset != 0 {
		return fmt.Sprintf("[%s, #%d]", o.Base, o.Offset)
	}
	return fmt.Sprintf("[%s]", o.Base)
}
