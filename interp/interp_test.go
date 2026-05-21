package interp

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type test struct {
	name    string
	program *program.Program
	opts    []func(*option)
	values  []types.Value
	err     error
	before  func(*testing.T, *Interpreter)
	after   func(*testing.T, *Interpreter)
}

var tests = []test{
	// --- stack: NOP, DROP, DUP, SWAP, SELECT ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.NOP),
			},
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.DROP),
			},
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.DUP),
			},
		),
		values: []types.Value{types.I32(42), types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.SWAP),
			},
		),
		values: []types.Value{types.I32(1), types.I32(2)},
	},
	// --- control: BR, BR_IF, BR_TABLE ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.BR, 5),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
			},
		),
		values: []types.Value{types.I32(2)},
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
		values: []types.Value{types.I32(3)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_TABLE, 2, 0, 5, 0),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 3),
			},
		),
		values: []types.Value{types.I32(3)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.SELECT),
			},
		),
		values: []types.Value{types.I32(2)},
	},
	// --- call: CONST_GET, CALL, RETURN, host functions ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(nil).Emit(
					instr.New(instr.I32_CONST, 1),
				).Build(),
			),
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				NewHostFunction(
					&types.FunctionType{
						Returns: []types.Type{types.TypeI32},
					},
					func(i *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
						return []types.Boxed{types.BoxI32(1)}, nil
					},
				),
			),
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{
					Returns: []types.Type{types.TypeI32},
				}).Emit(
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.RETURN),
				).Build(),
			),
		),
		values: []types.Value{types.I32(1)},
	},
	// --- globals: GLOBAL_GET, GLOBAL_SET, GLOBAL_TEE ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.GLOBAL_GET, 0),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.GLOBAL_SET, 0),
			},
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.GLOBAL_TEE, 0),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 7),
				instr.New(instr.GLOBAL_TEE, 0),
				instr.New(instr.GLOBAL_GET, 0),
				instr.New(instr.I64_ADD),
			},
		),
		values: []types.Value{types.I64(14)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))),
				instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.GLOBAL_GET, 0),
			},
		),
		values: []types.Value{types.F32(1.5)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(1.5)),
				instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.GLOBAL_GET, 0),
			},
		),
		values: []types.Value{types.F64(1.5)},
	},
	// --- locals: LOCAL_GET, LOCAL_SET, LOCAL_TEE ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(nil).WithLocals(types.TypeI32).Emit(
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.LOCAL_SET, 0),
					instr.New(instr.LOCAL_GET, 0),
				).Build(),
			),
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(nil).WithLocals(types.TypeI32).Emit(
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.LOCAL_SET, 0),
				).Build(),
			),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(nil).WithLocals(types.TypeI32).Emit(
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.LOCAL_TEE, 0),
				).Build(),
			),
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
			},
			program.WithConstants(types.NewFunctionBuilder(nil).Build()),
		),
		values: []types.Value{types.NewFunctionBuilder(nil).Build()},
	},
	// --- refs: REF_NULL, REF_TEST, REF_CAST, REF_IS_NULL, REF_EQ, REF_NE ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.REF_NULL),
			},
		),
		values: []types.Value{types.Null},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.REF_TEST, 0),
			},
			program.WithTypes(types.TypeI32),
		),
		values: []types.Value{types.True},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.REF_CAST, 0),
			},
			program.WithTypes(types.TypeI32),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.REF_IS_NULL),
			},
			program.WithConstants(types.String("foo")),
		),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.REF_EQ),
			},
			program.WithConstants(types.String("foo")),
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.REF_NE),
			},
			program.WithConstants(types.String("foo")),
		),
		values: []types.Value{types.I32(0)},
	},
	// --- i32: I32_CONST, arithmetic, bitwise, comparison, conversions ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
			},
		),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_ADD),
			},
		),
		values: []types.Value{types.I32(3)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_SUB),
			},
		),
		values: []types.Value{types.I32(2)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 4),
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_MUL),
			},
		),
		values: []types.Value{types.I32(12)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 10),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_DIV_S),
			},
		),
		values: []types.Value{types.I32(5)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 10),
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_DIV_U),
			},
		),
		values: []types.Value{types.I32(3)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 10),
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_REM_S),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 10),
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_REM_U),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SHL),
			},
		),
		values: []types.Value{types.I32(2)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SHR_S),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SHR_U),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_XOR),
			},
		),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_AND),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_OR),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_EQZ),
			},
		),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_EQ),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_NE),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_LT_S),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_LT_U),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_GT_S),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_GT_U),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_LE_S),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_LE_U),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_GE_S),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_GE_U),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_TO_I64_S),
			},
		),
		values: []types.Value{types.I64(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_TO_I64_U),
			},
		),
		values: []types.Value{types.I64(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_TO_F32_S),
			},
		),
		values: []types.Value{types.F32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_TO_F32_U),
			},
		),
		values: []types.Value{types.F32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_TO_F64_S),
			},
		),
		values: []types.Value{types.F64(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_TO_F64_U),
			},
		),
		values: []types.Value{types.F64(42)},
	},
	// --- i64: I64_CONST, arithmetic, bitwise, comparison, conversions ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 42),
			},
		),
		values: []types.Value{types.I64(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_ADD),
			},
		),
		values: []types.Value{types.I64(3)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 5),
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_SUB),
			},
		),
		values: []types.Value{types.I64(2)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 4),
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_MUL),
			},
		),
		values: []types.Value{types.I64(12)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 10),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_DIV_S),
			},
		),
		values: []types.Value{types.I64(5)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 10),
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_DIV_U),
			},
		),
		values: []types.Value{types.I64(3)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 10),
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_REM_S),
			},
		),
		values: []types.Value{types.I64(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 10),
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_REM_U),
			},
		),
		values: []types.Value{types.I64(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_SHL),
			},
		),
		values: []types.Value{types.I64(2)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_SHR_S),
			},
		),
		values: []types.Value{types.I64(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_SHR_U),
			},
		),
		values: []types.Value{types.I64(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_EQZ),
			},
		),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_EQ),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_NE),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_LT_S),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_LT_U),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_GT_S),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_GT_U),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_LE_S),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_LE_U),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_GE_S),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_GE_U),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 42),
				instr.New(instr.I64_TO_I32),
			},
		),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 42),
				instr.New(instr.I64_TO_F32_S),
			},
		),
		values: []types.Value{types.F32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 42),
				instr.New(instr.I64_TO_F32_U),
			},
		),
		values: []types.Value{types.F32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 42),
				instr.New(instr.I64_TO_F64_S),
			},
		),
		values: []types.Value{types.F64(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 42),
				instr.New(instr.I64_TO_F64_U),
			},
		),
		values: []types.Value{types.F64(42)},
	},
	// --- f32: F32_CONST, arithmetic, comparison, conversions ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
			},
		),
		values: []types.Value{types.F32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.5))),
				instr.New(instr.F32_ADD),
			},
		),
		values: []types.Value{types.F32(4.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(5.5))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_SUB),
			},
		),
		values: []types.Value{types.F32(3.5)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(4.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
				instr.New(instr.F32_MUL),
			},
		),
		values: []types.Value{types.F32(12.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(10.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_DIV),
			},
		),
		values: []types.Value{types.F32(5.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
				instr.New(instr.F32_EQ),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
				instr.New(instr.F32_NE),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_LT),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_GT),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_LE),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_GE),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				instr.New(instr.F32_TO_I32_S),
			},
		),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				instr.New(instr.F32_TO_I32_U),
			},
		),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				instr.New(instr.F32_TO_I64_S),
			},
		),
		values: []types.Value{types.I64(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				instr.New(instr.F32_TO_I64_U),
			},
		),
		values: []types.Value{types.I64(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				instr.New(instr.F32_TO_F64),
			},
		),
		values: []types.Value{types.F64(42)},
	},
	// --- f64: F64_CONST, arithmetic, comparison, conversions ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(42)),
			},
		),
		values: []types.Value{types.F64(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(1.5)),
				instr.New(instr.F64_CONST, math.Float64bits(2.5)),
				instr.New(instr.F64_ADD),
			},
		),
		values: []types.Value{types.F64(4.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(5.5)),
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_SUB),
			},
		),
		values: []types.Value{types.F64(3.5)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(4.0)),
				instr.New(instr.F64_CONST, math.Float64bits(3.0)),
				instr.New(instr.F64_MUL),
			},
		),
		values: []types.Value{types.F64(12.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(10.0)),
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_DIV),
			},
		),
		values: []types.Value{types.F64(5.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(1.0)),
				instr.New(instr.F64_CONST, math.Float64bits(1.0)),
				instr.New(instr.F64_EQ),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_CONST, math.Float64bits(1.0)),
				instr.New(instr.F64_NE),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(1.0)),
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_LT),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(3.0)),
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_GT),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_LE),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(3.0)),
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_GE),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(42)),
				instr.New(instr.F64_TO_I32_S),
			},
		),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(42)),
				instr.New(instr.F64_TO_I32_U),
			},
		),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(42)),
				instr.New(instr.F64_TO_I64_S),
			},
		),
		values: []types.Value{types.I64(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(42)),
				instr.New(instr.F64_TO_I64_U),
			},
		),
		values: []types.Value{types.I64(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(42)),
				instr.New(instr.F64_TO_F32),
			},
		),
		values: []types.Value{types.F32(42)},
	},
	// --- string: STRING_ENCODE_UTF32, STRING_NEW_UTF32, STRING_LEN, STRING_CONCAT, comparisons ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.STRING_ENCODE_UTF32),
				instr.New(instr.STRING_NEW_UTF32),
			},
			program.WithConstants(types.String("foo")),
		),
		values: []types.Value{types.String("foo")},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.STRING_LEN),
			},
			program.WithConstants(types.String("foo")),
		),
		values: []types.Value{types.I32(3)},
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
		values: []types.Value{types.String("foobar")},
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
		values: []types.Value{types.I32(1)},
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
		values: []types.Value{types.I32(0)},
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
		values: []types.Value{types.I32(0)},
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
		values: []types.Value{types.I32(1)},
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
		values: []types.Value{types.I32(0)},
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
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.STRING_ENCODE_UTF32),
			},
			program.WithConstants(types.String("foo")),
		),
		values: []types.Value{types.I32Array("foo")},
	},
	// --- array: ARRAY_NEW, ARRAY_NEW_DEFAULT, ARRAY_GET, ARRAY_SET, ARRAY_FILL ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeI32)),
		),
		values: []types.Value{types.I32Array{1}},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeI64)),
		),
		values: []types.Value{types.I64Array{1}},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeF32)),
		),
		values: []types.Value{types.F32Array{42}},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(42)),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeF64)),
		),
		values: []types.Value{types.F64Array{42}},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeRef)),
		),
		values: []types.Value{types.NewArray(types.NewArrayType(types.TypeRef), types.BoxI32(1))},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeI32)),
		),
		values: []types.Value{make(types.I32Array, 1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeI64)),
		),
		values: []types.Value{make(types.I64Array, 1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeF32)),
		),
		values: []types.Value{make(types.F32Array, 1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeF64)),
		),
		values: []types.Value{make(types.F64Array, 1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeRef)),
		),
		values: []types.Value{&types.Array{Typ: types.NewArrayType(types.TypeRef), Elems: make([]types.Boxed, 1)}},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.ARRAY_GET),
			},
			program.WithTypes(types.NewArrayType(types.TypeI32)),
		),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.ARRAY_GET),
			},
			program.WithTypes(types.NewArrayType(types.TypeI64)),
		),
		values: []types.Value{types.I64(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.ARRAY_GET),
			},
			program.WithTypes(types.NewArrayType(types.TypeF32)),
		),
		values: []types.Value{types.F32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.ARRAY_GET),
			},
			program.WithTypes(types.NewArrayType(types.TypeF64)),
		),
		values: []types.Value{types.F64(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.ARRAY_GET),
			},
			program.WithTypes(types.NewArrayType(types.TypeRef)),
		),
		values: []types.Value{types.F64(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_SET),
			},
			program.WithTypes(types.NewArrayType(types.TypeI32)),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_SET),
			},
			program.WithTypes(types.NewArrayType(types.TypeI64)),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				instr.New(instr.ARRAY_SET),
			},
			program.WithTypes(types.NewArrayType(types.TypeF32)),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.F64_CONST, math.Float64bits(42)),
				instr.New(instr.ARRAY_SET),
			},
			program.WithTypes(types.NewArrayType(types.TypeF64)),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_SET),
			},
			program.WithTypes(types.NewArrayType(types.TypeRef)),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_FILL),
			},
			program.WithTypes(types.NewArrayType(types.TypeI32)),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_FILL),
			},
			program.WithTypes(types.NewArrayType(types.TypeI64)),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_FILL),
			},
			program.WithTypes(types.NewArrayType(types.TypeF32)),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.F64_CONST, math.Float64bits(42)),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_FILL),
			},
			program.WithTypes(types.NewArrayType(types.TypeF64)),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_FILL),
			},
			program.WithTypes(types.NewArrayType(types.TypeRef)),
		),
		values: nil,
	},
	// --- struct: STRUCT_NEW, STRUCT_NEW_DEFAULT, STRUCT_GET, STRUCT_SET ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.STRUCT_NEW, 0),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32))),
		),
		values: []types.Value{types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32)), types.BoxI32(1))},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.STRUCT_NEW, 0),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI64))),
		),
		values: []types.Value{types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI64)), types.BoxI64(1))},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				instr.New(instr.STRUCT_NEW, 0),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeF32))),
		),
		values: []types.Value{types.NewStruct(types.NewStructType(types.NewStructField(types.TypeF32)), types.BoxF32(42))},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(42)),
				instr.New(instr.STRUCT_NEW, 0),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeF64))),
		),
		values: []types.Value{types.NewStruct(types.NewStructType(types.NewStructField(types.TypeF64)), types.BoxF64(42))},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.STRUCT_NEW, 0),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeRef))),
		),
		values: []types.Value{types.NewStruct(types.NewStructType(types.NewStructField(types.TypeRef)), types.BoxI32(1))},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32))),
		),
		values: []types.Value{types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32)))},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI64))),
		),
		values: []types.Value{types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI64)))},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeF32))),
		),
		values: []types.Value{types.NewStruct(types.NewStructType(types.NewStructField(types.TypeF32)))},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeF64))),
		),
		values: []types.Value{types.NewStruct(types.NewStructType(types.NewStructField(types.TypeF64)))},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeRef))),
		),
		values: []types.Value{types.NewStruct(types.NewStructType(types.NewStructField(types.TypeRef)))},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.STRUCT_GET),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32))),
		),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.STRUCT_GET),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI64))),
		),
		values: []types.Value{types.I64(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.STRUCT_GET),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeF32))),
		),
		values: []types.Value{types.F32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.STRUCT_GET),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeF64))),
		),
		values: []types.Value{types.F64(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.STRUCT_GET),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeRef))),
		),
		values: []types.Value{types.F64(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.STRUCT_SET),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32))),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.STRUCT_SET),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI64))),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				instr.New(instr.STRUCT_SET),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeF32))),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.F64_CONST, math.Float64bits(42)),
				instr.New(instr.STRUCT_SET),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeF64))),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.STRUCT_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.STRUCT_SET),
			},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeRef))),
		),
		values: nil,
	},
	// --- ref: ARRAY_LEN (standalone handler) ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.ARRAY_LEN),
			},
			program.WithTypes(types.NewArrayType(types.TypeI32)),
		),
		values: []types.Value{types.I32(3)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 4),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.ARRAY_LEN),
			},
			program.WithTypes(types.NewArrayType(types.TypeRef)),
		),
		values: []types.Value{types.I32(4)},
	},
	// --- map: MAP_NEW, MAP_NEW_DEFAULT, MAP_LEN, MAP_GET, MAP_LOOKUP, MAP_SET, MAP_DELETE, MAP_CLEAR ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.MAP_LEN),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)),
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 4),
				instr.New(instr.MAP_NEW_DEFAULT, 0),
				instr.New(instr.MAP_LEN),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)),
		),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_GET),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)),
		),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.MAP_GET),
			},
			program.WithConstants(types.String("key"), types.String("key")),
			program.WithTypes(types.NewMapType(types.TypeString, types.TypeI32)),
		),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(math.Copysign(0, -1))))),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(0))),
				instr.New(instr.MAP_GET),
			},
			program.WithTypes(types.NewMapType(types.TypeF32, types.TypeI32)),
		),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_LOOKUP),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)),
		),
		values: []types.Value{types.I32(1), types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 41),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_GET),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)),
		),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.MAP_LOOKUP),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)),
		),
		values: []types.Value{types.I32(0), types.I32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.MAP_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.MAP_SET),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.DUP),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_DELETE),
				instr.New(instr.MAP_LEN),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)),
		),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.DUP),
				instr.New(instr.MAP_CLEAR),
				instr.New(instr.MAP_LEN),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)),
		),
		values: []types.Value{types.I32(0)},
	},
	// --- recursive: fibonacci (i32), factorial (i64) ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 20),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{
					Params:  []types.Type{types.TypeI32},
					Returns: []types.Type{types.TypeI32},
				}).Emit(
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_LT_S),
					instr.New(instr.BR_IF, 26),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_SUB),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_SUB),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
					instr.New(instr.I32_ADD),
					instr.New(instr.RETURN),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.RETURN),
				).Build(),
			),
		),
		values: []types.Value{types.I32(6765)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 10),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{
					Params:  []types.Type{types.TypeI64},
					Returns: []types.Type{types.TypeI64},
				}).Emit(
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_LE_S),
					instr.New(instr.BR_IF, 16),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_SUB),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I64_MUL),
					instr.New(instr.RETURN),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.RETURN),
				).Build(),
			),
		),
		values: []types.Value{types.I64(3628800)},
	},
}

