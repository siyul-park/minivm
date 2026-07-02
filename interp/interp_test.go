package interp

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type upperMarshaler struct{}

func (upperMarshaler) Marshal(_ *Interpreter, v any) (types.Value, error) {
	s, ok := v.(string)
	if !ok {
		return nil, ErrUnsupportedMarshalType
	}
	return types.String(strings.ToUpper(s)), nil
}

func (upperMarshaler) Unmarshal(_ *Interpreter, v types.Value, dst any) error {
	s, ok := v.(types.String)
	if !ok {
		return ErrInvalidUnmarshalTarget
	}
	p, ok := dst.(*string)
	if !ok {
		return ErrInvalidUnmarshalTarget
	}
	*p = strings.ToLower(string(s))
	return nil
}

var runTests = []struct {
	program *program.Program
	values  []types.Value
	err     error
}{
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.NOP)}),
		values:  []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.UNREACHABLE)}),
		err:     ErrUnreachableExecuted,
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.DROP)}),
		values:  []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 7), instr.New(instr.DUP)}),
		values:  []types.Value{types.I32(7), types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.SWAP)}),
		values:  []types.Value{types.I32(1), types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 1), instr.New(instr.SELECT),
		}),
		values: []types.Value{types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, 999),
			instr.New(instr.I32_CONST, 1),
		}),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.BR_IF, 5),
			instr.New(instr.I32_CONST, 999),
			instr.New(instr.I32_CONST, 1),
		}),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.BR_TABLE, 1, 5, 0),
			instr.New(instr.I32_CONST, 999),
			instr.New(instr.I32_CONST, 1),
		}),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32, types.TypeI32}}).
			Emit(instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 20), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(20), types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.RETURN_CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(6)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.YIELD)}),
		err:     ErrYield,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.I32_CONST, 41),
			instr.New(instr.RESUME),
			instr.New(instr.CORO_VALUE),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.CORO_DONE),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.CORO_VALUE),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.GLOBAL_SET, 0), instr.New(instr.GLOBAL_GET, 0),
		}),
		values: []types.Value{types.I32(3)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 4), instr.New(instr.GLOBAL_SET, 0), instr.New(instr.GLOBAL_GET, 0),
		}),
		values: []types.Value{types.I32(4)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 6), instr.New(instr.GLOBAL_TEE, 0)}),
		values:  []types.Value{types.I32(6)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.LOCAL_SET, 0), instr.New(instr.LOCAL_GET, 0),
		}, program.WithLocals(types.TypeI32)),
		values: []types.Value{types.I32(5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.LOCAL_SET, 0), instr.New(instr.LOCAL_GET, 0),
		}, program.WithLocals(types.TypeI32)),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 9), instr.New(instr.LOCAL_TEE, 0)}, program.WithLocals(types.TypeI32)),
		values:  []types.Value{types.I32(9)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0)}, program.WithConstants(types.I32(11))),
		values:  []types.Value{types.I32(11)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			WithCaptures(types.TypeI32).Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			WithCaptures(types.TypeI32).Emit(
			instr.New(instr.I32_CONST, 99), instr.New(instr.UPVAL_SET, 0), instr.New(instr.UPVAL_GET, 0), instr.New(instr.RETURN),
		).MustBuild())),
		values: []types.Value{types.I32(99)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.REF_NULL)}),
		values:  []types.Value{types.Null},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.REF_NEW)}),
		values:  []types.Value{types.I32(5)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 9), instr.New(instr.REF_NEW), instr.New(instr.REF_GET)}),
		values:  []types.Value{types.I32(9)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 77), instr.New(instr.REF_SET),
			instr.New(instr.REF_GET),
		}),
		values: []types.Value{types.I32(77)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.REF_TEST, 0)}, program.WithTypes(types.TypeI32)),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.REF_CAST, 0)}, program.WithTypes(types.TypeI32)),
		values:  []types.Value{types.I32(5)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.REF_NULL), instr.New(instr.REF_IS_NULL)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.REF_NULL), instr.New(instr.REF_NULL), instr.New(instr.REF_EQ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.REF_NULL), instr.New(instr.I32_CONST, 5), instr.New(instr.REF_NEW), instr.New(instr.REF_NE)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 42)}),
		values:  []types.Value{types.I32(42)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_ADD)}),
		values:  []types.Value{types.I32(5)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_SUB)}),
		values:  []types.Value{types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 4), instr.New(instr.I32_MUL)}),
		values:  []types.Value{types.I32(12)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-7)), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_DIV_S),
		}),
		values: []types.Value{types.I32(-3)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_DIV_U),
		}),
		values: []types.Value{types.I32(int32(uint32(math.MaxUint32) / 2))},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-7)), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_REM_S),
		}),
		values: []types.Value{types.I32(-1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_REM_U),
		}),
		values: []types.Value{types.I32(int32(uint32(math.MaxUint32) % 3))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_SHL)}),
		values:  []types.Value{types.I32(8)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-8)), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SHR_S),
		}),
		values: []types.Value{types.I32(-4)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SHR_U),
		}),
		values: []types.Value{types.I32(int32(uint32(math.MaxUint32) >> 1))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 12), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_AND)}),
		values:  []types.Value{types.I32(8)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 12), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_OR)}),
		values:  []types.Value{types.I32(14)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 12), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_XOR)}),
		values:  []types.Value{types.I32(6)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CLZ)}),
		values:  []types.Value{types.I32(31)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 8), instr.New(instr.I32_CTZ)}),
		values:  []types.Value{types.I32(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 7), instr.New(instr.I32_POPCNT)}),
		values:  []types.Value{types.I32(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 4), instr.New(instr.I32_ROTL)}),
		values:  []types.Value{types.I32(16)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 16), instr.New(instr.I32_CONST, 4), instr.New(instr.I32_ROTR)}),
		values:  []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 255), instr.New(instr.I32_EXTEND8_S)}),
		values:  []types.Value{types.I32(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 65535), instr.New(instr.I32_EXTEND16_S)}),
		values:  []types.Value{types.I32(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.I32_EQZ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 5), instr.New(instr.I32_EQ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 6), instr.New(instr.I32_NE)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LT_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LT_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_GT_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_GT_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_LE_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_GE_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_GE_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_TO_I64_S)}),
		values:  []types.Value{types.I64(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_TO_I64_U)}),
		values:  []types.Value{types.I64(int64(uint32(math.MaxUint32)))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_TO_F32_S)}),
		values:  []types.Value{types.F32(float32(int32(-1)))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_TO_F32_U)}),
		values:  []types.Value{types.F32(float32(uint32(math.MaxUint32)))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_TO_F64_S)}),
		values:  []types.Value{types.F64(float64(int32(-1)))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_TO_F64_U)}),
		values:  []types.Value{types.F64(float64(uint32(math.MaxUint32)))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(1))), instr.New(instr.I32_REINTERPRET_F32)}),
		values:  []types.Value{types.I32(int32(math.Float32bits(1)))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 42)}),
		values:  []types.Value{types.I64(42)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 2), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_ADD)}),
		values:  []types.Value{types.I64(5)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 5), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_SUB)}),
		values:  []types.Value{types.I64(2)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 3), instr.New(instr.I64_CONST, 4), instr.New(instr.I64_MUL)}),
		values:  []types.Value{types.I64(12)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(-7)), instr.New(instr.I64_CONST, 2), instr.New(instr.I64_DIV_S),
		}),
		values: []types.Value{types.I64(-3)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, 2), instr.New(instr.I64_DIV_U),
		}),
		values: []types.Value{types.I64(int64(uint64(math.MaxUint64) / 2))},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(-7)), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_REM_S),
		}),
		values: []types.Value{types.I64(-1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_REM_U),
		}),
		values: []types.Value{types.I64(int64(uint64(math.MaxUint64) % 3))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 1), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_SHL)}),
		values:  []types.Value{types.I64(8)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(-8)), instr.New(instr.I64_CONST, 1), instr.New(instr.I64_SHR_S),
		}),
		values: []types.Value{types.I64(-4)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, 1), instr.New(instr.I64_SHR_U),
		}),
		values: []types.Value{types.I64(int64(uint64(math.MaxUint64) >> 1))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 12), instr.New(instr.I64_CONST, 10), instr.New(instr.I64_XOR)}),
		values:  []types.Value{types.I64(6)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 12), instr.New(instr.I64_CONST, 10), instr.New(instr.I64_AND)}),
		values:  []types.Value{types.I64(8)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 12), instr.New(instr.I64_CONST, 10), instr.New(instr.I64_OR)}),
		values:  []types.Value{types.I64(14)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 1), instr.New(instr.I64_CLZ)}),
		values:  []types.Value{types.I64(63)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 8), instr.New(instr.I64_CTZ)}),
		values:  []types.Value{types.I64(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 7), instr.New(instr.I64_POPCNT)}),
		values:  []types.Value{types.I64(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 1), instr.New(instr.I64_CONST, 4), instr.New(instr.I64_ROTL)}),
		values:  []types.Value{types.I64(16)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 16), instr.New(instr.I64_CONST, 4), instr.New(instr.I64_ROTR)}),
		values:  []types.Value{types.I64(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 255), instr.New(instr.I64_EXTEND8_S)}),
		values:  []types.Value{types.I64(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 65535), instr.New(instr.I64_EXTEND16_S)}),
		values:  []types.Value{types.I64(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, uint64(uint32(math.MaxUint32))), instr.New(instr.I64_EXTEND32_S)}),
		values:  []types.Value{types.I64(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 0), instr.New(instr.I64_EQZ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 5), instr.New(instr.I64_CONST, 5), instr.New(instr.I64_EQ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 5), instr.New(instr.I64_CONST, 6), instr.New(instr.I64_NE)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, 0), instr.New(instr.I64_LT_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, 0), instr.New(instr.I64_LT_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 0), instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_GT_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 0), instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_GT_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_LE_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, 0), instr.New(instr.I64_LE_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 0), instr.New(instr.I64_CONST, 0), instr.New(instr.I64_GE_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 0), instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_GE_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, uint64(int64(1)<<32+1)), instr.New(instr.I64_TO_I32)}),
		values:  []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_TO_F32_S)}),
		values:  []types.Value{types.F32(float32(int64(-1)))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_TO_F32_U)}),
		values:  []types.Value{types.F32(float32(uint64(math.MaxUint64)))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_TO_F64_S)}),
		values:  []types.Value{types.F64(float64(int64(-1)))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_TO_F64_U)}),
		values:  []types.Value{types.F64(float64(uint64(math.MaxUint64)))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(1)), instr.New(instr.I64_REINTERPRET_F64)}),
		values:  []types.Value{types.I64(int64(math.Float64bits(1)))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5)))}),
		values:  []types.Value{types.F32(1.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2.25))), instr.New(instr.F32_ADD),
		}),
		values: []types.Value{types.F32(3.75)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(5.5))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2.25))), instr.New(instr.F32_SUB),
		}),
		values: []types.Value{types.F32(3.25)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2.5))), instr.New(instr.F32_CONST, uint64(math.Float32bits(4))), instr.New(instr.F32_MUL),
		}),
		values: []types.Value{types.F32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(10))), instr.New(instr.F32_CONST, uint64(math.Float32bits(4))), instr.New(instr.F32_DIV),
		}),
		values: []types.Value{types.F32(2.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(-7))), instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_REM),
		}),
		values: []types.Value{types.F32(-1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(-7))), instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_MOD),
		}),
		values: []types.Value{types.F32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1))), instr.New(instr.F32_CONST, 0), instr.New(instr.F32_REM),
		}),
		err: ErrDivideByZero,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1))), instr.New(instr.F32_CONST, 0), instr.New(instr.F32_MOD),
		}),
		err: ErrDivideByZero,
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(-3.5))), instr.New(instr.F32_ABS)}),
		values:  []types.Value{types.F32(3.5)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(3.5))), instr.New(instr.F32_NEG)}),
		values:  []types.Value{types.F32(-3.5)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(9))), instr.New(instr.F32_SQRT)}),
		values:  []types.Value{types.F32(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(1.2))), instr.New(instr.F32_CEIL)}),
		values:  []types.Value{types.F32(2)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(1.8))), instr.New(instr.F32_FLOOR)}),
		values:  []types.Value{types.F32(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(-1.8))), instr.New(instr.F32_TRUNC)}),
		values:  []types.Value{types.F32(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(2.5))), instr.New(instr.F32_NEAREST)}),
		values:  []types.Value{types.F32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_CONST, uint64(math.Float32bits(5))), instr.New(instr.F32_MIN),
		}),
		values: []types.Value{types.F32(3)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_CONST, uint64(math.Float32bits(5))), instr.New(instr.F32_MAX),
		}),
		values: []types.Value{types.F32(5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_CONST, uint64(math.Float32bits(-1))), instr.New(instr.F32_COPYSIGN),
		}),
		values: []types.Value{types.F32(-3)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_EQ),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_NE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_LT),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_GT),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_LE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_GE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(-3.7))), instr.New(instr.F32_TO_I32_S)}),
		values:  []types.Value{types.I32(-3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(3.7))), instr.New(instr.F32_TO_I32_U)}),
		values:  []types.Value{types.I32(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(-3.7))), instr.New(instr.F32_TO_I64_S)}),
		values:  []types.Value{types.I64(-3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(3.7))), instr.New(instr.F32_TO_I64_U)}),
		values:  []types.Value{types.I64(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))), instr.New(instr.F32_TO_F64)}),
		values:  []types.Value{types.F64(float64(float32(1.5)))},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, uint64(math.Float32bits(1))), instr.New(instr.F32_REINTERPRET_I32)}),
		values:  []types.Value{types.F32(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(2.5))}),
		values:  []types.Value{types.F64(2.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(1.5)), instr.New(instr.F64_CONST, math.Float64bits(2.25)), instr.New(instr.F64_ADD),
		}),
		values: []types.Value{types.F64(3.75)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(5.5)), instr.New(instr.F64_CONST, math.Float64bits(2.25)), instr.New(instr.F64_SUB),
		}),
		values: []types.Value{types.F64(3.25)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.F64_CONST, math.Float64bits(4)), instr.New(instr.F64_MUL),
		}),
		values: []types.Value{types.F64(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(10)), instr.New(instr.F64_CONST, math.Float64bits(4)), instr.New(instr.F64_DIV),
		}),
		values: []types.Value{types.F64(2.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(-7)), instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_REM),
		}),
		values: []types.Value{types.F64(-1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(-7)), instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_MOD),
		}),
		values: []types.Value{types.F64(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(1)), instr.New(instr.F64_CONST, 0), instr.New(instr.F64_REM),
		}),
		err: ErrDivideByZero,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(1)), instr.New(instr.F64_CONST, 0), instr.New(instr.F64_MOD),
		}),
		err: ErrDivideByZero,
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(-3.5)), instr.New(instr.F64_ABS)}),
		values:  []types.Value{types.F64(3.5)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(3.5)), instr.New(instr.F64_NEG)}),
		values:  []types.Value{types.F64(-3.5)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(9)), instr.New(instr.F64_SQRT)}),
		values:  []types.Value{types.F64(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(1.2)), instr.New(instr.F64_CEIL)}),
		values:  []types.Value{types.F64(2)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(1.8)), instr.New(instr.F64_FLOOR)}),
		values:  []types.Value{types.F64(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(-1.8)), instr.New(instr.F64_TRUNC)}),
		values:  []types.Value{types.F64(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.F64_NEAREST)}),
		values:  []types.Value{types.F64(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_CONST, math.Float64bits(5)), instr.New(instr.F64_MIN),
		}),
		values: []types.Value{types.F64(3)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_CONST, math.Float64bits(5)), instr.New(instr.F64_MAX),
		}),
		values: []types.Value{types.F64(5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_CONST, math.Float64bits(-1)), instr.New(instr.F64_COPYSIGN),
		}),
		values: []types.Value{types.F64(-3)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_EQ),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_NE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_LT),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_GT),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_LE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_GE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(-3.7)), instr.New(instr.F64_TO_I32_S)}),
		values:  []types.Value{types.I32(-3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(3.7)), instr.New(instr.F64_TO_I32_U)}),
		values:  []types.Value{types.I32(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(-3.7)), instr.New(instr.F64_TO_I64_S)}),
		values:  []types.Value{types.I64(-3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(3.7)), instr.New(instr.F64_TO_I64_U)}),
		values:  []types.Value{types.I64(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(1.5)), instr.New(instr.F64_TO_F32)}),
		values:  []types.Value{types.F32(1.5)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, math.Float64bits(1)), instr.New(instr.F64_REINTERPRET_I64)}),
		values:  []types.Value{types.F64(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 72), instr.New(instr.I32_CONST, 105), instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.STRING_NEW_UTF32),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.String("Hi")},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.STRING_LEN)}, program.WithConstants(types.String("Hi"))),
		values:  []types.Value{types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_CONCAT)},
			program.WithConstants(types.String("Hi"), types.String("There"))),
		values: []types.Value{types.String("HiThere")},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_EQ)},
			program.WithConstants(types.String("Go"), types.String("Go"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_NE)},
			program.WithConstants(types.String("Go"), types.String("No"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_LT)},
			program.WithConstants(types.String("Go"), types.String("No"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_GT)},
			program.WithConstants(types.String("No"), types.String("Go"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_LE)},
			program.WithConstants(types.String("Go"), types.String("Go"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_GE)},
			program.WithConstants(types.String("Go"), types.String("Go"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.STRING_ENCODE_UTF32)}, program.WithConstants(types.String("Hi"))),
		values:  []types.Value{types.TypedArray[int32]{72, 105}},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 30), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW, 0),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.TypedArray[int32]{10, 20, 30}},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW_DEFAULT, 0)}, program.WithTypes(types.TypeI32Array)),
		values:  []types.Value{types.TypedArray[int32]{0, 0, 0}},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.ARRAY_LEN),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 100), instr.New(instr.I32_CONST, 200), instr.New(instr.I32_CONST, 300), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.I32(200)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 99), instr.New(instr.ARRAY_SET),
			instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.I32(99)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 7), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_FILL),
			instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 9), instr.New(instr.I32_CONST, 8), instr.New(instr.I32_CONST, 7), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_COPY),
			instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.I32(9)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 4), instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_APPEND),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.TypedArray[int32]{1, 2, 3, 4}},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_DELETE),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 30), instr.New(instr.I32_CONST, 40), instr.New(instr.I32_CONST, 4), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_SLICE),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.TypedArray[int32]{20, 30}},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.STRUCT_NEW, 0),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)))),
		values: []types.Value{types.NewStruct(
			types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)),
			types.BoxI32(7), types.BoxF64(2.5),
		)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.STRUCT_NEW_DEFAULT, 0)},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)))),
		values: []types.Value{types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)))},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.STRUCT_NEW, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.STRUCT_GET),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)))),
		values: []types.Value{types.F64(2.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.STRUCT_NEW, 0),
			instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 99), instr.New(instr.STRUCT_SET),
			instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)))),
		values: []types.Value{types.I32(99)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 2), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 4), instr.New(instr.MAP_NEW_DEFAULT, 0),
			instr.New(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 2), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.I32_CONST, 2), instr.New(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_LOOKUP),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I1(true), types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 20), instr.New(instr.MAP_SET),
			instr.New(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 2), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_DELETE),
			instr.New(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.DUP),
			instr.New(instr.MAP_CLEAR),
			instr.New(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 2), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.MAP_KEYS), instr.New(instr.ARRAY_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.MAP_ITER), instr.New(instr.CORO_VALUE),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 99),
			instr.New(instr.THROW),
			instr.New(instr.I32_CONST, 0),
		}, program.WithHandlers(instr.Handler{Start: 0, End: 6, Catch: 11, Depth: 0})),
		values: []types.Value{types.I32(99)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 7), instr.New(instr.ERROR_NEW)}),
		values:  []types.Value{types.NewError(types.ErrorCode(7), "5", types.BoxI32(5))},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 7), instr.New(instr.ERROR_NEW), instr.New(instr.ERROR_GET),
		}),
		values: []types.Value{types.I32(5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 7), instr.New(instr.ERROR_NEW), instr.New(instr.ERROR_CODE),
		}),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.STRING_ITER), instr.New(instr.CORO_VALUE)}, program.WithConstants(types.String("Hi"))),
		values:  []types.Value{types.I32(72)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.ARRAY_GET),
		}, program.WithConstants(types.TypedArray[int32]{3, 5})),
		values: []types.Value{types.I32(5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.STRUCT_GET),
		}, program.WithConstants(types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32)), types.BoxI32(7)))),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.REF_GET),
		}, program.WithConstants(types.I64(math.MaxInt64))),
		values: []types.Value{types.I64(math.MaxInt64)},
	},
}

