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

// Frame is the Arch capability Build needs to allocate registers: how the
// target's rows move control, which registers a bank offers, and how a
// register is parked in and recovered from the frame's spill area.
type Frame interface {
	// Flow reports how control leaves inst.
	Flow(inst Instruction) Flow
	// Writes reports, per operand slot (Dst, Src1, Src2, Src3), whether inst
	// writes that operand rather than reads it. A MemOperand is always read,
	// whatever slot it sits in: its base register is a use.
	Writes(inst Instruction) [4]bool
	// Registers lists the allocatable registers of one bank in preference
	// order, each at Width64; Build narrows the width to the value's own.
	Registers(typ RegType) []PReg
	// Spill stores r into spill slot n and Reload loads it back, choosing the
	// access by r's bank and width. Slot n lives at byte offset 8n above SP:
	// the spill area is the lowest part of the frame, and SP does not move
	// between prologue and epilogue.
	Spill(r Reg, slot int) Instruction
	Reload(r Reg, slot int) Instruction
}

// Flow is how control leaves a row.
type Flow uint8

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

const (
	// FlowNext falls through to the next row.
	FlowNext Flow = iota
	// FlowJump goes unconditionally to the Src2 label.
	FlowJump
	// FlowBranch goes to the Src2 label or falls through.
	FlowBranch
	// FlowCall falls through after clobbering every allocatable register.
	FlowCall
	// FlowEnd leaves the rows the allocator can see: a return, an indirect
	// jump, a trap.
	FlowEnd
)

// Stable assembler errors.
var (
	ErrInvalidOperand   = errors.New("invalid operand")
	ErrInvalidArgs      = errors.New("invalid arguments")
	ErrBranchOutOfRange = errors.New("branch offset out of range")
)
