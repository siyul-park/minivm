#include "textflag.h"

// func invoke(addr uintptr, argv uintptr)
//
// argv layout:
//   argv[0]              : header (packed uint64)
//   argv[1..nScratch]    : scratch save slots (X10–X15), in/out
//   argv[1+nScratch+...] : param inputs / return outputs
//
// Header bit layout:
//   bits[ 7: 0] = nParams
//   bits[15: 8] = nReturns
//   bits[23:16] = nScratch
//   bits[31:24] = paramTypes   (float bitmask, 1=float)
//   bits[39:32] = returnTypes  (float bitmask, 1=float)
//   bits[47:40] = paramWidths  (64-bit bitmask, 1=64-bit)
//   bits[55:48] = returnWidths (64-bit bitmask, 1=64-bit)
//
// Register map:
//   R8  = argv base (reloaded after BL)
//   R9  = header / call target
//   R14 = values_base
//   R19 = param/return count
//   R20 = scratch base pointer
//   R21 = scratch count
//   R22 = paramTypes / returnTypes  (shifted right per param)
//   R23 = paramWidths / returnWidths (shifted right per param)
//
// Note: R18 is reserved by the Go toolchain; never use it.
// TBZ with non-constant bit index is unsupported; use AND+CBZ instead.

TEXT ·invoke(SB), NOSPLIT, $0-16
    MOVD argv+8(FP), R8
    MOVD (R8), R9             // R9 = header

    AND  $0xFF, R9, R19       // R19 = nParams
    UBFX $16, R9, $8, R21    // R21 = nScratch
    UBFX $24, R9, $8, R22    // R22 = paramTypes
    UBFX $40, R9, $8, R23    // R23 = paramWidths

    // values_base = argv + (1 + nScratch) * 8
    ADD  $8, R8, R14
    LSL  $3, R21, R10
    ADD  R10, R14, R14

    // ---- Load params into ABI registers ----
    CBZ R19, load_scratch

    AND  $1, R22, R10; CBZ R10, p0_int
    AND  $1, R23, R10; CBZ R10, p0_f32
    FMOVD  0(R14), F0; B p0_done
p0_f32:
    FMOVS  0(R14), F0; B p0_done
p0_int:
    MOVD   0(R14), R0
p0_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, load_scratch

    AND  $1, R22, R10; CBZ R10, p1_int
    AND  $1, R23, R10; CBZ R10, p1_f32
    FMOVD  8(R14), F1; B p1_done
p1_f32:
    FMOVS  8(R14), F1; B p1_done
p1_int:
    MOVD   8(R14), R1
p1_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, load_scratch

    AND  $1, R22, R10; CBZ R10, p2_int
    AND  $1, R23, R10; CBZ R10, p2_f32
    FMOVD 16(R14), F2; B p2_done
p2_f32:
    FMOVS 16(R14), F2; B p2_done
p2_int:
    MOVD  16(R14), R2
p2_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, load_scratch

    AND  $1, R22, R10; CBZ R10, p3_int
    AND  $1, R23, R10; CBZ R10, p3_f32
    FMOVD 24(R14), F3; B p3_done
p3_f32:
    FMOVS 24(R14), F3; B p3_done
p3_int:
    MOVD  24(R14), R3
p3_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, load_scratch

    AND  $1, R22, R10; CBZ R10, p4_int
    AND  $1, R23, R10; CBZ R10, p4_f32
    FMOVD 32(R14), F4; B p4_done
p4_f32:
    FMOVS 32(R14), F4; B p4_done
p4_int:
    MOVD  32(R14), R4
p4_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, load_scratch

    AND  $1, R22, R10; CBZ R10, p5_int
    AND  $1, R23, R10; CBZ R10, p5_f32
    FMOVD 40(R14), F5; B p5_done
p5_f32:
    FMOVS 40(R14), F5; B p5_done
p5_int:
    MOVD  40(R14), R5
p5_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, load_scratch

    AND  $1, R22, R10; CBZ R10, p6_int
    AND  $1, R23, R10; CBZ R10, p6_f32
    FMOVD 48(R14), F6; B p6_done
p6_f32:
    FMOVS 48(R14), F6; B p6_done
p6_int:
    MOVD  48(R14), R6
p6_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, load_scratch

    AND  $1, R22, R10; CBZ R10, p7_int
    AND  $1, R23, R10; CBZ R10, p7_f32
    FMOVD 56(R14), F7; B load_scratch
p7_f32:
    FMOVS 56(R14), F7; B load_scratch
p7_int:
    MOVD  56(R14), R7

    // ---- Load scratch registers (X10–X15) ----
