#include "textflag.h"

// func invoke(addr uintptr, argv uintptr)
//
// Header at argv[0]:
//   bits[7:0]   = nParams
//   bits[15:8]  = nReturns
//   bits[23:16] = nReserved  (≤ 6; scratch registers X10–X15 in order)
//   bits[31:24] = paramTypes  (float bitmask)
//   bits[39:32] = returnTypes (float bitmask)
//
// argv layout:
//   argv[0]:              header
//   argv[1..nReserved]:   scratch outputs — written after the call
//   argv[nReserved+1..]:  params in / returns out (max(nParams,nReturns) slots)
//
// Scratch registers are X10–X15. X8 and X9 are free for use as temporaries
// after BL, so no extra stack frame is needed.
TEXT ·invoke(SB), NOSPLIT, $0-16
    MOVD argv+8(FP), R8
    MOVD 0(R8), R9               // header

    // values_base = argv + (1+nReserved)*8
    UBFX $16, R9, $8, R10       // R10 = nReserved
    ADD $8, R8, R14              // R14 = argv+8
    LSL $3, R10, R10             // R10 = nReserved*8
    ADD R10, R14, R14            // R14 = values_base

    AND $0xFF, R9, R10           // R10 = nParams
    UBFX $24, R9, $8, R12       // R12 = paramTypes

    CBZ R10, call
    TBZ $0, R12, 1(PC); FMOVD 0(R14), F0
    TBZ $0, R12, 2(PC); B 2(PC); MOVD 0(R14), R0
    SUB $1, R10; CBZ R10, call

    TBZ $1, R12, 1(PC); FMOVD 8(R14), F1
    TBZ $1, R12, 2(PC); B 2(PC); MOVD 8(R14), R1
    SUB $1, R10; CBZ R10, call

    TBZ $2, R12, 1(PC); FMOVD 16(R14), F2
    TBZ $2, R12, 2(PC); B 2(PC); MOVD 16(R14), R2
    SUB $1, R10; CBZ R10, call

    TBZ $3, R12, 1(PC); FMOVD 24(R14), F3
    TBZ $3, R12, 2(PC); B 2(PC); MOVD 24(R14), R3
    SUB $1, R10; CBZ R10, call

    TBZ $4, R12, 1(PC); FMOVD 32(R14), F4
    TBZ $4, R12, 2(PC); B 2(PC); MOVD 32(R14), R4
    SUB $1, R10; CBZ R10, call

    TBZ $5, R12, 1(PC); FMOVD 40(R14), F5
    TBZ $5, R12, 2(PC); B 2(PC); MOVD 40(R14), R5
    SUB $1, R10; CBZ R10, call

    TBZ $6, R12, 1(PC); FMOVD 48(R14), F6
    TBZ $6, R12, 2(PC); B 2(PC); MOVD 48(R14), R6
    SUB $1, R10; CBZ R10, call

    TBZ $7, R12, 1(PC); FMOVD 56(R14), F7
    TBZ $7, R12, 2(PC); B 2(PC); MOVD 56(R14), R7

call:
    MOVD addr+0(FP), R15
    BL (R15)

    // Save scratch outputs (X10–X15) to argv[1..nReserved].
    // X8 and X9 are not scratch outputs, so R8 and R9 are free as temporaries.
    MOVD argv+8(FP), R8         // R8 = argv
    MOVD 0(R8), R9              // R9 = header
    UBFX $16, R9, $8, R9       // R9 = nReserved
    ADD $8, R8, R8              // R8 = argv+8 (scratch slots base)

    CBZ R9, save_returns
    MOVD R10, 0(R8)
    SUB $1, R9; CBZ R9, save_returns
    MOVD R11, 8(R8)
    SUB $1, R9; CBZ R9, save_returns
    MOVD R12, 16(R8)
    SUB $1, R9; CBZ R9, save_returns
    MOVD R13, 24(R8)
    SUB $1, R9; CBZ R9, save_returns
    MOVD R14, 32(R8)
    SUB $1, R9; CBZ R9, save_returns
    MOVD R15, 40(R8)

save_returns:
    // values_base = argv + (1+nReserved)*8
    MOVD argv+8(FP), R8
    MOVD 0(R8), R9
    UBFX $8, R9, $8, R11        // R11 = nReturns
    UBFX $32, R9, $8, R13       // R13 = returnTypes

    UBFX $16, R9, $8, R10       // R10 = nReserved
    ADD $8, R8, R14              // R14 = argv+8
    LSL $3, R10, R10             // R10 = nReserved*8
    ADD R10, R14, R14            // R14 = values_base

    CBZ R11, return
    TBZ $0, R13, 1(PC); FMOVD F0, 0(R14)
    TBZ $0, R13, 2(PC); B 2(PC); MOVD R0, 0(R14)
    SUB $1, R11; CBZ R11, return

    TBZ $1, R13, 1(PC); FMOVD F1, 8(R14)
    TBZ $1, R13, 2(PC); B 2(PC); MOVD R1, 8(R14)
    SUB $1, R11; CBZ R11, return

    TBZ $2, R13, 1(PC); FMOVD F2, 16(R14)
    TBZ $2, R13, 2(PC); B 2(PC); MOVD R2, 16(R14)
    SUB $1, R11; CBZ R11, return

    TBZ $3, R13, 1(PC); FMOVD F3, 24(R14)
    TBZ $3, R13, 2(PC); B 2(PC); MOVD R3, 24(R14)
    SUB $1, R11; CBZ R11, return

    TBZ $4, R13, 1(PC); FMOVD F4, 32(R14)
    TBZ $4, R13, 2(PC); B 2(PC); MOVD R4, 32(R14)
    SUB $1, R11; CBZ R11, return

    TBZ $5, R13, 1(PC); FMOVD F5, 40(R14)
    TBZ $5, R13, 2(PC); B 2(PC); MOVD R5, 40(R14)
    SUB $1, R11; CBZ R11, return

    TBZ $6, R13, 1(PC); FMOVD F6, 48(R14)
    TBZ $6, R13, 2(PC); B 2(PC); MOVD R6, 48(R14)
    SUB $1, R11; CBZ R11, return

    TBZ $7, R13, 1(PC); FMOVD F7, 56(R14)
    TBZ $7, R13, 2(PC); B 2(PC); MOVD R7, 56(R14)

return:
    RET
