#include "textflag.h"

// func invoke(addr uintptr, ctx uintptr)
//
// ctx is passed to native code in R0. R19-R26 are callee-saved under AAPCS64
// and used by the JIT allocator, so the Go trampoline preserves them.
// Note: R18 is reserved by the Go toolchain; never use it.

TEXT ·invoke(SB), NOSPLIT, $80-16
    STP  (R19, R20),  8(RSP)
    STP  (R21, R22), 24(RSP)
    STP  (R23, R24), 40(RSP)
    STP  (R25, R26), 56(RSP)
    MOVD addr+0(FP), R9
    MOVD ctx+8(FP), R0
    BL   (R9)
    LDP   8(RSP), (R19, R20)
    LDP  24(RSP), (R21, R22)
    LDP  40(RSP), (R23, R24)
    LDP  56(RSP), (R25, R26)
    RET
