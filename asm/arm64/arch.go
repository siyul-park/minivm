package arm64

import "github.com/siyul-park/minivm/asm"

var Arch = &asm.Arch{
	Registers: asm.NewRegInfo(31, 32, []uint8{FP.ID(), LR.ID()}, nil),
	Encoder:   NewEncoder(),
	ABI:       NewABI(),
	// X10-X15: caller-saved scratch registers, outside the param/return ABI range (X0-X7).
	// X8 and X9 are intentionally excluded so the invoke trampoline can use them
	// as temporaries without conflicting with reserved inputs/outputs.
	Scratch: asm.NewRegMask([]uint8{10, 11, 12, 13, 14, 15}),
}
