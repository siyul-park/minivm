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

// Entry describes an additional callable entry into a RelocObject, with its
// byte offset inside Chunk and the parameter register layout at that boundary.
type Entry struct {
	Offset int
	Params []PReg
}

// RelocObject is the output of Assembler.Compile: encoded machine code plus
// the physical instruction list, exported labels, and unresolved references.
// Sig is kept so the caller can construct a Caller after Link completes.
type RelocObject struct {
	Chunk *Chunk
	Sig   *Signature
	// Entries maps callable entry labels to their offset and params.
	Entries map[int]Entry
	// Instrs holds the post-register-allocation instruction list.
	// Unresolved cross-block branches retain their LabelOperand in Src2
	// so that Link can re-encode them once the target address is known.
	Instrs []Instruction
	Relocs []Relocation
}

// writeBytes writes src into the executable memory at addr.
// The buffer must be unsealed before calling this.
func writeBytes(addr unsafe.Pointer, src []byte) {
	dst := unsafe.Slice((*byte)(addr), len(src))
	copy(dst, src)
}
