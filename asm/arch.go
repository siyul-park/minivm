package asm

type Arch struct {
	Registers *RegInfo
	Encoder   Encoder
	ABI       ABI
}

func (a *Arch) NewCaller(sig *Signature, chunk *Chunk) (Caller, error) {
	return a.ABI.NewCaller(sig, chunk)
}
