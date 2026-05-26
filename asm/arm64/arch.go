package arm64

import "github.com/siyul-park/minivm/asm"

var Arch = &asm.Arch{
	// 31 integer registers (X0–X30) + 32 float registers (V0–V31).
	// FP (X29) and LR (X30) are reserved by the Go ABI.
	Registers: asm.NewRegInfo(31, 32, []uint8{FP.ID(), LR.ID(), X15.ID()}, nil),
	Encoder:   NewEncoder(),
	ABI:       abi{},

	// X10–X14: caller-saved scratch registers preserved across the invoke
	// trampoline call. X0–X7 are ABI param/return registers. X8/X9 are
	// invoke temporaries. X15 is reserved for the trampoline header.
	// X16/X17 (IP0/IP1), X18–X21 are invoke internals.
	// X22–X28 are callee-saved and must not be used as scratch here.
	Scratch: asm.NewRegMask([]uint8{10, 11, 12, 13, 14}),
}
