#include "textflag.h"

// func flush(start, end uintptr)
//
// Cleans every data-cache line of [start, end) to the point of unification,
// then invalidates every instruction-cache line of it. The stride is the
// architectural minimum line size, 16 bytes, rather than the one CTR_EL0
// reports, because darwin traps CTR_EL0 at EL0 (SIGILL) and a stride below
// the real line size only repeats a maintenance operation on the same line.
// IC IVAU is spelled as a word because the assembler has no mnemonic for it.
TEXT ·flush(SB), NOSPLIT, $0-16
	MOVD	start+0(FP), R0
	MOVD	end+8(FP), R1
	AND	$~15, R0, R2
	MOVD	R2, R3
clean:
	DC	CVAU, R3
	ADD	$16, R3, R3
	CMP	R1, R3
	BLT	clean
	DSB	$0xB
invalidate:
	WORD	$0xD50B7522 // IC IVAU, R2
	ADD	$16, R2, R2
	CMP	R1, R2
	BLT	invalidate
	DSB	$0xB
	ISB	$15
	RET
