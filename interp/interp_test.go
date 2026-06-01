package interp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/jit"
	_ "github.com/siyul-park/minivm/jit/amd64"
	_ "github.com/siyul-park/minivm/jit/arm64"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type test struct {
	program *program.Program
	values  []types.Value
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
				instr.New(instr.I32_CONST, 0x7FFFFFFF),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_ADD),
			},
		),
		values: []types.Value{types.I32(math.MinInt32)},
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
	// --- refs: REF_NEW, REF_GET, REF_SET, REF_NULL, REF_TEST, REF_CAST, REF_IS_NULL, REF_EQ, REF_NE ---
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
				instr.New(instr.I32_CONST, 7),
				instr.New(instr.REF_NEW),
				instr.New(instr.REF_GET),
			},
		),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.REF_NEW),
				instr.New(instr.DUP),
				instr.New(instr.I32_CONST, 9),
				instr.New(instr.REF_SET),
				instr.New(instr.REF_GET),
			},
		),
		values: []types.Value{types.I32(9)},
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
		// ref.test discriminates a heap value (any holding a ref) against its type.
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.REF_TEST, 0),
			},
			program.WithConstants(types.String("foo")),
			program.WithTypes(types.TypeString),
		),
		values: []types.Value{types.True},
	},
	{
		// ref.test rejects a heap value against a mismatching type.
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.REF_TEST, 0),
			},
			program.WithConstants(types.String("foo")),
			program.WithTypes(types.TypeI32),
		),
		values: []types.Value{types.False},
	},
	{
		// ref.test rejects a primitive (any holding a scalar) against a mismatching type.
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.REF_TEST, 0),
			},
			program.WithTypes(types.TypeF64),
		),
		values: []types.Value{types.False},
	},
	{
		// A ref-typed (any) global round-trips a primitive and discriminates it.
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.GLOBAL_GET, 0),
				instr.New(instr.REF_TEST, 0),
			},
			program.WithTypes(types.TypeI32),
		),
		values: []types.Value{types.True},
	},
	{
		// A ref-typed (any) global round-trips a heap reference and discriminates it.
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.GLOBAL_GET, 0),
				instr.New(instr.REF_TEST, 0),
			},
			program.WithConstants(types.String("foo")),
			program.WithTypes(types.TypeString),
		),
		values: []types.Value{types.True},
	},
	{
		// Overwriting a heap ref in an any-slot with a primitive releases the ref
		// and leaves a clean scalar (no spurious release of the primitive bits).
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.I32_CONST, 7),
				instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.GLOBAL_GET, 0),
				instr.New(instr.REF_TEST, 0),
			},
			program.WithConstants(types.String("foo")),
			program.WithTypes(types.TypeI32),
		),
		values: []types.Value{types.True},
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
				instr.New(instr.I32_CONST, 0xFFFFFFF9),
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_DIV_S),
			},
		),
		values: []types.Value{types.I32(-2)},
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
				instr.New(instr.I32_CONST, 0xFFFFFFF9),
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_REM_S),
			},
		),
		values: []types.Value{types.I32(-1)},
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
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 32),
				instr.New(instr.I32_SHL),
			},
		),
		values: []types.Value{types.I32(1)},
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
				instr.New(instr.I32_CONST, 0xFFFFFFFF),
				instr.New(instr.I32_CONST, 1),
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
		values: []types.Value{types.TypedArray[int32]("foo")},
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
		values: []types.Value{types.TypedArray[int32]{1}},
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
		values: []types.Value{types.TypedArray[int64]{1}},
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
		values: []types.Value{types.TypedArray[float32]{42}},
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
		values: []types.Value{types.TypedArray[float64]{42}},
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
		values: []types.Value{make(types.TypedArray[int32], 1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeI64)),
		),
		values: []types.Value{make(types.TypedArray[int64], 1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeF32)),
		),
		values: []types.Value{make(types.TypedArray[float32], 1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeF64)),
		),
		values: []types.Value{make(types.TypedArray[float64], 1)},
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
	// --- array: []i8 (Binary) ---
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 0x1FF),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeI8)),
		),
		values: []types.Value{types.TypedArray[int8]{-1}},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeI8)),
		),
		values: []types.Value{make(types.TypedArray[int8], 1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 0x1FF),
				instr.New(instr.ARRAY_SET),
			},
			program.WithTypes(types.NewArrayType(types.TypeI8)),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 0xFF),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.ARRAY_GET),
			},
			program.WithTypes(types.NewArrayType(types.TypeI8)),
		),
		values: []types.Value{types.I32(0xFF)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 0xAB),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.ARRAY_FILL),
			},
			program.WithTypes(types.NewArrayType(types.TypeI8)),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 4),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.ARRAY_COPY),
			},
			program.WithTypes(types.NewArrayType(types.TypeI8)),
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.ARRAY_LEN),
			},
			program.WithTypes(types.NewArrayType(types.TypeI8)),
		),
		values: []types.Value{types.I32(3)},
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
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I64_CONST, uint64(int64(1<<50))),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_GET),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI64)),
		),
		values: []types.Value{types.I64(1 << 50)},
	},
	// --- closures: CLOSURE_NEW, UPVAL_GET, UPVAL_SET ---
	{
		// no-capture closure: behaves like calling the function directly
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CLOSURE_NEW),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{
					Returns: []types.Type{types.TypeI32},
				}).Emit(
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.RETURN),
				).Build(),
			),
		),
		values: []types.Value{types.I32(42)},
	},
	{
		// single mutable closure: a counter, called three times yields 3
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CLOSURE_NEW),
				instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.GLOBAL_GET, 0),
				instr.New(instr.CALL),
				instr.New(instr.DROP),
				instr.New(instr.GLOBAL_GET, 0),
				instr.New(instr.CALL),
				instr.New(instr.DROP),
				instr.New(instr.GLOBAL_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{
					Returns: []types.Type{types.TypeI32},
				}).WithCaptures(types.TypeI32).Emit(
					instr.New(instr.UPVAL_GET, 0),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_ADD),
					instr.New(instr.UPVAL_SET, 0),
					instr.New(instr.UPVAL_GET, 0),
					instr.New(instr.RETURN),
				).Build(),
			),
		),
		values: []types.Value{types.I32(3)},
	},
	{
		// two closures sharing one heap-boxed variable via ref.new
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.REF_NEW),
				instr.New(instr.DUP),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CLOSURE_NEW),
				instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.CLOSURE_NEW),
				instr.New(instr.GLOBAL_SET, 1),
				instr.New(instr.GLOBAL_GET, 0),
				instr.New(instr.CALL),
				instr.New(instr.GLOBAL_GET, 0),
				instr.New(instr.CALL),
				instr.New(instr.GLOBAL_GET, 1),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{}).
					WithCaptures(types.TypeRef).Emit(
					instr.New(instr.UPVAL_GET, 0),
					instr.New(instr.UPVAL_GET, 0),
					instr.New(instr.REF_GET),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_ADD),
					instr.New(instr.REF_SET),
					instr.New(instr.RETURN),
				).Build(),
				types.NewFunctionBuilder(&types.FunctionType{
					Returns: []types.Type{types.TypeI32},
				}).WithCaptures(types.TypeRef).Emit(
					instr.New(instr.UPVAL_GET, 0),
					instr.New(instr.REF_GET),
					instr.New(instr.RETURN),
				).Build(),
			),
		),
		values: []types.Value{types.I32(2)},
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

