package jit

// Segment scratch layout shared by lowerers and interpreter adapters.
// Stack values use asm.Signature.Returns; scratch carries VM context and
// control metadata only.
const (
	ScratchStack = iota
	ScratchGlobals
	ScratchBP
	ScratchNext
	ScratchCount
)
