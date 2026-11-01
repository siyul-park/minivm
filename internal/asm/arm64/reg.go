package arm64

import "github.com/siyul-park/minivm/internal/asm"

// ARM64 integer and floating-point register names.
const (
	X0  = asm.Int64 + 0
	X1  = asm.Int64 + 1
	X2  = asm.Int64 + 2
	X3  = asm.Int64 + 3
	X4  = asm.Int64 + 4
	X5  = asm.Int64 + 5
	X6  = asm.Int64 + 6
	X7  = asm.Int64 + 7
	X8  = asm.Int64 + 8
	X9  = asm.Int64 + 9
	X10 = asm.Int64 + 10
	X11 = asm.Int64 + 11
	X12 = asm.Int64 + 12
	X13 = asm.Int64 + 13
	X14 = asm.Int64 + 14
	X15 = asm.Int64 + 15
	X16 = asm.Int64 + 16
	X17 = asm.Int64 + 17
	X18 = asm.Int64 + 18
	X19 = asm.Int64 + 19
	X20 = asm.Int64 + 20
	X21 = asm.Int64 + 21
	X22 = asm.Int64 + 22
	X23 = asm.Int64 + 23
	X24 = asm.Int64 + 24
	X25 = asm.Int64 + 25
	X26 = asm.Int64 + 26
	X27 = asm.Int64 + 27
	X28 = asm.Int64 + 28
	X29 = asm.Int64 + 29 // FP (Frame Pointer)
	X30 = asm.Int64 + 30 // LR (Link Register)

	FP = X29
	LR = X30
	// Ctx holds the *asm.State for as long as native code runs. The
	// trampoline in internal/asm hands it over in this register and reads
	// it back here on every exit, so native code never writes it.
	Ctx = X26
	XZR = asm.Int64 + 31
	// SP shares register id 31 with XZR. In data-processing instructions
	// field 31 reads as the zero register; in add/subtract-immediate and
	// load/store forms it encodes the stack pointer. Use SP where the stack
	// pointer is meant so call sites do not appear to clobber XZR.
	SP = asm.Int64 + 31

	W0  = asm.Int32 + 0
	W1  = asm.Int32 + 1
	W2  = asm.Int32 + 2
	W3  = asm.Int32 + 3
	W4  = asm.Int32 + 4
	W5  = asm.Int32 + 5
	W6  = asm.Int32 + 6
	W7  = asm.Int32 + 7
	W8  = asm.Int32 + 8
	W9  = asm.Int32 + 9
	W10 = asm.Int32 + 10
	W11 = asm.Int32 + 11
	W12 = asm.Int32 + 12
	W13 = asm.Int32 + 13
	W14 = asm.Int32 + 14
	W15 = asm.Int32 + 15
	W16 = asm.Int32 + 16
	W17 = asm.Int32 + 17
	W18 = asm.Int32 + 18
	W19 = asm.Int32 + 19
	W20 = asm.Int32 + 20
	W21 = asm.Int32 + 21
	W22 = asm.Int32 + 22
	W23 = asm.Int32 + 23
	W24 = asm.Int32 + 24
	W25 = asm.Int32 + 25
	W26 = asm.Int32 + 26
	W27 = asm.Int32 + 27
	W28 = asm.Int32 + 28
	W29 = asm.Int32 + 29
	W30 = asm.Int32 + 30
	WZR = asm.Int32 + 31

	D0  = asm.Float64 + 0
	D1  = asm.Float64 + 1
	D2  = asm.Float64 + 2
	D3  = asm.Float64 + 3
	D4  = asm.Float64 + 4
	D5  = asm.Float64 + 5
	D6  = asm.Float64 + 6
	D7  = asm.Float64 + 7
	D8  = asm.Float64 + 8
	D9  = asm.Float64 + 9
	D10 = asm.Float64 + 10
	D11 = asm.Float64 + 11
	D12 = asm.Float64 + 12
	D13 = asm.Float64 + 13
	D14 = asm.Float64 + 14
	D15 = asm.Float64 + 15
	D16 = asm.Float64 + 16
	D17 = asm.Float64 + 17
	D18 = asm.Float64 + 18
	D19 = asm.Float64 + 19
	D20 = asm.Float64 + 20
	D21 = asm.Float64 + 21
	D22 = asm.Float64 + 22
	D23 = asm.Float64 + 23
	D24 = asm.Float64 + 24
	D25 = asm.Float64 + 25
	D26 = asm.Float64 + 26
	D27 = asm.Float64 + 27
	D28 = asm.Float64 + 28
	D29 = asm.Float64 + 29
	D30 = asm.Float64 + 30
	D31 = asm.Float64 + 31

	S0  = asm.Float32 + 0
	S1  = asm.Float32 + 1
	S2  = asm.Float32 + 2
	S3  = asm.Float32 + 3
	S4  = asm.Float32 + 4
	S5  = asm.Float32 + 5
	S6  = asm.Float32 + 6
	S7  = asm.Float32 + 7
	S8  = asm.Float32 + 8
	S9  = asm.Float32 + 9
	S10 = asm.Float32 + 10
	S11 = asm.Float32 + 11
	S12 = asm.Float32 + 12
	S13 = asm.Float32 + 13
	S14 = asm.Float32 + 14
	S15 = asm.Float32 + 15
	S16 = asm.Float32 + 16
	S17 = asm.Float32 + 17
	S18 = asm.Float32 + 18
	S19 = asm.Float32 + 19
	S20 = asm.Float32 + 20
	S21 = asm.Float32 + 21
	S22 = asm.Float32 + 22
	S23 = asm.Float32 + 23
	S24 = asm.Float32 + 24
	S25 = asm.Float32 + 25
	S26 = asm.Float32 + 26
	S27 = asm.Float32 + 27
	S28 = asm.Float32 + 28
	S29 = asm.Float32 + 29
	S30 = asm.Float32 + 30
	S31 = asm.Float32 + 31
)
