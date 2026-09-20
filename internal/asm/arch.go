package asm

import "errors"

// Arch bundles everything an Assembler needs to target a specific
// architecture. Concrete arches expose a package-level New() Arch factory
// instead of init-time globals.
type Arch interface {
	Encoder() Encoder
}

// Encoder turns one architecture-neutral Instruction into its machine
// encoding. Implementations must be pure: same input → same output.
type Encoder interface {
	Encode(inst Instruction) ([]byte, error)
}

// Relaxer is an optional Arch capability implemented by architectures that
// can rewrite a branch instruction with an out-of-range immediate
// displacement into an equivalent multi-instruction sequence that fits.
// Build type-asserts Arch for Relaxer and, when present, runs a fixpoint
// pass over label branches before final encoding.
type Relaxer interface {
	// Relax inspects a PC-relative label-branch instruction and its
	// resolved byte displacement (target - instruction address). It
	// returns a replacement instruction sequence when disp does not fit
	// the immediate field encoded by inst, and false when inst is not a
	// label branch or the displacement already fits.
	Relax(inst Instruction, disp int64) ([]Instruction, bool)
}

var (
	ErrInvalidOperand   = errors.New("invalid operand")
	ErrInvalidArgs      = errors.New("invalid arguments")
	ErrBranchOutOfRange = errors.New("branch offset out of range")
)
