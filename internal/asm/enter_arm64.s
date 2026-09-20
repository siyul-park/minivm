#include "go_asm.h"
#include "textflag.h"

// The native stack trampoline. R26 holds the *Context for as long as native
// code runs and native code never writes it; R16 and R17 are scratch. Go's
// SP, FP, and LR live in the context from the moment control leaves Go until
// the moment it returns, so nothing below ever touches the goroutine stack.

// func enter(code uintptr, ctx *Context)
TEXT ·enter(SB), NOSPLIT|NOFRAME, $0-16
	MOVD	code+0(FP), R16
	MOVD	ctx+8(FP), R26
	MOVD	RSP, R17
	MOVD	R17, Context_sp(R26)
	MOVD	R29, Context_fp(R26)
	MOVD	R30, Context_lr(R26)
	MOVD	Context_NSP(R26), R17
	MOVD	R17, RSP
	BL	(R16)
	// A native activation returned: its entry SP is the native SP again.
	MOVD	RSP, R17
	MOVD	R17, Context_NSP(R26)
	MOVD	$const_TrapReturn, R17
	MOVD	R17, Context_Trap(R26)
	MOVD	Context_sp(R26), R17
	MOVD	R17, RSP
	MOVD	Context_fp(R26), R29
	MOVD	Context_lr(R26), R30
	RET

// func resume(ctx *Context)
//
// Resuming is the exit stub's call returning: every register the stub saved
// comes back and LR is the resume address, so native code that exits keeps
// its own LR in its frame exactly as it would around any other call.
TEXT ·resume(SB), NOSPLIT|NOFRAME, $0-8
	MOVD	ctx+0(FP), R26
	MOVD	RSP, R17
	MOVD	R17, Context_sp(R26)
	MOVD	R29, Context_fp(R26)
	MOVD	R30, Context_lr(R26)
	MOVD	Context_NSP(R26), R17
	MOVD	R17, RSP
	FMOVD	(Context_Fregs+0)(R26), F0
	FMOVD	(Context_Fregs+8)(R26), F1
	FMOVD	(Context_Fregs+16)(R26), F2
	FMOVD	(Context_Fregs+24)(R26), F3
	FMOVD	(Context_Fregs+32)(R26), F4
	FMOVD	(Context_Fregs+40)(R26), F5
	FMOVD	(Context_Fregs+48)(R26), F6
	FMOVD	(Context_Fregs+56)(R26), F7
	FMOVD	(Context_Fregs+64)(R26), F8
	FMOVD	(Context_Fregs+72)(R26), F9
	FMOVD	(Context_Fregs+80)(R26), F10
	FMOVD	(Context_Fregs+88)(R26), F11
	FMOVD	(Context_Fregs+96)(R26), F12
	FMOVD	(Context_Fregs+104)(R26), F13
	FMOVD	(Context_Fregs+112)(R26), F14
	FMOVD	(Context_Fregs+120)(R26), F15
	FMOVD	(Context_Fregs+128)(R26), F16
	FMOVD	(Context_Fregs+136)(R26), F17
	FMOVD	(Context_Fregs+144)(R26), F18
	FMOVD	(Context_Fregs+152)(R26), F19
	FMOVD	(Context_Fregs+160)(R26), F20
	FMOVD	(Context_Fregs+168)(R26), F21
	FMOVD	(Context_Fregs+176)(R26), F22
	FMOVD	(Context_Fregs+184)(R26), F23
	FMOVD	(Context_Fregs+192)(R26), F24
	FMOVD	(Context_Fregs+200)(R26), F25
	FMOVD	(Context_Fregs+208)(R26), F26
	FMOVD	(Context_Fregs+216)(R26), F27
	FMOVD	(Context_Fregs+224)(R26), F28
	FMOVD	(Context_Fregs+232)(R26), F29
	FMOVD	(Context_Fregs+240)(R26), F30
	FMOVD	(Context_Fregs+248)(R26), F31
	MOVD	(Context_Regs+0)(R26), R0
	MOVD	(Context_Regs+8)(R26), R1
	MOVD	(Context_Regs+16)(R26), R2
	MOVD	(Context_Regs+24)(R26), R3
	MOVD	(Context_Regs+32)(R26), R4
	MOVD	(Context_Regs+40)(R26), R5
	MOVD	(Context_Regs+48)(R26), R6
	MOVD	(Context_Regs+56)(R26), R7
	MOVD	(Context_Regs+64)(R26), R8
	MOVD	(Context_Regs+72)(R26), R9
	MOVD	(Context_Regs+80)(R26), R10
	MOVD	(Context_Regs+88)(R26), R11
	MOVD	(Context_Regs+96)(R26), R12
	MOVD	(Context_Regs+104)(R26), R13
	MOVD	(Context_Regs+112)(R26), R14
	MOVD	(Context_Regs+120)(R26), R15
	MOVD	(Context_Regs+152)(R26), R19
	MOVD	(Context_Regs+160)(R26), R20
	MOVD	(Context_Regs+168)(R26), R21
	MOVD	(Context_Regs+176)(R26), R22
	MOVD	(Context_Regs+184)(R26), R23
	MOVD	(Context_Regs+192)(R26), R24
	MOVD	(Context_Regs+200)(R26), R25
	MOVD	(Context_Regs+216)(R26), R27
	MOVD	(Context_Regs+232)(R26), R29
	MOVD	Context_PC(R26), R30
	RET

