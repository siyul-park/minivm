package asm

// registers is the arm64 save area of the exit protocol. The trampoline keeps
// Go's SP, FP, and LR here while native code runs; the exit stub saves the
// native register file, regs and fregs, for Go to read and Resume to restore.
type registers struct {
	sp, fp, lr uintptr
	regs       [32]uint64
	fregs      [32]uint64
}
