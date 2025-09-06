package instr

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypeOf(t *testing.T) {
	tests := []struct {
		opcode Opcode
	}{
		{opcode: NOP},
		{opcode: UNREACHABLE},
		{opcode: DROP},
		{opcode: DUP},
		{opcode: SWAP},
		{opcode: I32_CONST},
		{opcode: I64_CONST},
		{opcode: F32_CONST},
		{opcode: F64_CONST},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("0x%X", tt.opcode), func(t *testing.T) {
			typ := TypeOf(tt.opcode)
			require.NotZero(t, typ)
		})
	}
}

func TestType_Size(t *testing.T) {
	typ := TypeOf(NOP)
	require.Equal(t, 1, typ.Size())
}
