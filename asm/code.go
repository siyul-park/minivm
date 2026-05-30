package asm

// Code is the output of Assembler.Build: a fully encoded byte sequence with
// its own label table, the externally unresolved label references that need
// Link to patch, the signature describing how this Code is callable, and
// any additional named entry points (with their own signatures) embedded in
// the byte stream.
//
// Build resolves every label that points inside the same Code. Only
// references to labels bound in other Codes survive in Relocs.
type Code struct {
	Bytes     []byte
	Labels    map[Label]int
	Relocs    []Relocation
	Signature Signature
	Entries   map[Label]Signature
}

// Relocation records an unresolved label reference inside a Code. At Link
// time the instruction at InstrIdx is re-encoded with the resolved offset
// and overwritten at Offset bytes into Bytes.
type Relocation struct {
	InstrIdx int
	Offset   int
	Label    Label
	Inst     Instruction
}
