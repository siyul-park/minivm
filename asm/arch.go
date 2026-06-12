package asm

import (
	"errors"
	"unsafe"
)

// Arch bundles everything an Assembler needs to target a specific
// architecture. Concrete arches expose a package-level New() Arch factory
// instead of init-time globals.
type Arch interface {
	Registers() RegInfo
	Encoder() Encoder
	ABI() ABI
	Frame() Frame
}

// Encoder turns one architecture-neutral Instruction into its machine
// encoding. Implementations must be pure: same input → same output.
type Encoder interface {
	Encode(inst Instruction) ([]byte, error)
}

// ABI describes a target architecture's call boundary policy. ABI is pure-policy
// — it does not own any executable memory.
type ABI interface {
	// NewCallable binds the raw native entry at addr, returning a Callable.
	// addr must point at executable memory whose lifetime outlives every Call.
	NewCallable(addr unsafe.Pointer) (Callable, error)
}

// Frame is an optional Arch capability that supplies the instructions a
// spilling register allocator injects when the physical register bank is
// exhausted. The allocator reserves a stack spill area at entry, moves
// values between physical registers and spill slots under pressure, and
// releases the area before every return.
//
// An Arch whose Frame method returns nil disables spilling: allocation
// fails with ErrNoRegistersAvailable once the bank is full.
//
// Slot indices are dense and zero-based; the allocator reports the high
// watermark so Enter/Leave can size the area. Each slot holds one 64-bit
// value.
type Frame interface {
	// Enter reserves space for spill slots at callable entry.
	// Returns nil when slots == 0.
	Enter(slots int) []Instruction
	// Leave releases the spill area. Emitted immediately before every
	// instruction Returns reports true for. Returns nil when slots == 0.
	Leave(slots int) []Instruction
	// Store writes reg into spill slot.
	Store(slot int, reg PReg) Instruction
	// Reload reads spill slot into reg.
	Reload(reg PReg, slot int) Instruction
	// Returns reports whether op transfers control out of the callable, so
	// the allocator must restore the stack with Leave before it.
	Returns(op uint16) bool
}

// Callable is a fully linked, directly invokable entry into the executable
// buffer. Implementations are produced by ABI.NewCallable and returned from
// Link.
type Callable interface {
	Call(ctx uintptr) error
}

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOperand = errors.New("invalid operand")
	ErrInvalidArgs    = errors.New("invalid arguments")
)
