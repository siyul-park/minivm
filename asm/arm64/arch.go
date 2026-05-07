package arm64

import "github.com/siyul-park/minivm/asm"

var Arch = &asm.Arch{
	Registers: asm.NewRegInfo(31, 32, []uint8{FP.ID(), LR.ID()}, nil),
	Encoder:   NewEncoder(),
	ABI:       NewABI(),
	// X9-X15: caller-saved scratch registers, outside the param/return ABI range (X0-X7).
	// Reserve() allocates from this mask in ascending ID order.
	Scratch: asm.NewRegMask([]uint8{9, 10, 11, 12, 13, 14, 15}),
}
