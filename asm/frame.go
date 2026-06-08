package asm

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