func TestInterpreter_Run(t *testing.T) {
	for _, tt := range runTests {
		t.Run(fmt.Sprint(tt.program), func(t *testing.T) {
			i := New(tt.program)
			defer i.Close()

			err := i.Run(context.Background())
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			for _, want := range tt.values {
				got, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
		})
	}

	t.Run("entry frame yield resumes on the next Run call", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.YIELD),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		})
		i := New(prog)
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrYield)
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(3), v)
	})
}

func TestInterpreter_Marshal(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()

	v, err := i.Marshal(int32(7))
	require.NoError(t, err)
	require.Equal(t, types.I32(7), v)
}

func TestInterpreter_Unmarshal(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()

	var dst int32
	require.NoError(t, i.Unmarshal(types.I32(7), &dst))
	require.Equal(t, int32(7), dst)
}

func TestInterpreter_Context(t *testing.T) {
	var got context.Context
	prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
	i := New(prog, WithTick(1), WithHook(func(i *Interpreter) error {
		got = i.Context()
		return nil
	}))
	defer i.Close()

	ctx := context.Background()
	require.NoError(t, i.Run(ctx))
	require.Equal(t, ctx, got)
}

func TestInterpreter_Func(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.NOP)})
	i := New(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), ErrYield)
	require.Equal(t, 0, i.Func())
}

