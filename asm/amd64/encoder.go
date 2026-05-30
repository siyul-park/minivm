package amd64

import "github.com/siyul-park/minivm/asm"

type encoder struct{}

func (encoder) Encode(_ asm.Instruction) ([]byte, error) {
	return nil, asm.ErrNotImplemented
}