func TestInterpreter_Context(t *testing.T) {
	t.Run("propagates value", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		ctx := context.WithValue(context.Background(), "key", "val")
		i.ctx = ctx
		require.Equal(t, ctx, i.Context())
	})
}

func TestInterpreter_Push(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(42)))
		require.Equal(t, 1, i.Len())
	})
	t.Run("overflow", func(t *testing.T) {
		i := New(program.New(nil), WithStack(1))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(1)))
		require.ErrorIs(t, i.Push(types.I32(2)), ErrStackOverflow)
	})
}

func TestInterpreter_Pop(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(42)))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), v)
	})
	t.Run("underflow", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := i.Pop()
		require.ErrorIs(t, err, ErrStackUnderflow)
	})
}

func TestInterpreter_Len(t *testing.T) {
	t.Run("increments on push", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.Equal(t, 0, i.Len())
		_ = i.Push(types.I32(1))
		require.Equal(t, 1, i.Len())
		_ = i.Push(types.I32(2))
		require.Equal(t, 2, i.Len())
	})
}

func TestInterpreter_Alloc(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.I32(7))
		require.NoError(t, err)
		require.Greater(t, addr, 0)
	})
	t.Run("boxed ref returns its ref", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.BoxI32(3))
		require.NoError(t, err)
		require.Greater(t, addr, 0)
	})
}

