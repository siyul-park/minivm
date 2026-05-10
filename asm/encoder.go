package asm

import "errors"

type Encoder interface {
	Encode(inst Instruction) ([]byte, error)
}

var ErrInvalidOperand = errors.New("invalid operand")

func Encode(encoder Encoder, insts []Instruction) ([]byte, error) {
	buf := make([]byte, 0, len(insts)*4)
	for _, inst := range insts {
		b, err := encoder.Encode(inst)
		if err != nil {
			return nil, err
		}
		buf = append(buf, b...)
	}
	return buf, nil
}
