#include "textflag.h"

// func invoke(addr uintptr, argv uintptr)
//
// argv layout (header-prefixed; the header is set by caller.Call, never by
// external callers):
//   argv[0]     : N, the scratch count in 0..5
//   argv[1..N]  : scratch slots, loaded into X10..X(9+N) on the way in and
//                 written back on the way out
//
// The count is read from the header so the trampoline carries a variable
// number of scratch registers without a fixed width. ARM64 cannot index
// registers at run time, so the load/store sequences are unrolled with an
// early exit once N registers have been moved.
//
// Note: R18 is reserved by the Go toolchain; never use it.

TEXT ·invoke(SB), NOSPLIT, $0-16
    MOVD argv+8(FP), R8
    MOVD 0(R8), R0          // R0 = N

    CBZ  R0, call
    MOVD  8(R8), R10
    CMP  $1, R0
    BEQ  call
    MOVD 16(R8), R11
    CMP  $2, R0
    BEQ  call
    MOVD 24(R8), R12
    CMP  $3, R0
    BEQ  call
    MOVD 32(R8), R13
    CMP  $4, R0
    BEQ  call
    MOVD 40(R8), R14

call:
    MOVD addr+0(FP), R9
    BL   (R9)

    MOVD argv+8(FP), R8
    MOVD 0(R8), R0          // R0 = N (reloaded; the body may clobber R0)

    CBZ  R0, done
    MOVD R10,  8(R8)
    CMP  $1, R0
    BEQ  done
    MOVD R11, 16(R8)
    CMP  $2, R0
    BEQ  done
    MOVD R12, 24(R8)
    CMP  $3, R0
    BEQ  done
    MOVD R13, 32(R8)
    CMP  $4, R0
    BEQ  done
    MOVD R14, 40(R8)

done:
    RET
