package asm

type ABI interface {
	NewCaller(sig *Signature, chunk *Chunk) (Caller, error)
}