type recordingMarshaler struct {
	marshalCalled   bool
	unmarshalCalled bool
}

func (m *recordingMarshaler) Marshal(_ *Interpreter, _ any) (types.Value, error) {
	m.marshalCalled = true
	return types.I32(9), nil
}

func (m *recordingMarshaler) Unmarshal(_ *Interpreter, _ types.Value, dst any) error {
	m.unmarshalCalled = true
	out, ok := dst.(*int32)
	if !ok {
		return errors.New("unexpected destination")
	}
	*out = 12
	return nil
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
	t.Run("interns strings", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.String("same")))
		first, err := i.Peek(0)
		require.NoError(t, err)
		require.Equal(t, types.KindRef, first.Kind())

		require.NoError(t, i.Push(types.String("same")))
		second, err := i.Peek(0)
		require.NoError(t, err)
		require.Equal(t, first.Ref(), second.Ref())
		require.Equal(t, 2, i.rc[first.Ref()])

		_, err = i.Pop()
		require.NoError(t, err)
		require.Contains(t, i.interned, "same")

		_, err = i.Pop()
		require.NoError(t, err)
		require.NotContains(t, i.interned, "same")

		filler, err := i.Alloc(types.I32(1))
		require.NoError(t, err)
		require.Equal(t, first.Ref(), filler)

		require.NoError(t, i.Push(types.String("same")))
		third, err := i.Peek(0)
		require.NoError(t, err)
		require.NotEqual(t, first.Ref(), third.Ref())
		require.Equal(t, 1, i.rc[third.Ref()])
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
	i := New(program.New(nil))
	defer i.Close()

	require.Equal(t, 0, i.Len())
	_ = i.Push(types.I32(1))
	require.Equal(t, 1, i.Len())
	_ = i.Push(types.I32(2))
	require.Equal(t, 2, i.Len())
}

func TestInterpreter_Alloc(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		for _, v := range []types.Value{types.I32(7), types.BoxI32(3)} {
			addr, err := i.Alloc(v)
			require.NoError(t, err)
			require.Greater(t, addr, 0)
		}
	})

	t.Run("interns strings", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		first, err := i.Alloc(types.String("same"))
		require.NoError(t, err)
		second, err := i.Alloc(types.String("same"))
		require.NoError(t, err)
		require.Equal(t, first, second)
		require.Equal(t, 2, i.rc[first])

		require.NoError(t, i.Release(first))
		require.Contains(t, i.interned, "same")

		require.NoError(t, i.Release(second))
		require.NotContains(t, i.interned, "same")
	})

	t.Run("collects unreachable cycle when heap fills", func(t *testing.T) {
		i := New(program.New(nil), WithHeap(2))
		defer i.Close()

		array := types.NewArray(types.NewArrayType(types.TypeRef))
		addr, err := i.Alloc(array)
		require.NoError(t, err)
		array.Elems = append(array.Elems, types.BoxRef(addr))

		_, err = i.Retain(addr)
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))

		reused, err := i.Alloc(types.I32(1))
		require.NoError(t, err)
		require.Equal(t, addr, reused)
		require.NoError(t, i.Release(reused))

		_, err = i.Load(addr)
		require.ErrorIs(t, err, ErrSegmentationFault)
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
	t.Run("segfault", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		for _, addr := range []int{-1, 9999} {
			_, err := i.Load(addr)
			require.ErrorIs(t, err, ErrSegmentationFault)
		}
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
	t.Run("segfault", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		for _, addr := range []int{-1, 9999} {
			require.ErrorIs(t, i.Store(addr, types.I32(1)), ErrSegmentationFault)
		}
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
	t.Run("segfault", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		for _, idx := range []int{-1, 9999} {
			_, err := i.Global(idx)
			require.ErrorIs(t, err, ErrSegmentationFault)
		}
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
	t.Run("segfault", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		for _, idx := range []int{-1, 9999} {
			_, err := i.Const(idx)
			require.ErrorIs(t, err, ErrSegmentationFault)
		}
	})
}

func TestInterpreter_Close(t *testing.T) {
	i := New(program.New(nil))
	require.NoError(t, i.Close())
}

func TestInterpreter_Reset(t *testing.T) {
	i := New(program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 7),
	}))
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	require.Greater(t, i.Len(), 0)
	i.Reset()
	require.Equal(t, 0, i.Len())
}

func TestInterpreter_Run(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.program.String(), func(t *testing.T) {
				i := New(tt.program)
				defer i.Close()
				if tt.before != nil {
					tt.before(t, i)
				}
				err := i.Run(context.Background())
				require.NoError(t, err)
				for _, val := range tt.values {
					v, err := i.Pop()
					require.NoError(t, err)
					require.Equal(t, val, v)
				}
			})
		}
	})
	t.Run("jit", func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.program.String(), func(t *testing.T) {
				i := New(tt.program, WithTick(1), WithCutoff(1), WithThreshold(1))
				defer i.Close()
				if tt.before != nil {
					tt.before(t, i)
				}
				err := i.Run(context.Background())
				require.NoError(t, err)
				for _, val := range tt.values {
					v, err := i.Pop()
					require.NoError(t, err)
					require.Equal(t, val, v)
				}
			})
		}
	})

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

}

func TestInterpreter_WithHook(t *testing.T) {
	t.Run("inspects interpreter", func(t *testing.T) {
		var lens []int
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.NOP),
		}), WithTick(1), WithHook(func(i *Interpreter) error {
			lens = append(lens, i.Len())
			return nil
		}))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, []int{0, 1}, lens)
	})

	t.Run("returns error", func(t *testing.T) {
		errHook := errors.New("hook failed")
		i := New(program.New([]instr.Instruction{
			instr.New(instr.NOP),
		}), WithTick(1), WithHook(func(i *Interpreter) error {
			return errHook
		}))
		defer i.Close()
		require.ErrorIs(t, i.Run(context.Background()), errHook)
	})

	t.Run("cancel observed on tick threaded", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		i := New(program.New([]instr.Instruction{
			instr.New(instr.NOP),
			instr.New(instr.I32_CONST, 0),
		}), WithTick(1), WithHook(func(i *Interpreter) error {
			calls++
			cancel()
			return nil
		}))
		defer i.Close()
		require.ErrorIs(t, i.Run(ctx), context.Canceled)
		require.Equal(t, 1, calls)
	})

	t.Run("cancel observed on tick jit", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		i := New(program.New([]instr.Instruction{
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
		}))), WithTick(1), WithThreshold(1), WithCutoff(1), WithHook(func(i *Interpreter) error {
			calls++
			cancel()
			return nil
		}))
		defer i.Close()
		require.ErrorIs(t, i.Run(ctx), context.Canceled)
		require.Equal(t, 1, calls)
	})
}

