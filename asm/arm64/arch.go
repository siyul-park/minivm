package arm64

import "github.com/siyul-park/minivm/asm"

func NewArch() *asm.Arch {
	return &asm.Arch{
		Registers: asm.NewRegInfo(31, 32, []uint8{FP.ID(), LR.ID()}, nil),
		Encoder:   NewEncoder(),
		ABI:       NewABI(),
	}
}
