package asm

import "errors"

// Encoder turns one architecture-neutral Instruction into its machine
// encoding. Implementations must be pure: same input → same output.
type Encoder interface {
	Encode(inst Instruction) ([]byte, error)
}

var ErrInvalidOperand = errors.New("invalid operand")
