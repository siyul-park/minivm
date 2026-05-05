package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVRegOperand(t *testing.T) {
	vr := NewVReg(0, RegTypeInt, Width64)
	op := V(vr)
	require.Equal(t, "vr0", op.String())
	op.operand() // satisfies the interface
}

func TestPRegOperand(t *testing.T) {
	pr := NewPReg(2, RegTypeInt, Width64)
	op := P(pr)
	require.Equal(t, "x2", op.String())
	op.operand()
}

func TestImmOperand(t *testing.T) {
	op := Imm(42)
	require.Equal(t, "#42", op.String())
	op.operand()
	require.Equal(t, int64(42), op.Value)
}

func TestMemOperand(t *testing.T) {
	vr := NewVReg(1, RegTypeInt, Width64)
	base := V(vr)

	op := Mem(base, 0)
	require.Equal(t, "[vr1]", op.String())
	op.operand()

	op2 := Mem(base, 8)
	require.Equal(t, "[vr1, #8]", op2.String())
}
