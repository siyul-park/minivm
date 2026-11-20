package jit

// enter runs native code at code on the state's native stack until that
// activation returns or exits. The trampoline reads State's fields through
// the go_asm.h offsets (State_link, State_pc, ...).
func enter(code uintptr, s *State)

// resume continues the last suspended activation from its saved native PC and
// native stack pointer, after restoring its saved register file.
func resume(s *State)

// exit is the stub native code branches to when it leaves other than by
// returning. It is native code's to call, never Go's.
func exit()

// exitPC returns exit's address, for State.stub.
func exitPC() uintptr
