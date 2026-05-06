package asm

import "unsafe"

// Relocation records an unresolved forward reference within a RelocObject.
// At Link time, the instruction at InstrIdx in RelocObject.Instrs is patched
// by re-encoding it with the resolved offset via the architecture's Encoder.
type Relocation struct {
	InstrIdx int // index in RelocObject.Instrs
	Offset   int // byte offset of the instruction within Chunk
	Label    int // target label ID to resolve
}

// RelocObject is the output of Assembler.Compile: encoded machine code plus
// the physical instruction list, exported labels, and unresolved references.
// Sig is kept so the caller can construct a Caller after Link completes.
type RelocObject struct {
	Chunk  *Chunk
	Sig    *Signature
	Instrs []Instruction // physical instructions after register allocation;
	// unresolved branches retain their LabelOperand in Src2
	Labels map[int]int // labelID → byte offset from Chunk start
	Relocs []Relocation
}

// writeBytes writes src into the executable memory at addr.
// The buffer must be unsealed before calling this.
func writeBytes(addr unsafe.Pointer, src []byte) {
	dst := unsafe.Slice((*byte)(addr), len(src))
	copy(dst, src)
}
