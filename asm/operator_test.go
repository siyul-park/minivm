package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestV(t *testing.T) {
	r := NewVReg(0, RegTypeInt, Width64)
	op := V(r)
	require.Equal(t, r, op.Reg)
	require.Equal(t, "vr0", op.String())
}

func TestP(t *testing.T) {
	r := NewPReg(0, RegTypeInt, Width64)
	op := P(r)
	require.Equal(t, r, op.Reg)
	require.Equal(t, "x0", op.String())
}

func TestImm(t *testing.T) {
	op := Imm(42)
	require.Equal(t, int64(42), op.Value)
	require.Equal(t, "#42", op.String())
}

func TestMem(t *testing.T) {
	r := NewPReg(1, RegTypeInt, Width64)
	op := Mem(P(r), 16)
	require.Equal(t, int64(16), op.Offset)
	require.Equal(t, "[x1, #16]", op.String())
}

func TestMem_ZeroOffset(t *testing.T) {
	r := NewPReg(2, RegTypeInt, Width64)
	op := Mem(P(r), 0)
	require.Equal(t, "[x2]", op.String())
}
