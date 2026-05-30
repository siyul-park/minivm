// Package amd64 is a stub backend confirming the asm public surface is
// arch-portable. Encoder.Encode and every ABI method return
// asm.ErrNotImplemented.
package amd64

import "github.com/siyul-park/minivm/asm"

// New returns an asm.Arch placeholder for amd64. Every operation that would
// actually need to emit machine code returns asm.ErrNotImplemented.
func New() asm.Arch {
	return arch{
		registers: asm.NewRegInfo(16, 16, nil, nil, nil),
		encoder:   encoder{},
		abi:       abi{},
	}
}

type arch struct {
	registers asm.RegInfo
	encoder   encoder
	abi       abi
}

func (a arch) Registers() asm.RegInfo { return a.registers }
func (a arch) Encoder() asm.Encoder   { return a.encoder }
func (a arch) ABI() asm.ABI           { return a.abi }
