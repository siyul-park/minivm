package asm

import "fmt"

// Operand is a parameter slot on an Instruction. Each kind carries its
// payload directly so encoders can pattern-match without further unwrapping.
type Operand interface {
	operand()
	String() string
}

// VRegOperand wraps a virtual register as an operand.
type VRegOperand struct {
	Reg VReg
}

// PRegOperand wraps a physical register as an operand.
type PRegOperand struct {
	Reg PReg
}

// ImmOperand is a signed 64-bit immediate.
type ImmOperand struct {
	Value int64
}

// LabelOperand references a Label by id. Inside the same Code, the offset
// is resolved during Build. References to labels bound in other Codes
// survive in Code.Relocs until Link patches them.
type LabelOperand struct {
	ID Label
}

// MemOperand is a base register plus signed byte displacement.
type MemOperand struct {
	Base   Operand
	Offset int64
}

// V wraps a VReg as an operand.
func V(r VReg) VRegOperand {
	return VRegOperand{Reg: r}
}

// P wraps a PReg as an operand.
func P(r PReg) PRegOperand {
	return PRegOperand{Reg: r}
}

// Imm wraps an immediate value as an operand.
func Imm(v int64) ImmOperand {
	return ImmOperand{Value: v}
}

// LabelOp wraps a label id as an operand.
func LabelOp(id Label) LabelOperand {
	return LabelOperand{ID: id}
}

// Mem wraps a base + offset memory reference as an operand.
func Mem(base Operand, offset int64) MemOperand {
	return MemOperand{Base: base, Offset: offset}
}

func (o VRegOperand) String() string {
	return o.Reg.String()
}

func (o PRegOperand) String() string {
	return o.Reg.String()
}

func (o ImmOperand) String() string {
	return fmt.Sprintf("#%d", o.Value)
}

func (o LabelOperand) String() string {
	return fmt.Sprintf("label%d", o.ID)
}

func (o MemOperand) String() string {
	if o.Offset != 0 {
		return fmt.Sprintf("[%s, #%d]", o.Base, o.Offset)
	}
	return fmt.Sprintf("[%s]", o.Base)
}

func (VRegOperand) operand() {}

func (PRegOperand) operand() {}

func (ImmOperand) operand() {}

func (LabelOperand) operand() {}

func (MemOperand) operand() {}
