package asm

// Signature describes the calling convention of a compiled block.
//
// Native ABI layout:
//
//	inputs:  [Reserved[0..N-1], Params[0..M-1]]   → X0..XN, X(N+1)..X(N+M)
//	outputs: [Reserved[0..N-1], Returns[0..K-1]]  → X0..XN, X(N+1)..X(N+K)
//
// Reserved slots share the same physical registers in both directions and carry
// metadata (e.g. next interpreter IP).  Width slices are element-wise and have
// the same length as their paired type slices.
type Signature struct {
	Reserved       []RegType
	ReservedWidths []RegWidth
	Params         []RegType
	ParamWidths    []RegWidth
	Returns        []RegType
	ReturnWidths   []RegWidth
}
