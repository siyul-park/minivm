package asm

// Signature describes the calling convention of a compiled block.
//
// Each PReg carries its physical register ID, type (int/float), and width,
// so no separate type or width slices are needed.
//
// ABI layout (inputs = Params, outputs = Returns):
//
//	inputs:  Params[0], Params[1], …   — physical registers X0/D0, X1/D1, …
//	outputs: Returns[0], Returns[1], … — same registers (different direction)
//
// Reserved registers (Arch.Scratch) live outside the ABI range and carry
// out-of-band metadata (e.g. next interpreter IP).
type Signature struct {
	Reserved []PReg
	Params   []PReg
	Returns  []PReg
}
