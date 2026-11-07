package arm64

import "github.com/siyul-park/minivm/internal/asm"

// The trampoline reaches State through these; go_asm.h defines each as
// const_<name>.
const (
	stateSP     = asm.OffsetLink
	stateFP     = asm.OffsetLink + 8
	stateLR     = asm.OffsetLink + 16
	statePC     = asm.OffsetPC
	stateNSP    = asm.OffsetNSP
	stateExited = asm.OffsetExited
	stateRegs   = asm.OffsetRegs
	stateFRegs  = asm.OffsetFRegs
)

// Enter runs native code at code on the state's native stack until that
// activation returns or exits. It reports whether the activation is suspended
// at an exit, for Resume, rather than returned.
func (Arch) Enter(code uintptr, s *asm.State) bool {
	enter(code, s)
	return s.Exited()
}

// Resume continues the last suspended activation from its saved native PC and
// native stack pointer, after restoring its saved register file. It reports
// the same as Enter.
func (Arch) Resume(s *asm.State) bool {
	resume(s)
	return s.Exited()
}

// Stub returns the address of the exit stub.
func (Arch) Stub() uintptr {
	return exitPC()
}

func enter(code uintptr, s *asm.State)
func resume(s *asm.State)

// exit is the stub native code branches to when it leaves other than by
// returning. It is native code's to call, never Go's.
func exit()

func exitPC() uintptr
