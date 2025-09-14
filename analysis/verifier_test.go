package analysis

import (
	"math"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestVerifier_Verify(t *testing.T) {
	tests := []struct {
		program *program.Program
	}{
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.DROP),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.DUP),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.SWAP),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.BR, 5),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.BR_IF, 5),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 3),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.FN_CONST, 0),
					instr.New(instr.CALL),
				},
				[]types.Value{
					types.NewFunction(
						[]instr.Instruction{
							instr.New(instr.I32_CONST, 1),
						},
					),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.FN_CONST, 0),
					instr.New(instr.CALL),
				},
				[]types.Value{
					types.NewFunction(
						[]instr.Instruction{
							instr.New(instr.I32_CONST, 1),
							instr.New(instr.RETURN),
						},
						types.FunctionWithReturns(types.TypeI32),
					),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.GLOBAL_SET, 0),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.GLOBAL_SET, 0),
					instr.New(instr.GLOBAL_GET, 0),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.FN_CONST, 0),
					instr.New(instr.CALL),
				},
				[]types.Value{
					types.NewFunction(
						[]instr.Instruction{
							instr.New(instr.I32_CONST, 1),
							instr.New(instr.LOCAL_SET, 0),
						},
						types.FunctionWithLocals(types.TypeI32),
					),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.FN_CONST, 0),
					instr.New(instr.CALL),
				},
				[]types.Value{
					types.NewFunction(
						[]instr.Instruction{
							instr.New(instr.I32_CONST, 1),
							instr.New(instr.LOCAL_SET, 0),
							instr.New(instr.LOCAL_GET, 0),
						},
						types.FunctionWithLocals(types.TypeI32),
					),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.FN_CONST, 0),
				},
				[]types.Value{types.NewFunction(nil)},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_ADD),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 5),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_SUB),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 4),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_MUL),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_DIV_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_DIV_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_REM_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_REM_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_SHL),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_SHR_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_SHR_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_XOR),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_AND),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_OR),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_EQ),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_NE),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_LT_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_GT_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_LE_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_GE_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_I64_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_I64_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_F32_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_F32_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_F64_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_F64_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_ADD),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 5),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_SUB),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 4),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_MUL),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_DIV_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_DIV_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_REM_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_REM_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_SHL),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_SHR_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_SHR_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_EQ),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_NE),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_LT_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_GT_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_LE_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_GE_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_I32),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_F32_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_F32_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_F64_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_F64_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.5))),
					instr.New(instr.F32_ADD),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(5.5))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_SUB),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(4.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
					instr.New(instr.F32_MUL),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(10.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_DIV),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
					instr.New(instr.F32_EQ),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
					instr.New(instr.F32_NE),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_LT),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_GT),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_LE),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_GE),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
					instr.New(instr.F32_TO_I32_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
					instr.New(instr.F32_TO_I32_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
					instr.New(instr.F32_TO_I64_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
					instr.New(instr.F32_TO_I64_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
					instr.New(instr.F32_TO_F64),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(1.5)),
					instr.New(instr.F64_CONST, math.Float64bits(2.5)),
					instr.New(instr.F64_ADD),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(5.5)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_SUB),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(4.0)),
					instr.New(instr.F64_CONST, math.Float64bits(3.0)),
					instr.New(instr.F64_MUL),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(10.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_DIV),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(1.0)),
					instr.New(instr.F64_CONST, math.Float64bits(1.0)),
					instr.New(instr.F64_EQ),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_CONST, math.Float64bits(1.0)),
					instr.New(instr.F64_NE),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(1.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_LT),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(3.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_GT),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_LE),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(3.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_GE),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_I32_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_I32_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_I64_S),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_I64_U),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_F32),
				},
				nil,
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.FN_CONST, 0),
					instr.New(instr.GLOBAL_SET, 0),
					instr.New(instr.I32_CONST, 20),
					instr.New(instr.GLOBAL_GET, 0),
					instr.New(instr.CALL),
				},
				[]types.Value{
					types.NewFunction(
						[]instr.Instruction{
							instr.New(instr.LOCAL_GET, 0),
							instr.New(instr.I32_CONST, 2),
							instr.New(instr.I32_LT_S),
							instr.New(instr.BR_IF, 36),
							instr.New(instr.LOCAL_GET, 0),
							instr.New(instr.I32_CONST, 1),
							instr.New(instr.I32_SUB),
							instr.New(instr.GLOBAL_GET, 0),
							instr.New(instr.CALL),
							instr.New(instr.LOCAL_GET, 0),
							instr.New(instr.I32_CONST, 2),
							instr.New(instr.I32_SUB),
							instr.New(instr.GLOBAL_GET, 0),
							instr.New(instr.CALL),
							instr.New(instr.I32_ADD),
							instr.New(instr.RETURN),
							instr.New(instr.LOCAL_GET, 0),
							instr.New(instr.RETURN),
						},
						types.FunctionWithParams(types.TypeI32),
						types.FunctionWithReturns(types.TypeI32),
					),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.FN_CONST, 0),
					instr.New(instr.GLOBAL_SET, 0),
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.GLOBAL_GET, 0),
					instr.New(instr.CALL),
				},
				[]types.Value{
					types.NewFunction(
						[]instr.Instruction{
							instr.New(instr.LOCAL_GET, 0),
							instr.New(instr.I64_CONST, 1),
							instr.New(instr.I64_LE_S),
							instr.New(instr.BR_IF, 28),
							instr.New(instr.LOCAL_GET, 0),
							instr.New(instr.I64_CONST, 1),
							instr.New(instr.I64_SUB),
							instr.New(instr.GLOBAL_GET, 0),
							instr.New(instr.CALL),
							instr.New(instr.LOCAL_GET, 0),
							instr.New(instr.I64_MUL),
							instr.New(instr.RETURN),
							instr.New(instr.I64_CONST, 1),
							instr.New(instr.RETURN),
						},
						types.FunctionWithParams(types.TypeI64),
						types.FunctionWithReturns(types.TypeI64),
					),
				},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.program.String(), func(t *testing.T) {
			v := NewVerifier(tt.program)
			err := v.Verify()
			require.NoError(t, err)
		})
	}
}
