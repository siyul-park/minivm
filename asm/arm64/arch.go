package arm64

import "github.com/siyul-park/minivm/asm"

type arch struct {
	registers asm.RegInfo
	encoder   *Encoder
	abi       abi
}

var _ asm.Arch = arch{}

// New returns an asm.Arch targeting ARM64. The arch's encoder and ABI are
// stateless singletons; allocate once per process.
func New() asm.Arch {
	return arch{
		registers: asm.NewRegInfo(
			31, 32,
			// FP (X29) and LR (X30) are reserved by the Go ABI. X15 is
			// reserved for the trampoline header.
			[]uint8{29, 30, 15},
			nil,
			// X10–X14: caller-saved scratch registers preserved across
			// the invoke trampoline.
			[]uint8{10, 11, 12, 13, 14},
		),
		encoder: NewEncoder(),
		abi:     abi{},
	}
}

func (a arch) Registers() asm.RegInfo { return a.registers }
func (a arch) Encoder() asm.Encoder   { return a.encoder }
func (a arch) ABI() asm.ABI           { return a.abi }
