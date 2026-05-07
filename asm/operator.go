package asm

import "fmt"

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

type LabelOperand struct {
	ID int
}

func (LabelOperand) operand() {}

func (o LabelOperand) String() string {
	return fmt.Sprintf("label%d", o.ID)
}

func Label(id int) LabelOperand {
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
