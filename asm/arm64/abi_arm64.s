#include "textflag.h"

// func invoke(addr uintptr, argv *uint64)
// argv layout: [param_num, return_num, param_types..., return_types..., values...]
TEXT ·invoke(SB), NOSPLIT, $32-16
    MOVD argv+8(FP), R9

    MOVD 0(R9), R10
    MOVD 8(R9), R11

    ADD R10, R11, R12
    ADD $2, R12
    LSL $3, R12
    ADD R9, R12

    MOVD R11, R19
    MOVD R12, R20

    CBZ R10, call
    MOVD 0(R12), R0
    SUB $1, R10; CBZ R10, call
    MOVD 8(R12), R1
    SUB $1, R10; CBZ R10, call
    MOVD 16(R12), R2
    SUB $1, R10; CBZ R10, call
    MOVD 24(R12), R3
    SUB $1, R10; CBZ R10, call
    MOVD 32(R12), R4
    SUB $1, R10; CBZ R10, call
    MOVD 40(R12), R5
    SUB $1, R10; CBZ R10, call
    MOVD 48(R12), R6
    SUB $1, R10; CBZ R10, call
    MOVD 56(R12), R7

call:
    MOVD addr+0(FP), R8
    BL (R8)

    MOVD R19, R11
    MOVD R20, R12

    CBZ R11, done
    MOVD R0, 0(R12)
    SUB $1, R11; CBZ R11, done
    MOVD R1, 8(R12)
    SUB $1, R11; CBZ R11, done
    MOVD R2, 16(R12)
    SUB $1, R11; CBZ R11, done
    MOVD R3, 24(R12)
    SUB $1, R11; CBZ R11, done
    MOVD R4, 32(R12)
    SUB $1, R11; CBZ R11, done
    MOVD R5, 40(R12)
    SUB $1, R11; CBZ R11, done
    MOVD R6, 48(R12)
    SUB $1, R11; CBZ R11, done
    MOVD R7, 56(R12)

done:
    RET
