package arm64

import "github.com/siyul-park/minivm/internal/asm"

// SpillBytes is the byte width of invoke's spill reserve: one 64-bit value
// per asm.MaxSpillSlots.
const SpillBytes = asm.MaxSpillSlots * 8

// SaveAreaBytes is the byte width invoke sets aside below its native stack
// reserve. R19-R26 occupy 64 of those bytes as four STP pairs starting at
// offset 8; the width is rounded to 80 to keep SP 16-byte aligned across the
// ADD and SUB that bracket the call.
const SaveAreaBytes = 80

// StackReserve returns the native stack reserve invoke must declare: room
// for asm.MaxSpillSlots spill slots plus callDepth native call-depth
// records of recordBytes each. The caller supplies recordBytes (see
// journal.Shift) rather than this package importing internal/journal,
// keeping architecture encoding free of a dependency on the frame-journal
// layout it has no other reason to know about.
func StackReserve(recordBytes, callDepth int) int {
	return SpillBytes + callDepth*recordBytes
}

// FrameSize returns invoke's total Go TEXT frame size for callDepth: the
// stack reserve plus the callee-saved register save area. abi_arm64.s's
// hand-written TEXT and ADD literals must match StackReserve and FrameSize
// for interp's native call-depth cap; see docs/jit-internals.md.
func FrameSize(recordBytes, callDepth int) int {
	return StackReserve(recordBytes, callDepth) + SaveAreaBytes
}
