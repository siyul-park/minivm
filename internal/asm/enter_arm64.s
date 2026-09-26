#include "go_asm.h"
#include "textflag.h"

// The native stack trampoline. R26 holds the *State for as long as native
// code runs and native code never writes it; R16 and R17 are scratch. Go's
// SP, FP, and LR live in the state from the moment control leaves Go until
// the moment it returns, so nothing below ever touches the goroutine stack.

// func enter(code uintptr, s *State)
TEXT ·enter(SB), NOSPLIT|NOFRAME, $0-16
	MOVD	code+0(FP), R16
	MOVD	s+8(FP), R26
	MOVD	RSP, R17
	MOVD	R17, State_sp(R26)
	MOVD	R29, State_fp(R26)
	MOVD	R30, State_lr(R26)
	MOVD	State_nsp(R26), R17
	MOVD	R17, RSP
	BL	(R16)
	// A native activation returned: its entry SP is the native SP again.
	MOVD	RSP, R17
	MOVD	R17, State_nsp(R26)
	MOVD	ZR, State_exited(R26)
	MOVD	State_sp(R26), R17
	MOVD	R17, RSP
	MOVD	State_fp(R26), R29
	MOVD	State_lr(R26), R30
	RET

// func resume(s *State)
//
// Resuming is the exit stub's call returning: every register the stub saved
// comes back and LR is the resume address, so native code that exits keeps
// its own LR in its frame exactly as it would around any other call.
TEXT ·resume(SB), NOSPLIT|NOFRAME, $0-8
	MOVD	s+0(FP), R26
	MOVD	RSP, R17
	MOVD	R17, State_sp(R26)
	MOVD	R29, State_fp(R26)
	MOVD	R30, State_lr(R26)
	MOVD	State_nsp(R26), R17
	MOVD	R17, RSP
	FMOVD	(State_fregs+0)(R26), F0
	FMOVD	(State_fregs+8)(R26), F1
	FMOVD	(State_fregs+16)(R26), F2
	FMOVD	(State_fregs+24)(R26), F3
	FMOVD	(State_fregs+32)(R26), F4
	FMOVD	(State_fregs+40)(R26), F5
	FMOVD	(State_fregs+48)(R26), F6
	FMOVD	(State_fregs+56)(R26), F7
	FMOVD	(State_fregs+64)(R26), F8
	FMOVD	(State_fregs+72)(R26), F9
	FMOVD	(State_fregs+80)(R26), F10
	FMOVD	(State_fregs+88)(R26), F11
	FMOVD	(State_fregs+96)(R26), F12
	FMOVD	(State_fregs+104)(R26), F13
	FMOVD	(State_fregs+112)(R26), F14
	FMOVD	(State_fregs+120)(R26), F15
	FMOVD	(State_fregs+128)(R26), F16
	FMOVD	(State_fregs+136)(R26), F17
	FMOVD	(State_fregs+144)(R26), F18
	FMOVD	(State_fregs+152)(R26), F19
	FMOVD	(State_fregs+160)(R26), F20
	FMOVD	(State_fregs+168)(R26), F21
	FMOVD	(State_fregs+176)(R26), F22
	FMOVD	(State_fregs+184)(R26), F23
	FMOVD	(State_fregs+192)(R26), F24
	FMOVD	(State_fregs+200)(R26), F25
	FMOVD	(State_fregs+208)(R26), F26
	FMOVD	(State_fregs+216)(R26), F27
	FMOVD	(State_fregs+224)(R26), F28
	FMOVD	(State_fregs+232)(R26), F29
	FMOVD	(State_fregs+240)(R26), F30
	FMOVD	(State_fregs+248)(R26), F31
	MOVD	(State_regs+0)(R26), R0
	MOVD	(State_regs+8)(R26), R1
	MOVD	(State_regs+16)(R26), R2
	MOVD	(State_regs+24)(R26), R3
	MOVD	(State_regs+32)(R26), R4
	MOVD	(State_regs+40)(R26), R5
	MOVD	(State_regs+48)(R26), R6
	MOVD	(State_regs+56)(R26), R7
	MOVD	(State_regs+64)(R26), R8
	MOVD	(State_regs+72)(R26), R9
	MOVD	(State_regs+80)(R26), R10
	MOVD	(State_regs+88)(R26), R11
	MOVD	(State_regs+96)(R26), R12
	MOVD	(State_regs+104)(R26), R13
	MOVD	(State_regs+112)(R26), R14
	MOVD	(State_regs+120)(R26), R15
	MOVD	(State_regs+152)(R26), R19
	MOVD	(State_regs+160)(R26), R20
	MOVD	(State_regs+168)(R26), R21
	MOVD	(State_regs+176)(R26), R22
	MOVD	(State_regs+184)(R26), R23
	MOVD	(State_regs+192)(R26), R24
	MOVD	(State_regs+200)(R26), R25
	MOVD	(State_regs+216)(R26), R27
	MOVD	(State_regs+232)(R26), R29
	MOVD	State_pc(R26), R30
	RET