func TestInterpreter_WithTick(t *testing.T) {
	t.Run("normal tick keeps threaded nop fusion", func(t *testing.T) {
		var ips []int
		i := New(program.New([]instr.Instruction{
			instr.New(instr.NOP),
			instr.New(instr.NOP),
			instr.New(instr.NOP),
			instr.New(instr.I32_CONST, 7),
		}), WithTick(2), WithThreshold(-1), WithHook(func(i *Interpreter) error {
			ips = append(ips, i.IP())
			return nil
		}))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, []int{3}, ips)
	})

	t.Run("tick one preserves threaded nop boundaries", func(t *testing.T) {
		var ips []int
		i := New(program.New([]instr.Instruction{
			instr.New(instr.NOP),
			instr.New(instr.NOP),
			instr.New(instr.NOP),
			instr.New(instr.I32_CONST, 7),
		}), WithTick(1), WithThreshold(-1), WithHook(func(i *Interpreter) error {
			ips = append(ips, i.IP())
			return nil
		}))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, []int{0, 1, 2, 3}, ips)
	})
}

func TestInterpreter_WithThreshold(t *testing.T) {
	t.Run("precise", func(t *testing.T) {
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_GET),
			},
			program.WithConstants(types.TypedArray[int32]{10, 20, 30}),
		), WithTick(1), WithThreshold(-1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(20), v)
	})

	t.Run("fused outside precise mode", func(t *testing.T) {
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_GET),
			},
			program.WithConstants(types.TypedArray[int32]{10, 20, 30}),
		), WithThreshold(-1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(20), v)
	})

	t.Run("i64 const fusion returns inline result", func(t *testing.T) {
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, 40),
			instr.New(instr.I64_CONST, 2),
			instr.New(instr.I64_ADD),
		}), WithThreshold(-1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I64(42), v)
	})

	t.Run("i64 const fusion preserves spilled result", func(t *testing.T) {
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, 1<<50),
			instr.New(instr.I64_CONST, 1),
			instr.New(instr.I64_ADD),
		}), WithThreshold(-1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I64((1<<50)+1), v)
	})

	t.Run("i64 const fusion preserves divide by zero", func(t *testing.T) {
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, 1),
			instr.New(instr.I64_CONST, 0),
			instr.New(instr.I64_DIV_S),
		}), WithThreshold(-1))
		defer i.Close()
		require.ErrorIs(t, i.Run(context.Background()), ErrDivideByZero)
	})

	t.Run("precise i64 bypass returns same result", func(t *testing.T) {
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, 40),
			instr.New(instr.I64_CONST, 2),
			instr.New(instr.I64_ADD),
		}), WithTick(1), WithThreshold(-1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I64(42), v)
	})

	t.Run("negative disables jit", func(t *testing.T) {
		p := prof.New()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		}), WithProfile(p), WithTick(1), WithThreshold(-1), WithCutoff(1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		require.Zero(t, p.Snapshot().JIT.Attempts)
	})

	t.Run("zero attempts jit on first sample", func(t *testing.T) {
		if jit.Active() == nil {
			t.Skip("jit is not available on this architecture")
		}
		p := prof.New()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		}), WithProfile(p), WithTick(1), WithThreshold(0), WithCutoff(1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, uint64(1), p.Snapshot().JIT.Attempts)
	})
}

func TestInterpreter_WithProfile(t *testing.T) {
	t.Run("records opcode samples", func(t *testing.T) {
		p := prof.New()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.DROP),
		}), WithProfile(p), WithTick(1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))

		snap := p.Snapshot()
		require.Equal(t, uint64(2), snap.Samples)
		require.Equal(t, uint64(2), snap.Funcs[0].Samples)
		require.Equal(t, uint64(1), p.IP(0, 0).Samples)
		require.Equal(t, uint64(1), p.IP(0, 5).Samples)
		opcodes := map[byte]uint64{}
		for _, op := range snap.Opcodes {
			opcodes[op.Code] = op.Samples
		}
		require.Equal(t, uint64(1), opcodes[byte(instr.I32_CONST)])
		require.Equal(t, uint64(1), opcodes[byte(instr.DROP)])
	})

	t.Run("records jit counters", func(t *testing.T) {
		if jit.Active() == nil {
			t.Skip("jit is not available on this architecture")
		}
		p := prof.New()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		}), WithProfile(p), WithTick(1), WithThreshold(1), WithCutoff(1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		jit := p.Snapshot().JIT
		require.Equal(t, uint64(1), jit.Attempts)
		require.NotZero(t, jit.Emits)
		require.NotZero(t, jit.Links)
		require.NotZero(t, jit.Bytes)
	})
}

func TestInterpreter_WithFuel(t *testing.T) {
	t.Run("zero is unlimited", func(t *testing.T) {
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.I32_CONST, 8),
			instr.New(instr.I32_ADD),
		}), WithTick(1), WithFuel(0))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(15), v)
	})

	t.Run("exhausts recursive execution", func(t *testing.T) {
		recursiveFn := types.NewFunctionBuilder(nil).Emit(
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		).Build()
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(recursiveFn),
		), WithTick(1), WithFuel(2))
		defer i.Close()
		require.ErrorIs(t, i.Run(context.Background()), ErrFuelExhausted)
	})

	t.Run("rounds up to tick interval", func(t *testing.T) {
		recursiveFn := types.NewFunctionBuilder(nil).Emit(
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		).Build()
		calls := 0
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(recursiveFn),
		), WithTick(2), WithFuel(3), WithHook(func(i *Interpreter) error {
			calls++
			return nil
		}))
		defer i.Close()
		require.ErrorIs(t, i.Run(context.Background()), ErrFuelExhausted)
		require.Equal(t, 2, calls)
	})

	t.Run("exhausts jit execution", func(t *testing.T) {
		i := New(program.New([]instr.Instruction{
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
		}))), WithTick(1), WithThreshold(1), WithCutoff(1), WithFuel(1))
		defer i.Close()
		require.ErrorIs(t, i.Run(context.Background()), ErrFuelExhausted)
	})

	t.Run("reset restores fuel", func(t *testing.T) {
		i := New(program.New([]instr.Instruction{
			instr.New(instr.NOP),
			instr.New(instr.I32_CONST, 7),
		}), WithTick(1), WithFuel(1))
		defer i.Close()
		require.ErrorIs(t, i.Run(context.Background()), ErrFuelExhausted)
		i.Reset()
		require.ErrorIs(t, i.Run(context.Background()), ErrFuelExhausted)
	})
}

