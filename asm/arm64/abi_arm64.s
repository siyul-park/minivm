#include "textflag.h"

// func invoke(addr uintptr, argv uintptr, rsv uintptr)
//
// Header at argv[0]:
//   bits[7:0]   = nParams
//   bits[15:8]  = nReturns
//   bits[23:16] = nReserved
//   bits[31:24] = paramTypes  (float bitmask)
//   bits[39:32] = returnTypes (float bitmask)
//
// argv: [header, param0, param1, …]   — params in, returns out (same slots)
// rsv:  scratch-register output buffer (nil → skip)
TEXT ·invoke(SB), NOSPLIT, $0-24
    MOVD argv+8(FP), R8
    MOVD 0(R8), R9

    AND $0xFF, R9, R10           // nParams
    UBFX $24, R9, $8, R12       // paramTypes (bits[31:24])
    ADD $8, R8, R14              // values_base

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

    // Decode nReserved from header (argv[0]), then save scratch regs to rsv.
    // Do this before R9 is reloaded as the header below.
    MOVD argv+8(FP), R8
    MOVD 0(R8), R11
    UBFX $16, R11, $8, R11      // R11 = nReserved (bits[23:16])
    CBZ R11, save_returns
    MOVD rsv+16(FP), R12
    CBZ R12, save_returns
    MOVD R9, 0(R12)             // Scratch[0] = X9
    SUB $1, R11; CBZ R11, save_returns
    MOVD R10, 8(R12)            // Scratch[1] = X10

save_returns:
    MOVD argv+8(FP), R8
    MOVD 0(R8), R9
    UBFX $8, R9, $8, R11        // nReturns (bits[15:8])
    UBFX $32, R9, $8, R13       // returnTypes (bits[39:32])
    ADD $8, R8, R14

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