func TestInterpreter_IP(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.NOP)})
	i := New(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), ErrYield)
	require.Equal(t, 6, i.IP())
}

func TestInterpreter_FP(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.NOP)})
	i := New(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), ErrYield)
	require.Equal(t, 1, i.FP())
}

func TestInterpreter_Opcode(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.NOP)})
	i := New(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), ErrYield)
	op, err := i.Opcode()
	require.NoError(t, err)
	require.Equal(t, instr.NOP, op)
}

func TestInterpreter_Frame(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.NOP)})
	i := New(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), ErrYield)
	fn, ip, bp, err := i.Frame(0)
	require.NoError(t, err)
	require.Equal(t, 0, fn)
	require.Equal(t, 6, ip)
	require.Equal(t, 0, bp)
}

func TestInterpreter_Const(t *testing.T) {
	i := New(program.New(nil, program.WithConstants(types.I32(9))))
	defer i.Close()

	v, err := i.Const(0)
	require.NoError(t, err)
	require.Equal(t, types.BoxI32(9), v)
}

func TestInterpreter_Global(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 4), instr.New(instr.GLOBAL_SET, 0)})
	i := New(prog)
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	v, err := i.Global(0)
	require.NoError(t, err)
	require.Equal(t, types.BoxI32(4), v)
}

func TestInterpreter_SetGlobal(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.GLOBAL_SET, 0)})
	i := New(prog)
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	require.NoError(t, i.SetGlobal(0, types.BoxI32(8)))
	v, err := i.Global(0)
	require.NoError(t, err)
	require.Equal(t, types.BoxI32(8), v)
}

func TestInterpreter_Local(t *testing.T) {
	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 6), instr.New(instr.LOCAL_SET, 0), instr.New(instr.YIELD),
	}, program.WithLocals(types.TypeI32))
	i := New(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), ErrYield)
	v, err := i.Local(0)
	require.NoError(t, err)
	require.Equal(t, types.BoxI32(6), v)
}

func TestInterpreter_SetLocal(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.YIELD)}, program.WithLocals(types.TypeI32))
	i := New(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), ErrYield)
	require.NoError(t, i.SetLocal(0, types.BoxI32(3)))
	v, err := i.Local(0)
	require.NoError(t, err)
	require.Equal(t, types.BoxI32(3), v)
}

