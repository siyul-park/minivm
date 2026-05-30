package asm

// Signature describes the ABI of a single callable entry: the physical
// registers used for args (in) and returns (out), plus any scratch
// registers reserved across the boundary.
//
// Signature is pure data. Multi-exit semantics, VM-stack mapping, and other
// caller-side concerns live above this package.
type Signature struct {
	Args    []PReg
	Returns []PReg
	Scratch []PReg
}
