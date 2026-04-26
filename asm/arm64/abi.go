package arm64

import "github.com/siyul-park/minivm/asm"

type ABI struct{}

var _ asm.ABI = (*ABI)(nil)

func NewABI() *ABI {
	return &ABI{}
}

func (a *ABI) NewCaller(sig *asm.Signature, chunk *asm.Chunk) (asm.Caller, error) {
	return NewCaller(sig, chunk)
}
