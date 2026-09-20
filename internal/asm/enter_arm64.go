package asm

// Enter runs native code at code on the state's native stack until that
// activation returns or exits. It reports whether the activation is
// suspended at an exit, for Resume, rather than returned.
func Enter(code uintptr, s *State) bool {
	enter(code, s)
	return s.exited != 0
}

// Resume continues the last suspended activation from its saved native PC
// and native stack pointer, after restoring its saved register file. It
// reports the same as Enter.
func Resume(s *State) bool {
	resume(s)
	return s.exited != 0
}

func enter(code uintptr, s *State)
func resume(s *State)

// exit is the stub native code branches to when it leaves other than by
// returning. It is native code's to call, never Go's.
func exit()

func exitPC() uintptr
