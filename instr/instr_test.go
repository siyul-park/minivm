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

func TestInstruction_Operand(t *testing.T) {
	t.Run("in range", func(t *testing.T) {
		instr := New(BR, 5)
		require.Equal(t, uint64(5), instr.Operand(0))
	})
	t.Run("out of range", func(t *testing.T) {
		instr := New(BR, 5)
		require.Zero(t, instr.Operand(1))
	})
}

func TestReadU8(t *testing.T) {
	require.Equal(t, 0xAB, ReadU8(0xDEADBEEF000000AB))
	require.Equal(t, 0xFF, ReadU8(0xFFFFFFFFFFFFFFFF))
	require.Equal(t, 0, ReadU8(0))
}

func TestReadI8(t *testing.T) {
	require.Equal(t, -1, ReadI8(uint64(uint8(0xFF))))
	require.Equal(t, -128, ReadI8(uint64(uint8(0x80))))
	require.Equal(t, 127, ReadI8(127))
	require.Equal(t, 0, ReadI8(0))
}

func TestReadU16(t *testing.T) {
	require.Equal(t, 0xCAFE, ReadU16(0xDEADBEEF0000CAFE))
	require.Equal(t, 0xFFFF, ReadU16(0xFFFFFFFFFFFFFFFF))
	require.Equal(t, 0, ReadU16(0))
}

func TestReadI16(t *testing.T) {
	require.Equal(t, -9, ReadI16(uint64(uint16(-9+1<<16))))
	require.Equal(t, -32768, ReadI16(uint64(uint16(0x8000))))
	require.Equal(t, 32767, ReadI16(32767))
}

func TestReadU32(t *testing.T) {
	require.Equal(t, 0xDEADBEEF, ReadU32(0xCAFEBABEDEADBEEF))
	require.Equal(t, 0, ReadU32(0))
}

func TestReadI32(t *testing.T) {
	require.Equal(t, -1, ReadI32(uint64(uint32(0xFFFFFFFF))))
	require.Equal(t, -2147483648, ReadI32(uint64(uint32(0x80000000))))
	require.Equal(t, 2147483647, ReadI32(2147483647))
}

func TestParseU8(t *testing.T) {
	code := []byte{0x00, 0xAB, 0xFF}
	require.Equal(t, 0x00, ParseU8(code, 0))
	require.Equal(t, 0xAB, ParseU8(code, 1))
	require.Equal(t, 0xFF, ParseU8(code, 2))
}

func TestParseI8(t *testing.T) {
	code := []byte{0x00, 0x7F, 0x80, 0xFF}
	require.Equal(t, 0, ParseI8(code, 0))
	require.Equal(t, 127, ParseI8(code, 1))
	require.Equal(t, -128, ParseI8(code, 2))
	require.Equal(t, -1, ParseI8(code, 3))
}

func TestParseU16(t *testing.T) {
	code := []byte{0x34, 0x12, 0xFF, 0xFF}
	require.Equal(t, 0x1234, ParseU16(code, 0))
	require.Equal(t, 0xFFFF, ParseU16(code, 2))
}

func TestParseI16(t *testing.T) {
	instr := New(BR, uint64(uint16(-9+1<<16)))
	require.Equal(t, -9, ParseI16(instr, 1))

	code := []byte{0x00, 0x80, 0xFF, 0x7F}
	require.Equal(t, -32768, ParseI16(code, 0))
	require.Equal(t, 32767, ParseI16(code, 2))
}

func TestParseU32(t *testing.T) {
	code := []byte{0x78, 0x56, 0x34, 0x12, 0xFF, 0xFF, 0xFF, 0xFF}
	require.Equal(t, 0x12345678, ParseU32(code, 0))
	require.Equal(t, int(uint32(0xFFFFFFFF)), ParseU32(code, 4))
}

func TestParseI32(t *testing.T) {
	code := []byte{0x00, 0x00, 0x00, 0x80, 0xFF, 0xFF, 0xFF, 0x7F, 0xFF, 0xFF, 0xFF, 0xFF}
	require.Equal(t, -2147483648, ParseI32(code, 0))
	require.Equal(t, 2147483647, ParseI32(code, 4))
	require.Equal(t, -1, ParseI32(code, 8))
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

func TestInstruction_SetOperand(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instr := New(I32_CONST, 1)
		instr.SetOperand(0, 42)
		require.Equal(t, []uint64{42}, instr.Operands())
	})
	t.Run("dynamic", func(t *testing.T) {
		instr := New(BR_TABLE, 2, 0, 1, 0)
		instr.SetOperand(1, 5)
		require.Equal(t, []uint64{2, 5, 1, 0}, instr.Operands())
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
