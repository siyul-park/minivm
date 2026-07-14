package arm64

import "github.com/siyul-park/minivm/asm"

// frame implements asm.Frame so the shared register allocator can spill to a
// native stack frame. X26 holds its stable base because native self-calls move
// SP while saving their LR and VM frame state. The invoke trampoline preserves
// X26, and arch excludes it from automatic allocation.
//
// The load/store and add/subtract-immediate forms used here read register
// field 31 as SP, so they emit against the SP alias rather than SP (same
// id, but SP names the stack-pointer role this context gives field 31).
// Slots are 8 bytes; the reserved area is rounded up to the 16-byte stack
// alignment AArch64 requires for SP-relative access.
type frame struct{}

const (
	spillSlotBytes = 8
	maxFrameAdjust = 4080
)

var _ asm.Frame = frame{}

// Enter reserves the spill area. SP updates stay within ARM64's unshifted
// add/sub immediate range while preserving 16-byte alignment after every step.
func (frame) Enter(slots int) []asm.Instruction {
	out := frame{}.Resume(slots)
	if len(out) == 0 {
		return nil
	}
	return append(out, ADDI(X26, SP, 0))
}

// Resume reserves the spill area after an intra-code call returned through
// the shared epilogue. It leaves spillBase unchanged so stores and reloads
// around the call keep addressing the outer activation's slots.
func (frame) Resume(slots int) []asm.Instruction {
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

// Store spills reg to slot relative to the stable spill base.
func (frame) Store(slot int, reg asm.PReg) asm.Instruction {
	return STR(spillReg(reg), X26, int16(slot*spillSlotBytes))
}

// Reload fills reg from a slot relative to the stable spill base.
func (frame) Reload(reg asm.PReg, slot int) asm.Instruction {
	return LDR(spillReg(reg), X26, int16(slot*spillSlotBytes))
}

// Returns reports whether op is the native return.
func (frame) Returns(op uint16) bool {
	return Op(op) == OpRET
}

// Calls reports whether op is an ARM64 branch with link.
func (frame) Calls(op uint16) bool {
	return Op(op) == OpBL
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
