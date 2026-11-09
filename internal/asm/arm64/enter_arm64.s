#include "go_asm.h"
#include "textflag.h"

// The native stack trampoline. R26 holds the *State for as long as native
// code runs and native code never writes it; R16 and R17 are scratch. Go's
// SP, FP, and LR live in the state from the moment control leaves Go until
// the moment it returns, so nothing below ever touches the goroutine stack.

// func enter(code uintptr, s *asm.State)
TEXT ·enter(SB), NOSPLIT|NOFRAME, $0-16
	MOVD	code+0(FP), R16
	MOVD	s+8(FP), R26
	MOVD	RSP, R17
	MOVD	R17, const_stateSP(R26)
	MOVD	R29, const_stateFP(R26)
	MOVD	R30, const_stateLR(R26)
	MOVD	const_stateNSP(R26), R17
	MOVD	R17, const_stateBase(R26)
	MOVD	R17, RSP
	BL	(R16)
	// A native activation returned: its entry SP is the native SP again.
	MOVD	RSP, R17
	MOVD	R17, const_stateNSP(R26)
	MOVD	ZR, const_stateExited(R26)
	MOVD	const_stateSP(R26), R17
	MOVD	R17, RSP
	MOVD	const_stateFP(R26), R29
	MOVD	const_stateLR(R26), R30
	RET

// func resume(s *asm.State)
//
// Resuming is the exit stub's call returning: every register the stub saved
// comes back and LR is the resume address, so native code that exits keeps
// its own LR in its frame exactly as it would around any other call.
TEXT ·resume(SB), NOSPLIT|NOFRAME, $0-8
	MOVD	s+0(FP), R26
	MOVD	RSP, R17
	MOVD	R17, const_stateSP(R26)
	MOVD	R29, const_stateFP(R26)
	MOVD	R30, const_stateLR(R26)
	MOVD	const_stateNSP(R26), R17
	MOVD	R17, RSP
	ADD	$const_stateFRegs, R26, R16
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
	LDP	(const_stateRegs+0)(R26), (R0, R1)
	LDP	(const_stateRegs+16)(R26), (R2, R3)
	LDP	(const_stateRegs+32)(R26), (R4, R5)
	LDP	(const_stateRegs+48)(R26), (R6, R7)
	LDP	(const_stateRegs+64)(R26), (R8, R9)
	LDP	(const_stateRegs+80)(R26), (R10, R11)
	LDP	(const_stateRegs+96)(R26), (R12, R13)
	LDP	(const_stateRegs+112)(R26), (R14, R15)
	LDP	(const_stateRegs+152)(R26), (R19, R20)
	LDP	(const_stateRegs+168)(R26), (R21, R22)
	LDP	(const_stateRegs+184)(R26), (R23, R24)
	MOVD	(const_stateRegs+200)(R26), R25
	MOVD	(const_stateRegs+216)(R26), R27
	MOVD	(const_stateRegs+232)(R26), R29
	MOVD	const_statePC(R26), R30
	RET

// func exit()
//
// Native code arrives here by BLR, so LR is where Resume continues; whatever
// it wrote before calling is its own to read back. Returning to Go reuses
// the SP, FP, and LR the most recent enter or resume saved.
TEXT ·exit(SB), NOSPLIT|NOFRAME, $0-0
	STP	(R0, R1), (const_stateRegs+0)(R26)
	STP	(R2, R3), (const_stateRegs+16)(R26)
	STP	(R4, R5), (const_stateRegs+32)(R26)
	STP	(R6, R7), (const_stateRegs+48)(R26)
	STP	(R8, R9), (const_stateRegs+64)(R26)
	STP	(R10, R11), (const_stateRegs+80)(R26)
	STP	(R12, R13), (const_stateRegs+96)(R26)
	STP	(R14, R15), (const_stateRegs+112)(R26)
	STP	(R19, R20), (const_stateRegs+152)(R26)
	STP	(R21, R22), (const_stateRegs+168)(R26)
	STP	(R23, R24), (const_stateRegs+184)(R26)
	MOVD	R25, (const_stateRegs+200)(R26)
	MOVD	R27, (const_stateRegs+216)(R26)
	MOVD	R29, (const_stateRegs+232)(R26)
	ADD	$const_stateFRegs, R26, R16
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
	MOVD	R30, const_statePC(R26)
	MOVD	RSP, R16
	MOVD	R16, const_stateNSP(R26)
	MOVD	$1, R16
	MOVD	R16, const_stateExited(R26)
	MOVD	const_stateSP(R26), R16
	MOVD	R16, RSP
	MOVD	const_stateFP(R26), R29
	MOVD	const_stateLR(R26), R30
	RET

// func exitPC() uintptr
TEXT ·exitPC(SB), NOSPLIT, $0-8
	MOVD	$·exit(SB), R0
	MOVD	R0, ret+0(FP)
	RET
