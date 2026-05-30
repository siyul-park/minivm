package asm

// Label identifies a position in the emitted instruction stream. Labels are
// allocated via Assembler.Label and bound via Assembler.Bind. Cross-Code
// references remain unresolved inside Code.Relocs until Link supplies their
// target addresses.
type Label int
