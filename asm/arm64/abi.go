package arm64

import "github.com/siyul-park/minivm/asm"

type abi struct{}

var _ asm.ABI = abi{}

func (abi) NewCaller(sig *asm.Signature, chunk *asm.Chunk) (asm.Caller, error) {
	return NewCaller(sig, chunk)
}

func (abi) MaxParams() int {
	return abiRegs
}

func (abi) MaxReturns() int {
	return abiRegs
}
