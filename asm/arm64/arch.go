package arm64

import "github.com/siyul-park/minivm/asm"

type arch struct {
	registers asm.RegInfo
	encoder   *Encoder
	abi       abi
	frame     frame
}

var _ asm.Arch = arch{}

// New returns an asm.Arch targeting ARM64. The arch's encoder, ABI, and
// frame are stateless singletons; allocate once per process.
func New() asm.Arch {
	return arch{
		registers: asm.NewRegInfo(
			31, 32,
			// Registers the Go ARM64 runtime owns and the native body must
			// never clobber: X18 (platform), X27 (REGTMP), X28 (g), X29
			// (FP), X30 (LR). The invoke trampoline is NOSPLIT and saves no
			// callee-state, so allocating any of these corrupts the caller.
			// The remaining low registers are volatile under Go's internal
			// ABI and free to use.
			[]uint8{18, 27, 28, 29, 30},
			nil,
			// X10–X14: caller-saved scratch registers preserved across
			// the invoke trampoline.
			// X0–X1: internal native return registers. X15: pinned
			// native call-depth register. They are not trampoline scratch
			// slots, but the JIT pins them, so the allocator must not
			// choose them opportunistically for unrelated live values.
			[]uint8{0, 1, 10, 11, 12, 13, 14, 15},
		),
		encoder: NewEncoder(),
		abi:     abi{},
		frame:   frame{},
	}
}

func (a arch) Registers() asm.RegInfo { return a.registers }
func (a arch) Encoder() asm.Encoder   { return a.encoder }
func (a arch) ABI() asm.ABI           { return a.abi }
func (a arch) Frame() asm.Frame       { return a.frame }