func TestInterpreter_Load(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()

	addr, err := i.Alloc(types.I32(5))
	require.NoError(t, err)
	v, err := i.Load(addr)
	require.NoError(t, err)
	require.Equal(t, types.I32(5), v)
}

func TestInterpreter_Store(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()

	addr, err := i.Alloc(types.I32(5))
	require.NoError(t, err)
	require.NoError(t, i.Store(addr, types.I32(9)))
	v, err := i.Load(addr)
	require.NoError(t, err)
	require.Equal(t, types.I32(9), v)
}

func TestInterpreter_Alloc(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()

	addr, err := i.Alloc(types.String("hi"))
	require.NoError(t, err)
	v, err := i.Load(addr)
	require.NoError(t, err)
	require.Equal(t, types.String("hi"), v)
}

func TestInterpreter_Retain(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()

	addr, err := i.Alloc(types.String("hi"))
	require.NoError(t, err)
	v, err := i.Retain(addr)
	require.NoError(t, err)
	require.Equal(t, types.String("hi"), v)
	require.NoError(t, i.Release(addr))
	require.NoError(t, i.Release(addr))
}

func TestInterpreter_Release(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()

	addr, err := i.Alloc(types.String("hi"))
	require.NoError(t, err)
	require.NoError(t, i.Release(addr))
	_, err = i.Load(addr)
	require.ErrorIs(t, err, ErrSegmentationFault)
}

func TestInterpreter_Push(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()

	require.NoError(t, i.Push(types.I32(4)))
	require.Equal(t, 1, i.Len())
}

func TestInterpreter_Pop(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()

	require.NoError(t, i.Push(types.I32(4)))
	v, err := i.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(4), v)
}

func TestInterpreter_Peek(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()

	require.NoError(t, i.Push(types.I32(4)))
	v, err := i.Peek(0)
	require.NoError(t, err)
	require.Equal(t, types.BoxI32(4), v)
	require.Equal(t, 1, i.Len())
}

func TestInterpreter_Len(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()

	require.Equal(t, 0, i.Len())
	require.NoError(t, i.Push(types.I32(1)))
	require.Equal(t, 1, i.Len())
}

func TestInterpreter_Close(t *testing.T) {
	i := New(program.New(nil))
	require.NoError(t, i.Close())
}

func TestInterpreter_Reset(t *testing.T) {
	t.Run("clears pushed values", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(1)))
		i.Reset()
		require.Equal(t, 0, i.Len())
	})

	t.Run("restarts module after unpopped result", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 7)})
		i := New(prog)
		defer i.Close()

		for range 64 {
			require.NoError(t, i.Run(context.Background()))
			require.Equal(t, 1, i.Len())
			i.Reset()
			require.Equal(t, 0, i.frames[0].bp)
			require.Equal(t, 0, i.frames[0].ip)
			require.Same(t, &i.frames[0], i.fr)
		}
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), v)
	})
}

func TestNew(t *testing.T) {
	i := New(program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5)}))
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	v, err := i.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(5), v)
}

func TestWithHook(t *testing.T) {
	calls := 0
	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD),
	})
	i := New(prog, WithTick(1), WithHook(func(i *Interpreter) error {
		calls++
		return nil
	}))
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	require.Equal(t, 3, calls)
}

func TestWithMarshaler(t *testing.T) {
	i := New(program.New(nil), WithMarshaler(upperMarshaler{}))
	defer i.Close()

	v, err := i.Marshal("go")
	require.NoError(t, err)
	require.Equal(t, types.String("GO"), v)

	var dst string
	require.NoError(t, i.Unmarshal(v, &dst))
	require.Equal(t, "go", dst)
}

func TestWithConverter(t *testing.T) {
	conv := Converter{
		VMType: types.TypeI64,
		Marshal: func(_ *Interpreter, v any) (types.Value, error) {
			return types.I64(v.(time.Duration)), nil
		},
		Unmarshal: func(_ *Interpreter, v types.Value, dst any) error {
			*(dst.(*time.Duration)) = time.Duration(v.(types.I64))
			return nil
		},
	}
	i := New(program.New(nil), WithConverter(reflect.TypeOf(time.Duration(0)), conv))
	defer i.Close()

	v, err := i.Marshal(5 * time.Second)
	require.NoError(t, err)
	require.Equal(t, types.I64(5*time.Second), v)

	var dst time.Duration
	require.NoError(t, i.Unmarshal(v, &dst))
	require.Equal(t, 5*time.Second, dst)
}

func TestWithCache(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
	cache := NewCache(prog)
	defer cache.Close()

	i := New(prog, WithCache(cache))
	defer i.Close()
	require.Same(t, cache, i.cache)
}

func TestWithTracer(t *testing.T) {
	tracer := NewTracer()
	i := New(program.New(nil), WithTracer(tracer))
	defer i.Close()
	require.Same(t, tracer, i.tracer)
}

func TestWithProfiler(t *testing.T) {
	p := prof.New()
	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD),
	})
	i := New(prog, WithProfiler(p), WithTick(1))
	require.NoError(t, i.Run(context.Background()))
	require.NoError(t, i.Close())

	total, ok := p.Metric("vm_samples_total")
	require.True(t, ok)
	require.Equal(t, float64(3), total)
}

func TestWithFrame(t *testing.T) {
	selfFn := types.NewFunctionBuilder(&types.FunctionType{}).Emit(
		instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
	).MustBuild()
	prog := program.New([]instr.Instruction{
		instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
	}, program.WithConstants(selfFn))
	i := New(prog, WithFrame(3))
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), ErrFrameOverflow)
}

func TestWithGlobals(t *testing.T) {
	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 9), instr.New(instr.GLOBAL_SET, 5), instr.New(instr.GLOBAL_GET, 5),
	})
	i := New(prog, WithGlobals(1))
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	v, err := i.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(9), v)
}

func TestWithStack(t *testing.T) {
	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 3),
	})
	i := New(prog, WithStack(2))
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), ErrStackOverflow)
}

func TestWithHeap(t *testing.T) {
	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW),
		instr.New(instr.I32_CONST, 2), instr.New(instr.REF_NEW),
		instr.New(instr.I32_CONST, 3), instr.New(instr.REF_NEW),
	})
	i := New(prog, WithHeap(1))
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	require.Equal(t, 3, i.Len())
}

func TestWithMaxHeap(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW)})
	i := New(prog, WithMaxHeap(1))
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), ErrHeapExhausted)
}

func TestWithTick(t *testing.T) {
	calls := 0
	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 4),
	})
	i := New(prog, WithTick(2), WithHook(func(i *Interpreter) error {
		calls++
		return nil
	}))
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	require.Equal(t, 2, calls)
}

