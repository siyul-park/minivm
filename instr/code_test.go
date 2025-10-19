package instr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshal(t *testing.T) {
	insts := []Instruction{
		New(I32_CONST, 1),
		New(I32_CONST, 2),
		New(I32_ADD),
	}
	code := Marshal(insts)
	require.Len(t, code, 11)
}

func TestUnmarshal(t *testing.T) {
	insts := []Instruction{
		New(I32_CONST, 1),
		New(I32_CONST, 2),
		New(I32_ADD),
	}
	actual := Unmarshal(Marshal(insts))
	require.Equal(t, insts, actual)
}

func TestDisassemble(t *testing.T) {
	insts := []Instruction{
		New(I32_CONST, 1),
		New(I32_CONST, 2),
		New(I32_ADD),
	}
	assembly := Disassemble(Marshal(insts))
	require.Equal(t, "0000:\ti32.const 0x00000001\n0005:\ti32.const 0x00000002\n0010:\ti32.add\n", assembly)
}