// func exit()
//
// Native code arrives here by BLR, so LR is where Resume continues; whatever
// it wrote before calling is its own to read back. Returning to Go reuses
// the SP, FP, and LR the most recent enter or resume saved.
TEXT ·exit(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	R0, (State_regs+0)(R26)
	MOVD	R1, (State_regs+8)(R26)
	MOVD	R2, (State_regs+16)(R26)
	MOVD	R3, (State_regs+24)(R26)
	MOVD	R4, (State_regs+32)(R26)
	MOVD	R5, (State_regs+40)(R26)
	MOVD	R6, (State_regs+48)(R26)
	MOVD	R7, (State_regs+56)(R26)
	MOVD	R8, (State_regs+64)(R26)
	MOVD	R9, (State_regs+72)(R26)
	MOVD	R10, (State_regs+80)(R26)
	MOVD	R11, (State_regs+88)(R26)
	MOVD	R12, (State_regs+96)(R26)
	MOVD	R13, (State_regs+104)(R26)
	MOVD	R14, (State_regs+112)(R26)
	MOVD	R15, (State_regs+120)(R26)
	MOVD	R19, (State_regs+152)(R26)
	MOVD	R20, (State_regs+160)(R26)
	MOVD	R21, (State_regs+168)(R26)
	MOVD	R22, (State_regs+176)(R26)
	MOVD	R23, (State_regs+184)(R26)
	MOVD	R24, (State_regs+192)(R26)
	MOVD	R25, (State_regs+200)(R26)
	MOVD	R27, (State_regs+216)(R26)
	MOVD	R29, (State_regs+232)(R26)
	FMOVD	F0, (State_fregs+0)(R26)
	FMOVD	F1, (State_fregs+8)(R26)
	FMOVD	F2, (State_fregs+16)(R26)
	FMOVD	F3, (State_fregs+24)(R26)
	FMOVD	F4, (State_fregs+32)(R26)
	FMOVD	F5, (State_fregs+40)(R26)
	FMOVD	F6, (State_fregs+48)(R26)
	FMOVD	F7, (State_fregs+56)(R26)
	FMOVD	F8, (State_fregs+64)(R26)
	FMOVD	F9, (State_fregs+72)(R26)
	FMOVD	F10, (State_fregs+80)(R26)
	FMOVD	F11, (State_fregs+88)(R26)
	FMOVD	F12, (State_fregs+96)(R26)
	FMOVD	F13, (State_fregs+104)(R26)
	FMOVD	F14, (State_fregs+112)(R26)
	FMOVD	F15, (State_fregs+120)(R26)
	FMOVD	F16, (State_fregs+128)(R26)
	FMOVD	F17, (State_fregs+136)(R26)
	FMOVD	F18, (State_fregs+144)(R26)
	FMOVD	F19, (State_fregs+152)(R26)
	FMOVD	F20, (State_fregs+160)(R26)
	FMOVD	F21, (State_fregs+168)(R26)
	FMOVD	F22, (State_fregs+176)(R26)
	FMOVD	F23, (State_fregs+184)(R26)
	FMOVD	F24, (State_fregs+192)(R26)
	FMOVD	F25, (State_fregs+200)(R26)
	FMOVD	F26, (State_fregs+208)(R26)
	FMOVD	F27, (State_fregs+216)(R26)
	FMOVD	F28, (State_fregs+224)(R26)
	FMOVD	F29, (State_fregs+232)(R26)
	FMOVD	F30, (State_fregs+240)(R26)
	FMOVD	F31, (State_fregs+248)(R26)
	MOVD	R30, State_pc(R26)
	MOVD	RSP, R16
	MOVD	R16, State_nsp(R26)
	MOVD	$1, R16
	MOVD	R16, State_exited(R26)
	MOVD	State_sp(R26), R16
	MOVD	R16, RSP
	MOVD	State_fp(R26), R29
	MOVD	State_lr(R26), R30
	RET

// func exitPC() uintptr
TEXT ·exitPC(SB), NOSPLIT, $0-8
	MOVD	$·exit(SB), R0
	MOVD	R0, ret+0(FP)
	RET
