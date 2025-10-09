package instr

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instr := New(NOP)
		require.Len(t, instr, 1)
		require.Equal(t, byte(NOP), instr[0])
	})
	t.Run("dynamic", func(t *testing.T) {
		instr := New(BR_TABLE, 2, 0, 1, 0)
		require.Len(t, instr, 8)
		require.Equal(t, byte(BR_TABLE), instr[0])
		require.Equal(t, byte(2), instr[1])
		require.Equal(t, uint16(0), *(*uint16)(unsafe.Pointer(&instr[2])))
		require.Equal(t, uint16(1), *(*uint16)(unsafe.Pointer(&instr[4])))
		require.Equal(t, uint16(0), *(*uint16)(unsafe.Pointer(&instr[6])))
	})
}

func TestInstruction_Type(t *testing.T) {
	instr := New(NOP)
	require.Equal(t, types[NOP], instr.Type())
}

func TestInstruction_Opcode(t *testing.T) {
	instr := New(NOP)
	require.Equal(t, NOP, instr.Opcode())
}

func TestInstruction_Operands(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instr := New(NOP)
		require.Empty(t, instr.Operands())
	})
	t.Run("dynamic", func(t *testing.T) {
		instr := New(BR_TABLE, 2, 0, 1, 0)
		require.Equal(t, []uint64{2, 0, 1, 0}, instr.Operands())
	})
}

func TestInstruction_Width(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instr := New(NOP)
		require.Equal(t, 1, instr.Width())
	})
	t.Run("dynamic", func(t *testing.T) {
		instr := New(BR_TABLE, 2, 0, 1, 0)
		require.Equal(t, 8, instr.Width())
	})
}

func TestInstruction_String(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instr := New(NOP)
		require.Equal(t, "nop", instr.String())
	})
	t.Run("dynamic", func(t *testing.T) {
		instr := New(BR_TABLE, 2, 0, 1, 0)
		require.Equal(t, "br_table 0x02 0x0000 0x0001 0x0000", instr.String())
	})
}
