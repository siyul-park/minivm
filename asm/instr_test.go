package asm_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
)

func TestInstruction_String(t *testing.T) {
	reg := asm.NewVReg(1, asm.RegTypeInt, asm.Width64)

	tests := []struct {
		inst asm.Instruction
		str  string
	}{
		{asm.Instruction{Op: 1}, "1"},
		{asm.Instruction{Op: 1, Dst: asm.V(reg)}, "1 vr1"},
		{asm.Instruction{Op: 1, Src2: asm.Imm(3)}, "1 #3"},
		{asm.Instruction{Op: 1, Dst: asm.V(reg), Src1: asm.Imm(2)}, "1 vr1, #2"},
		{
			asm.Instruction{Op: 1, Dst: asm.V(reg), Src1: asm.Imm(2), Src2: asm.Imm(3), Src3: asm.Imm(4)},
			"1 vr1, #2, #3, #4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.inst.String())
		})
	}
}
