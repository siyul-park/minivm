package asm

import "errors"

// Arch bundles everything an Assembler needs to target a specific
// architecture. Concrete arches expose a package-level New() Arch factory
// instead of init-time globals.
type Arch interface {
	Registers() RegInfo
	Encoder() Encoder
	ABI() ABI
	Frame() Frame
}

var ErrNotImplemented = errors.New("not implemented")
