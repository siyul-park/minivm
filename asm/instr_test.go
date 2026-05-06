package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstruction_String(t *testing.T) {
	r0 := NewPReg(0, RegTypeInt, Width64)
	r1 := NewPReg(1, RegTypeInt, Width64)
	r2 := NewPReg(2, RegTypeInt, Width64)

	tests := []struct {
		inst Instruction
		str  string
	}{
		{
			inst: Instruction{Op: 1, Dst: P(r0), Src1: P(r1), Src2: P(r2)},
			str:  "1 x0, x1, x2",
		},
		{
			inst: Instruction{Op: 2, Dst: P(r0), Src1: P(r1)},
			str:  "2 x0, x1",
		},
		{
			inst: Instruction{Op: 3, Dst: P(r0)},
			str:  "3 x0",
		},
		{
			inst: Instruction{Op: 4},
			str:  "4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.inst.String())
		})
	}
}
