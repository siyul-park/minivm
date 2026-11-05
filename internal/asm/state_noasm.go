//go:build !arm64

package asm

// registers is a plain register file: no trampoline fills it here, but code
// that reads or writes saved registers still runs.
type registers struct {
	regs  [32]uint64
	fregs [32]uint64
}
