package asm

// Signature describes the scratch registers carried across a callable entry.
//
// Signature is pure data. Multi-exit semantics, VM-stack mapping, and other
// caller-side concerns live above this package.
type Signature struct {
	Scratch []PReg
}
