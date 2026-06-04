package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestV(t *testing.T) {
	reg := NewVReg(1, RegTypeInt, Width64)
	require.Equal(t, VRegOperand{Reg: reg}, V(reg))
}

func TestP(t *testing.T) {
	reg := NewPReg(1, RegTypeInt, Width64)
	require.Equal(t, PRegOperand{Reg: reg}, P(reg))
}

func TestImm(t *testing.T) {
	require.Equal(t, ImmOperand{Value: 1}, Imm(1))
}

func TestLabelOp(t *testing.T) {
	require.Equal(t, LabelOperand{ID: 1}, LabelOp(1))
}

func TestMem(t *testing.T) {
	base := P(NewPReg(1, RegTypeInt, Width64))
	require.Equal(t, MemOperand{Base: base, Offset: 8}, Mem(base, 8))
}

func TestVRegOperand_String(t *testing.T) {
	require.Equal(t, "vr1", V(NewVReg(1, RegTypeInt, Width64)).String())
}

func TestPRegOperand_String(t *testing.T) {
	require.Equal(t, "x1", P(NewPReg(1, RegTypeInt, Width64)).String())
}

func TestImmOperand_String(t *testing.T) {
	require.Equal(t, "#-1", Imm(-1).String())
}

func TestLabelOperand_String(t *testing.T) {
	require.Equal(t, "label1", LabelOp(1).String())
}

func TestMemOperand_String(t *testing.T) {
	base := P(NewPReg(1, RegTypeInt, Width64))

	require.Equal(t, "[x1]", Mem(base, 0).String())
	require.Equal(t, "[x1, #8]", Mem(base, 8).String())
}
