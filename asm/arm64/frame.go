package arm64

import "github.com/siyul-park/minivm/asm"

// frame implements asm.Frame so the shared register allocator can spill to a
// stack-pointer-relative frame. Native code may make framed self-calls, but
// those calls save and restore SP around the branch. The VM garbage collector
// never runs while native code holds the stack.
//
// The load/store and add/subtract-immediate forms used here read register
// field 31 as SP, so they emit against the SP alias rather than SP (same
// id, but SP names the stack-pointer role this context gives field 31).
// Slots are 8 bytes; the reserved area is rounded up to the 16-byte stack
// alignment AArch64 requires for SP-relative access.
type frame struct{}

var _ asm.Frame = frame{}

const (
	spillSlotBytes = 8
	maxFrameAdjust = 4080
)

// Enter reserves the spill area. SP updates stay within ARM64's unshifted
// add/sub immediate range while preserving 16-byte alignment after every step.
func (frame) Enter(slots int) []asm.Instruction {
	if slots <= 0 {
		return nil
	}
	n := frameBytes(slots)
	out := make([]asm.Instruction, 0, (n+maxFrameAdjust-1)/maxFrameAdjust)
	for n > 0 {
		chunk := n
		if chunk > maxFrameAdjust {
			chunk = maxFrameAdjust
		}
		out = append(out, SUBI(SP, SP, uint16(chunk)))
		n -= chunk
	}
	return out
}

// Leave releases the spill area with the same chunking as Enter.
func (frame) Leave(slots int) []asm.Instruction {
	if slots <= 0 {
		return nil
	}
	n := frameBytes(slots)
	out := make([]asm.Instruction, 0, (n+maxFrameAdjust-1)/maxFrameAdjust)
	for n > 0 {
		chunk := n
		if chunk > maxFrameAdjust {
			chunk = maxFrameAdjust
		}
		out = append(out, ADDI(SP, SP, uint16(chunk)))
		n -= chunk
	}
	return out
}

// Store spills reg to slot: STR reg, [SP, #slot*8].
func (frame) Store(slot int, reg asm.PReg) asm.Instruction {
	return STR(spillReg(reg), SP, int16(slot*spillSlotBytes))
}

// Reload fills reg from slot: LDR reg, [SP, #slot*8].
func (frame) Reload(reg asm.PReg, slot int) asm.Instruction {
	return LDR(spillReg(reg), SP, int16(slot*spillSlotBytes))
}

// Returns reports whether op is the native return.
func (frame) Returns(op uint16) bool {
	return Op(op) == OpRET
}

// spillReg widens reg to its 64-bit view so a full slot is stored and
// reloaded regardless of the value's declared width.
func spillReg(reg asm.PReg) asm.PReg {
	return asm.NewPReg(reg.ID(), reg.Type(), asm.Width64)
}

// frameBytes rounds the slot area up to the 16-byte SP alignment.
func frameBytes(slots int) int {
	b := slots * spillSlotBytes
	b = (b + 15) &^ 15
	return b
}
