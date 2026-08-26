#include "textflag.h"

// func invoke(addr uintptr, ctx unsafe.Pointer)
//
// ctx is passed to native code in R0. R19-R26 are callee-saved under AAPCS64
// and used by the JIT allocator, so the Go trampoline preserves them.
// The frame also reserves 4096 spill bytes plus 4096 bytes for generated calls.
// Native code starts at the top of that reserve and may move SP downward
// without crossing the Go stack frame or bypassing Go's stack-growth check.
// Note: R18 is reserved by the Go toolchain; never use it.

TEXT ·invoke(SB), $8272-16
    STP  (R19, R20),  8(RSP)
    STP  (R21, R22), 24(RSP)
    STP  (R23, R24), 40(RSP)
    STP  (R25, R26), 56(RSP)
    MOVD addr+0(FP), R9
    MOVD ctx+8(FP), R0
    ADD  $8192, RSP
    ADD  $80, RSP
    BL   (R9)
    SUB  $80, RSP
    SUB  $8192, RSP
    LDP   8(RSP), (R19, R20)
    LDP  24(RSP), (R21, R22)
    LDP  40(RSP), (R23, R24)
    LDP  56(RSP), (R25, R26)
    RET
