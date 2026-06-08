package asm

import "unsafe"

// ABI describes a target architecture's call boundary policy: which scratch
// registers survive the trampoline and what binds a Code to a Callable.
// ABI is pure-policy — it does not own any executable memory.
type ABI interface {
	// Scratch returns the set of physical registers reserved for
	// pass-through context across the trampoline boundary (X10..X14 on
	// arm64). Order is stable.
	Scratch() []PReg

	// NewCallable binds the raw native entry at addr to sig, returning a
	// Callable. addr must point at executable memory whose lifetime
	// outlives every Call.
	NewCallable(sig Signature, addr unsafe.Pointer) (Callable, error)
}