func TestInterpreter_WithDebugger(t *testing.T) {
	t.Run("breakpoint stops before instruction", func(t *testing.T) {
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

	t.Run("conditional breakpoint", func(t *testing.T) {
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

	t.Run("helpers inspect current frame", func(t *testing.T) {
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
		require.Equal(t, 1, i.FP())
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

	makeCallProg := func() *program.Program {
		callee := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.RETURN),
		).Build()
		return program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.DROP),
		}, program.WithConstants(callee))
	}

	t.Run("step enters call", func(t *testing.T) {
		dbg := NewDebugger()
		dbg.Break(0, 3)
		i := New(makeCallProg(), WithDebugger(dbg))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		dbg.Step()
		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		require.Equal(t, 1, i.Func())
		require.Equal(t, 0, i.IP())
		require.Equal(t, 2, i.FP())
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
		i := New(makeCallProg(), WithDebugger(dbg))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		dbg.Next()
		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		require.Equal(t, 0, i.Func())
		require.Equal(t, 4, i.IP())
		require.Equal(t, 1, i.FP())
		require.Equal(t, 1, i.Len())
	})

	t.Run("finish stops in caller", func(t *testing.T) {
		dbg := NewDebugger()
		dbg.Break(0, 3)
		i := New(makeCallProg(), WithDebugger(dbg))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		dbg.Step()
		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		dbg.Finish()
		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		require.Equal(t, 0, i.Func())
		require.Equal(t, 4, i.IP())
		require.Equal(t, 1, i.FP())
	})
}

func TestInterpreter_JIT(t *testing.T) {
	t.Run("passes stack inputs as segment args", func(t *testing.T) {
		if jit.Active() == nil {
			t.Skip("jit is not available on this architecture")
		}
		p := prof.New()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_ADD),
		}), WithProfile(p), WithCutoff(1))
		defer i.Close()
		p.Add(0, 0, byte(instr.I32_ADD))
		require.NoError(t, i.Push(types.I32(7)))
		require.NoError(t, i.Push(types.I32(5)))
		require.NoError(t, i.jit(0))
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(12), v)

		jit := p.Snapshot().JIT
		require.Equal(t, uint64(1), jit.Attempts)
		require.NotZero(t, jit.Emits)
		require.NotZero(t, jit.Links)
	})

	t.Run("compiles numeric globals", func(t *testing.T) {
		if jit.Active() == nil {
			t.Skip("jit is not available on this architecture")
		}
		p := prof.New()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 9),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0),
		}), WithProfile(p), WithCutoff(1))
		defer i.Close()
		p.Add(0, 0, byte(instr.I32_CONST))
		i.globals = append(i.globals, types.BoxI32(1))
		require.NoError(t, i.jit(0))
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(9), v)
		jit := p.Snapshot().JIT
		require.Equal(t, uint64(1), jit.Attempts)
		require.NotZero(t, jit.Emits)
		require.NotZero(t, jit.Links)
	})

	t.Run("skips ref globals", func(t *testing.T) {
		if jit.Active() == nil {
			t.Skip("jit is not available on this architecture")
		}
		p := prof.New()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.GLOBAL_GET, 0),
		}), WithProfile(p), WithCutoff(1))
		defer i.Close()
		p.Add(0, 0, byte(instr.GLOBAL_GET))
		i.globals = append(i.globals, types.BoxedNull)
		require.NoError(t, i.jit(0))
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.Null, v)
		jit := p.Snapshot().JIT
		require.Equal(t, uint64(1), jit.Attempts)
		require.Zero(t, jit.Emits)
		require.Zero(t, jit.Links)
	})

	t.Run("executes compiled prefix before unsupported opcode", func(t *testing.T) {
		if jit.Active() == nil {
			t.Skip("jit is not available on this architecture")
		}
		p := prof.New()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 41),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.REF_NULL),
		}), WithProfile(p), WithCutoff(1))
		defer i.Close()
		p.Add(0, 0, byte(instr.I32_CONST))

		require.NoError(t, i.jit(0))
		require.Equal(t, uint64(1), p.Snapshot().JIT.Emits)
		require.NoError(t, i.Run(context.Background()))

		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.Null, value)
		value, err = i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), value)
	})

	t.Run("links branches", func(t *testing.T) {
		if jit.Active() == nil {
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

		cases := []struct {
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
		for _, tt := range cases {
			tt := tt
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

	t.Run("merges fallthrough block with internal entry", func(t *testing.T) {
		if jit.Active() == nil {
			t.Skip("jit is not available on this architecture")
		}

		code := []instr.Instruction{
			instr.New(instr.I32_CONST, 40),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.BR, 8),
			instr.New(instr.I32_CONST, 40),
			instr.New(instr.BR, uint64(uint16(1<<16-17))),
			instr.New(instr.NOP),
		}
		p := prof.New()
		p.Add(0, 0, byte(instr.I32_CONST))
		p.Add(0, 5, byte(instr.I32_CONST))
		p.Add(0, 5, byte(instr.I32_CONST))
		p.Add(0, 14, byte(instr.I32_CONST))
		i := New(program.New(code), WithProfile(p), WithCutoff(1))
		defer i.Close()

		require.NoError(t, i.jit(0))
		jit := p.Snapshot().JIT
		require.Equal(t, uint64(3), jit.Emits)
		require.Equal(t, uint64(4), jit.Links)

		i.fr.ip = 5
		require.NoError(t, i.Push(types.I32(40)))
		require.NoError(t, i.Run(context.Background()))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), value)

		i.fr.ip = 14
		require.NoError(t, i.Run(context.Background()))
		value, err = i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), value)
	})

	t.Run("keeps internal entry with cold predecessor", func(t *testing.T) {
		if jit.Active() == nil {
			t.Skip("jit is not available on this architecture")
		}

		code := []instr.Instruction{
			instr.New(instr.I32_CONST, 40),
			instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, 40),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		}
		p := prof.New()
		p.Add(0, 8, byte(instr.I32_CONST))
		i := New(program.New(code), WithProfile(p), WithCutoff(1))
		defer i.Close()

		require.NoError(t, i.jit(0))
		jit := p.Snapshot().JIT
		require.Equal(t, uint64(1), jit.Emits)
		require.Equal(t, uint64(2), jit.Links)

		require.NoError(t, i.Run(context.Background()))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), value)
	})

	t.Run("splits trace when internal entry rejects", func(t *testing.T) {
		if jit.Active() == nil {
			t.Skip("jit is not available on this architecture")
		}
		p := prof.New()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 42),
			instr.New(instr.REF_NULL),
			instr.New(instr.BR, 8),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.BR, uint64(uint16(1<<16-12))),
			instr.New(instr.NOP),
		}), WithProfile(p), WithCutoff(1))
		defer i.Close()
		p.Add(0, 0, byte(instr.I32_CONST))
		p.Add(0, 5, byte(instr.REF_NULL))
		p.Add(0, 9, byte(instr.I32_CONST))

		require.NoError(t, i.jit(0))
		require.NoError(t, i.Run(context.Background()))

		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.Null, value)
		value, err = i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), value)
	})

	t.Run("compiles forced successor after merged prefix rejects", func(t *testing.T) {
		if jit.Active() == nil {
			t.Skip("jit is not available on this architecture")
		}
		p := prof.New()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.NOP),
			instr.New(instr.REF_NULL),
			instr.New(instr.NOP),
			instr.New(instr.BR, 3),
			instr.New(instr.BR, uint64(uint16(1<<16-7))),
			instr.New(instr.NOP),
		}), WithProfile(p), WithCutoff(1))
		defer i.Close()
		p.Add(0, 0, byte(instr.NOP))

		require.NoError(t, i.jit(0))
		jit := p.Snapshot().JIT
		require.Equal(t, uint64(2), jit.Emits)
		require.Equal(t, uint64(2), jit.Links)
	})

	t.Run("skips cold segments", func(t *testing.T) {
		if jit.Active() == nil {
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
		require.NoError(t, i.jit(0))
		jit := p.Snapshot().JIT
		require.Equal(t, uint64(1), jit.Attempts)
		require.Equal(t, uint64(1), jit.Emits)
		require.Equal(t, uint64(1), jit.Links)
		require.Equal(t, uint64(1), jit.Skips)
	})

	t.Run("canceled execution", func(t *testing.T) {
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

func TestWithMarshaler(t *testing.T) {
	i := New(program.New(nil), WithMarshaler(&recordingMarshaler{}))
	defer i.Close()

	m, ok := i.marshaler.(*recordingMarshaler)
	require.True(t, ok)

	v, err := i.Marshal("ignored")
	require.NoError(t, err)
	require.Equal(t, types.I32(9), v)
	require.True(t, m.marshalCalled)

	var out int32
	require.NoError(t, i.Unmarshal(types.I32(1), &out))
	require.Equal(t, int32(12), out)
	require.True(t, m.unmarshalCalled)
}

func TestInterpreter_Marshal(t *testing.T) {
	t.Run("primitives", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		tests := []struct {
			in   any
			want types.Value
		}{
			{true, types.True},
			{int32(7), types.I32(7)},
			{int64(8), types.I64(8)},
			{float32(1.5), types.F32(1.5)},
			{float64(2.5), types.F64(2.5)},
			{"minivm", types.String("minivm")},
		}
		for _, tt := range tests {
			t.Run(fmt.Sprint(tt.in), func(t *testing.T) {
				got, err := i.Marshal(tt.in)
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("defined primitive with methods marshals as primitive", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(hostUserID(41))
		require.NoError(t, err)
		require.Equal(t, types.I64(41), got)
	})

	t.Run("defined primitive with methods uses primitive opcodes", func(t *testing.T) {
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_ADD),
			},
		))
		defer i.Close()

		got, err := i.Marshal(hostUserID(41))
		require.NoError(t, err)
		require.NoError(t, i.Push(got))

		require.NoError(t, i.Run(context.Background()))
		out, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I64(42), out)
	})

	t.Run("custom marshaler routes marshal calls", func(t *testing.T) {
		custom := &recordingMarshaler{}
		i := New(program.New(nil), WithMarshaler(custom))
		defer i.Close()

		_, err := i.Marshal(struct{ Count int32 }{Count: 1})
		require.NoError(t, err)
		require.True(t, custom.marshalCalled)
	})

	t.Run("unsigned primitives preserve raw bits", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(uint32(math.MaxUint32))
		require.NoError(t, err)
		require.Equal(t, types.I32(-1), got)

		got, err = i.Marshal(uint64(math.MaxUint64))
		require.NoError(t, err)
		require.Equal(t, types.I64(-1), got)
	})

	t.Run("nil pointer", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var p *int
		got, err := i.Marshal(p)
		require.NoError(t, err)
		require.Equal(t, types.Null, got)

		var id *hostUserID
		got, err = i.Marshal(id)
		require.NoError(t, err)
		require.Equal(t, types.Null, got)
	})

	t.Run("primitive slices", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal([]int32{1, 2})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int32]{1, 2}, got)

		got, err = i.Marshal([]uint32{math.MaxUint32})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int32]{-1}, got)

		got, err = i.Marshal([]int{1, 2})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int64]{1, 2}, got)

		got, err = i.Marshal([]uint64{math.MaxUint64})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int64]{-1}, got)

		got, err = i.Marshal([]int8{1, -1})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int8]{1, -1}, got)

		got, err = i.Marshal([]uint8{0x00, 0x7F, 0xFF})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int8]{0, 0x7F, -1}, got)

		got, err = i.Marshal([]byte{0xAB, 0xCD})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int8]{-0x55, -0x33}, got)
	})

	t.Run("reference slice", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal([]string{"a", "b"})
		require.NoError(t, err)

		arr, ok := got.(*types.Array)
		require.True(t, ok)
		require.True(t, arr.Typ.Elem.Equals(types.TypeString))
		require.Len(t, arr.Elems, 2)

		first, err := i.Load(arr.Elems[0].Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("a"), first)
	})

	t.Run("reference slice survives small heap", func(t *testing.T) {
		i := New(program.New(nil), WithHeap(1))
		defer i.Close()

		got, err := i.Marshal([]string{"a", "b"})
		require.NoError(t, err)

		arr, ok := got.(*types.Array)
		require.True(t, ok)
		require.Len(t, arr.Elems, 2)

		first, err := i.Load(arr.Elems[0].Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("a"), first)

		second, err := i.Load(arr.Elems[1].Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("b"), second)
	})

	t.Run("map", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(map[string]int32{"a": 1})
		require.NoError(t, err)

		m, ok := got.(*types.Map)
		require.True(t, ok)
		require.True(t, m.Typ.Key.Equals(types.TypeString))
		require.True(t, m.Typ.Elem.Equals(types.TypeI32))
		keyRef := types.Boxed(0)
		m.Range(func(_ types.MapKey, entry types.MapEntry) {
			keyRef = entry.Key
		})
		require.Equal(t, types.KindRef, keyRef.Kind())
		key, err := i.Load(keyRef.Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("a"), key)
		entry, ok := m.Get(types.MapKey{Kind: types.KindRef, Bits: uint64(keyRef.Ref())})
		require.True(t, ok)
		require.Equal(t, types.BoxI32(1), entry.Value)
	})

	t.Run("primitive keyed maps", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		i32, err := i.Marshal(map[int32]int32{1: 2})
		require.NoError(t, err)
		mI32, ok := i32.(*types.TypedMap[int32])
		require.True(t, ok)
		gotI32, ok := mI32.Get(1)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), gotI32)

		i64, err := i.Marshal(map[int64]string{1: "a"})
		require.NoError(t, err)
		mI64, ok := i64.(*types.TypedMap[int64])
		require.True(t, ok)
		gotI64, ok := mI64.Get(1)
		require.True(t, ok)
		str, err := i.Load(gotI64.Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("a"), str)

		f64, err := i.Marshal(map[float64]int32{math.Copysign(0, -1): 1})
		require.NoError(t, err)
		mF64, ok := f64.(*types.TypedMap[float64])
		require.True(t, ok)
		gotF64, ok := mF64.Get(0)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(1), gotF64)
	})

	t.Run("ref identity map keys", func(t *testing.T) {
		type key struct {
			ID int32
		}
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(map[key]int32{{ID: 1}: 2})
		require.NoError(t, err)

		m, ok := got.(*types.Map)
		require.True(t, ok)
		require.Equal(t, types.KindRef, m.Typ.KeyKind)

		var entry types.MapEntry
		m.Range(func(_ types.MapKey, e types.MapEntry) {
			entry = e
		})
		require.Equal(t, types.KindRef, entry.Key.Kind())
		require.Equal(t, types.BoxI32(2), entry.Value)
		loaded, err := i.Load(entry.Key.Ref())
		require.NoError(t, err)
		_, ok = loaded.(*types.Struct)
		require.True(t, ok)
	})

	t.Run("int map uses i64 value boxes", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(map[string]int{"a": 1})
		require.NoError(t, err)

		m, ok := got.(*types.Map)
		require.True(t, ok)
		var keyRef types.Boxed
		m.Range(func(_ types.MapKey, entry types.MapEntry) {
			keyRef = entry.Key
		})
		entry, ok := m.Get(types.MapKey{Kind: types.KindRef, Bits: uint64(keyRef.Ref())})
		require.True(t, ok)
		require.True(t, m.Typ.Elem.Equals(types.TypeI64))
		require.Equal(t, types.KindI64, entry.Value.Kind())
	})

	t.Run("struct exported fields", func(t *testing.T) {
		type sample struct {
			Name  string
			Count int32
		}
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(sample{Name: "go", Count: 3})
		require.NoError(t, err)

		s, ok := got.(*types.Struct)
		require.True(t, ok)
		require.Len(t, s.Typ.Fields, 2)
		require.Equal(t, "Name", s.Typ.Fields[0].Name)
		require.Equal(t, "Count", s.Typ.Fields[1].Name)
		require.True(t, s.Typ.Fields[0].Type.Equals(types.TypeString))
		require.True(t, s.Typ.Fields[1].Type.Equals(types.TypeI32))
		require.Equal(t, types.BoxI32(3), s.FieldByName("Count"))
	})

	t.Run("struct with private field routes to HostObject", func(t *testing.T) {
		type sample struct {
			Name   string
			Count  int32
			hidden int32
		}
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(sample{Name: "go", Count: 3, hidden: 9})
		require.NoError(t, err)

		ho, ok := got.(*HostObject)
		require.True(t, ok)
		require.Len(t, ho.Typ.Fields, 2)
		require.Equal(t, "Name", ho.Typ.Fields[0].Name)
		require.Equal(t, "Count", ho.Typ.Fields[1].Name)
		require.Equal(t, types.BoxI32(3), ho.Field(1))
	})

	t.Run("struct ref field", func(t *testing.T) {
		type sample struct {
			Ref types.Ref
		}
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(sample{Ref: types.Null})
		require.NoError(t, err)

		s, ok := got.(*types.Struct)
		require.True(t, ok)
		require.True(t, s.Typ.Fields[0].Type.Equals(types.TypeRef))
		require.Equal(t, types.BoxedNull, s.FieldByName("Ref"))
	})

	t.Run("struct value field allocates ref", func(t *testing.T) {
		type sample struct {
			Value types.Value
		}
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(sample{Value: types.I32(7)})
		require.NoError(t, err)

		s, ok := got.(*types.Struct)
		require.True(t, ok)
		require.True(t, s.Typ.Fields[0].Type.Equals(types.TypeRef))

		field := s.FieldByName("Value")
		require.Equal(t, types.KindRef, field.Kind())
		value, err := i.Load(field.Ref())
		require.NoError(t, err)
		require.Equal(t, types.I32(7), value)
	})

	t.Run("function", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		for _, f := range []any{
			func(v int32) (int32, error) { return v + 1, nil },
			func(v types.I32) types.I32 { return v + 1 },
		} {
			got, err := i.Marshal(f)
			require.NoError(t, err)
			fn, ok := got.(*HostFunction)
			require.True(t, ok)
			require.True(t, fn.Typ.Params[0].Equals(types.TypeI32))
			require.True(t, fn.Typ.Returns[0].Equals(types.TypeI32))
			returns, err := fn.Fn(i, []types.Boxed{types.BoxI32(4)})
			require.NoError(t, err)
			require.Equal(t, []types.Boxed{types.BoxI32(5)}, returns)
		}
	})

	t.Run("function error return", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(func() error {
			return errors.New("boom")
		})
		require.NoError(t, err)

		fn, ok := got.(*HostFunction)
		require.True(t, ok)
		require.Empty(t, fn.Typ.Params)
		require.Empty(t, fn.Typ.Returns)

		_, err = fn.Fn(i, nil)
		require.EqualError(t, err, "boom")
	})

	t.Run("unsigned function preserves raw bits", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(func(v uint64) uint64 {
			require.Equal(t, uint64(math.MaxUint64), v)
			return v
		})
		require.NoError(t, err)

		fn, ok := got.(*HostFunction)
		require.True(t, ok)
		require.True(t, fn.Typ.Params[0].Equals(types.TypeI64))
		require.True(t, fn.Typ.Returns[0].Equals(types.TypeI64))

		returns, err := fn.Fn(i, []types.Boxed{types.BoxI64(-1)})
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI64(-1)}, returns)
	})

	t.Run("boxed ref input", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("ref"))
		require.NoError(t, err)

		got, err := i.Marshal(types.BoxRef(addr))
		require.NoError(t, err)
		require.Equal(t, types.String("ref"), got)
	})

	t.Run("unsupported kind", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := i.Marshal(make(chan int))
		require.ErrorIs(t, err, ErrUnsupportedMarshalType)
	})

	t.Run("recursive struct pointer", func(t *testing.T) {
		type node struct{ Next *node }
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(node{})
		require.NoError(t, err)
		s, ok := got.(*types.Struct)
		require.True(t, ok)
		require.True(t, s.Typ.Fields[0].Type.Equals(types.TypeRef))
		require.Equal(t, types.BoxedNull, s.FieldByName("Next"))

		n := &node{}
		n.Next = n
		_, err = i.Marshal(n)
		require.ErrorIs(t, err, ErrMarshalCycle)
	})

	t.Run("shared pointer is not a cycle", func(t *testing.T) {
		type sample struct {
			First  *int32
			Second *int32
		}
		i := New(program.New(nil))
		defer i.Close()

		n := int32(7)
		got, err := i.Marshal(sample{First: &n, Second: &n})
		require.NoError(t, err)

		s, ok := got.(*types.Struct)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(7), s.FieldByName("First"))
		require.Equal(t, types.BoxI32(7), s.FieldByName("Second"))
	})

	t.Run("interface slice holds mixed dynamic kinds", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal([]any{int32(1), "x", float64(2.5)})
		require.NoError(t, err)

		arr, ok := got.(*types.Array)
		require.True(t, ok)
		require.True(t, arr.Typ.Elem.Equals(types.TypeRef))
		require.Len(t, arr.Elems, 3)
		for _, elem := range arr.Elems {
			require.Equal(t, types.KindRef, elem.Kind())
		}

		first, err := i.Load(arr.Elems[0].Ref())
		require.NoError(t, err)
		require.Equal(t, types.I32(1), first)
		second, err := i.Load(arr.Elems[1].Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("x"), second)
		third, err := i.Load(arr.Elems[2].Ref())
		require.NoError(t, err)
		require.Equal(t, types.F64(2.5), third)
	})

	t.Run("interface struct field marshals as ref", func(t *testing.T) {
		type box struct {
			Value any
			Tag   int32
		}
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(box{Value: "hi", Tag: 7})
		require.NoError(t, err)

		s, ok := got.(*types.Struct)
		require.True(t, ok)
		require.True(t, s.Typ.Fields[0].Type.Equals(types.TypeRef))
		require.Equal(t, types.BoxI32(7), s.FieldByName("Tag"))

		value := s.FieldByName("Value")
		require.Equal(t, types.KindRef, value.Kind())
		loaded, err := i.Load(value.Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("hi"), loaded)
	})

	t.Run("interface valued map marshals values as ref", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(map[string]any{"a": int32(1)})
		require.NoError(t, err)

		m, ok := got.(*types.Map)
		require.True(t, ok)
		require.True(t, m.Typ.Elem.Equals(types.TypeRef))

		var value types.Boxed
		m.Range(func(_ types.MapKey, entry types.MapEntry) {
			value = entry.Value
		})
		require.Equal(t, types.KindRef, value.Kind())
		loaded, err := i.Load(value.Ref())
		require.NoError(t, err)
		require.Equal(t, types.I32(1), loaded)
	})

	t.Run("nil interface marshals to null", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var v any
		got, err := i.Marshal([]any{v})
		require.NoError(t, err)
		arr, ok := got.(*types.Array)
		require.True(t, ok)
		require.Equal(t, types.BoxedNull, arr.Elems[0])
	})
}

