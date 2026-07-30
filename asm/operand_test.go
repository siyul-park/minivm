package asm_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
)

func TestV(t *testing.T) {
	reg := asm.NewVReg(1, asm.RegTypeInt, asm.Width64)

	require.Equal(t, asm.VRegOperand{Reg: reg}, asm.V(reg))
	require.Equal(t, "vr1", asm.V(reg).String())
}

func TestP(t *testing.T) {
	reg := asm.NewPReg(1, asm.RegTypeInt, asm.Width64)

	require.Equal(t, asm.PRegOperand{Reg: reg}, asm.P(reg))
	require.Equal(t, "x1", asm.P(reg).String())
}

func TestImm(t *testing.T) {
	require.Equal(t, asm.ImmOperand{Value: -1}, asm.Imm(-1))
	require.Equal(t, "#-1", asm.Imm(-1).String())
}

func TestMem(t *testing.T) {
	base := asm.P(asm.NewPReg(1, asm.RegTypeInt, asm.Width64))

	require.Equal(t, asm.MemOperand{Base: base, Offset: 8}, asm.Mem(base, 8))
	require.Equal(t, "[x1]", asm.Mem(base, 0).String())
	require.Equal(t, "[x1, #8]", asm.Mem(base, 8).String())
}

func TestLabelOperand_String(t *testing.T) {
	require.Equal(t, "label1", asm.LabelOperand{ID: 1}.String())
}
