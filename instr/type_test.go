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
		{opcode: RETURN_CALL},

		{opcode: YIELD},
		{opcode: RESUME},
		{opcode: CORO_DONE},
		{opcode: CORO_VALUE},

		{opcode: GLOBAL_GET},
		{opcode: GLOBAL_SET},
		{opcode: GLOBAL_TEE},

		{opcode: LOCAL_GET},
		{opcode: LOCAL_SET},
		{opcode: LOCAL_TEE},

		{opcode: CONST_GET},

		{opcode: REF_NULL},

		{opcode: REF_TEST},
		{opcode: REF_CAST},

		{opcode: REF_IS_NULL},
		{opcode: REF_EQ},
		{opcode: REF_NE},

		{opcode: I32_CONST},

		{opcode: I32_XOR},
		{opcode: I32_AND},
		{opcode: I32_OR},

		{opcode: I32_CLZ},
		{opcode: I32_CTZ},
		{opcode: I32_POPCNT},
		{opcode: I32_ROTL},
		{opcode: I32_ROTR},

		{opcode: I32_EXTEND8_S},
		{opcode: I32_EXTEND16_S},

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

		{opcode: I32_REINTERPRET_F32},

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

		{opcode: I64_XOR},
		{opcode: I64_AND},
		{opcode: I64_OR},

		{opcode: I64_CLZ},
		{opcode: I64_CTZ},
		{opcode: I64_POPCNT},
		{opcode: I64_ROTL},
		{opcode: I64_ROTR},

		{opcode: I64_EXTEND8_S},
		{opcode: I64_EXTEND16_S},
		{opcode: I64_EXTEND32_S},

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

		{opcode: I64_REINTERPRET_F64},

		{opcode: F32_CONST},

		{opcode: F32_ADD},
		{opcode: F32_SUB},
		{opcode: F32_MUL},
		{opcode: F32_DIV},

		{opcode: F32_ABS},
		{opcode: F32_NEG},
		{opcode: F32_SQRT},
		{opcode: F32_CEIL},
		{opcode: F32_FLOOR},
		{opcode: F32_TRUNC},
		{opcode: F32_NEAREST},
		{opcode: F32_MIN},
		{opcode: F32_MAX},
		{opcode: F32_COPYSIGN},

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

		{opcode: F32_REINTERPRET_I32},

		{opcode: F64_CONST},

		{opcode: F64_ADD},
		{opcode: F64_SUB},
		{opcode: F64_MUL},
		{opcode: F64_DIV},

		{opcode: F64_ABS},
		{opcode: F64_NEG},
		{opcode: F64_SQRT},
		{opcode: F64_CEIL},
		{opcode: F64_FLOOR},
		{opcode: F64_TRUNC},
		{opcode: F64_NEAREST},
		{opcode: F64_MIN},
		{opcode: F64_MAX},
		{opcode: F64_COPYSIGN},

		{opcode: F64_EQ},
		{opcode: F64_NE},
		{opcode: F64_LT},
		{opcode: F64_GT},
		{opcode: F64_LE},
		{opcode: F64_GE},

		{opcode: F64_REINTERPRET_I64},

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
		{opcode: STRING_ITER},

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

		{opcode: MAP_NEW},
		{opcode: MAP_NEW_DEFAULT},

		{opcode: MAP_LEN},
		{opcode: MAP_GET},
		{opcode: MAP_LOOKUP},
		{opcode: MAP_SET},
		{opcode: MAP_DELETE},
		{opcode: MAP_CLEAR},
		{opcode: MAP_KEYS},
		{opcode: MAP_ITER},

		{opcode: REF_NEW},
		{opcode: REF_GET},
		{opcode: REF_SET},

		{opcode: CLOSURE_NEW},

		{opcode: THROW},
		{opcode: ERROR_NEW},
		{opcode: ERROR_GET},
		{opcode: ERROR_CODE},

		{opcode: UPVAL_GET},
		{opcode: UPVAL_SET},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("0x%X", tt.opcode), func(t *testing.T) {
			typ := TypeOf(tt.opcode)
			require.NotZero(t, typ)
		})
	}
}
