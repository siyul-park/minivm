package asm

import "errors"

type Encoder interface {
	Encode(inst Instruction) ([]byte, error)
}

var ErrInvalidOperand = errors.New("invalid operand")