func TestInterpreter_Load(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, _ := i.Alloc(types.I32(7))
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.I32(7), v)
	})
	t.Run("segfault negative", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := i.Load(-1)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})
	t.Run("segfault out of bounds", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := i.Load(9999)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})
}

func TestInterpreter_Store(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, _ := i.Alloc(types.I32(7))
		require.NoError(t, i.Store(addr, types.I64(99)))
		v, _ := i.Load(addr)
		require.Equal(t, types.I64(99), v)
	})
	t.Run("segfault negative", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.ErrorIs(t, i.Store(-1, types.I32(1)), ErrSegmentationFault)
	})
	t.Run("segfault out of bounds", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.ErrorIs(t, i.Store(9999, types.I32(1)), ErrSegmentationFault)
	})
}

func TestInterpreter_Retain(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, _ := i.Alloc(types.I32(5))
		v, err := i.Retain(addr)
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})
	t.Run("segfault", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := i.Retain(9999)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})
}

func TestInterpreter_Release(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, _ := i.Alloc(types.I32(5))
		i.Retain(addr)
		require.NoError(t, i.Release(addr))
	})
	t.Run("segfault", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.ErrorIs(t, i.Release(9999), ErrSegmentationFault)
	})
}

func TestInterpreter_Global(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.GLOBAL_SET, 0),
			},
		)
		i := New(prog, WithGlobals(4))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, int32(42), v.I32())
	})
	t.Run("segfault negative", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := i.Global(-1)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})
	t.Run("segfault out of bounds", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := i.Global(9999)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})
}

