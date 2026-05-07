package asm

// Arch bundles everything an Assembler needs to target a specific architecture.
type Arch struct {
	Registers RegInfo
	Scratch   RegMask
	Encoder   Encoder
	ABI       ABI
}

func (a *Arch) NewCaller(sig *Signature, chunk *Chunk) (Caller, error) {
	return a.ABI.NewCaller(sig, chunk)
}