// func exit()
//
// Native code arrives here by BLR with Exit and Trap already written, so LR
// is where Resume continues. Returning to Go reuses the SP, FP, and LR the
// most recent enter or resume saved.
TEXT ·exit(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	R0, (Context_Regs+0)(R26)
	MOVD	R1, (Context_Regs+8)(R26)
	MOVD	R2, (Context_Regs+16)(R26)
	MOVD	R3, (Context_Regs+24)(R26)
	MOVD	R4, (Context_Regs+32)(R26)
	MOVD	R5, (Context_Regs+40)(R26)
	MOVD	R6, (Context_Regs+48)(R26)
	MOVD	R7, (Context_Regs+56)(R26)
	MOVD	R8, (Context_Regs+64)(R26)
	MOVD	R9, (Context_Regs+72)(R26)
	MOVD	R10, (Context_Regs+80)(R26)
	MOVD	R11, (Context_Regs+88)(R26)
	MOVD	R12, (Context_Regs+96)(R26)
	MOVD	R13, (Context_Regs+104)(R26)
	MOVD	R14, (Context_Regs+112)(R26)
	MOVD	R15, (Context_Regs+120)(R26)
	MOVD	R19, (Context_Regs+152)(R26)
	MOVD	R20, (Context_Regs+160)(R26)
	MOVD	R21, (Context_Regs+168)(R26)
	MOVD	R22, (Context_Regs+176)(R26)
	MOVD	R23, (Context_Regs+184)(R26)
	MOVD	R24, (Context_Regs+192)(R26)
	MOVD	R25, (Context_Regs+200)(R26)
	MOVD	R27, (Context_Regs+216)(R26)
	MOVD	R29, (Context_Regs+232)(R26)
	FMOVD	F0, (Context_Fregs+0)(R26)
	FMOVD	F1, (Context_Fregs+8)(R26)
	FMOVD	F2, (Context_Fregs+16)(R26)
	FMOVD	F3, (Context_Fregs+24)(R26)
	FMOVD	F4, (Context_Fregs+32)(R26)
	FMOVD	F5, (Context_Fregs+40)(R26)
	FMOVD	F6, (Context_Fregs+48)(R26)
	FMOVD	F7, (Context_Fregs+56)(R26)
	FMOVD	F8, (Context_Fregs+64)(R26)
	FMOVD	F9, (Context_Fregs+72)(R26)
	FMOVD	F10, (Context_Fregs+80)(R26)
	FMOVD	F11, (Context_Fregs+88)(R26)
	FMOVD	F12, (Context_Fregs+96)(R26)
	FMOVD	F13, (Context_Fregs+104)(R26)
	FMOVD	F14, (Context_Fregs+112)(R26)
	FMOVD	F15, (Context_Fregs+120)(R26)
	FMOVD	F16, (Context_Fregs+128)(R26)
	FMOVD	F17, (Context_Fregs+136)(R26)
	FMOVD	F18, (Context_Fregs+144)(R26)
	FMOVD	F19, (Context_Fregs+152)(R26)
	FMOVD	F20, (Context_Fregs+160)(R26)
	FMOVD	F21, (Context_Fregs+168)(R26)
	FMOVD	F22, (Context_Fregs+176)(R26)
	FMOVD	F23, (Context_Fregs+184)(R26)
	FMOVD	F24, (Context_Fregs+192)(R26)
	FMOVD	F25, (Context_Fregs+200)(R26)
	FMOVD	F26, (Context_Fregs+208)(R26)
	FMOVD	F27, (Context_Fregs+216)(R26)
	FMOVD	F28, (Context_Fregs+224)(R26)
	FMOVD	F29, (Context_Fregs+232)(R26)
	FMOVD	F30, (Context_Fregs+240)(R26)
	FMOVD	F31, (Context_Fregs+248)(R26)
	MOVD	R30, Context_PC(R26)
	MOVD	RSP, R16
	MOVD	R16, Context_NSP(R26)
	MOVD	Context_sp(R26), R16
	MOVD	R16, RSP
	MOVD	Context_fp(R26), R29
	MOVD	Context_lr(R26), R30
	RET

// func exitPC() uintptr
TEXT ·exitPC(SB), NOSPLIT, $0-8
	MOVD	$·exit(SB), R0
	MOVD	R0, ret+0(FP)
	RET