func TestInterpreter_SetGlobal(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.GLOBAL_SET, 0),
			},
		)
		i := New(prog, WithGlobals(4))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.NoError(t, i.SetGlobal(0, types.BoxI32(99)))
		v, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, int32(99), v.I32())
	})
	t.Run("segfault negative", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.ErrorIs(t, i.SetGlobal(-1, types.BoxI32(0)), ErrSegmentationFault)
	})
}

func TestInterpreter_Const(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		prog := program.New(nil, program.WithConstants(types.I32(42)))
		i := New(prog)
		defer i.Close()

		v, err := i.Const(0)
		require.NoError(t, err)
		require.Equal(t, int32(42), v.I32())
	})
	t.Run("segfault negative", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := i.Const(-1)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})
	t.Run("segfault out of bounds", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := i.Const(9999)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})
}

func TestInterpreter_Close(t *testing.T) {
	t.Run("no error", func(t *testing.T) {
		i := New(program.New(nil))
		require.NoError(t, i.Close())
	})
}

func TestInterpreter_Reset(t *testing.T) {
	t.Run("clears stack", func(t *testing.T) {
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
		}))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Greater(t, i.Len(), 0)

		i.Reset()
		require.Equal(t, 0, i.Len())
	})
}

func TestInterpreter_Run(t *testing.T) {
	modes := []struct {
		name string
		opts []func(*option)
	}{
		{name: "default"},
		{name: "jit", opts: []func(*option){WithTick(1), WithThreshold(1), WithCutoff(1)}},
	}
	for _, mode := range modes {
		mode := mode
		t.Run(mode.name, func(t *testing.T) {
			for _, tt := range tests {
				tt := tt
				name := tt.name
				if name == "" {
					name = tt.program.String()
				}
				t.Run(name, func(t *testing.T) {
					opts := append([]func(*option){}, tt.opts...)
					opts = append(opts, mode.opts...)

					i := New(tt.program, opts...)
					defer i.Close()

					if tt.before != nil {
						tt.before(t, i)
					}

					err := i.Run(context.Background())
					if tt.err != nil {
						require.ErrorIs(t, err, tt.err)
					} else {
						require.NoError(t, err)
						for _, val := range tt.values {
							v, err := i.Pop()
							require.NoError(t, err)
							require.Equal(t, val, v)
						}
					}

					if tt.after != nil {
						tt.after(t, i)
					}
				})
			}
		})
	}

	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		i := New(program.New([]instr.Instruction{
			instr.New(instr.NOP),
		}), WithTick(1))
		defer i.Close()

		err := i.Run(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("canceled recursive execution", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		gate := NewHostFunction(&types.FunctionType{}, func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release
			return nil, nil
		})
		fn := types.NewFunctionBuilder(nil).Emit(
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		).Build()
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn, gate),
		), WithFrame(1024))
		defer i.Close()

		errCh := make(chan error, 1)
		go func() {
			errCh <- i.Run(ctx)
		}()

		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for recursive run to start")
		}
		cancel()
		close(release)

		select {
		case err := <-errCh:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for canceled run")
		}
	})

	t.Run("single run cases", func(t *testing.T) {
		var lens []int
		var ips []int
		var preciseIPs []int
		errHook := errors.New("hook failed")
		profile := prof.New()
		refProgram := program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_GET),
			},
			program.WithConstants(types.I32Array{10, 20, 30}),
		)
		jitDisabledProfile := prof.New()
		thresholdProfile := prof.New()
		counterProfile := prof.New()
		recursiveFn := types.NewFunctionBuilder(nil).Emit(
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		).Build()
		fuelCalls := 0

		cases := []test{
			{
				name: "hook inspects interpreter",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 7),
					instr.New(instr.NOP),
				}),
				opts: []func(*option){
					WithTick(1),
					WithHook(func(i *Interpreter) error {
						lens = append(lens, i.Len())
						return nil
					}),
				},
				after: func(t *testing.T, i *Interpreter) {
					require.Equal(t, []int{0, 1}, lens)
				},
			},
			{
				name: "normal tick keeps threaded nop fusion",
				program: program.New([]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 7),
				}),
				opts: []func(*option){
					WithTick(2),
					WithThreshold(-1),
					WithHook(func(i *Interpreter) error {
						ips = append(ips, i.IP())
						return nil
					}),
				},
				after: func(t *testing.T, i *Interpreter) {
					require.Equal(t, []int{3}, ips)
				},
			},
			{
				name: "tick one preserves threaded nop boundaries",
				program: program.New([]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 7),
				}),
				opts: []func(*option){
					WithTick(1),
					WithThreshold(-1),
					WithHook(func(i *Interpreter) error {
						preciseIPs = append(preciseIPs, i.IP())
						return nil
					}),
				},
				after: func(t *testing.T, i *Interpreter) {
					require.Equal(t, []int{0, 1, 2, 3}, preciseIPs)
				},
			},
			{
				name: "profile records opcode samples",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 7),
					instr.New(instr.DROP),
				}),
				opts: []func(*option){WithProfile(profile), WithTick(1)},
				after: func(t *testing.T, i *Interpreter) {
					snap := profile.Snapshot()
					require.Equal(t, uint64(2), snap.Samples)
					require.Equal(t, uint64(2), snap.Funcs[0].Samples)
					require.Equal(t, uint64(1), profile.IP(0, 0).Samples)
					require.Equal(t, uint64(1), profile.IP(0, 5).Samples)

					opcodes := map[byte]uint64{}
					for _, op := range snap.Opcodes {
						opcodes[op.Code] = op.Samples
					}
					require.Equal(t, uint64(1), opcodes[byte(instr.I32_CONST)])
					require.Equal(t, uint64(1), opcodes[byte(instr.DROP)])
				},
			},
			{
				name: "hook returns error",
				program: program.New([]instr.Instruction{
					instr.New(instr.NOP),
				}),
				opts: []func(*option){
					WithTick(1),
					WithHook(func(i *Interpreter) error {
						return errHook
					}),
				},
				err: errHook,
			},
			{
				name:    "precise",
				program: refProgram,
				opts:    []func(*option){WithTick(1), WithThreshold(-1)},
				values:  []types.Value{types.I32(20)},
			},
			{
				name:    "fused outside precise mode",
				program: refProgram,
				opts:    []func(*option){WithThreshold(-1)},
				values:  []types.Value{types.I32(20)},
			},
			{
				name: "negative threshold disables jit",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_ADD),
				}),
				opts: []func(*option){WithProfile(jitDisabledProfile), WithTick(1), WithThreshold(-1), WithCutoff(1)},
				after: func(t *testing.T, i *Interpreter) {
					require.Zero(t, jitDisabledProfile.Snapshot().JIT.Attempts)
				},
			},
			{
				name: "threshold zero attempts jit on first sample",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_ADD),
				}),
				opts: []func(*option){WithProfile(thresholdProfile), WithTick(1), WithThreshold(0), WithCutoff(1)},
				before: func(t *testing.T, i *Interpreter) {
					if arch == nil {
						t.Skip("jit is not available on this architecture")
					}
				},
				after: func(t *testing.T, i *Interpreter) {
					require.Equal(t, uint64(1), thresholdProfile.Snapshot().JIT.Attempts)
				},
			},
			{
				name: "profile records jit counters",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_ADD),
				}),
				opts: []func(*option){WithProfile(counterProfile), WithTick(1), WithThreshold(1), WithCutoff(1)},
				before: func(t *testing.T, i *Interpreter) {
					if arch == nil {
						t.Skip("jit is not available on this architecture")
					}
				},
				after: func(t *testing.T, i *Interpreter) {
					jit := counterProfile.Snapshot().JIT
					require.Equal(t, uint64(1), jit.Attempts)
					require.NotZero(t, jit.Emits)
					require.NotZero(t, jit.Links)
					require.NotZero(t, jit.Bytes)
				},
			},
			func() test {
				p := prof.New()
				return test{
					name: "jit compiles numeric globals",
					program: program.New([]instr.Instruction{
						instr.New(instr.I32_CONST, 9),
						instr.New(instr.GLOBAL_SET, 0),
						instr.New(instr.GLOBAL_GET, 0),
					}),
					opts:   []func(*option){WithProfile(p), WithCutoff(1)},
					values: []types.Value{types.I32(9)},
					before: func(t *testing.T, i *Interpreter) {
						if arch == nil {
							t.Skip("jit is not available on this architecture")
						}
						p.Add(0, 0, byte(instr.I32_CONST))
						i.globals = append(i.globals, types.BoxI32(1))
						require.NoError(t, i.jit(0))
					},
					after: func(t *testing.T, i *Interpreter) {
						jit := p.Snapshot().JIT
						require.Equal(t, uint64(1), jit.Attempts)
						require.NotZero(t, jit.Emits)
						require.NotZero(t, jit.Links)
					},
				}
			}(),
			func() test {
				p := prof.New()
				return test{
					name: "jit skips ref globals",
					program: program.New([]instr.Instruction{
						instr.New(instr.GLOBAL_GET, 0),
					}),
					opts:   []func(*option){WithProfile(p), WithCutoff(1)},
					values: []types.Value{types.Null},
					before: func(t *testing.T, i *Interpreter) {
						if arch == nil {
							t.Skip("jit is not available on this architecture")
						}
						p.Add(0, 0, byte(instr.GLOBAL_GET))
						i.globals = append(i.globals, types.BoxedNull)
						require.NoError(t, i.jit(0))
					},
					after: func(t *testing.T, i *Interpreter) {
						jit := p.Snapshot().JIT
						require.Equal(t, uint64(1), jit.Attempts)
						require.Zero(t, jit.Emits)
						require.Zero(t, jit.Links)
					},
				}
			}(),
			{
				name: "fuel zero is unlimited",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 7),
					instr.New(instr.I32_CONST, 8),
					instr.New(instr.I32_ADD),
				}),
				opts:   []func(*option){WithTick(1), WithFuel(0)},
				values: []types.Value{types.I32(15)},
			},
			{
				name: "fuel exhausts recursive execution",
				program: program.New(
					[]instr.Instruction{
						instr.New(instr.CONST_GET, 0),
						instr.New(instr.CALL),
					},
					program.WithConstants(recursiveFn),
				),
				opts: []func(*option){WithTick(1), WithFuel(2)},
				err:  ErrFuelExhausted,
			},
			{
				name: "fuel rounds up to tick interval",
				program: program.New(
					[]instr.Instruction{
						instr.New(instr.CONST_GET, 0),
						instr.New(instr.CALL),
					},
					program.WithConstants(recursiveFn),
				),
				opts: []func(*option){
					WithTick(2),
					WithFuel(3),
					WithHook(func(i *Interpreter) error {
						fuelCalls++
						return nil
					}),
				},
				err: ErrFuelExhausted,
				after: func(t *testing.T, i *Interpreter) {
					require.Equal(t, 2, fuelCalls)
				},
			},
			{
				name: "fuel exhausts jit execution",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 4),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 5),
					instr.New(instr.DROP),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				}, program.WithConstants(NewHostFunction(&types.FunctionType{}, func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
					return nil, nil
				}))),
				opts: []func(*option){WithTick(1), WithThreshold(1), WithCutoff(1), WithFuel(1)},
				err:  ErrFuelExhausted,
			},
		}
		for _, tt := range cases {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				i := New(tt.program, tt.opts...)
				defer i.Close()

				if tt.before != nil {
					tt.before(t, i)
				}

				err := i.Run(context.Background())
				if tt.err != nil {
					require.ErrorIs(t, err, tt.err)
				} else {
					require.NoError(t, err)
					for _, val := range tt.values {
						v, err := i.Pop()
						require.NoError(t, err)
						require.Equal(t, val, v)
					}
				}

				if tt.after != nil {
					tt.after(t, i)
				}
			})
		}
	})

	t.Run("hook cancel is observed on next tick", func(t *testing.T) {
		cases := []struct {
			name    string
			program *program.Program
			opts    []func(*option)
		}{
			{
				name: "threaded",
				program: program.New([]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 0),
				}),
				opts: []func(*option){WithTick(1)},
			},
			{
				name: "jit",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 4),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 5),
					instr.New(instr.DROP),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				}, program.WithConstants(NewHostFunction(&types.FunctionType{}, func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
					return nil, nil
				}))),
				opts: []func(*option){WithTick(1), WithThreshold(1), WithCutoff(1)},
			},
		}
		for _, tt := range cases {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				calls := 0
				opts := append([]func(*option){}, tt.opts...)
				opts = append(opts, WithHook(func(i *Interpreter) error {
					calls++
					cancel()
					return nil
				}))

				i := New(tt.program, opts...)
				defer i.Close()

				err := i.Run(ctx)
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, 1, calls)
			})
		}
	})

	t.Run("debugger breakpoint stops before instruction", func(t *testing.T) {
		dbg := NewDebugger()
		id := dbg.Break(0, 0)
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
		}), WithDebugger(dbg))
		defer i.Close()

		err := i.Run(context.Background())
		require.ErrorIs(t, err, ErrStopped)
		require.Equal(t, Stop{Func: 0, IP: 0, Breakpoint: id}, dbg.Stop())
		require.Equal(t, 0, i.Len())

		dbg.Continue()
		err = i.Run(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, i.Len())
		require.Equal(t, uint64(1), dbg.Breakpoints()[0].Hits)
	})

	t.Run("debugger conditional breakpoint", func(t *testing.T) {
		dbg := NewDebugger()
		id := dbg.BreakIf(0, 5, func(i *Interpreter) bool {
			return i.Len() == 1
		})
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.DROP),
		}), WithDebugger(dbg))
		defer i.Close()

		err := i.Run(context.Background())
		require.ErrorIs(t, err, ErrStopped)
		require.Equal(t, id, dbg.Stop().Breakpoint)
		require.Equal(t, 1, i.Len())
	})

	t.Run("debugger helpers inspect current frame", func(t *testing.T) {
		dbg := NewDebugger()
		dbg.Break(0, 0)
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
		}), WithDebugger(dbg))
		defer i.Close()

		err := i.Run(context.Background())
		require.ErrorIs(t, err, ErrStopped)

		require.Equal(t, 0, i.Func())
		require.Equal(t, 0, i.IP())
		require.Equal(t, 1, i.FrameDepth())
		op, err := i.Opcode()
		require.NoError(t, err)
		require.Equal(t, instr.I32_CONST, op)
		fn, ip, bp, err := i.Frame(0)
		require.NoError(t, err)
		require.Equal(t, 0, fn)
		require.Equal(t, 0, ip)
		require.Equal(t, 0, bp)
		_, _, _, err = i.Frame(1)
		require.ErrorIs(t, err, ErrFrameUnderflow)
	})

	t.Run("debugger step next and finish around calls", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.RETURN),
		).Build()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.DROP),
		}, program.WithConstants(fn))

		t.Run("step enters call", func(t *testing.T) {
			dbg := NewDebugger()
			dbg.Break(0, 3)
			i := New(prog, WithDebugger(dbg))
			defer i.Close()

			require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
			dbg.Step()
			require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
			require.Equal(t, 1, i.Func())
			require.Equal(t, 0, i.IP())
			require.Equal(t, 2, i.FrameDepth())
			fn, ip, _, err := i.Frame(0)
			require.NoError(t, err)
			require.Equal(t, 1, fn)
			require.Equal(t, 0, ip)
			fn, ip, _, err = i.Frame(1)
			require.NoError(t, err)
			require.Equal(t, 0, fn)
			require.Equal(t, 4, ip)
		})

		t.Run("next steps over call", func(t *testing.T) {
			dbg := NewDebugger()
			dbg.Break(0, 3)
			i := New(prog, WithDebugger(dbg))
			defer i.Close()

			require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
			dbg.Next()
			require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
			require.Equal(t, 0, i.Func())
			require.Equal(t, 4, i.IP())
			require.Equal(t, 1, i.FrameDepth())
			require.Equal(t, 1, i.Len())
		})

		t.Run("finish stops in caller", func(t *testing.T) {
			dbg := NewDebugger()
			dbg.Break(0, 3)
			i := New(prog, WithDebugger(dbg))
			defer i.Close()

			require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
			dbg.Step()
			require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
			dbg.Finish()
			require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
			require.Equal(t, 0, i.Func())
			require.Equal(t, 4, i.IP())
			require.Equal(t, 1, i.FrameDepth())
		})
	})

	t.Run("reset restores fuel", func(t *testing.T) {
		i := New(program.New([]instr.Instruction{
			instr.New(instr.NOP),
			instr.New(instr.I32_CONST, 7),
		}), WithTick(1), WithFuel(1))
		defer i.Close()

		err := i.Run(context.Background())
		require.ErrorIs(t, err, ErrFuelExhausted)

		i.Reset()

		err = i.Run(context.Background())
		require.ErrorIs(t, err, ErrFuelExhausted)
	})

	t.Run("jit links branches", func(t *testing.T) {
		if arch == nil {
			t.Skip("jit is not available on this architecture")
		}

		loop := types.NewFunctionBuilder(nil).WithLocals(types.TypeI32).Emit(
			instr.New(instr.I32_CONST, 3),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_TEE, 0),
			instr.New(instr.BR_IF, uint64(uint16(-13+1<<16))),
			instr.New(instr.LOCAL_GET, 0),
		).Build()

		tests := []struct {
			name     string
			program  *program.Program
			profile  func(*prof.Stats)
			jitAddr  func(*Interpreter) int
			value    types.Value
			minLinks uint64
		}{
			{
				name: "cold forward target",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.BR, 5),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 32),
					instr.New(instr.I32_ADD),
				}),
				profile:  func(p *prof.Stats) { p.Add(0, 0, byte(instr.I32_CONST)) },
				jitAddr:  func(*Interpreter) int { return 0 },
				value:    types.I32(42),
				minLinks: 2,
			},
			{
				name: "param order at target",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.BR, 0),
					instr.New(instr.I32_SUB),
				}),
				profile: func(p *prof.Stats) { p.Add(0, 0, byte(instr.I32_CONST)) },
				jitAddr: func(*Interpreter) int { return 0 },
				value:   types.I32(7),
			},
			{
				name: "signed backward br_if",
				program: program.New(
					[]instr.Instruction{
						instr.New(instr.CONST_GET, 0),
						instr.New(instr.CALL),
					},
					program.WithConstants(loop),
				),
				profile: func(p *prof.Stats) { p.Add(1, 7, byte(instr.LOCAL_GET)) },
				jitAddr: func(i *Interpreter) int { return i.constants[0].Ref() },
				value:   types.I32(0),
			},
			{
				name: "br_table first target",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.BR_TABLE, 2, 0, 8, 16),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.BR, 16),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.BR, 8),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.BR, 0),
					instr.New(instr.NOP),
				}),
				profile: func(p *prof.Stats) { p.Add(0, 0, byte(instr.I32_CONST)) },
				jitAddr: func(*Interpreter) int { return 0 },
				value:   types.I32(1),
			},
			{
				name: "br_table second target",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.BR_TABLE, 2, 0, 8, 16),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.BR, 16),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.BR, 8),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.BR, 0),
					instr.New(instr.NOP),
				}),
				profile: func(p *prof.Stats) { p.Add(0, 0, byte(instr.I32_CONST)) },
				jitAddr: func(*Interpreter) int { return 0 },
				value:   types.I32(2),
			},
			{
				name: "br_table default",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.BR_TABLE, 2, 0, 8, 16),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.BR, 16),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.BR, 8),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.BR, 0),
					instr.New(instr.NOP),
				}),
				profile: func(p *prof.Stats) { p.Add(0, 0, byte(instr.I32_CONST)) },
				jitAddr: func(*Interpreter) int { return 0 },
				value:   types.I32(3),
			},
			{
				name: "br_table negative default",
				program: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 0xFFFFFFFF),
					instr.New(instr.BR_TABLE, 2, 0, 8, 16),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.BR, 16),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.BR, 8),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.BR, 0),
					instr.New(instr.NOP),
				}),
				profile: func(p *prof.Stats) { p.Add(0, 0, byte(instr.I32_CONST)) },
				jitAddr: func(*Interpreter) int { return 0 },
				value:   types.I32(3),
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				p := prof.New()
				tt.profile(p)
				i := New(tt.program, WithProfile(p), WithCutoff(1))
				defer i.Close()

				err := i.jit(tt.jitAddr(i))
				require.NoError(t, err)
				err = i.Run(context.Background())
				require.NoError(t, err)

				val, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, tt.value, val)
				require.GreaterOrEqual(t, p.Snapshot().JIT.Links, tt.minLinks)
			})
		}
	})

	t.Run("jit skips cold segments", func(t *testing.T) {
		if arch == nil {
			t.Skip("jit is not available on this architecture")
		}
		p := prof.New()
		p.Add(0, 0, byte(instr.NOP))
		i := New(program.New([]instr.Instruction{
			instr.New(instr.NOP),
			instr.New(instr.NOP),
			instr.New(instr.NOP),
			instr.New(instr.NOP),
			instr.New(instr.NOP),
			instr.New(instr.CALL),
			instr.New(instr.NOP),
			instr.New(instr.NOP),
			instr.New(instr.NOP),
			instr.New(instr.NOP),
			instr.New(instr.NOP),
		}), WithProfile(p), WithCutoff(1))
		defer i.Close()

		err := i.jit(0)
		require.NoError(t, err)

		jit := p.Snapshot().JIT
		require.Equal(t, uint64(1), jit.Attempts)
		require.Equal(t, uint64(1), jit.Emits)
		require.Equal(t, uint64(1), jit.Links)
		require.Equal(t, uint64(1), jit.Skips)
	})

	t.Run("canceled jit execution", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		calls := 0
		gate := NewHostFunction(&types.FunctionType{}, func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
			calls++
			if calls < 64 {
				return nil, nil
			}
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release
			return nil, nil
		})
		fn := types.NewFunctionBuilder(nil).Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 3),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 4),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.DROP),
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		).Build()
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn, gate),
		), WithFrame(1024), WithTick(1), WithThreshold(1), WithCutoff(1))
		defer i.Close()

		errCh := make(chan error, 1)
		go func() {
			errCh <- i.Run(ctx)
		}()

		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for jit run to start")
		}
		cancel()
		close(release)

		select {
		case err := <-errCh:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for canceled run")
		}
	})
}

