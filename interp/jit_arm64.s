#include "funcdata.h"
#include "textflag.h"

TEXT ·call(SB), NOSPLIT|NOFRAME, $0-8
    MOVD addr+0(FP), R27
    JMP  (R27)

TEXT ·call_ret1(SB), $0-8
    MOVD addr+0(FP), R27
    JMP  (R27)
