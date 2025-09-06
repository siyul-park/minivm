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
		{opcode: I32_ADD},
		{opcode: I32_SUB},
		{opcode: I32_MUL},
		{opcode: I32_DIV_S},
		{opcode: I32_DIV_U},
		{opcode: I32_REM_S},
		{opcode: I32_REM_U},

		{opcode: I64_CONST},
		{opcode: I64_ADD},
		{opcode: I64_SUB},
		{opcode: I64_MUL},
		{opcode: I64_DIV_S},
		{opcode: I64_DIV_U},
		{opcode: I64_REM_S},
		{opcode: I64_REM_U},

		{opcode: F32_CONST},
		{opcode: F32_ADD},
		{opcode: F32_SUB},
		{opcode: F32_MUL},
		{opcode: F32_DIV},

		{opcode: F64_CONST},
		{opcode: F64_ADD},
		{opcode: F64_SUB},
		{opcode: F64_MUL},
		{opcode: F64_DIV},
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
