package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstruction_Def(t *testing.T) {
	reg := NewVReg(1, RegTypeInt, Width64)

	got, ok := (Instruction{Dst: V(reg)}).Def()
	require.True(t, ok)
	require.Equal(t, reg, got)

	got, ok = (Instruction{Dst: Imm(1)}).Def()
	require.False(t, ok)
	require.Zero(t, got)
}

func TestInstruction_Uses(t *testing.T) {
	a := NewVReg(1, RegTypeInt, Width64)
	b := NewVReg(2, RegTypeInt, Width64)
	c := NewVReg(3, RegTypeInt, Width64)

	inst := Instruction{
		Dst:  Mem(V(a), 8),
		Src1: V(b),
		Src2: Mem(V(c), 16),
		Src3: Imm(1),
	}

	require.Equal(t, []VReg{a, b, c}, inst.Uses())
}

func TestInstruction_String(t *testing.T) {
	reg := NewVReg(1, RegTypeInt, Width64)

	require.Equal(t, "1", (Instruction{Op: 1}).String())
	require.Equal(t, "1 vr1", (Instruction{Op: 1, Dst: V(reg)}).String())
	require.Equal(t, "1 vr1, #2", (Instruction{Op: 1, Dst: V(reg), Src1: Imm(2)}).String())
	require.Equal(t, "1 vr1, #2, #3", (Instruction{Op: 1, Dst: V(reg), Src1: Imm(2), Src2: Imm(3)}).String())
}
