package arm64_test

import (
	"github.com/siyul-park/minivm/internal/asm"
	target "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/types"
)

// vr is the n-th machine-local virtual register the Machine allocates for
// its own scratch (m.vreg): the first call after Prologue is -2, decreasing
// by one per further call.
func vr(n int) asm.VReg { return asm.NewVReg(int32(-1-n), asm.RegTypeInt, asm.Width64) }

// retainRows is the row sequence m.retain emits for ref: the shared
// reference-count increment TestMachine_Lower's "retain counts any
// reference up" case already specifies independently. retain's own skip
// label is self-allocated (not Site-provided), so it is the next label
// after Prologue's two (end, entry) in every test here, matching
// TestMachine_Lower's hardcoded 2 for the same reason.
func retainRows(ref asm.VReg) []asm.Instruction {
	rows := []asm.Instruction{target.LSRI(target.X16, ref, 49)}
	rows = append(rows, target.LDI(target.X17, types.Tag(types.KindRef)>>49)...)
	rows = append(rows,
		target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, asm.Label(2)),
		target.SBFX(target.X17, ref, 0, 32),
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetRC)),
		target.LSLI(target.X17, target.X17, 3),
		target.ADD(target.X16, target.X16, target.X17),
		target.LDR(target.X17, target.X16, 0),
		target.ADDI(target.X17, target.X17, 1), target.STR(target.X17, target.X16, 0),
	)
	return rows
}

// releaseRows is the row sequence m.release emits for ref, resuming at
// resume — the shared reference-count decrement TestMachine_Lower's
// "release counts a non-null reference down" case already specifies
// independently.
func releaseRows(ref asm.VReg) []asm.Instruction {
	rows := []asm.Instruction{target.LSRI(target.X16, ref, 49)}
	rows = append(rows, target.LDI(target.X17, types.Tag(types.KindRef)>>49)...)
	rows = append(rows,
		target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, resume),
		target.SBFX(target.X17, ref, 0, 32),
		target.CBZLabel(target.X17, resume),
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetRC)),
		target.LSLI(target.X17, target.X17, 3),
		target.ADD(target.X16, target.X16, target.X17),
		target.LDR(target.X17, target.X16, 0),
		target.CMPI(target.X17, 1), target.BCondLabel(target.OpBLE, exit),
		target.SUBI(target.X17, target.X17, 1), target.STR(target.X17, target.X16, 0),
	)
	return rows
}