func TestInterpreter_Unmarshal(t *testing.T) {
	t.Run("primitives", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var b bool
		require.NoError(t, i.Unmarshal(types.True, &b))
		require.True(t, b)

		var n int32
		require.NoError(t, i.Unmarshal(types.I32(7), &n))
		require.Equal(t, int32(7), n)
	})

	t.Run("unsigned primitives preserve raw bits", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var u32 uint32
		require.NoError(t, i.Unmarshal(types.I32(-1), &u32))
		require.Equal(t, uint32(math.MaxUint32), u32)

		var u64 uint64
		require.NoError(t, i.Unmarshal(types.I64(-1), &u64))
		require.Equal(t, uint64(math.MaxUint64), u64)

		var signed int64
		require.NoError(t, i.Unmarshal(types.I64(-1), &signed))
		require.Equal(t, int64(-1), signed)
	})

	t.Run("non nil pointer destination required", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.ErrorIs(t, i.Unmarshal(types.I32(1), nil), ErrInvalidUnmarshalTarget)
		var p *int32
		require.ErrorIs(t, i.Unmarshal(types.I32(1), p), ErrInvalidUnmarshalTarget)
	})

	t.Run("slice", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var out []int32
		require.NoError(t, i.Unmarshal(types.TypedArray[int32]{1, 2}, &out))
		require.Equal(t, []int32{1, 2}, out)

		var u32 []uint32
		require.NoError(t, i.Unmarshal(types.TypedArray[int32]{-1}, &u32))
		require.Equal(t, []uint32{math.MaxUint32}, u32)

		var u64 []uint64
		require.NoError(t, i.Unmarshal(types.TypedArray[int64]{-1}, &u64))
		require.Equal(t, []uint64{math.MaxUint64}, u64)

		var bs []byte
		require.NoError(t, i.Unmarshal(types.TypedArray[int8]{0x00, 0x7F, -1}, &bs))
		require.Equal(t, []byte{0x00, 0x7F, 0xFF}, bs)

		var i8s []int8
		require.NoError(t, i.Unmarshal(types.TypedArray[int8]{-1, 0x7F}, &i8s))
		require.Equal(t, []int8{-1, 0x7F}, i8s)
	})

	t.Run("map", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(map[string]int32{"a": 1})
		require.NoError(t, err)

		var out map[string]int32
		require.NoError(t, i.Unmarshal(got, &out))
		require.Equal(t, map[string]int32{"a": 1}, out)
	})

	t.Run("primitive keyed maps", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		in := map[int32]int32{1: 2}
		got, err := i.Marshal(in)
		require.NoError(t, err)
		var out map[int32]int32
		require.NoError(t, i.Unmarshal(got, &out))
		require.Equal(t, in, out)

		stringsByI64 := map[int64]string{1: "a"}
		got, err = i.Marshal(stringsByI64)
		require.NoError(t, err)
		var outStrings map[int64]string
		require.NoError(t, i.Unmarshal(got, &outStrings))
		require.Equal(t, stringsByI64, outStrings)

		floats := map[float64]int32{0: 1}
		got, err = i.Marshal(floats)
		require.NoError(t, err)
		var outFloats map[float64]int32
		require.NoError(t, i.Unmarshal(got, &outFloats))
		require.Equal(t, floats, outFloats)
	})

	t.Run("ref identity map keys", func(t *testing.T) {
		type key struct {
			ID int32
		}
		i := New(program.New(nil))
		defer i.Close()

		in := map[key]int32{{ID: 1}: 2}
		got, err := i.Marshal(in)
		require.NoError(t, err)
		var out map[key]int32
		require.NoError(t, i.Unmarshal(got, &out))
		require.Equal(t, in, out)
	})

	t.Run("struct matches by name", func(t *testing.T) {
		type sample struct {
			Count int32
			Name  string
		}
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(struct {
			Name  string
			Count int32
		}{Name: "go", Count: 3})
		require.NoError(t, err)

		var out sample
		require.NoError(t, i.Unmarshal(got, &out))
		require.Equal(t, sample{Name: "go", Count: 3}, out)
	})

	t.Run("pointer target", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var out *int32
		require.NoError(t, i.Unmarshal(types.I32(4), &out))
		require.NotNil(t, out)
		require.Equal(t, int32(4), *out)
	})

	t.Run("host object pointer target", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		id := hostUserID(41)
		got, err := i.Marshal(&id)
		require.NoError(t, err)

		ho, ok := got.(*HostObject)
		require.True(t, ok)
		ho.SetField(0, types.BoxI64(99))

		var out *hostUserID
		require.NoError(t, i.Unmarshal(got, &out))
		require.NotNil(t, out)
		require.Equal(t, hostUserID(99), *out)
	})

	t.Run("value target", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var out types.Value
		require.NoError(t, i.Unmarshal(types.I32(4), &out))
		require.Equal(t, types.I32(4), out)
	})

	t.Run("interface target yields vm-native value", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var n any
		require.NoError(t, i.Unmarshal(types.I32(42), &n))
		require.Equal(t, types.I32(42), n)

		var s any
		require.NoError(t, i.Unmarshal(types.String("hi"), &s))
		require.Equal(t, types.String("hi"), s)
	})

	t.Run("interface slice round-trip", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal([]any{int32(1), "x", float64(2.5)})
		require.NoError(t, err)

		var out []any
		require.NoError(t, i.Unmarshal(got, &out))
		require.Equal(t, []any{types.I32(1), types.String("x"), types.F64(2.5)}, out)
	})

	t.Run("error cases", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var n int8
		require.ErrorIs(t, i.Unmarshal(types.I32(128), &n), ErrValueOverflow)
		var m int32
		require.ErrorIs(t, i.Unmarshal(types.String("bad"), &m), ErrTypeMismatch)
		var u32 uint32
		require.ErrorIs(t, i.Unmarshal(types.I64(-1), &u32), ErrValueOverflow)
	})
}