load_scratch:
    ADD  $8, R8, R20
    MOVD R21, R10
    CBZ  R10, call

    MOVD  0(R20), R10; SUB $1, R21; CBZ R21, call
    MOVD  8(R20), R11; SUB $1, R21; CBZ R21, call
    MOVD 16(R20), R12; SUB $1, R21; CBZ R21, call
    MOVD 24(R20), R13; SUB $1, R21; CBZ R21, call
    MOVD 32(R20), R14; SUB $1, R21; CBZ R21, call
    MOVD 40(R20), R15

call:
    MOVD addr+0(FP), R9
    BL   (R9)

    // ---- Save scratch registers ----
    MOVD argv+8(FP), R8
    MOVD (R8), R9
    UBFX $16, R9, $8, R21
    ADD  $8, R8, R20

    CBZ  R21, store
    MOVD R10,  0(R20); SUB $1, R21; CBZ R21, store
    MOVD R11,  8(R20); SUB $1, R21; CBZ R21, store
    MOVD R12, 16(R20); SUB $1, R21; CBZ R21, store
    MOVD R13, 24(R20); SUB $1, R21; CBZ R21, store
    MOVD R14, 32(R20); SUB $1, R21; CBZ R21, store
    MOVD R15, 40(R20)

    // ---- Store return values ----
store:
    MOVD argv+8(FP), R8
    MOVD (R8), R9

    UBFX $8,  R9, $8, R19    // R19 = nReturns
    UBFX $16, R9, $8, R21    // R21 = nScratch
    UBFX $32, R9, $8, R22    // R22 = returnTypes
    UBFX $48, R9, $8, R23    // R23 = returnWidths

    ADD  $8, R8, R14
    LSL  $3, R21, R10
    ADD  R10, R14, R14

    CBZ R19, ret

    AND  $1, R22, R10; CBZ R10, r0_int
    AND  $1, R23, R10; CBZ R10, r0_f32
    FMOVD F0,  0(R14); B r0_done
r0_f32:
    // f32: move bits to integer register first to zero-extend to 64 bits
    FMOVS F0, R10; MOVD R10,  0(R14); B r0_done
r0_int:
    MOVD  R0,  0(R14)
r0_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, ret

    AND  $1, R22, R10; CBZ R10, r1_int
    AND  $1, R23, R10; CBZ R10, r1_f32
    FMOVD F1,  8(R14); B r1_done
r1_f32:
    FMOVS F1, R10; MOVD R10,  8(R14); B r1_done
r1_int:
    MOVD  R1,  8(R14)
r1_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, ret

    AND  $1, R22, R10; CBZ R10, r2_int
    AND  $1, R23, R10; CBZ R10, r2_f32
    FMOVD F2, 16(R14); B r2_done
r2_f32:
    FMOVS F2, R10; MOVD R10, 16(R14); B r2_done
r2_int:
    MOVD  R2, 16(R14)
r2_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, ret

    AND  $1, R22, R10; CBZ R10, r3_int
    AND  $1, R23, R10; CBZ R10, r3_f32
    FMOVD F3, 24(R14); B r3_done
r3_f32:
    FMOVS F3, R10; MOVD R10, 24(R14); B r3_done
r3_int:
    MOVD  R3, 24(R14)
r3_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, ret

    AND  $1, R22, R10; CBZ R10, r4_int
    AND  $1, R23, R10; CBZ R10, r4_f32
    FMOVD F4, 32(R14); B r4_done
r4_f32:
    FMOVS F4, R10; MOVD R10, 32(R14); B r4_done
r4_int:
    MOVD  R4, 32(R14)
r4_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, ret

    AND  $1, R22, R10; CBZ R10, r5_int
    AND  $1, R23, R10; CBZ R10, r5_f32
    FMOVD F5, 40(R14); B r5_done
r5_f32:
    FMOVS F5, R10; MOVD R10, 40(R14); B r5_done
r5_int:
    MOVD  R5, 40(R14)
r5_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, ret

    AND  $1, R22, R10; CBZ R10, r6_int
    AND  $1, R23, R10; CBZ R10, r6_f32
    FMOVD F6, 48(R14); B r6_done
r6_f32:
    FMOVS F6, R10; MOVD R10, 48(R14); B r6_done
r6_int:
    MOVD  R6, 48(R14)
r6_done:
    LSR $1, R22, R22; LSR $1, R23, R23
    SUB $1, R19; CBZ R19, ret

    AND  $1, R22, R10; CBZ R10, r7_int
    AND  $1, R23, R10; CBZ R10, r7_f32
    FMOVD F7, 56(R14); B ret
r7_f32:
    FMOVS F7, R10; MOVD R10, 56(R14); B ret
r7_int:
    MOVD  R7, 56(R14)

ret:
    RET
