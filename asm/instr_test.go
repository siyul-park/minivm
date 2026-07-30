package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstruction_String(t *testing.T) {
	reg := NewVReg(1, RegTypeInt, Width64)

	tests := []struct {
		inst Instruction
		str  string
	}{
		{Instruction{Op: 1}, "1"},
		{Instruction{Op: 1, Dst: V(reg)}, "1 vr1"},
		{Instruction{Op: 1, Src2: Imm(3)}, "1 #3"},
		{Instruction{Op: 1, Dst: V(reg), Src1: Imm(2)}, "1 vr1, #2"},
		{
			Instruction{Op: 1, Dst: V(reg), Src1: Imm(2), Src2: Imm(3), Src3: Imm(4)},
			"1 vr1, #2, #3, #4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.inst.String())
		})
	}
}
