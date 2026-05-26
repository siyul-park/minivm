package asm

// Arch bundles everything an Assembler needs to target a specific architecture.
type Arch struct {
	Registers RegInfo
	Scratch   RegMask
	Encoder   Encoder
	ABI       ABI
}
