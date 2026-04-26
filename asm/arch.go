package asm

type Arch struct {
	Registers *RegInfo
	Encoder   Encoder
	ABI       ABI
}
