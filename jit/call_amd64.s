#include "textflag.h"

TEXT ·callMachineCode(SB), NOSPLIT, $0-16
    MOVQ addr+0(FP), AX
    CALL AX
    MOVQ AX, ret+8(FP)
    MOVQ $0, ret1+16(FP)
    RET

