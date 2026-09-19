// Package amd64 is a stub backend confirming the asm public surface is
// arch-portable. Encoder.Encode and every ABI method return
// asm.ErrNotImplemented.
package amd64

import "github.com/siyul-park/minivm/internal/asm"

type arch struct {
	registers asm.RegInfo
	encoder   encoder
	abi       abi
}

var _ asm.Arch = arch{}

// New returns the amd64 assembler architecture placeholder. Operations that
// emit or invoke machine code return asm.ErrNotImplemented.
func New() arch {
	return arch{
		registers: asm.NewRegInfo(16, 16, nil, nil, nil),
		encoder:   encoder{},
		abi:       abi{},
	}
}

// Registers returns the target register set.
func (a arch) Registers() asm.RegInfo { return a.registers }

// Encoder returns the target instruction encoder.
func (a arch) Encoder() asm.Encoder { return a.encoder }

// ABI returns the target call-boundary policy.
func (a arch) ABI() asm.ABI { return a.abi }

// Frame returns nil because amd64 does not implement spilling.
func (a arch) Frame() asm.Frame { return nil }
