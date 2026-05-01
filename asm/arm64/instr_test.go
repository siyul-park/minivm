//go:build arm64

package arm64

import (
	"testing"

	"github.com/siyul-park/minivm/asm"
	"github.com/stretchr/testify/require"
)

func TestNewInstr(t *testing.T) {
	tests := []struct {
		instr    asm.Instruction
		expected asm.Instruction
	}{
		{
			instr:    ADD(X0, X1, X2),
			expected: asm.Instruction{Op: uint16(OpADD), Dst: asm.P(X0), Src1: asm.P(X1), Src2: asm.P(X2)},
		},
		{
			instr:    ADDI(X0, X1, 42),
			expected: asm.Instruction{Op: uint16(OpADDI), Dst: asm.P(X0), Src1: asm.P(X1), Src2: asm.ImmOperand{Value: 42}},
		},
		{
			instr:    LSL(X0, X1, X2),
			expected: asm.Instruction{Op: uint16(OpLSL), Dst: asm.P(X0), Src1: asm.P(X1), Src2: asm.P(X2)},
		},
		{
			instr:    LSR(X0, X1, X2),
			expected: asm.Instruction{Op: uint16(OpLSR), Dst: asm.P(X0), Src1: asm.P(X1), Src2: asm.P(X2)},
		},
		{
			instr:    CMP(X2, X3),
			expected: asm.Instruction{Op: uint16(OpCMP), Dst: nil, Src1: asm.P(X2), Src2: asm.P(X3)},
		},
		{
			instr:    CMPI(X2, 7),
			expected: asm.Instruction{Op: uint16(OpCMPI), Dst: nil, Src1: asm.P(X2), Src2: asm.ImmOperand{Value: 7}},
		},
		{
			instr:    MOV(X0, X1),
			expected: asm.Instruction{Op: uint16(OpMOV), Dst: asm.P(X0), Src1: asm.P(X1), Src2: nil},
		},
		{
			instr:    MOVZ(X0, 0x1234, 16),
			expected: asm.Instruction{Op: uint16(OpMOVZ), Dst: asm.P(X0), Src1: asm.ImmOperand{Value: 0x1234}, Src2: asm.ImmOperand{Value: 16}},
		},
		{
			instr:    LDR(X0, X1, 32),
			expected: asm.Instruction{Op: uint16(OpLDR), Dst: asm.P(X0), Src1: asm.Mem(asm.P(X1), 32), Src2: nil},
		},
		{
			instr:    STR(X0, X1, 16),
			expected: asm.Instruction{Op: uint16(OpSTR), Dst: asm.Mem(asm.P(X1), 16), Src1: asm.P(X0), Src2: nil},
		},
		{
			instr:    RET(),
			expected: asm.Instruction{Op: uint16(OpRET), Dst: nil, Src1: nil, Src2: nil},
		},
		{
			instr:    B(8),
			expected: asm.Instruction{Op: uint16(OpB), Dst: nil, Src1: nil, Src2: asm.ImmOperand{Value: 8}},
		},
		{
			instr:    BR(X3),
			expected: asm.Instruction{Op: uint16(OpBR), Dst: nil, Src1: asm.P(X3), Src2: nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.instr.String(), func(t *testing.T) {
			require.Equal(t, tt.expected, tt.instr)
		})
	}
}
