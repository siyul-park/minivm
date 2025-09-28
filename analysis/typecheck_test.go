package analysis

import (
	"math"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestTypeCheckPass_Run(t *testing.T) {
	tests := []struct {
		program *program.Program
	}{
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.DROP),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.DUP),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.SWAP),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.BR, 5),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
				},
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
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(
					types.NewFunction(
						[]instr.Instruction{
							instr.New(instr.I32_CONST, 1),
						},
					),
				),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(
					types.NewFunction(
						[]instr.Instruction{
							instr.New(instr.I32_CONST, 1),
							instr.New(instr.RETURN),
						},
						types.FunctionWithReturns(types.TypeI32),
					),
				),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.GLOBAL_SET, 0),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.GLOBAL_SET, 0),
					instr.New(instr.GLOBAL_GET, 0),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(
					types.NewFunction(
						[]instr.Instruction{
							instr.New(instr.I32_CONST, 1),
							instr.New(instr.LOCAL_SET, 0),
						},
						types.FunctionWithLocals(types.TypeI32),
					),
				),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(
					types.NewFunction(
						[]instr.Instruction{
							instr.New(instr.I32_CONST, 1),
							instr.New(instr.LOCAL_SET, 0),
							instr.New(instr.LOCAL_GET, 0),
						},
						types.FunctionWithLocals(types.TypeI32),
					),
				),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
				},
				program.WithConstants(types.NewFunction(nil)),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.RTT_CANON, 0),
				},
				program.WithTypes(types.TypeI32),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_ADD),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 5),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_SUB),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 4),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_MUL),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_DIV_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_DIV_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_REM_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_REM_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_SHL),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_SHR_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_SHR_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_XOR),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_AND),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_OR),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_EQ),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_NE),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_LT_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_GT_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_LE_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_GE_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_I64_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_I64_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_F32_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_F32_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_F64_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_F64_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_ADD),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 5),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_SUB),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 4),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_MUL),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_DIV_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_DIV_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_REM_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_REM_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_SHL),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_SHR_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_SHR_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_EQ),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_NE),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_LT_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_GT_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_LE_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_GE_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_I32),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_F32_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_F32_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_F64_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_F64_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.5))),
					instr.New(instr.F32_ADD),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(5.5))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_SUB),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(4.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
					instr.New(instr.F32_MUL),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(10.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_DIV),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
					instr.New(instr.F32_EQ),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
					instr.New(instr.F32_NE),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_LT),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_GT),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_LE),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_GE),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
					instr.New(instr.F32_TO_I32_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
					instr.New(instr.F32_TO_I32_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
					instr.New(instr.F32_TO_I64_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
					instr.New(instr.F32_TO_I64_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
					instr.New(instr.F32_TO_F64),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(1.5)),
					instr.New(instr.F64_CONST, math.Float64bits(2.5)),
					instr.New(instr.F64_ADD),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(5.5)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_SUB),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(4.0)),
					instr.New(instr.F64_CONST, math.Float64bits(3.0)),
					instr.New(instr.F64_MUL),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(10.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_DIV),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(1.0)),
					instr.New(instr.F64_CONST, math.Float64bits(1.0)),
					instr.New(instr.F64_EQ),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_CONST, math.Float64bits(1.0)),
					instr.New(instr.F64_NE),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(1.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_LT),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(3.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_GT),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_LE),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(3.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_GE),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_I32_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_I32_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_I64_S),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_I64_U),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_F32),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.STRING_LEN),
				},
				program.WithConstants(types.String("foo")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.STRING_CONCAT),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.STRING_EQ),
				},
				program.WithConstants(types.String("foo")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.STRING_NE),
				},
				program.WithConstants(types.String("foo")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.STRING_LT),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.STRING_GT),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.STRING_LE),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.STRING_GE),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.RTT_CANON, 0),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.ARRAY_NEW),
				},
				program.WithTypes(types.NewArrayType(types.TypeI32)),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.RTT_CANON, 0),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.ARRAY_NEW),
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.ARRAY_GET),
				},
				program.WithTypes(types.NewArrayType(types.TypeI32)),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.RTT_CANON, 0),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.ARRAY_NEW),
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.ARRAY_SET),
				},
				program.WithTypes(types.NewArrayType(types.TypeI32)),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.GLOBAL_SET, 0),
					instr.New(instr.I32_CONST, 20),
					instr.New(instr.GLOBAL_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(
					types.NewFunction(
						[]instr.Instruction{
							instr.New(instr.LOCAL_GET, 0),
							instr.New(instr.I32_CONST, 2),
							instr.New(instr.I32_LT_S),
							instr.New(instr.BR_IF, 26),
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
				),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.GLOBAL_SET, 0),
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.GLOBAL_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(
					types.NewFunction(
						[]instr.Instruction{
							instr.New(instr.LOCAL_GET, 0),
							instr.New(instr.I64_CONST, 1),
							instr.New(instr.I64_LE_S),
							instr.New(instr.BR_IF, 16),
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
				),
			),
		},
	}

	for _, tt := range tests {
		p := NewTypeCheckPass()

		t.Run(tt.program.String(), func(t *testing.T) {
			b := NewModuleBuilder(tt.program)
			m, err := b.Build()
			require.NoError(t, err)

			err = p.Run(m)
			require.NoError(t, err)
		})
	}
}
