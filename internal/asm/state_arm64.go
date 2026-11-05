package asm

// registers is the arm64 save area of the exit protocol. The trampoline keeps
// Go's SP, FP, and LR here while native code runs; the exit stub saves the
// native register file here for Go to read and Resume to restore.
type registers struct {
	sp, fp, lr uintptr
	regs       [32]uint64
	fregs      [32]uint64
}

func (s *registers) cell(r PReg) *uint64 {
	if r.ID() >= uint8(len(s.regs)) {
		panic("asm: invalid register")
	}
	switch r.Type() {
	case RegTypeInt:
		return &s.regs[r.ID()]
	case RegTypeFloat:
		return &s.fregs[r.ID()]
	default:
		panic("asm: invalid register type")
	}
}
