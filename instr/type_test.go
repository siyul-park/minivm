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

		{opcode: BR},
		{opcode: BR_IF},
		{opcode: BR_TABLE},

		{opcode: SELECT},

		{opcode: CALL},
		{opcode: RETURN},

		{opcode: GLOBAL_GET},
		{opcode: GLOBAL_SET},
		{opcode: GLOBAL_TEE},

		{opcode: LOCAL_GET},
		{opcode: LOCAL_SET},
		{opcode: LOCAL_TEE},

		{opcode: CONST_GET},

		{opcode: REF_TEST},
		{opcode: REF_CAST},

		{opcode: REF_EQ},
		{opcode: REF_NE},

		{opcode: I32_CONST},

		{opcode: I32_XOR},
		{opcode: I32_AND},
		{opcode: I32_OR},

		{opcode: I32_ADD},
		{opcode: I32_SUB},
		{opcode: I32_MUL},
		{opcode: I32_DIV_S},
		{opcode: I32_DIV_U},
		{opcode: I32_REM_S},
		{opcode: I32_REM_U},
		{opcode: I32_SHL},
		{opcode: I32_SHR_S},
		{opcode: I32_SHR_U},

		{opcode: I32_EQZ},
		{opcode: I32_EQ},
		{opcode: I32_NE},
		{opcode: I32_LT_S},
		{opcode: I32_LT_U},
		{opcode: I32_GT_S},
		{opcode: I32_GT_U},
		{opcode: I32_LE_S},
		{opcode: I32_LE_U},
		{opcode: I32_GE_S},
		{opcode: I32_GE_U},

		{opcode: I32_TO_I64_S},
		{opcode: I32_TO_I64_U},
		{opcode: I32_TO_F32_S},
		{opcode: I32_TO_F32_U},
		{opcode: I32_TO_F64_S},
		{opcode: I32_TO_F64_U},

		{opcode: I64_CONST},

		{opcode: I64_ADD},
		{opcode: I64_SUB},
		{opcode: I64_MUL},
		{opcode: I64_DIV_S},
		{opcode: I64_DIV_U},
		{opcode: I64_REM_S},
		{opcode: I64_REM_U},
		{opcode: I64_SHL},
		{opcode: I64_SHR_S},
		{opcode: I64_SHR_U},

		{opcode: I64_EQZ},
		{opcode: I64_EQ},
		{opcode: I64_NE},
		{opcode: I64_LT_S},
		{opcode: I64_LT_U},
		{opcode: I64_GT_S},
		{opcode: I64_GT_U},
		{opcode: I64_LE_S},
		{opcode: I64_LE_U},
		{opcode: I64_GE_S},
		{opcode: I64_GE_U},

		{opcode: I64_TO_I32},
		{opcode: I64_TO_F32_S},
		{opcode: I64_TO_F32_U},
		{opcode: I64_TO_F64_S},
		{opcode: I64_TO_F64_U},

		{opcode: F32_CONST},

		{opcode: F32_ADD},
		{opcode: F32_SUB},
		{opcode: F32_MUL},
		{opcode: F32_DIV},

		{opcode: F32_EQ},
		{opcode: F32_NE},
		{opcode: F32_LT},
		{opcode: F32_GT},
		{opcode: F32_LE},
		{opcode: F32_GE},

		{opcode: F32_TO_I32_S},
		{opcode: F32_TO_I32_U},
		{opcode: F32_TO_I64_S},
		{opcode: F32_TO_I64_U},
		{opcode: F32_TO_F64},

		{opcode: F64_CONST},

		{opcode: F64_ADD},
		{opcode: F64_SUB},
		{opcode: F64_MUL},
		{opcode: F64_DIV},

		{opcode: F64_EQ},
		{opcode: F64_NE},
		{opcode: F64_LT},
		{opcode: F64_GT},
		{opcode: F64_LE},
		{opcode: F64_GE},

		{opcode: STRING_NEW_UTF32},

		{opcode: STRING_LEN},
		{opcode: STRING_CONCAT},

		{opcode: STRING_EQ},
		{opcode: STRING_NE},
		{opcode: STRING_LT},
		{opcode: STRING_GT},
		{opcode: STRING_LE},
		{opcode: STRING_GE},

		{opcode: STRING_ENCODE_UTF32},

		{opcode: ARRAY_NEW},
		{opcode: ARRAY_NEW_DEFAULT},

		{opcode: ARRAY_LEN},
		{opcode: ARRAY_GET},
		{opcode: ARRAY_SET},
		{opcode: ARRAY_FILL},
		{opcode: ARRAY_COPY},

		{opcode: STRUCT_NEW},
		{opcode: STRUCT_NEW_DEFAULT},

		{opcode: STRUCT_GET},
		{opcode: STRUCT_SET},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("0x%X", tt.opcode), func(t *testing.T) {
			typ := TypeOf(tt.opcode)
			require.NotZero(t, typ)
		})
	}
}
