package asm

import "fmt"

type Operand interface {
	operand()
}

type RegOperand struct {
	Reg Register
}

func (RegOperand) operand() {}

func (o RegOperand) String() string {
	return o.Reg.String()
}

type ImmOperand struct {
	Value int64
}

func (ImmOperand) operand() {}

func (o ImmOperand) String() string {
	return fmt.Sprintf("#%d", o.Value)
}

type MemOperand struct {
	Base   Register
	Offset int64
}

func (MemOperand) operand() {}

func (o MemOperand) String() string {
	if o.Offset != 0 {
		return fmt.Sprintf("[%s, #%d]", o.Base, o.Offset)
	}
	return fmt.Sprintf("[%s]", o.Base)
}