func TestWithThreshold(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 7)})
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), v)
	})

	t.Run("jits top-level entry", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		})
		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(3), v)
		if runtime.GOARCH != "arm64" {
			return
		}
		require.NotNil(t, i.fallbacks[anchor{addr: 0, ip: 0}])
		require.Equal(t, float64(1), i.local.Value("vm_jit_emits_total"))
	})

	t.Run("jits prefix before f64 rem terminal", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		prog := program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(7.5)),
			instr.New(instr.F64_CONST, math.Float64bits(2)),
			instr.New(instr.F64_REM),
		})
		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.F64(1.5), got)
		require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("jits prefix before string read terminal", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.STRING_LEN),
		}, program.WithConstants(types.String("hello")))
		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(5), got)
		require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("jits top-level loop", func(t *testing.T) {
		b := program.NewBuilder()
		loop := b.Label()
		b.Locals(types.TypeI32).
			Emit(instr.I32_CONST, 0).
			Emit(instr.LOCAL_SET, 0).
			Bind(loop).
			Emit(instr.LOCAL_GET, 0).
			Emit(instr.I32_CONST, 1).
			Emit(instr.I32_ADD).
			Emit(instr.LOCAL_TEE, 0).
			Emit(instr.I32_CONST, 1100).
			Emit(instr.I32_LT_S).
			BrIf(loop).
			Emit(instr.LOCAL_GET, 0)
		prog, err := b.Build()
		require.NoError(t, err)
		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1100), v)
		if runtime.GOARCH != "arm64" {
			return
		}
		var looped bool
		for _, ip := range i.tracer.anchors(0) {
			looped = looped || ip > 0
		}
		require.True(t, looped)
		require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("jits top-level loop-free branch tree over constant f64 array", func(t *testing.T) {
		row := make([]float64, 8)
		b := program.NewBuilder()
		featIdx := b.Const(types.TypedArray[float64](row))
		b.Emit(instr.F64_CONST, math.Float64bits(0))
		for split := range 16 {
			b.Emit(instr.CONST_GET, uint64(featIdx))
			b.Emit(instr.I32_CONST, uint64(uint32(split%8)))
			b.Emit(instr.ARRAY_GET)
			b.Emit(instr.F64_CONST, math.Float64bits(0.5))
			b.Emit(instr.F64_LE)
			left, end := b.Label(), b.Label()
			b.BrIf(left)
			b.Emit(instr.F64_CONST, math.Float64bits(0.02))
			b.Emit(instr.F64_ADD)
			b.Br(end)
			b.Bind(left)
			b.Emit(instr.F64_CONST, math.Float64bits(0.01))
			b.Emit(instr.F64_ADD)
			b.Bind(end)
		}
		prog, err := b.Build()
		require.NoError(t, err)
		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()

		for range 256 {
			i.Reset()
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.InDelta(t, 0.16, float64(got.(types.F64)), 1e-9)
		}
		if runtime.GOARCH != "arm64" {
			return
		}
		require.NotNil(t, i.fallbacks[anchor{addr: 0, ip: 0}])
		require.GreaterOrEqual(t, i.local.Value("vm_jit_attempts_total"), float64(1))
		require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("jits called loop-free branch tree over constant f64 array", func(t *testing.T) {
		row := make([]float64, 8)
		b := program.NewBuilder()
		featIdx := b.Const(types.TypedArray[float64](row))
		fb := types.NewFunctionBuilder(nil).WithReturns(types.TypeF64)
		fb.Emit(instr.New(instr.F64_CONST, math.Float64bits(0)))
		for split := range 16 {
			fb.Emit(instr.New(instr.CONST_GET, uint64(featIdx)))
			fb.Emit(instr.New(instr.I32_CONST, uint64(uint32(split%8))))
			fb.Emit(instr.New(instr.ARRAY_GET))
			fb.Emit(instr.New(instr.F64_CONST, math.Float64bits(0.5)))
			fb.Emit(instr.New(instr.F64_LE))
			left, end := fb.Label(), fb.Label()
			fb.BrIf(left)
			fb.Emit(instr.New(instr.F64_CONST, math.Float64bits(0.02)))
			fb.Emit(instr.New(instr.F64_ADD))
			fb.Br(end)
			fb.Bind(left)
			fb.Emit(instr.New(instr.F64_CONST, math.Float64bits(0.01)))
			fb.Emit(instr.New(instr.F64_ADD))
			fb.Bind(end)
		}
		fn, err := fb.Emit(instr.New(instr.RETURN)).Build()
		require.NoError(t, err)
		b.Const(fn)
		b.ConstGet(fn)
		b.Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)
		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()

		for range 256 {
			i.Reset()
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.InDelta(t, 0.16, float64(got.(types.F64)), 1e-9)
		}
		if runtime.GOARCH != "arm64" {
			return
		}
		require.NotNil(t, i.fallbacks[anchor{addr: 0, ip: 0}])
		require.GreaterOrEqual(t, i.local.Value("vm_jit_attempts_total"), float64(1))
		require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("jits top-level accumulator over many scalar calls", func(t *testing.T) {
		b := program.NewBuilder()
		b.Emit(instr.I32_CONST, 0)
		var want int32
		for idx := range 12 {
			weight := int32(idx%5 + 1)
			bias := -int32(idx%3 + 1)
			arg := int32(idx*7 + 3)
			want += arg*weight + bias

			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			})
			fn.Emit(
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, uint64(uint32(weight))),
				instr.New(instr.I32_MUL),
				instr.New(instr.I32_CONST, uint64(uint32(bias))),
				instr.New(instr.I32_ADD),
				instr.New(instr.RETURN),
			)
			built, err := fn.Build()
			require.NoError(t, err)
			b.Emit(instr.I32_CONST, uint64(uint32(arg))).
				ConstGet(built).
				Emit(instr.CALL).
				Emit(instr.I32_ADD)
		}
		prog, err := b.Build()
		require.NoError(t, err)
		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()

		for range 64 {
			i.Reset()
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(want), got)
		}
		if runtime.GOARCH != "arm64" {
			return
		}
		require.NotNil(t, i.fallbacks[anchor{addr: 0, ip: 0}])
		require.GreaterOrEqual(t, i.local.Value("vm_jit_attempts_total"), float64(1))
		require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("jits array get from host-pushed f64 array argument", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeF64Array).
			WithReturns(types.TypeF64)
		eval.Emit(instr.New(instr.F64_CONST, math.Float64bits(0)))
		for idx := range 64 {
			eval.Emit(instr.New(instr.LOCAL_GET, 0)).
				Emit(instr.New(instr.I32_CONST, uint64(uint32(idx%8)))).
				Emit(instr.New(instr.ARRAY_GET)).
				Emit(instr.New(instr.F64_ADD))
		}
		fn, err := eval.Emit(instr.New(instr.RETURN)).Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		row := make([]float64, 8)
		arr := types.TypedArray[float64](row)
		for n := range 50000 {
			i.Reset()
			var sum float64
			for idx := range row {
				row[idx] = float64((n+idx)%10) / 10
				sum += row[idx]
			}
			require.NoError(t, i.Push(arr))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.InDelta(t, 8*sum, float64(got.(types.F64)), 1e-9)
		}
		require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("jits array get from host-pushed i1 array argument", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeI1Array).
			WithReturns(types.TypeI32)
		eval.Emit(instr.New(instr.I32_CONST, 0))
		for idx := range 64 {
			eval.Emit(instr.New(instr.LOCAL_GET, 0)).
				Emit(instr.New(instr.I32_CONST, uint64(uint32(idx%8)))).
				Emit(instr.New(instr.ARRAY_GET)).
				Emit(instr.New(instr.I32_ADD))
		}
		fn, err := eval.Emit(instr.New(instr.RETURN)).Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		row := make([]bool, 8)
		arr := types.TypedArray[bool](row)
		for n := range 5000 {
			i.Reset()
			var sum int32
			for idx := range row {
				row[idx] = (n+idx)%3 == 0
				if row[idx] {
					sum++
				}
			}
			require.NoError(t, i.Push(arr))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(8*sum), got)
		}
		require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("jits array get from host-pushed i8 array argument", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeI8Array).
			WithReturns(types.TypeI32)
		eval.Emit(instr.New(instr.I32_CONST, 0))
		for idx := range 64 {
			eval.Emit(instr.New(instr.LOCAL_GET, 0)).
				Emit(instr.New(instr.I32_CONST, uint64(uint32(idx%8)))).
				Emit(instr.New(instr.ARRAY_GET)).
				Emit(instr.New(instr.I32_ADD))
		}
		fn, err := eval.Emit(instr.New(instr.RETURN)).Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		row := make([]int8, 8)
		arr := types.TypedArray[int8](row)
		for n := range 5000 {
			i.Reset()
			var sum int32
			for idx := range row {
				row[idx] = int8((n+idx)%9 - 4)
				sum += int32(row[idx])
			}
			require.NoError(t, i.Push(arr))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(8*sum), got)
		}
		require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("jits array get from host-pushed i32 array argument", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeI32Array).
			WithReturns(types.TypeI32)
		eval.Emit(instr.New(instr.I32_CONST, 0))
		for idx := range 64 {
			eval.Emit(instr.New(instr.LOCAL_GET, 0)).
				Emit(instr.New(instr.I32_CONST, uint64(uint32(idx%8)))).
				Emit(instr.New(instr.ARRAY_GET)).
				Emit(instr.New(instr.I32_ADD))
		}
		fn, err := eval.Emit(instr.New(instr.RETURN)).Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		row := make([]int32, 8)
		arr := types.TypedArray[int32](row)
		for n := range 5000 {
			i.Reset()
			var sum int32
			for idx := range row {
				row[idx] = int32((n+idx)%17 - 8)
				sum += row[idx]
			}
			require.NoError(t, i.Push(arr))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(8*sum), got)
		}
		require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("jits array get from host-pushed i64 array argument", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeI64Array).
			WithReturns(types.TypeI64)
		eval.Emit(instr.New(instr.I64_CONST, 0))
		for idx := range 64 {
			eval.Emit(instr.New(instr.LOCAL_GET, 0)).
				Emit(instr.New(instr.I32_CONST, uint64(uint32(idx%8)))).
				Emit(instr.New(instr.ARRAY_GET)).
				Emit(instr.New(instr.I64_ADD))
		}
		fn, err := eval.Emit(instr.New(instr.RETURN)).Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		row := make([]int64, 8)
		arr := types.TypedArray[int64](row)
		for n := range 5000 {
			i.Reset()
			var sum int64
			for idx := range row {
				row[idx] = int64((n+idx)%17 - 8)
				sum += row[idx]
			}
			require.NoError(t, i.Push(arr))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I64(8*sum), got)
		}
		require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("jits array get from host-pushed f32 array argument", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeF32Array).
			WithReturns(types.TypeF32)
		eval.Emit(instr.New(instr.F32_CONST, uint64(math.Float32bits(0))))
		for idx := range 64 {
			eval.Emit(instr.New(instr.LOCAL_GET, 0)).
				Emit(instr.New(instr.I32_CONST, uint64(uint32(idx%8)))).
				Emit(instr.New(instr.ARRAY_GET)).
				Emit(instr.New(instr.F32_ADD))
		}
		fn, err := eval.Emit(instr.New(instr.RETURN)).Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		row := make([]float32, 8)
		arr := types.TypedArray[float32](row)
		for n := range 5000 {
			i.Reset()
			var sum float64
			for idx := range row {
				row[idx] = float32((n+idx)%10) / 10
				sum += float64(row[idx])
			}
			require.NoError(t, i.Push(arr))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.InDelta(t, 8*sum, float64(got.(types.F32)), 1e-5)
		}
		require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("jits array set for host-pushed primitive array arguments", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		for _, tt := range []struct {
			name  string
			typ   types.Type
			value types.Value
			array types.Value
		}{
			{
				name:  "i1",
				typ:   types.TypeI1Array,
				value: types.I1(true),
				array: types.TypedArray[bool](make([]bool, 8)),
			},
			{
				name:  "i8",
				typ:   types.TypeI8Array,
				value: types.I8(-3),
				array: types.TypedArray[int8](make([]int8, 8)),
			},
			{
				name:  "i32",
				typ:   types.TypeI32Array,
				value: types.I32(-33),
				array: types.TypedArray[int32](make([]int32, 8)),
			},
			{
				name:  "i64",
				typ:   types.TypeI64Array,
				value: types.I64(-55),
				array: types.TypedArray[int64](make([]int64, 8)),
			},
			{
				name:  "f32",
				typ:   types.TypeF32Array,
				value: types.F32(1.25),
				array: types.TypedArray[float32](make([]float32, 8)),
			},
			{
				name:  "f64",
				typ:   types.TypeF64Array,
				value: types.F64(2.5),
				array: types.TypedArray[float64](make([]float64, 8)),
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				eval := types.NewFunctionBuilder(nil).
					WithParams(tt.typ).
					WithReturns(types.TypeI32)
				for idx := range 64 {
					eval.Emit(instr.New(instr.LOCAL_GET, 0)).
						Emit(instr.New(instr.I32_CONST, uint64(uint32(idx%8)))).
						Emit(instr.New(instr.CONST_GET, 1)).
						Emit(instr.New(instr.ARRAY_SET))
				}
				fn, err := eval.Emit(instr.New(instr.I32_CONST, 7)).
					Emit(instr.New(instr.RETURN)).
					Build()
				require.NoError(t, err)
				prog := program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				}, program.WithConstants(fn, tt.value))

				i := New(prog, WithTick(1), WithThreshold(0))
				defer i.Close()
				for range 5000 {
					i.Reset()
					require.NoError(t, i.Push(tt.array))
					require.NoError(t, i.Run(context.Background()))
					got, err := i.Pop()
					require.NoError(t, err)
					require.Equal(t, types.I32(7), got)
				}
				switch row := tt.array.(type) {
				case types.TypedArray[bool]:
					for _, got := range row {
						require.True(t, got)
					}
				case types.TypedArray[int8]:
					for _, got := range row {
						require.Equal(t, int8(-3), got)
					}
				case types.TypedArray[int32]:
					for _, got := range row {
						require.Equal(t, int32(-33), got)
					}
				case types.TypedArray[int64]:
					for _, got := range row {
						require.Equal(t, int64(-55), got)
					}
				case types.TypedArray[float32]:
					for _, got := range row {
						require.Equal(t, float32(1.25), got)
					}
				case types.TypedArray[float64]:
					for _, got := range row {
						require.Equal(t, 2.5, got)
					}
				}
				require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
			})
		}
	})

	t.Run("jits struct get from host-pushed primitive struct argument", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		typ := types.NewStructType(
			types.NewStructField(types.TypeI1),
			types.NewStructField(types.TypeI8),
			types.NewStructField(types.TypeI32),
			types.NewStructField(types.TypeI64),
			types.NewStructField(types.TypeF32),
			types.NewStructField(types.TypeF64),
		)
		for _, tt := range []struct {
			name  string
			idx   uint32
			typ   types.Type
			value types.Boxed
			want  types.Value
		}{
			{name: "i1", idx: 0, typ: types.TypeI1, value: types.BoxI1(true), want: types.I1(true)},
			{name: "i8", idx: 1, typ: types.TypeI8, value: types.BoxI8(-3), want: types.I8(-3)},
			{name: "i32", idx: 2, typ: types.TypeI32, value: types.BoxI32(-33), want: types.I32(-33)},
			{name: "i64", idx: 3, typ: types.TypeI64, value: types.BoxI64(-55), want: types.I64(-55)},
			{name: "f32", idx: 4, typ: types.TypeF32, value: types.BoxF32(1.25), want: types.F32(1.25)},
			{name: "f64", idx: 5, typ: types.TypeF64, value: types.BoxF64(2.5), want: types.F64(2.5)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				eval := types.NewFunctionBuilder(nil).
					WithParams(typ).
					WithReturns(tt.typ)
				eval.Emit(instr.New(instr.LOCAL_GET, 0)).
					Emit(instr.New(instr.I32_CONST, uint64(tt.idx))).
					Emit(instr.New(instr.STRUCT_GET)).
					Emit(instr.New(instr.RETURN))
				fn, err := eval.Build()
				require.NoError(t, err)
				prog := program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				}, program.WithConstants(fn))

				i := New(prog, WithTick(1), WithThreshold(0))
				defer i.Close()
				s := types.NewStruct(typ)
				for range 5000 {
					i.Reset()
					s.SetField(int(tt.idx), tt.value)
					require.NoError(t, i.Push(s))
					require.NoError(t, i.Run(context.Background()))
					got, err := i.Pop()
					require.NoError(t, err)
					require.Equal(t, tt.want, got)
				}
				require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
			})
		}
	})

	t.Run("jits struct set for host-pushed primitive struct argument", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		typ := types.NewStructType(
			types.NewStructField(types.TypeI1),
			types.NewStructField(types.TypeI8),
			types.NewStructField(types.TypeI32),
			types.NewStructField(types.TypeI64),
			types.NewStructField(types.TypeF32),
			types.NewStructField(types.TypeF64),
		)
		for _, tt := range []struct {
			name  string
			idx   uint32
			value types.Value
			want  types.Boxed
		}{
			{name: "i1", idx: 0, value: types.I1(true), want: types.BoxI1(true)},
			{name: "i8", idx: 1, value: types.I8(-3), want: types.BoxI8(-3)},
			{name: "i32", idx: 2, value: types.I32(-33), want: types.BoxI32(-33)},
			{name: "i64", idx: 3, value: types.I64(-55), want: types.BoxI64(-55)},
			{name: "f32", idx: 4, value: types.F32(1.25), want: types.BoxF32(1.25)},
			{name: "f64", idx: 5, value: types.F64(2.5), want: types.BoxF64(2.5)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				eval := types.NewFunctionBuilder(nil).
					WithParams(typ).
					WithReturns(types.TypeI32)
				for range 64 {
					eval.Emit(instr.New(instr.LOCAL_GET, 0)).
						Emit(instr.New(instr.I32_CONST, uint64(tt.idx))).
						Emit(instr.New(instr.CONST_GET, 1)).
						Emit(instr.New(instr.STRUCT_SET))
				}
				fn, err := eval.Emit(instr.New(instr.I32_CONST, 7)).
					Emit(instr.New(instr.RETURN)).
					Build()
				require.NoError(t, err)
				prog := program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				}, program.WithConstants(fn, tt.value))

				i := New(prog, WithTick(1), WithThreshold(0))
				defer i.Close()
				s := types.NewStruct(typ)
				for range 5000 {
					i.Reset()
					require.NoError(t, i.Push(s))
					require.NoError(t, i.Run(context.Background()))
					got, err := i.Pop()
					require.NoError(t, err)
					require.Equal(t, types.I32(7), got)
					require.Equal(t, tt.want, s.Field(int(tt.idx)))
				}
				require.GreaterOrEqual(t, i.local.Value("vm_jit_emits_total"), float64(1))
			})
		}
	})

	t.Run("jits learned br_if continuations", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		b := types.NewFunctionBuilder(nil).WithParams(types.TypeI32).WithReturns(types.TypeI32)
		neg := b.Label()
		small := b.Label()
		tiny := b.Label()
		b.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.I32_LT_S)).
			BrIf(neg).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 10)).
			Emit(instr.New(instr.I32_LT_S)).
			BrIf(small).
			Emit(instr.New(instr.I32_CONST, 2)).
			Emit(instr.New(instr.RETURN)).
			Bind(neg).
			Emit(instr.New(instr.I32_CONST, i32operand(-1))).
			Emit(instr.New(instr.RETURN)).
			Bind(small).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 5)).
			Emit(instr.New(instr.I32_LT_S)).
			BrIf(tiny).
			Emit(instr.New(instr.I32_CONST, 1)).
			Emit(instr.New(instr.RETURN)).
			Bind(tiny).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.RETURN))
		eval, err := b.Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(eval))

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		root := anchor{addr: i.constants[0].Ref(), ip: 0}

		// Record the root trace through two distinct paths before warming a side exit.
		i.Reset()
		require.NoError(t, i.Push(types.I32(20)))
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(2), v)

		i.Reset()
		require.NoError(t, i.Push(types.I32(7)))
		require.NoError(t, i.Run(context.Background()))
		v, err = i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1), v)

		// Warm the arg=3 side exit until its learned continuation is compiled. The
		// branch is identified by the i32.const its captured trace returns; once it
		// runs native the journal stops counting it, so its hit counter freezes at
		// the exit threshold.
		id := -1
		for range exitThreshold * 4 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(3)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(0), v)

			tree := i.tracer.rootAt(root)
			require.NotNil(t, tree)
			for bid, branch := range tree.branches {
				if branch == nil {
					continue
				}
				for _, op := range branch.ops {
					if op.op != instr.I32_CONST || op.fn < 0 || op.fn >= len(i.instrs) {
						continue
					}
					code := i.instrs[op.fn]
					if op.ip+5 <= len(code) && int32(instr.ParseI32(code, op.ip+1)) == 0 {
						id = bid
					}
				}
			}
			if id >= 0 && tree.hits[id] >= exitThreshold {
				break
			}
		}
		require.GreaterOrEqual(t, id, 0, "no branch returning i32.const 0 was learned")
		hits := i.tracer.rootAt(root).hits[id]
		require.Equal(t, int64(exitThreshold), hits)

		for range 3 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(3)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(0), v)
		}
		require.Equal(t, hits, i.tracer.rootAt(root).hits[id])
	})

	t.Run("jits learned br_if continuations over mutable f64 row", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		row := make([]float64, 2)
		b := types.NewFunctionBuilder(nil).
			WithParams(types.TypeF64Array).
			WithReturns(types.TypeF64)
		left := b.Label()
		leftLow := b.Label()
		b.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(0.5))).
			Emit(instr.New(instr.F64_LE)).
			BrIf(left).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(2))).
			Emit(instr.New(instr.RETURN)).
			Bind(left).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 1)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(0.25))).
			Emit(instr.New(instr.F64_LE)).
			BrIf(leftLow).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(1))).
			Emit(instr.New(instr.RETURN)).
			Bind(leftLow).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(-3))).
			Emit(instr.New(instr.RETURN))
		eval, err := b.Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
		}, program.WithConstants(types.TypedArray[float64](row), eval))

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		root := anchor{addr: 0, ip: 0}

		row[0], row[1] = 0.8, 0.8
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.F64(2), v)

		id := -1
		for range exitThreshold * 4 {
			i.Reset()
			row[0], row[1] = 0.2, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(1), v)

			tree := i.tracer.rootAt(root)
			require.NotNil(t, tree)
			for bid, branch := range tree.branches {
				if branch == nil || tree.hits[bid] < exitThreshold {
					continue
				}
				for _, op := range branch.ops {
					if op.op != instr.F64_CONST || op.fn < 0 || op.fn >= len(i.instrs) {
						continue
					}
					code := i.instrs[op.fn]
					if op.ip+9 <= len(code) && math.Float64frombits(binary.LittleEndian.Uint64(code[op.ip+1:op.ip+9])) == 1 {
						id = bid
					}
				}
			}
			if id >= 0 {
				break
			}
		}
		require.GreaterOrEqual(t, id, 0, "no branch returning f64.const 1 was learned")
		hits := i.tracer.rootAt(root).hits[id]
		require.Equal(t, int64(exitThreshold), hits)

		id = -1
		for range exitThreshold * 4 {
			i.Reset()
			row[0], row[1] = 0.2, 0.1
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(-3), v)

			tree := i.tracer.rootAt(root)
			require.NotNil(t, tree)
			for bid, branch := range tree.branches {
				if branch == nil || tree.hits[bid] < exitThreshold {
					continue
				}
				for _, op := range branch.ops {
					if op.op != instr.F64_CONST || op.fn < 0 || op.fn >= len(i.instrs) {
						continue
					}
					code := i.instrs[op.fn]
					if op.ip+9 <= len(code) && math.Float64frombits(binary.LittleEndian.Uint64(code[op.ip+1:op.ip+9])) == -3 {
						id = bid
					}
				}
			}
			if id >= 0 {
				break
			}
		}
		require.GreaterOrEqual(t, id, 0, "no branch returning f64.const -3 was learned")
		hits = i.tracer.rootAt(root).hits[id]
		require.Equal(t, int64(exitThreshold), hits)

		for range 3 {
			i.Reset()
			row[0], row[1] = 0.2, 0.1
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(-3), v)
		}
		require.Equal(t, hits, i.tracer.rootAt(root).hits[id])
	})

	t.Run("falls back learned callee branch through caller tail", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		row := make([]float64, 2)
		first := types.NewFunctionBuilder(nil).
			WithParams(types.TypeF64Array).
			WithReturns(types.TypeF64)
		firstLeft := first.Label()
		first.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(0.5))).
			Emit(instr.New(instr.F64_LE)).
			BrIf(firstLeft).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(2))).
			Emit(instr.New(instr.RETURN)).
			Bind(firstLeft).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(1))).
			Emit(instr.New(instr.RETURN))
		firstFn, err := first.Build()
		require.NoError(t, err)

		second := types.NewFunctionBuilder(nil).
			WithParams(types.TypeF64Array).
			WithReturns(types.TypeF64)
		secondLeft := second.Label()
		second.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 1)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(0.5))).
			Emit(instr.New(instr.F64_LE)).
			BrIf(secondLeft).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(20))).
			Emit(instr.New(instr.RETURN)).
			Bind(secondLeft).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(10))).
			Emit(instr.New(instr.RETURN))
		secondFn, err := second.Build()
		require.NoError(t, err)

		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeF64Array).
			WithReturns(types.TypeF64)
		eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.CONST_GET, 1)).
			Emit(instr.New(instr.CALL)).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.CONST_GET, 2)).
			Emit(instr.New(instr.CALL)).
			Emit(instr.New(instr.F64_ADD)).
			Emit(instr.New(instr.RETURN))
		evalFn, err := eval.Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CONST_GET, 3),
			instr.New(instr.CALL),
		}, program.WithConstants(types.TypedArray[float64](row), firstFn, secondFn, evalFn))

		i := New(prog, WithTick(1), WithThreshold(1))
		defer i.Close()
		root := anchor{addr: 0, ip: 0}

		var v types.Value
		for range 4 {
			i.Reset()
			row[0], row[1] = 0.8, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(22), v)
			if i.fallbacks[root] != nil {
				break
			}
		}
		require.NotNil(t, i.fallbacks[root])

		id := -1
		for range exitThreshold * 4 {
			i.Reset()
			row[0], row[1] = 0.2, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(21), v)

			tree := i.tracer.rootAt(root)
			require.NotNil(t, tree)
			for bid, branch := range tree.branches {
				if branch == nil || tree.hits[bid] < exitThreshold {
					continue
				}
				for _, op := range branch.ops {
					if op.op != instr.F64_CONST || op.fn < 0 || op.fn >= len(i.instrs) {
						continue
					}
					code := i.instrs[op.fn]
					if op.ip+9 <= len(code) && math.Float64frombits(binary.LittleEndian.Uint64(code[op.ip+1:op.ip+9])) == 1 {
						id = bid
					}
				}
			}
			if id >= 0 {
				break
			}
		}
		require.GreaterOrEqual(t, id, 0, "no first callee branch returning f64.const 1 was learned")
		hits := i.tracer.rootAt(root).hits[id]
		require.Equal(t, int64(exitThreshold), hits)

		for range 3 {
			i.Reset()
			row[0], row[1] = 0.2, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(21), v)
		}
		require.Greater(t, i.tracer.rootAt(root).hits[id], hits)
	})

	t.Run("keeps inlined callee params across nested learned branch deopt", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		row := make([]float64, 2)
		first := types.NewFunctionBuilder(nil).
			WithParams(types.TypeF64Array).
			WithReturns(types.TypeF64)
		firstLeft := first.Label()
		first.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(0.5))).
			Emit(instr.New(instr.F64_LE)).
			BrIf(firstLeft).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(2))).
			Emit(instr.New(instr.RETURN)).
			Bind(firstLeft).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(1))).
			Emit(instr.New(instr.RETURN))
		firstFn, err := first.Build()
		require.NoError(t, err)

		second := types.NewFunctionBuilder(nil).
			WithParams(types.TypeF64Array).
			WithReturns(types.TypeF64)
		secondLeft := second.Label()
		secondLeftLow := second.Label()
		secondRightLow := second.Label()
		second.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 1)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(0.5))).
			Emit(instr.New(instr.F64_LE)).
			BrIf(secondLeft).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(0.25))).
			Emit(instr.New(instr.F64_LE)).
			BrIf(secondRightLow).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(20))).
			Emit(instr.New(instr.RETURN)).
			Bind(secondRightLow).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(30))).
			Emit(instr.New(instr.RETURN)).
			Bind(secondLeft).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(0.25))).
			Emit(instr.New(instr.F64_LE)).
			BrIf(secondLeftLow).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(10))).
			Emit(instr.New(instr.RETURN)).
			Bind(secondLeftLow).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(-10))).
			Emit(instr.New(instr.RETURN))
		secondFn, err := second.Build()
		require.NoError(t, err)

		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeF64Array).
			WithReturns(types.TypeF64)
		eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.CONST_GET, 1)).
			Emit(instr.New(instr.CALL)).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.CONST_GET, 2)).
			Emit(instr.New(instr.CALL)).
			Emit(instr.New(instr.F64_ADD)).
			Emit(instr.New(instr.RETURN))
		evalFn, err := eval.Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CONST_GET, 3),
			instr.New(instr.CALL),
		}, program.WithConstants(types.TypedArray[float64](row), firstFn, secondFn, evalFn))

		i := New(prog, WithTick(1), WithThreshold(1))
		defer i.Close()
		root := anchor{addr: 0, ip: 0}

		var v types.Value
		for range 4 {
			i.Reset()
			row[0], row[1] = 0.8, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(22), v)
			if i.fallbacks[root] != nil {
				break
			}
		}
		require.NotNil(t, i.fallbacks[root])

		id := -1
		for range exitThreshold * 4 {
			i.Reset()
			row[0], row[1] = 0.2, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(31), v)

			tree := i.tracer.rootAt(root)
			require.NotNil(t, tree)
			for bid, branch := range tree.branches {
				if branch == nil || tree.hits[bid] < exitThreshold {
					continue
				}
				for _, op := range branch.ops {
					if op.op != instr.F64_CONST || op.fn < 0 || op.fn >= len(i.instrs) {
						continue
					}
					code := i.instrs[op.fn]
					if op.ip+9 <= len(code) && math.Float64frombits(binary.LittleEndian.Uint64(code[op.ip+1:op.ip+9])) == 1 {
						id = bid
					}
				}
			}
			if id >= 0 {
				break
			}
		}
		require.GreaterOrEqual(t, id, 0, "no first callee branch returning f64.const 1 was learned")
		hits := i.tracer.rootAt(root).hits[id]
		require.Equal(t, int64(exitThreshold), hits)

		for range 3 {
			i.Reset()
			row[0], row[1] = 0.2, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(31), v)
		}
		require.Greater(t, i.tracer.rootAt(root).hits[id], hits)

		for range 3 {
			i.Reset()
			row[0], row[1] = 0.2, 0.1
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(-9), v)
		}
		require.Greater(t, i.tracer.rootAt(root).hits[id], hits)
	})

	t.Run("jits learned br_table continuations", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		b := types.NewFunctionBuilder(nil).WithParams(types.TypeI32).WithReturns(types.TypeI32)
		zero := b.Label()
		one := b.Label()
		two := b.Label()
		def := b.Label()
		b.Emit(instr.New(instr.LOCAL_GET, 0)).
			BrTable(def, zero, one, two).
			Bind(zero).
			Emit(instr.New(instr.I32_CONST, 10)).
			Emit(instr.New(instr.RETURN)).
			Bind(one).
			Emit(instr.New(instr.I32_CONST, 11)).
			Emit(instr.New(instr.RETURN)).
			Bind(two).
			Emit(instr.New(instr.I32_CONST, 12)).
			Emit(instr.New(instr.RETURN)).
			Bind(def).
			Emit(instr.New(instr.I32_CONST, 99)).
			Emit(instr.New(instr.RETURN))
		eval, err := b.Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(eval))

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		root := anchor{addr: i.constants[0].Ref(), ip: 0}

		// Record the root trace through table index 0 before warming index 1.
		i.Reset()
		require.NoError(t, i.Push(types.I32(0)))
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(10), v)

		// Warm the index=1 side exit until its learned continuation is compiled;
		// once native, the journal stops counting it and its hit counter freezes.
		id := -1
		for range exitThreshold * 4 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(1)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(11), v)

			tree := i.tracer.rootAt(root)
			require.NotNil(t, tree)
			for bid, branch := range tree.branches {
				if branch == nil {
					continue
				}
				for _, op := range branch.ops {
					if op.op != instr.I32_CONST || op.fn < 0 || op.fn >= len(i.instrs) {
						continue
					}
					code := i.instrs[op.fn]
					if op.ip+5 <= len(code) && int32(instr.ParseI32(code, op.ip+1)) == 11 {
						id = bid
					}
				}
			}
			if id >= 0 && tree.hits[id] >= exitThreshold {
				break
			}
		}
		require.GreaterOrEqual(t, id, 0, "no branch returning i32.const 11 was learned")
		hits := i.tracer.rootAt(root).hits[id]
		require.Equal(t, int64(exitThreshold), hits)

		for range 3 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(1)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(11), v)
		}
		require.Equal(t, hits, i.tracer.rootAt(root).hits[id])

		// The default target still deopts correctly after index 1 is learned.
		i.Reset()
		require.NoError(t, i.Push(types.I32(4)))
		require.NoError(t, i.Run(context.Background()))
		v, err = i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(99), v)
	})
}

func TestWithFuel(t *testing.T) {
	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD),
	})
	i := New(prog, WithTick(1), WithFuel(2))
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), ErrFuelExhausted)
}

func i32operand(v int32) uint64 {
	return uint64(uint32(v))
}

func i64operand(v int64) uint64 {
	return uint64(v)
}
