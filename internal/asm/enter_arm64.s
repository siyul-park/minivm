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
	ADD	$State_fregs, R26, R16
	FLDPD	0(R16), (F0, F1)
	FLDPD	16(R16), (F2, F3)
	FLDPD	32(R16), (F4, F5)
	FLDPD	48(R16), (F6, F7)
	FLDPD	64(R16), (F8, F9)
	FLDPD	80(R16), (F10, F11)
	FLDPD	96(R16), (F12, F13)
	FLDPD	112(R16), (F14, F15)
	FLDPD	128(R16), (F16, F17)
	FLDPD	144(R16), (F18, F19)
	FLDPD	160(R16), (F20, F21)
	FLDPD	176(R16), (F22, F23)
	FLDPD	192(R16), (F24, F25)
	FLDPD	208(R16), (F26, F27)
	FLDPD	224(R16), (F28, F29)
	FLDPD	240(R16), (F30, F31)
	LDP	(State_regs+0)(R26), (R0, R1)
	LDP	(State_regs+16)(R26), (R2, R3)
	LDP	(State_regs+32)(R26), (R4, R5)
	LDP	(State_regs+48)(R26), (R6, R7)
	LDP	(State_regs+64)(R26), (R8, R9)
	LDP	(State_regs+80)(R26), (R10, R11)
	LDP	(State_regs+96)(R26), (R12, R13)
	LDP	(State_regs+112)(R26), (R14, R15)
	LDP	(State_regs+152)(R26), (R19, R20)
	LDP	(State_regs+168)(R26), (R21, R22)
	LDP	(State_regs+184)(R26), (R23, R24)
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
	STP	(R0, R1), (State_regs+0)(R26)
	STP	(R2, R3), (State_regs+16)(R26)
	STP	(R4, R5), (State_regs+32)(R26)
	STP	(R6, R7), (State_regs+48)(R26)
	STP	(R8, R9), (State_regs+64)(R26)
	STP	(R10, R11), (State_regs+80)(R26)
	STP	(R12, R13), (State_regs+96)(R26)
	STP	(R14, R15), (State_regs+112)(R26)
	STP	(R19, R20), (State_regs+152)(R26)
	STP	(R21, R22), (State_regs+168)(R26)
	STP	(R23, R24), (State_regs+184)(R26)
	MOVD	R25, (State_regs+200)(R26)
	MOVD	R27, (State_regs+216)(R26)
	MOVD	R29, (State_regs+232)(R26)
	ADD	$State_fregs, R26, R16
	FSTPD	(F0, F1), 0(R16)
	FSTPD	(F2, F3), 16(R16)
	FSTPD	(F4, F5), 32(R16)
	FSTPD	(F6, F7), 48(R16)
	FSTPD	(F8, F9), 64(R16)
	FSTPD	(F10, F11), 80(R16)
	FSTPD	(F12, F13), 96(R16)
	FSTPD	(F14, F15), 112(R16)
	FSTPD	(F16, F17), 128(R16)
	FSTPD	(F18, F19), 144(R16)
	FSTPD	(F20, F21), 160(R16)
	FSTPD	(F22, F23), 176(R16)
	FSTPD	(F24, F25), 192(R16)
	FSTPD	(F26, F27), 208(R16)
	FSTPD	(F28, F29), 224(R16)
	FSTPD	(F30, F31), 240(R16)
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