func BenchmarkInterpreter_Run(b *testing.B) {
	b.Run("threaded", func(b *testing.B) {
		for _, tt := range tests {
			b.Run(tt.program.String(), func(b *testing.B) {
				ctx, cancel := context.WithCancel(context.TODO())
				defer cancel()

				i := New(tt.program, WithThreshold(-1))
				defer i.Close()

				b.ReportAllocs()
				b.ResetTimer()

				for n := 0; n < b.N; n++ {
					_ = i.Run(ctx)
					i.Reset()
				}
				b.StopTimer()
				require.NoError(b, i.Run(ctx))
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
				b.StopTimer()
				require.NoError(b, i.Run(ctx))
			})
		}
	})
}

func BenchmarkInterpreter_Alloc(b *testing.B) {
	b.Run("free slot reuse", func(b *testing.B) {
		i := New(program.New(nil), WithHeap(2))
		defer i.Close()

		b.ReportAllocs()
		b.ResetTimer()

		var err error
		for n := 0; n < b.N; n++ {
			var addr int
			addr, err = i.Alloc(types.I32(1))
			if err != nil {
				break
			}
			err = i.Release(addr)
			if err != nil {
				break
			}
		}
		b.StopTimer()
		require.NoError(b, err)
	})

	b.Run("small heap cyclic gc", func(b *testing.B) {
		i := New(program.New(nil), WithHeap(2))
		defer i.Close()
		typ := types.NewArrayType(types.TypeRef)

		b.ReportAllocs()
		b.ResetTimer()

		var err error
		for n := 0; n < b.N; n++ {
			array := types.NewArray(typ)
			var addr int
			addr, err = i.Alloc(array)
			if err != nil {
				break
			}
			array.Elems = append(array.Elems, types.BoxRef(addr))
			_, err = i.Retain(addr)
			if err != nil {
				break
			}
			err = i.Release(addr)
			if err != nil {
				break
			}

			var leaf int
			leaf, err = i.Alloc(types.I32(1))
			if err != nil {
				break
			}
			err = i.Release(leaf)
			if err != nil {
				break
			}
		}
		b.StopTimer()
		require.NoError(b, err)
	})
}