func TestDebugger_Breakpoints(t *testing.T) {
	var dbg Debugger
	first := dbg.Break(0, 0)
	second := dbg.Break(0, 1)

	require.True(t, dbg.Enable(first, false))
	require.False(t, dbg.Enable(99, false))
	require.True(t, dbg.Clear(second))
	require.False(t, dbg.Clear(second))

	bps := dbg.Breakpoints()
	require.Len(t, bps, 1)
	require.Equal(t, first, bps[0].ID)
	require.False(t, bps[0].Enabled)
}

func BenchmarkInterpreter_Run(b *testing.B) {
	b.Run("default", func(b *testing.B) {
		for _, tt := range tests {
			b.Run(tt.program.String(), func(b *testing.B) {
				ctx, cancel := context.WithCancel(context.TODO())
				defer cancel()

				i := New(tt.program)
				defer i.Close()

				b.ResetTimer()

				for n := 0; n < b.N; n++ {
					_ = i.Run(ctx)
					i.Reset()
				}
			})
		}
	})

	b.Run("jit", func(b *testing.B) {
		for _, tt := range tests {
			b.Run(tt.program.String(), func(b *testing.B) {
				ctx, cancel := context.WithCancel(context.TODO())
				defer cancel()

				i := New(tt.program, WithTick(1), WithThreshold(1), WithCutoff(1))
				defer i.Close()

				b.ResetTimer()

				for n := 0; n < b.N; n++ {
					_ = i.Run(ctx)
					i.Reset()
				}
			})
		}
	})
}
