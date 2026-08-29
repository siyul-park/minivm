package asm_test

import (
	"testing"

	asm "github.com/siyul-park/minivm/internal/asm"
	"github.com/stretchr/testify/require"
)

func TestVirtual(t *testing.T) {
	reg := asm.NewVReg(1, asm.RegTypeInt, asm.Width64)

	require.Equal(t, asm.VRegOperand{Reg: reg}, asm.Virtual(reg))
}

func TestPhysical(t *testing.T) {
	reg := asm.NewPReg(1, asm.RegTypeInt, asm.Width64)

	require.Equal(t, asm.PRegOperand{Reg: reg}, asm.Physical(reg))
}

func TestImm(t *testing.T) {
	require.Equal(t, asm.ImmOperand{Value: -1}, asm.Imm(-1))
}

func TestMem(t *testing.T) {
	base := asm.Physical(asm.NewPReg(1, asm.RegTypeInt, asm.Width64))

	require.Equal(t, asm.MemOperand{Base: base, Offset: 8}, asm.Mem(base, 8))
}

func TestVRegOperand_String(t *testing.T) {
	require.Equal(t, "vr1", asm.Virtual(asm.NewVReg(1, asm.RegTypeInt, asm.Width64)).String())
}

func TestPRegOperand_String(t *testing.T) {
	require.Equal(t, "x1", asm.Physical(asm.NewPReg(1, asm.RegTypeInt, asm.Width64)).String())
}

func TestImmOperand_String(t *testing.T) {
	require.Equal(t, "#-1", asm.Imm(-1).String())
}

func TestLabelOperand_String(t *testing.T) {
	require.Equal(t, "label1", asm.LabelOperand{ID: 1}.String())
}

func TestMemOperand_String(t *testing.T) {
	base := asm.Physical(asm.NewPReg(1, asm.RegTypeInt, asm.Width64))

	require.Equal(t, "[x1]", asm.Mem(base, 0).String())
	require.Equal(t, "[x1, #8]", asm.Mem(base, 8).String())
}