func BenchmarkInterpreter_Release(b *testing.B) {
	b.Run("primitive struct", func(b *testing.B) {
		i := New(program.New(nil), WithHeap(2))
		defer i.Close()
		typ := types.NewStructType(types.NewStructField(types.TypeI32))

		b.ReportAllocs()
		b.ResetTimer()

		var err error
		for n := 0; n < b.N; n++ {
			var addr int
			addr, err = i.Alloc(types.NewStruct(typ))
			if err != nil {
				break
			}
			err = i.Release(addr)
			if err != nil {
				break
			}
		}
		b.StopTimer()
		require.NoError(b, err)
	})

	b.Run("ref array", func(b *testing.B) {
		i := New(program.New(nil), WithHeap(3))
		defer i.Close()
		typ := types.NewArrayType(types.TypeRef)

		b.ReportAllocs()
		b.ResetTimer()

		var err error
		for n := 0; n < b.N; n++ {
			var child int
			child, err = i.Alloc(types.I32(1))
			if err != nil {
				break
			}
			var addr int
			addr, err = i.Alloc(types.NewArray(typ, types.BoxRef(child)))
			if err != nil {
				break
			}
			err = i.Release(addr)
			if err != nil {
				break
			}
		}
		b.StopTimer()
		require.NoError(b, err)
	})

	b.Run("ref struct", func(b *testing.B) {
		i := New(program.New(nil), WithHeap(3))
		defer i.Close()
		typ := types.NewStructType(types.NewStructField(types.TypeRef))

		b.ReportAllocs()
		b.ResetTimer()

		var err error
		for n := 0; n < b.N; n++ {
			var child int
			child, err = i.Alloc(types.I32(1))
			if err != nil {
				break
			}
			var addr int
			addr, err = i.Alloc(types.NewStruct(typ, types.BoxRef(child)))
			if err != nil {
				break
			}
			err = i.Release(addr)
			if err != nil {
				break
			}
		}
		b.StopTimer()
		require.NoError(b, err)
	})

	b.Run("ref valued map", func(b *testing.B) {
		i := New(program.New(nil), WithHeap(3))
		defer i.Close()
		typ := types.NewMapType(types.TypeI32, types.TypeRef)

		b.ReportAllocs()
		b.ResetTimer()

		var err error
		for n := 0; n < b.N; n++ {
			var child int
			child, err = i.Alloc(types.I32(1))
			if err != nil {
				break
			}
			m := types.NewTypedMap[int32](typ, 1)
			m.Set(1, types.BoxRef(child))
			var addr int
			addr, err = i.Alloc(m)
			if err != nil {
				break
			}
			err = i.Release(addr)
			if err != nil {
				break
			}
		}
		b.StopTimer()
		require.NoError(b, err)
	})
}

func BenchmarkInterpreter_Marshal(b *testing.B) {
	type plainStruct struct {
		Name  string
		Count int32
		Ratio float64
	}
	cases := []struct {
		name  string
		value any
	}{
		{"i32", int32(42)},
		{"string", "hello"},
		{"slice_i32", []int32{1, 2, 3, 4, 5, 6, 7, 8}},
		{"map_string_i32", map[string]int32{"a": 1, "b": 2, "c": 3, "d": 4}},
		{"struct_plain", plainStruct{Name: "n", Count: 7, Ratio: 1.5}},
		{"host_object", hostCounter{Count: 1}},
		{"function", func(a, b int32) int32 { return a + b }},
		{"nested_slice_struct", []plainStruct{{Name: "a"}, {Name: "b"}, {Name: "c"}}},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			i := New(program.New(nil))
			defer i.Close()
			b.ReportAllocs()
			b.ResetTimer()
			var err error
			for n := 0; n < b.N; n++ {
				_, err = i.Marshal(c.value)
				if err != nil {
					break
				}
			}
			b.StopTimer()
			require.NoError(b, err)
		})
	}
}

func BenchmarkInterpreter_Unmarshal(b *testing.B) {
	type plainStruct struct {
		Name  string
		Count int32
		Ratio float64
	}
	cases := []struct {
		name string
		src  any
		dst  func() any
	}{
		{"i32", int32(42), func() any { return new(int32) }},
		{"string", "hello", func() any { return new(string) }},
		{"slice_i32", []int32{1, 2, 3, 4, 5, 6, 7, 8}, func() any { return new([]int32) }},
		{"map_string_i32", map[string]int32{"a": 1, "b": 2, "c": 3, "d": 4}, func() any { return new(map[string]int32) }},
		{"struct_plain", plainStruct{Name: "n", Count: 7, Ratio: 1.5}, func() any { return new(plainStruct) }},
		{"host_object", hostCounter{Count: 1}, func() any { return new(hostCounter) }},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			i := New(program.New(nil))
			defer i.Close()
			val, err := i.Marshal(c.src)
			require.NoError(b, err)
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				err = i.Unmarshal(val, c.dst())
				if err != nil {
					break
				}
			}
			b.StopTimer()
			require.NoError(b, err)
		})
	}
}
