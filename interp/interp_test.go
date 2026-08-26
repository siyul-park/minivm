package interp

import (
	"context"
	"math"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type upperCodec byte

type contextKey byte

type trackedValue struct {
	refs   []types.Ref
	closed int
}

func (v *trackedValue) Kind() types.Kind { return types.KindRef }
func (v *trackedValue) Type() types.Type { return types.TypeAny }
func (v *trackedValue) String() string   { return "tracked" }

func (v *trackedValue) Refs(dst []types.Ref) []types.Ref {
	return append(dst, v.refs...)
}

func (v *trackedValue) Close() error {
	v.closed++
	return nil
}

func (upperCodec) Marshal(_ *Interpreter, v any) (types.Value, error) {
	s, ok := v.(string)
	if !ok {
		return nil, ErrUnsupportedMarshalType
	}
	return types.String(strings.ToUpper(s)), nil
}

func (upperCodec) Unmarshal(_ *Interpreter, v types.Value, dst any) error {
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
		}, program.WithGlobals(types.TypeI32)),
		values: []types.Value{types.I32(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 6), instr.New(instr.GLOBAL_TEE, 0)}, program.WithGlobals(types.TypeI32)),
		values:  []types.Value{types.I32(6)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.LOCAL_SET, 0), instr.New(instr.LOCAL_GET, 0),
		}, program.WithLocals(types.TypeI32)),
		values: []types.Value{types.I32(5)},
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
			Captures(types.TypeI32).Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Captures(types.TypeI32).Emit(
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
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 77), instr.New(instr.REF_SET),
		}, program.WithConstants(types.NewFunctionBuilder(nil).Emit(instr.New(instr.RETURN)).MustBuild())),
		err: ErrTypeMismatch,
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
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_CONCAT),
			instr.New(instr.CONST_GET, 2), instr.New(instr.STRING_EQ),
		}, program.WithConstants(types.String("Hi"), types.String("There"), types.String("HiThere"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_CONCAT),
			instr.New(instr.CONST_GET, 2), instr.New(instr.STRING_NE),
		}, program.WithConstants(types.String("Hi"), types.String("There"), types.String("HiThere"))),
		values: []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{
			// Keep the first join live in a local, extend a copy of it, then
			// compare the local against its original content: an append that
			// rewrote published bytes would change what the local reads.
			instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_CONCAT),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_CONCAT),
			instr.New(instr.DROP),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 2), instr.New(instr.STRING_EQ),
		}, program.WithConstants(types.String("Hi"), types.String("There"), types.String("HiThere")),
			program.WithLocals(types.TypeString)),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_CONCAT)}, program.WithConstants(types.String(""), types.String(""))),
		values:  []types.Value{types.String("")},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_CONCAT),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 2), instr.New(instr.STRING_CONCAT),
			instr.New(instr.DROP),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 3), instr.New(instr.STRING_EQ),
		}, program.WithConstants(
			types.String("abc"), types.String("def"), types.String("0123456789abcdef"), types.String("abcdef")),
			program.WithLocals(types.TypeString)),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_CONCAT), instr.New(instr.DROP),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 2), instr.New(instr.STRING_CONCAT),
			instr.New(instr.CONST_GET, 3), instr.New(instr.STRING_EQ),
		}, program.WithConstants(types.String("a"), types.String("b"), types.String("c"), types.String("ac"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.REF_NULL), instr.New(instr.CONST_GET, 0), instr.New(instr.STRING_EQ),
		}, program.WithConstants(types.String("Go"))),
		err: ErrTypeMismatch,
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
		program: program.New(
			[]instr.Instruction{instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW_DEFAULT, 0)},
			program.WithTypes(types.NewArrayType(types.TypeAny)),
		),
		values: []types.Value{types.NewArray(types.NewArrayType(types.TypeAny), types.BoxedNull, types.BoxedNull)},
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
			instr.New(instr.I32_CONST, 5), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 1), instr.New(instr.F64_CONST, math.Float64bits(1.5)), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_FILL),
			instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeF64Array)),
		values: []types.Value{types.F64(1.5)},
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
		// array.new_default's type index names a ref-element array type, so
		// it allocates the generic *types.Array boxed-element representation
		// (never TypedArray[int32]), while the local it is stored into is
		// declared types.TypeI32Array. array.get's fused LOCAL_GET path
		// proves only the local's declared element kind at threading time,
		// so it must fall back from its specialized TypedArray[int32]
		// assertion to the *types.Array representation actually on the heap
		// instead of trapping.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		},
			program.WithTypes(types.NewArrayType(types.TypeAny)),
			program.WithLocals(types.TypeI32Array),
		),
		values: []types.Value{types.Null},
	},
	{
		// LOCAL_SET does not recheck the declared type of the slot it writes
		// into, so a local declared as a concrete i32-element array can still
		// hold a different concrete element kind (here f32) at runtime.
		// array.get's fused LOCAL_GET path proves only the local's declared
		// element kind at threading time, so a miss on its specialized
		// TypedArray[int32] assertion must fall back through every other
		// concrete TypedArray[_] representation, not just *types.Array,
		// instead of trapping a case the unfused handler accepts.
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		},
			program.WithConstants(types.TypedArray[float32]{1.5}),
			program.WithLocals(types.TypeI32Array),
		),
		values: []types.Value{types.F32(1.5)},
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
			instr.New(instr.I32_CONST, 7), instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.STRUCT_NEW, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))),
			program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))),
		),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.STRUCT_NEW, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.STRUCT_GET),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))),
			program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))),
		),
		values: []types.Value{types.F64(2.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.STRUCT_NEW, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 0), instr.New(instr.STRUCT_GET),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))),
			program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))),
			program.WithConstants(types.I32(1)),
		),
		values: []types.Value{types.F64(2.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.STRUCT_NEW, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
		},
			program.WithConstants(types.String("hi")),
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeString))),
			program.WithLocals(types.NewStructType(types.NewStructField(types.TypeString))),
		),
		values: []types.Value{types.String("hi")},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
		}, program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32)))),
		err: ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
		}, program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32)))),
		err: ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.REF_NULL), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
		}, program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32)))),
		err: ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 9), instr.New(instr.STRUCT_NEW, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.STRUCT_GET),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32))),
			program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))),
		),
		err: ErrSegmentationFault,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.STRUCT_NEW, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 5), instr.New(instr.STRUCT_GET),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32))),
			program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32))),
		),
		err: ErrSegmentationFault,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI32Array),
			program.WithGlobals(types.TypeI32Array),
		),
		values: []types.Value{types.I32(20)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.STRUCT_NEW, 0),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))),
			program.WithGlobals(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))),
		),
		values: []types.Value{types.I32(7)},
	},
	{
		// Mirrors the LOCAL_GET parity case above: array.new_default's type
		// index names a ref-element array type, so the heap value is the
		// generic *types.Array representation, while the global it is stored
		// into is declared types.TypeI32Array. array.get's fused GLOBAL_GET
		// path proves only the global's declared element kind at threading
		// time, so it must fall back to the *types.Array representation
		// actually on the heap instead of trapping.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		},
			program.WithTypes(types.NewArrayType(types.TypeAny)),
			program.WithGlobals(types.TypeI32Array),
		),
		values: []types.Value{types.Null},
	},
	{
		// The generated GLOBAL_SET handler does not recheck the declared type
		// of the slot it writes into, so a global declared as a concrete
		// i32-element array can still hold a different concrete element kind
		// (here f32) at runtime. array.get's fused GLOBAL_GET path proves
		// only the global's declared element kind at threading time, so a
		// miss on its specialized TypedArray[int32] assertion must fall back
		// through every other concrete TypedArray[_] representation, not
		// just *types.Array, instead of trapping a case the unfused handler
		// accepts.
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		},
			program.WithConstants(types.TypedArray[float32]{1.5}),
			program.WithGlobals(types.TypeI32Array),
		),
		values: []types.Value{types.F32(1.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		}, program.WithGlobals(types.TypeI32Array)),
		err: ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
		}, program.WithGlobals(types.NewStructType(types.NewStructField(types.TypeI32)))),
		err: ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		},
			program.WithTypes(types.TypeI32Array),
			program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Captures(types.TypeI32Array).
				Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_GET), instr.New(instr.RETURN)).
				MustBuild()),
		),
		values: []types.Value{types.I32(20)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.STRUCT_NEW, 0),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))),
			program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Captures(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))).
				Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET), instr.New(instr.RETURN)).
				MustBuild()),
		),
		values: []types.Value{types.I32(7)},
	},
	{
		// Mirrors the LOCAL_GET parity case above, but the ref-element array
		// is captured as an upvalue declared types.TypeI32Array instead of
		// stored into a local.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		},
			program.WithTypes(types.NewArrayType(types.TypeAny)),
			program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Captures(types.TypeI32Array).
				Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET), instr.New(instr.RETURN)).
				MustBuild()),
		),
		values: []types.Value{types.Null},
	},
	{
		// CLOSURE_NEW does not recheck a captured value's type against the
		// callee's declared Captures, so an upvalue declared as a concrete
		// i32-element array can still hold a different concrete element kind
		// (here f32) at runtime. array.get's fused UPVAL_GET path proves only
		// the upvalue's declared element kind at threading time, so a miss on
		// its specialized TypedArray[int32] assertion must fall back through
		// every other concrete TypedArray[_] representation, not just
		// *types.Array, instead of trapping a case the unfused handler
		// accepts.
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		},
			program.WithConstants(
				types.TypedArray[float32]{1.5},
				types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeF32}}).
					Captures(types.TypeI32Array).
					Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET), instr.New(instr.RETURN)).
					MustBuild(),
			),
		),
		values: []types.Value{types.F32(1.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		},
			program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Captures(types.TypeI32Array).
				Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET), instr.New(instr.RETURN)).
				MustBuild()),
		),
		err: ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		},
			program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Captures(types.NewStructType(types.NewStructField(types.TypeI32))).
				Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET), instr.New(instr.RETURN)).
				MustBuild()),
		),
		err: ErrTypeMismatch,
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
			instr.New(instr.CONST_GET, 2), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_CONCAT),
			instr.New(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeString, types.TypeI32)),
			program.WithConstants(types.String("Hi"), types.String("There"), types.String("HiThere"))),
		values: []types.Value{types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_CONCAT),
			instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.CONST_GET, 2), instr.New(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeString, types.TypeI32)),
			program.WithConstants(types.String("Hi"), types.String("There"), types.String("HiThere"))),
		values: []types.Value{types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 2), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_CONCAT),
			instr.New(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeAny, types.TypeI32)),
			program.WithConstants(types.String("Hi"), types.String("There"), types.String("HiThere"))),
		values: []types.Value{types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 0), instr.New(instr.I32_EQZ),
			instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.I32_CONST, 0), instr.New(instr.I32_EQZ), instr.New(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeAny, types.TypeI32))),
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
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 4),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(NewHostFunction(
			&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			func(_ *Interpreter, args []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(args[0].I32() + args[1].I32())}, nil
			},
		))),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 5), instr.New(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array)),
		err: ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 9), instr.New(instr.ARRAY_SET),
		}, program.WithTypes(types.TypeI32Array)),
		err: ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 7), instr.New(instr.I32_CONST, 5), instr.New(instr.ARRAY_FILL),
		}, program.WithTypes(types.TypeI32Array)),
		err: ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 5), instr.New(instr.ARRAY_DELETE),
		}, program.WithTypes(types.TypeI32Array)),
		err: ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 5),
			instr.New(instr.ARRAY_COPY),
		}, program.WithTypes(types.TypeI32Array)),
		err: ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_CONST, 4), instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 6), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, uint64(^uint32(0))),
			instr.New(instr.ARRAY_COPY),
		}, program.WithTypes(types.TypeI32Array)),
		err: ErrIndexOutOfRange,
	},
}

// runTestName renders a runTests case's program to a single-line name, so the
// program itself documents the case instead of a hand-written label that can
// drift out of sync with it. It reads the program's canonical String() dump,
// keeps only the ".code" section (ignoring any ".locals", ".constants", etc.
// that follow), strips each line's "%04d:\t" offset prefix, and joins the
// remaining instruction text with "; ".
func runTestName(prog *program.Program) string {
	lines := strings.Split(prog.String(), "\n")
	var parts []string
	for _, line := range lines[1:] { // lines[0] is always the ".code" header.
		if strings.HasPrefix(line, ".") {
			break
		}
		if line == "" {
			continue
		}
		if _, rest, ok := strings.Cut(line, ":\t"); ok {
			line = rest
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "; ")
}

func TestInterpreter_Run(t *testing.T) {
	t.Run("covers every runtime opcode", func(t *testing.T) {
		covered := make(map[instr.Opcode]struct{})
		names := make(map[string]int)
		for _, tt := range runTests {
			name := runTestName(tt.program)
			require.NotEmpty(t, name)
			names[name]++
			codes := [][]byte{tt.program.Code}
			for _, constant := range tt.program.Constants {
				if fn, ok := constant.(*types.Function); ok {
					codes = append(codes, fn.Code)
				}
			}
			for _, code := range codes {
				for ip := 0; ip < len(code); {
					inst := instr.Instruction(code[ip:])
					covered[inst.Opcode()] = struct{}{}
					width := inst.Width()
					require.Positive(t, width)
					require.LessOrEqual(t, ip+width, len(code))
					ip += width
				}
			}
		}

		var missing []string
		for code := 0; code < 256; code++ {
			op := instr.Opcode(code)
			if !instr.Valid(op) {
				continue
			}
			if _, ok := covered[op]; !ok {
				missing = append(missing, instr.TypeOf(op).Mnemonic)
			}
		}
		require.Empty(t, missing)

		// A derived name collides when two cases render the same program, which
		// is not itself wrong (Go's testing package disambiguates with a "#01"
		// suffix) but is worth surfacing: one of the two is likely redundant.
		var collisions int
		for name, count := range names {
			if count > 1 {
				collisions += count - 1
				t.Logf("derived name used by %d cases: %q", count, name)
			}
		}
		if collisions > 0 {
			t.Logf("%d runTests case(s) collide on their derived name", collisions)
		}
	})

	t.Run("releases frame slots on return", func(t *testing.T) {
		// The callee returns a scalar, so the reference the caller passed in is
		// discarded with the frame instead of handed back. A teardown that keeps
		// it leaks one slot per call and exhausts a bounded heap.
		callee := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeAny},
			Returns: []types.Type{types.TypeI32},
		}).Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).MustBuild()

		b := program.NewBuilder()
		fn := b.Const(callee)
		loop := b.Label()
		done := b.Label()
		b.Locals(types.TypeI32)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 4*heapRunway).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1).Emit(instr.REF_NEW)
		b.Emit(instr.CONST_GET, uint64(fn)).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, program.Verify(prog))

		i := New(prog, WithHeapLimit(heapRunway))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
	})

	t.Run("string.concat reads the result after releasing both last operand references", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.STRING_CONCAT)})
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Push(types.String("left")))
		require.NoError(t, i.Push(types.String("right")))
		require.NoError(t, i.Run(context.Background()))

		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.String("leftright"), value)
	})

	t.Run("releases every string.concat intermediate", func(t *testing.T) {
		// Each join consumes both operands and publishes one result, so an
		// accumulating loop holds one live string at a time. A join that kept an
		// operand ref leaks one slot per iteration and exhausts a bounded heap.
		b := program.NewBuilder()
		loop := b.Label()
		done := b.Label()
		b.Locals(types.TypeString, types.TypeI32)
		b.ConstGet(types.String("")).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4*heapRunway).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).ConstGet(types.String("x")).Emit(instr.STRING_CONCAT).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.STRING_LEN)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, program.Verify(prog))

		i := New(prog, WithHeapLimit(heapRunway))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		got, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(4*heapRunway), got)
	})

	t.Run("STRUCT_GET fused on a declared struct local traps type mismatch when the runtime field's kind diverges from the declared field's kind", func(t *testing.T) {
		// The local's declared type only proves what the fused handler
		// specializes for at threading time; storing a different-shaped
		// struct (same field count, mismatched field kind) at the same
		// index must still trap instead of reinterpreting the raw bits
		// under the specialized (wrong) kind. Standalone execution has no
		// specialization to diverge from, so this is exercised only under
		// fusion.
		prog := program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(1.5)), instr.New(instr.STRUCT_NEW, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeF64))),
			program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32))),
		)
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		err := i.Run(context.Background())
		require.ErrorIs(t, err, ErrTypeMismatch)
	})

	for _, tt := range []struct {
		name        string
		typ         types.Type
		initial     types.Boxed
		replacement types.Boxed
		want        types.Value
	}{
		{name: "i1", typ: types.TypeI1, initial: types.BoxI1(false), replacement: types.BoxI1(true), want: types.I1(true)},
		{name: "i8", typ: types.TypeI8, initial: types.BoxI8(1), replacement: types.BoxI8(2), want: types.I8(2)},
		{name: "i32", typ: types.TypeI32, initial: types.BoxI32(1), replacement: types.BoxI32(2), want: types.I32(2)},
		{name: "i64", typ: types.TypeI64, initial: types.BoxI64(1), replacement: types.BoxI64(2), want: types.I64(2)},
		{name: "f32", typ: types.TypeF32, initial: types.BoxF32(1), replacement: types.BoxF32(2), want: types.F32(2)},
		{name: "f64", typ: types.TypeF64, initial: types.BoxF64(1), replacement: types.BoxF64(2), want: types.F64(2)},
	} {
		t.Run("ref set and get round-trip "+tt.name, func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0),
				instr.New(instr.REF_NEW),
				instr.New(instr.DUP),
				instr.New(instr.GLOBAL_GET, 1),
				instr.New(instr.REF_SET),
				instr.New(instr.REF_GET),
			}, program.WithGlobals(tt.typ, tt.typ))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			require.NoError(t, i.SetGlobal(0, tt.initial))
			require.NoError(t, i.SetGlobal(1, tt.replacement))

			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	t.Run("never tiers up below an unreachable threshold", func(t *testing.T) {
		b := program.NewBuilder()
		loop := b.Label()
		done := b.Label()
		b.Locals(types.TypeI32)
		b.Emit(instr.I32_CONST, 0).
			Emit(instr.LOCAL_SET, 0).
			Bind(loop).
			Emit(instr.LOCAL_GET, 0).
			Emit(instr.I32_CONST, 64).
			Emit(instr.I32_GE_S).
			BrIf(done).
			Emit(instr.LOCAL_GET, 0).
			Emit(instr.I32_CONST, 1).
			Emit(instr.I32_ADD).
			Emit(instr.LOCAL_SET, 0).
			Br(loop).
			Bind(done).
			Emit(instr.LOCAL_GET, 0)
		prog, err := b.Build()
		require.NoError(t, err)

		i := New(prog, WithTick(1<<20), WithThreshold(1<<30))
		defer i.Close()
		require.Empty(t, i.tracer.loops)

		require.NoError(t, i.Run(context.Background()))
		value, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(64), value)
		require.Empty(t, i.tracer.loops)
		require.Empty(t, i.tried)
	})

	if runtime.GOARCH == "arm64" {
		t.Run("preserves native handlers when cooling backedges", func(t *testing.T) {
			b := program.NewBuilder()
			loop := b.Label()
			done := b.Label()
			b.Locals(types.TypeI32)
			b.Emit(instr.I32_CONST, 0).
				Emit(instr.LOCAL_SET, 0).
				Bind(loop).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.I32_CONST, 2).
				Emit(instr.I32_GE_S).
				BrIf(done).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.I32_CONST, 1).
				Emit(instr.I32_ADD).
				Emit(instr.LOCAL_SET, 0).
				Br(loop).
				Bind(done).
				Emit(instr.LOCAL_GET, 0)
			prog, err := b.Build()
			require.NoError(t, err)

			i := New(prog, WithThreshold(1))
			defer i.Close()
			headers := i.tracer.headers(i, 0)
			require.NotEmpty(t, headers)
			root := anchor{ip: headers[0]}
			fallback := i.code[0][root.ip]
			native := func(*Interpreter) {}
			i.exits[root] = fallback
			i.code[0][root.ip] = native

			require.True(t, i.backedges[0])

			i.cool(0)

			require.Equal(t, reflect.ValueOf(native).Pointer(), reflect.ValueOf(i.code[0][root.ip]).Pointer())
			require.NotNil(t, i.exits[root])
			require.False(t, i.backedges[0])
		})
	}

	if runtime.GOARCH == "arm64" {
		t.Run("ARM64 in-loop branch rejoins the header natively", func(t *testing.T) {
			const size = int32(8)
			b := program.NewBuilder()
			b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
			loop := b.Label()
			odd := b.Label()
			advance := b.Label()
			done := b.Label()
			b.ConstGet(types.TypedArray[int32]{0, 1, 2, 3, 4, 5, 6, 7}).Emit(instr.LOCAL_SET, 0)
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET)
			b.Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(odd)
			b.Br(advance)
			b.Bind(odd)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
			b.Bind(advance)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Br(loop)
			b.Bind(done)
			b.Emit(instr.LOCAL_GET, 2)
			prog, err := b.Build()
			require.NoError(t, err)

			profile := prof.New()
			const runs = 32
			func() {
				jit := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
				threaded := New(prog, WithTick(1), WithThreshold(-1))
				defer func() {
					require.NoError(t, threaded.Close())
					require.NoError(t, jit.Close())
				}()
				for n := 0; n < runs; n++ {
					require.NoError(t, jit.Run(context.Background()))
					require.NoError(t, threaded.Run(context.Background()))
					got, err := jit.PopBoxed()
					require.NoError(t, err)
					want, err := threaded.PopBoxed()
					require.NoError(t, err)
					require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
					require.Equal(t, types.BoxI32(size/2), got)
					require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
					jit.Reset()
					threaded.Reset()
				}
			}()

			// ARRAY_GET's declared/const-array kind resolution (see arrayKind
			// in interp/jit_plan.go) now lets the static planner cover this
			// whole module, in-loop branch included, as one flat entry that
			// the compiler tries before the trace frontend's loop-anchored
			// fold path; either frontend must still avoid a repeated
			// exit-and-reenter round trip for the in-loop branch.
			entries := float64(0)
			for _, metric := range profile.Metrics() {
				if metric.Name != "vm_jit_native_entries_total" {
					continue
				}
				labels := map[string]string{}
				for _, label := range metric.Labels {
					labels[label.Key] = label.Value
				}
				if labels["func"] == "0" {
					entries += metric.Value
				}
			}
			require.Greater(t, entries, float64(0), "expected a native entry metric")
			require.Less(t, entries/runs, float64(8), "in-loop branch still exits the native loop")
		})

		t.Run("ARM64 folded return leg tears down the loop frame", func(t *testing.T) {
			const size = int32(8)
			body := types.NewFunctionBuilder(nil).Captures(types.TypeI32Array).Returns(types.TypeI32)
			body.Locals(types.TypeI32, types.TypeI32)
			done := body.Label()
			body.Emit(instr.New(instr.I32_CONST, 0)).Emit(instr.New(instr.LOCAL_SET, 0))
			body.Emit(instr.New(instr.I32_CONST, 0)).Emit(instr.New(instr.LOCAL_SET, 1))
			loop := body.Label()
			body.Bind(loop)
			body.Emit(instr.New(instr.LOCAL_GET, 0)).Emit(instr.New(instr.I32_CONST, uint64(uint32(size)))).Emit(instr.New(instr.I32_GE_S)).BrIf(done)
			body.Emit(instr.New(instr.LOCAL_GET, 1)).Emit(instr.New(instr.UPVAL_GET, 0)).Emit(instr.New(instr.LOCAL_GET, 0)).Emit(instr.New(instr.ARRAY_GET)).Emit(instr.New(instr.I32_ADD)).Emit(instr.New(instr.LOCAL_SET, 1))
			body.Emit(instr.New(instr.LOCAL_GET, 0)).Emit(instr.New(instr.I32_CONST, 1)).Emit(instr.New(instr.I32_ADD)).Emit(instr.New(instr.LOCAL_SET, 0))
			body.Br(loop)
			body.Bind(done)
			body.Emit(instr.New(instr.LOCAL_GET, 1)).Emit(instr.New(instr.RETURN))
			fn, err := body.Build()
			require.NoError(t, err)

			b := program.NewBuilder()
			b.Const(fn)
			b.ConstGet(types.TypedArray[int32]{3, 3, 3, 3, 3, 3, 3, 3})
			b.ConstGet(fn).Emit(instr.CLOSURE_NEW).Emit(instr.CALL)
			prog, err := b.Build()
			require.NoError(t, err)

			jit := New(prog, WithTick(1), WithThreshold(0))
			threaded := New(prog, WithTick(1), WithThreshold(-1))
			t.Cleanup(func() { require.NoError(t, threaded.Close()) })
			t.Cleanup(func() { require.NoError(t, jit.Close()) })
			for n := 0; n < 32; n++ {
				require.NoError(t, jit.Run(context.Background()))
				require.NoError(t, threaded.Run(context.Background()))
				got, err := jit.PopBoxed()
				require.NoError(t, err)
				want, err := threaded.PopBoxed()
				require.NoError(t, err)
				require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
				require.Equal(t, types.BoxI32(3*size), got)
				require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
				jit.Reset()
				threaded.Reset()
			}
		})

		t.Run("ARM64 folded completed leg finishes top-level code", func(t *testing.T) {
			const size = int32(8)
			b := program.NewBuilder()
			b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
			loop := b.Label()
			done := b.Label()
			b.ConstGet(types.TypedArray[int32]{1, 2, 3, 4, 5, 6, 7, 8}).Emit(instr.LOCAL_SET, 0)
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Br(loop)
			b.Bind(done)
			b.Emit(instr.LOCAL_GET, 2)
			prog, err := b.Build()
			require.NoError(t, err)

			jit := New(prog, WithTick(1), WithThreshold(0))
			threaded := New(prog, WithTick(1), WithThreshold(-1))
			t.Cleanup(func() { require.NoError(t, threaded.Close()) })
			t.Cleanup(func() { require.NoError(t, jit.Close()) })
			for n := 0; n < 32; n++ {
				require.NoError(t, jit.Run(context.Background()))
				require.NoError(t, threaded.Run(context.Background()))
				got, err := jit.PopBoxed()
				require.NoError(t, err)
				want, err := threaded.PopBoxed()
				require.NoError(t, err)
				require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
				require.Equal(t, types.BoxI32(36), got)
				require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
				jit.Reset()
				threaded.Reset()
			}
		})
	}
	modes := []struct {
		name string
		opts []Option
	}{
		{name: "standalone", opts: []Option{WithTick(1), WithThreshold(-1)}},
		{name: "fused", opts: []Option{WithThreshold(-1)}},
	}
	for _, tt := range runTests {
		name := runTestName(tt.program)
		for _, mode := range modes {
			t.Run(name+"/"+mode.name, func(t *testing.T) {
				i := New(tt.program, mode.opts...)
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
				require.Equal(t, len(tt.program.Locals), i.Len(), "unexpected values remain on the operand stack")
			})
		}
	}

	var benchmarkNumeric []instr.Instruction
	for range 64 {
		benchmarkNumeric = append(benchmarkNumeric,
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.DROP),
		)
	}
	benchmarkNumeric = append(benchmarkNumeric, instr.New(instr.I32_CONST, 42))

	if runtime.GOARCH == "arm64" {
		t.Run("I32Add/Straight/JITCold", func(t *testing.T) {
			profile := prof.New()
			vm := New(program.New(benchmarkNumeric), WithTick(1), WithThreshold(0), WithProfiler(profile))
			defer vm.Close()

			require.NoError(t, vm.Run(context.Background()))
			value, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(42), value)
			vm.Flush()
			var emits float64
			for _, metric := range profile.Metrics() {
				if metric.Name == "vm_jit_emits_total" {
					emits += metric.Value
				}
			}
			require.Greater(t, emits, float64(0))
		})

		arrayExitValues := types.TypedArray[float64]{7}
		arrayExitBuilder := types.NewFunctionBuilder(nil).
			Params(types.TypeI32, types.TypeF64Array).
			Returns(types.TypeF64)
		arrayExitFunction, err := arrayExitBuilder.Emit(
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.ARRAY_GET),
			instr.New(instr.RETURN),
		).Build()
		require.NoError(t, err)
		arrayExit := program.New(
			[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.CALL)},
			program.WithConstants(arrayExitValues, arrayExitFunction),
		)
		t.Run("Array/Get/JITExit", func(t *testing.T) {
			profile := prof.New()
			vm := New(arrayExit, WithProfiler(profile), WithTick(1), WithThreshold(0))
			for range 8 {
				vm.Reset()
				require.NoError(t, vm.Push(types.I32(0)))
				require.NoError(t, vm.Run(context.Background()))
				value, err := vm.Pop()
				require.NoError(t, err)
				require.Equal(t, types.F64(7), value)
			}

			vm.Reset()
			require.NoError(t, vm.Push(types.I32(-1)))
			require.ErrorIs(t, vm.Run(context.Background()), ErrIndexOutOfRange)
			require.NoError(t, vm.Close())

			var exits float64
			for _, metric := range profile.Metrics() {
				if metric.Name == "vm_jit_native_exits_total" {
					exits += metric.Value
				}
			}
			require.Greater(t, exits, float64(0))
		})

		divideFunction := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Params(types.TypeI32, types.TypeI32).
			Emit(
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.I32_DIV_S),
				instr.New(instr.RETURN),
			).
			MustBuild()
		divide := program.New(
			[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
			program.WithConstants(divideFunction),
		)
		t.Run("I32Div/JITDeopt", func(t *testing.T) {
			profile := prof.New()
			vm := New(divide, WithProfiler(profile), WithTick(1), WithThreshold(0))
			for range 8 {
				vm.Reset()
				require.NoError(t, vm.Push(types.I32(90)))
				require.NoError(t, vm.Push(types.I32(3)))
				require.NoError(t, vm.Run(context.Background()))
				value, err := vm.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I32(30), value)
			}
			require.NotNil(t, vm.stub(1))

			vm.Reset()
			require.NoError(t, vm.Push(types.I32(90)))
			require.NoError(t, vm.Push(types.I32(0)))
			require.ErrorIs(t, vm.Run(context.Background()), ErrDivideByZero)
			require.NoError(t, vm.Close())

			var exits float64
			for _, metric := range profile.Metrics() {
				if metric.Name == "vm_jit_native_exits_total" {
					exits += metric.Value
				}
			}
			require.Greater(t, exits, float64(0))
		})
	}

	parityPrograms := []struct {
		name string
		prog *program.Program
	}{
		{
			name: "integer arithmetic",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 20),
				instr.New(instr.I32_CONST, 22),
				instr.New(instr.I32_ADD),
			}),
		},
		{
			name: "local arithmetic store",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_ADD),
				instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.LOCAL_GET, 0),
			}, program.WithLocals(types.TypeI32)),
		},
		{
			name: "global mutation",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 7),
				instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.GLOBAL_GET, 0),
			}, program.WithGlobals(types.TypeI32)),
		},
		{
			name: "array access",
			prog: program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_GET),
			}, program.WithConstants(types.TypedArray[int32]{10, 20, 30})),
		},
		{
			name: "divide by zero trap",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_DIV_S),
			}),
		},
		{
			name: "coroutine state",
			prog: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
					instr.New(instr.CORO_DONE),
				},
				program.WithConstants(
					types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.YIELD),
						instr.New(instr.RETURN),
					).MustBuild(),
				),
			),
		},
	}
	type outcome struct {
		values  []types.Value
		globals []types.Boxed
		code    types.ErrorCode
	}
	run := func(t *testing.T, prog *program.Program, opts ...Option) outcome {
		t.Helper()
		i := New(prog, opts...)
		defer i.Close()
		err := i.Run(context.Background())
		result := outcome{code: ErrorCode(err)}
		for i.Len() > 0 {
			value, popErr := i.Pop()
			require.NoError(t, popErr)
			result.values = append(result.values, value)
		}
		for index := range prog.Globals {
			value, globalErr := i.Global(index)
			require.NoError(t, globalErr)
			result.globals = append(result.globals, value)
		}
		return result
	}
	for _, tt := range parityPrograms {
		oracle := run(t, tt.prog, WithTick(1), WithThreshold(-1))
		t.Run("parity/"+tt.name+"/fused", func(t *testing.T) {
			require.Equal(t, oracle, run(t, tt.prog, WithThreshold(-1)))
		})
		if runtime.GOARCH == "arm64" {
			t.Run("parity/"+tt.name+"/jit warm", func(t *testing.T) {
				i := New(tt.prog, WithThreshold(0))
				defer i.Close()
				require.Equal(t, oracle.code, ErrorCode(i.Run(context.Background())))
				i.Reset()

				err := i.Run(context.Background())
				result := outcome{code: ErrorCode(err)}
				for i.Len() > 0 {
					value, popErr := i.Pop()
					require.NoError(t, popErr)
					result.values = append(result.values, value)
				}
				for index := range tt.prog.Globals {
					value, globalErr := i.Global(index)
					require.NoError(t, globalErr)
					result.globals = append(result.globals, value)
				}
				require.Equal(t, oracle, result)
			})
		}
	}

	t.Run("parity/host callback effect", func(t *testing.T) {
		runHost := func(opts ...Option) (types.Value, int) {
			calls := 0
			host := NewHostFunction(
				&types.FunctionType{Returns: []types.Type{types.TypeI32}},
				func(_ *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
					calls++
					return []types.Boxed{types.BoxI32(42)}, nil
				},
			)
			prog := program.New(
				[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
				program.WithConstants(host),
			)
			i := New(prog, opts...)
			defer i.Close()
			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			return value, calls
		}

		want, calls := runHost(WithTick(1), WithThreshold(-1))
		require.Equal(t, 1, calls)
		got, calls := runHost(WithThreshold(-1))
		require.Equal(t, want, got)
		require.Equal(t, 1, calls)
		if runtime.GOARCH == "arm64" {
			got, calls = runHost(WithThreshold(0))
			require.Equal(t, want, got)
			require.Equal(t, 1, calls)
		}
	})

	t.Run("parity/host struct field write reaches the Go value", func(t *testing.T) {
		type counter struct {
			Count  int32
			hidden int32
		}
		bump := func(c *counter) int32 { c.hidden++; return c.Count }

		runHost := func(opts ...Option) (types.Value, int32) {
			setup := New(program.New(nil))
			defer setup.Close()
			src := &counter{Count: 7}
			host, err := NewRegistry().Marshal(setup, src)
			require.NoError(t, err)

			prog := program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.DUP),
				instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 99), instr.New(instr.STRUCT_SET),
				instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
			}, program.WithConstants(host))
			i := New(prog, opts...)
			defer i.Close()
			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			// The Go value carries the write, so a method reading it agrees
			// with what the guest just stored.
			return value, bump(src)
		}

		want, seen := runHost(WithTick(1), WithThreshold(-1))
		require.Equal(t, types.I32(99), want)
		require.Equal(t, int32(99), seen)
		got, seen := runHost(WithThreshold(-1))
		require.Equal(t, want, got)
		require.Equal(t, int32(99), seen)
		if runtime.GOARCH == "arm64" {
			got, seen = runHost(WithThreshold(0))
			require.Equal(t, want, got)
			require.Equal(t, int32(99), seen)
		}
	})

	t.Run("parity/host container writes reach the Go value", func(t *testing.T) {
		runHost := func(value any, code []instr.Instruction, opts ...Option) types.Value {
			setup := New(program.New(nil))
			defer setup.Close()
			host, err := NewRegistry().Marshal(setup, value)
			require.NoError(t, err)

			i := New(program.New(code, program.WithConstants(host)), opts...)
			defer i.Close()
			require.NoError(t, i.Run(context.Background()))
			out, err := i.Pop()
			require.NoError(t, err)
			return out
		}

		for _, tt := range []struct {
			name string
			run  func(opts ...Option) (types.Value, any)
			want any
		}{
			{
				name: "array element",
				run: func(opts ...Option) (types.Value, any) {
					src := []int32{7}
					out := runHost(src, []instr.Instruction{
						instr.New(instr.CONST_GET, 0),
						instr.New(instr.DUP),
						instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 99), instr.New(instr.ARRAY_SET),
						instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
					}, opts...)
					return out, src
				},
				want: []int32{99},
			},
			{
				name: "map entry",
				run: func(opts ...Option) (types.Value, any) {
					src := map[int32]int32{}
					out := runHost(src, []instr.Instruction{
						instr.New(instr.CONST_GET, 0),
						instr.New(instr.DUP),
						instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 99), instr.New(instr.MAP_SET),
						instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_GET),
					}, opts...)
					return out, src
				},
				want: map[int32]int32{1: 99},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				// The guest wrote into Go memory, so every mode leaves the
				// write in the Go value rather than in a VM copy of it.
				value, src := tt.run(WithTick(1), WithThreshold(-1))
				require.Equal(t, types.I32(99), value)
				require.Equal(t, tt.want, src)

				value, src = tt.run(WithThreshold(-1))
				require.Equal(t, types.I32(99), value)
				require.Equal(t, tt.want, src)

				if runtime.GOARCH == "arm64" {
					value, src = tt.run(WithThreshold(0))
					require.Equal(t, types.I32(99), value)
					require.Equal(t, tt.want, src)
				}
			})
		}
	})

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

	t.Run("SELECT keeps the selected ref and releases the discarded ref", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.I32_CONST, 2), instr.New(instr.REF_NEW), // heap[2]
			instr.New(instr.I32_CONST, 1), // cond != 0 selects the deeper operand
			instr.New(instr.SELECT),
		})
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		top, err := i.Peek(0)
		require.NoError(t, err)
		require.Equal(t, 1, top.Ref())
		rc1, err := i.RefCount(1)
		require.NoError(t, err)
		require.Equal(t, 1, rc1) // selected ref survives on the stack
		_, err = i.RefCount(2)
		require.ErrorIs(t, err, ErrSegmentationFault) // discarded ref released to zero
	})

	t.Run("GLOBAL_TEE retains the ref stored into the global slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.GLOBAL_TEE, 0), // duplicates ownership: stack + global
			instr.New(instr.DROP),          // drop stack copy; global still owns
		}, program.WithGlobals(types.TypeAny))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		g, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, 1, g.Ref())
		rc, err := i.RefCount(1)
		require.NoError(t, err)
		require.Equal(t, 1, rc) // global slot keeps the ref alive
	})

	t.Run("LOCAL_TEE retains the ref stored into the local slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.LOCAL_TEE, 0), // duplicates ownership: stack + local
			instr.New(instr.DROP),         // drop stack copy; local still owns
		}, program.WithLocals(types.TypeI32Array))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		l, err := i.Local(0)
		require.NoError(t, err)
		require.Equal(t, 1, l.Ref())
		rc, err := i.RefCount(1)
		require.NoError(t, err)
		require.Equal(t, 1, rc) // local slot keeps the ref alive
	})

	t.Run("REF_EQ releases both consumed refs", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.I32_CONST, 2), instr.New(instr.REF_NEW), // heap[2]
			instr.New(instr.REF_EQ),
		})
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		_, err := i.RefCount(1)
		require.ErrorIs(t, err, ErrSegmentationFault)
		_, err = i.RefCount(2)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})

	t.Run("REF_NE releases both consumed refs", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.I32_CONST, 2), instr.New(instr.REF_NEW), // heap[2]
			instr.New(instr.REF_NE),
		})
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		_, err := i.RefCount(1)
		require.ErrorIs(t, err, ErrSegmentationFault)
		_, err = i.RefCount(2)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})

	t.Run("REF_TEST releases the consumed ref", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.REF_TEST, 0),
		}, program.WithTypes(types.TypeI32))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		_, err := i.RefCount(1)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})

	t.Run("REF_IS_NULL releases the consumed ref", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.REF_IS_NULL),
		})
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		_, err := i.RefCount(1)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})

	t.Run("fused trapping sources use the remaining stack slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(8))),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2))),
			instr.New(instr.F32_DIV),
		}, program.WithGlobals(types.TypeF32))
		i := New(prog, WithStack(2), WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.F32(4), value)
	})

	t.Run("fused trapping sources report overflow on the second push", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(8))),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2))),
			instr.New(instr.F32_DIV),
		}, program.WithGlobals(types.TypeF32))
		i := New(prog, WithStack(1), WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrStackOverflow)
		require.Equal(t, 1, i.Len())
	})

	t.Run("CONST_GET reports overflow before retaining a ref", func(t *testing.T) {
		fn := types.NewFunction(&types.FunctionType{}, nil, nil)
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.CONST_GET, 0),
		}, program.WithConstants(fn))
		i := New(prog, WithStack(1), WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrStackOverflow)
		require.Equal(t, 1, i.Len())
	})

	t.Run("fused REF_IS_NULL reports overflow before pushing", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.REF_IS_NULL),
		}, program.WithGlobals(types.TypeAny))
		i := New(prog, WithStack(1), WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrStackOverflow)
		require.Equal(t, 1, i.Len())
	})

	t.Run("fused sources push once and need one free slot", func(t *testing.T) {
		// Both operands stay in temporaries, so the fused handler grows the
		// stack by exactly one slot no matter how many sources it folded.
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 6),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.I32_ADD),
		}, program.WithGlobals(types.TypeI32))
		i := New(prog, WithStack(1), WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(12), value)
	})

	t.Run("fused sources report overflow before reading their operands", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 6),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.I32_ADD),
		}, program.WithGlobals(types.TypeI32))
		i := New(prog, WithStack(1), WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrStackOverflow)
		require.Equal(t, 1, i.Len())
	})

	t.Run("STRUCT_NEW_DEFAULT reports stack overflow before mutating sp", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.STRUCT_NEW_DEFAULT, 0),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32))))
		i := New(prog, WithStack(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrStackOverflow)
		require.Equal(t, 1, i.Len())
	})

	t.Run("LOCAL_GET rejects one-past-current local slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.DROP),
			instr.New(instr.LOCAL_GET, 0),
		}, program.WithLocals(types.TypeI32))
		i := New(prog, WithTick(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrSegmentationFault)
	})

	t.Run("LOCAL_GET rejects undeclared metadata without panicking during threading", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.LOCAL_GET, 0)})
		i := New(prog, WithTick(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrSegmentationFault)
	})

	t.Run("LOCAL_SET rejects one-past-current local slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.DROP),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.LOCAL_SET, 1),
		}, program.WithLocals(types.TypeI32, types.TypeI32))
		i := New(prog, WithTick(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrSegmentationFault)
	})

	t.Run("LOCAL_TEE rejects one-past-current local slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.DROP),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.LOCAL_TEE, 1),
		}, program.WithLocals(types.TypeI32, types.TypeI32))
		i := New(prog, WithTick(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrSegmentationFault)
	})

	t.Run("fused LOCAL_GET rejects one-past-current local slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.DROP),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, i32operand(1)),
			instr.New(instr.I32_ADD),
		}, program.WithLocals(types.TypeI32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrSegmentationFault)
	})

	t.Run("GLOBAL_SET rejects an undeclared global slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.GLOBAL_SET, 0),
		})
		i := New(prog)
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrSegmentationFault)
	})

	t.Run("GLOBAL_TEE rejects an undeclared global slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.GLOBAL_TEE, 0),
		})
		i := New(prog)
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrSegmentationFault)
	})

	t.Run("unseeded declared globals read kind-correct zeros", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.I32_CONST, i32operand(2)),
			instr.New(instr.I32_ADD), // fuses without any prior GLOBAL_SET/SetGlobal
			instr.New(instr.GLOBAL_GET, 1),
			instr.New(instr.GLOBAL_GET, 2),
		}, program.WithGlobals(types.TypeI32, types.TypeF64, types.TypeAny))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 3, i.Len())
		v0, err := i.Peek(2)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(2), v0)
		v1, err := i.Peek(1)
		require.NoError(t, err)
		require.Equal(t, types.BoxF64(0), v1)
		v2, err := i.Peek(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxedNull, v2)
	})

	t.Run("GLOBAL_GET declares and reads an I32 global with a fused superinstruction", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.I32_CONST, i32operand(2)),
			instr.New(instr.I32_ADD),
		}, program.WithGlobals(types.TypeI32))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 1, i.Len())
		v, err := i.Peek(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(7), v)
	})

	t.Run("GLOBAL_TEE retains the ref stored into a declared ref global", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.GLOBAL_TEE, 0),
			instr.New(instr.DROP),
		}, program.WithGlobals(types.TypeAny))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		g, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, 1, g.Ref())
		rc, err := i.RefCount(1)
		require.NoError(t, err)
		require.Equal(t, 1, rc)
	})

	t.Run("I64 local rejects non-I64 heap refs", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.REF_NEW),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I64_CONST, i64operand(1)),
			instr.New(instr.I64_ADD),
		}, program.WithLocals(types.TypeI64))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrTypeMismatch)
	})

	t.Run("ARRAY_NEW_DEFAULT rejects negative size with VM error", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-1)),
			instr.New(instr.ARRAY_NEW_DEFAULT, 0),
		}, program.WithTypes(types.TypeI32Array))
		i := New(prog)
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrSegmentationFault)
	})

	t.Run("ARRAY_FILL releases every overwritten ref element", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW_DEFAULT, 1), // outer heap[1]
			instr.New(instr.DUP), instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0), // inner heap[2]
			instr.New(instr.ARRAY_SET),
			instr.New(instr.DUP), instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0), // inner heap[3]
			instr.New(instr.ARRAY_SET),
			instr.New(instr.DUP), instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0), // inner heap[4]
			instr.New(instr.ARRAY_SET),
			instr.New(instr.DUP), instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0), // fill value heap[5]
			instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_FILL),
		}, program.WithTypes(types.TypeI32Array, types.NewArrayType(types.TypeI32Array)))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		_, err := i.RefCount(2)
		require.ErrorIs(t, err, ErrSegmentationFault) // every overwritten element is released,
		_, err = i.RefCount(3)
		require.ErrorIs(t, err, ErrSegmentationFault) // not just the first one
		_, err = i.RefCount(4)
		require.ErrorIs(t, err, ErrSegmentationFault)
		rc5, err := i.RefCount(5)
		require.NoError(t, err)
		require.Equal(t, 3, rc5) // fill value owned once per filled slot
	})

	t.Run("host call with an all-scalar signature works through the generic path (exact, fusion disabled)", func(t *testing.T) {
		hostFn := NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			func(_ *Interpreter, args []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(args[0].I32() * args[1].I32())}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 6), instr.New(instr.I32_CONST, 7),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(hostFn))
		i := New(prog, WithTick(1)) // exact: disables fusion, forcing the generic callHost path
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), v)
	})

	t.Run("host call releases a ref param the callee does not return (fused)", func(t *testing.T) {
		hostFn := NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeAny}, Returns: []types.Type{types.TypeI32}},
			func(_ *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(1)}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 9), instr.New(instr.REF_NEW), // heap[1] is hostFn; heap[2] is this ref
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(hostFn))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.RefCount(2)
		require.ErrorIs(t, err, ErrSegmentationFault) // arg not returned: host cleanup released it
	})

	t.Run("host call releases a ref param the callee does not return (generic, exact)", func(t *testing.T) {
		hostFn := NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeAny}, Returns: []types.Type{types.TypeI32}},
			func(_ *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(1)}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 9), instr.New(instr.REF_NEW),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(hostFn))
		i := New(prog, WithTick(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.RefCount(2)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})

	for _, tt := range []struct {
		name string
		opts []Option
	}{
		{name: "fused"},
		{name: "generic", opts: []Option{WithTick(1)}},
	} {
		t.Run("host call releases the consumed callable ref on fused and generic paths "+tt.name, func(t *testing.T) {
			hostFn := NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
				func(_ *Interpreter, args []types.Boxed) ([]types.Boxed, error) {
					return []types.Boxed{args[0]}, nil
				})
			prog := program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 9),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			}, program.WithConstants(hostFn))
			i := New(prog, tt.opts...)
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			rc, err := i.RefCount(1)
			require.NoError(t, err)
			require.Equal(t, 1, rc)
		})
	}

	t.Run("generic host call can return the consumed callable ref", func(t *testing.T) {
		hostFn := NewHostFunction(&types.FunctionType{Returns: []types.Type{types.TypeAny}},
			func(i *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				v, peekErr := i.Peek(0)
				if peekErr != nil {
					return nil, peekErr
				}
				return []types.Boxed{v}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(hostFn))
		i := New(prog, WithTick(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		rc, err := i.RefCount(1)
		require.NoError(t, err)
		require.Equal(t, 2, rc)
	})

	t.Run("host call releases a promoted i64 param even though I64 is declared (not the scalar fast path)", func(t *testing.T) {
		huge := int64(1) << 50
		hostFn := NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI32}},
			func(_ *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(1)}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(huge)), // heap[1] is hostFn; heap[2] is this promoted i64
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(hostFn))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.RefCount(2)
		require.ErrorIs(t, err, ErrSegmentationFault) // promoted i64 arg released: I64 params keep the generic scanning path
	})

	t.Run("UPVAL_GET retains a ref capture (generic path)", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{}).
			Captures(types.TypeAny).Emit(
			instr.New(instr.UPVAL_GET, 0), instr.New(instr.DROP),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.REF_NEW), // heap[1] is fn; heap[2] is this capture
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		maxRC := 0
		i := New(prog, WithTick(1), WithHook(func(i *Interpreter) error {
			if count, hookErr := i.RefCount(2); hookErr == nil && count > maxRC {
				maxRC = count
			}
			return nil
		}))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 2, maxRC) // UPVAL_GET's retainBox held the capture live alongside its pushed copy
	})

	t.Run("UPVAL_SET releases a ref capture when overwritten (generic path)", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Captures(types.TypeAny).Emit(
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW),
			instr.New(instr.UPVAL_SET, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.REF_NEW), // heap[1] is fn; heap[2] is this capture
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.RefCount(2)
		require.ErrorIs(t, err, ErrSegmentationFault) // old ref capture released on overwrite
	})

	t.Run("UPVAL_SET releases a promoted i64 capture even though I64 is declared (not the scalar fast path)", func(t *testing.T) {
		oldHuge := int64(1) << 50
		newHuge := int64(1) << 51
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI64}}).
			Captures(types.TypeI64).Emit(
			instr.New(instr.I64_CONST, i64operand(newHuge)),
			instr.New(instr.UPVAL_SET, 0),
			instr.New(instr.UPVAL_GET, 0),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(oldHuge)), // heap[1] is fn; heap[2] is the old promoted capture
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.RefCount(2)
		require.ErrorIs(t, err, ErrSegmentationFault) // old promoted capture released: I64 captures keep the generic ref-aware path
	})

	t.Run("fused LOCAL_GET+CONST binop computes correctly for i32 (interp-only)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.I32_ADD),
		}, program.WithLocals(types.TypeI32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(8), v)
	})

	t.Run("fused LOCAL_GET+CONST binop computes correctly for i64 (interp-only)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(5)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_CONST, i64operand(3)), instr.New(instr.I64_ADD),
		}, program.WithLocals(types.TypeI64))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I64(8), v)
	})

	t.Run("fused LOCAL_GET+CONST binop computes correctly for f32 (interp-only)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(5))), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_ADD),
		}, program.WithLocals(types.TypeF32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.F32(8), v)
	})

	t.Run("fused LOCAL_GET+CONST binop computes correctly for f64 (interp-only)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(5)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_ADD),
		}, program.WithLocals(types.TypeF64))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.F64(8), v)
	})

	t.Run("fused LOCAL_GET+LOCAL_GET binop computes correctly for i32 (interp-only)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD),
		}, program.WithLocals(types.TypeI32, types.TypeI32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(8), v)
	})

	type parityState struct {
		code    types.ErrorCode
		ip      int
		fp      int
		sp      int
		stack   []types.Boxed
		globals []types.Boxed
		rc      map[int]int
	}

	huge := int64(1) << 50
	fn := types.NewFunctionBuilder(nil).Emit(instr.New(instr.RETURN)).MustBuild()
	parity := []struct {
		name string
		prog *program.Program
		err  error
	}{
		{
			name: "promoted i64 eqz branch preserves state",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)),
				instr.New(instr.I64_EQZ),
				instr.New(instr.BR_IF, 0),
			}),
		},
		{
			name: "promoted i64 comparison branch preserves state",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)),
				instr.New(instr.I64_CONST, i64operand(huge)),
				instr.New(instr.I64_EQ),
				instr.New(instr.BR_IF, 0),
			}),
		},
		{
			name: "promoted i64 local binary preserves state",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(1)),
				instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.I64_CONST, i64operand(huge)),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I64_ADD),
				instr.New(instr.DROP),
			}, program.WithLocals(types.TypeI64)),
		},
		{
			name: "local ref drop preserves ownership",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 7),
				instr.New(instr.REF_NEW),
				instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.DROP),
			}, program.WithLocals(types.TypeAny)),
		},
		{
			name: "function constant drop preserves ownership",
			prog: program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.DROP),
			}, program.WithConstants(fn)),
		},
		{
			name: "string constant drop preserves ownership",
			prog: program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.DROP),
			}, program.WithConstants(types.String("value"))),
		},
		{
			name: "i32 divide by zero preserves trap state",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 90),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_DIV_S),
			}),
			err: ErrDivideByZero,
		},
		{
			name: "promoted i64 divide by zero preserves trap state",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)),
				instr.New(instr.I64_CONST, 0),
				instr.New(instr.I64_DIV_S),
			}),
			err: ErrDivideByZero,
		},
		{
			name: "promoted i64 local divide by zero preserves trap state",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)),
				instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I64_CONST, 0),
				instr.New(instr.I64_DIV_S),
			}, program.WithLocals(types.TypeI64)),
			err: ErrDivideByZero,
		},
	}
	for _, tt := range parity {
		t.Run(tt.name, func(t *testing.T) {
			states := make([]parityState, 0, 2)
			for _, opts := range [][]Option{
				{WithTick(1)},
				{WithThreshold(-1)},
			} {
				i := New(tt.prog, opts...)
				err := i.Run(context.Background())
				if tt.err == nil {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, tt.err)
				}

				state := parityState{
					code: ErrorCode(err),
					ip:   i.IP(),
					fp:   i.FP(),
					sp:   i.Len(),
					rc:   make(map[int]int),
				}
				for idx := 0; idx < state.sp; idx++ {
					v, peekErr := i.Peek(state.sp - 1 - idx)
					require.NoError(t, peekErr)
					state.stack = append(state.stack, v)
				}
				for idx := range tt.prog.Globals {
					v, globalErr := i.Global(idx)
					require.NoError(t, globalErr)
					state.globals = append(state.globals, v)
				}
				for addr := 1; addr < i.HeapCap(); addr++ {
					count, rcErr := i.RefCount(addr)
					if rcErr != nil || count == 0 {
						continue
					}
					state.rc[addr] = count
				}
				states = append(states, state)
				require.NoError(t, i.Close())
			}
			require.Equal(t, states[0], states[1])
		})
	}

	// Regression: fused rhs loaders must borrow promoted I64 values without
	// releasing the reference owned by the source slot.
	huge = int64(1) << 62
	upval := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI64}}).
		Captures(types.TypeI64).Emit(
		instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.UPVAL_GET, 0), instr.New(instr.I64_ADD), instr.New(instr.DROP),
		instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.UPVAL_GET, 0), instr.New(instr.I64_ADD),
		instr.New(instr.RETURN),
	).MustBuild()

	upvalI32Const := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Captures(types.TypeI32).Emit(
		instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
	).MustBuild()
	upvalI64Const := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI64}}).
		Captures(types.TypeI64).Emit(
		instr.New(instr.UPVAL_GET, 0), instr.New(instr.I64_CONST, i64operand(3)), instr.New(instr.I64_ADD), instr.New(instr.RETURN),
	).MustBuild()
	upvalF32Const := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeF32}}).
		Captures(types.TypeF32).Emit(
		instr.New(instr.UPVAL_GET, 0), instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_ADD), instr.New(instr.RETURN),
	).MustBuild()
	upvalF64Const := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeF64}}).
		Captures(types.TypeF64).Emit(
		instr.New(instr.UPVAL_GET, 0), instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_ADD), instr.New(instr.RETURN),
	).MustBuild()
	upvalLocal := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Captures(types.TypeI32).Locals(types.TypeI32).Emit(
		instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.LOCAL_SET, 0),
		instr.New(instr.UPVAL_GET, 0), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
	).MustBuild()
	globalUpval := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI64}}).
		Captures(types.TypeI64).Emit(
		instr.New(instr.GLOBAL_GET, 0), instr.New(instr.UPVAL_GET, 0), instr.New(instr.I64_ADD), instr.New(instr.RETURN),
	).MustBuild()
	upvalPair := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Captures(types.TypeI32, types.TypeI32).Emit(
		instr.New(instr.UPVAL_GET, 0), instr.New(instr.UPVAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
	).MustBuild()
	fusions := []struct {
		name string
		prog *program.Program
		want types.Value
	}{
		{
			name: "local and local i64",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(5)), instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.I64_CONST, i64operand(3)), instr.New(instr.LOCAL_SET, 1),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I64_ADD),
			}, program.WithLocals(types.TypeI64, types.TypeI64)),
			want: types.I64(8),
		},
		{
			name: "local and local f32",
			prog: program.New([]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(5))), instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.LOCAL_SET, 1),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.F32_ADD),
			}, program.WithLocals(types.TypeF32, types.TypeF32)),
			want: types.F32(8),
		},
		{
			name: "local and local f64",
			prog: program.New([]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(5)), instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.LOCAL_SET, 1),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.F64_ADD),
			}, program.WithLocals(types.TypeF64, types.TypeF64)),
			want: types.F64(8),
		},
		{
			name: "upval and i32 constant",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.CALL),
			}, program.WithConstants(upvalI32Const)),
			want: types.I32(8),
		},
		{
			name: "upval and i64 constant",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(5)), instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.CALL),
			}, program.WithConstants(upvalI64Const)),
			want: types.I64(8),
		},
		{
			name: "upval and f32 constant",
			prog: program.New([]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(5))), instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.CALL),
			}, program.WithConstants(upvalF32Const)),
			want: types.F32(8),
		},
		{
			name: "upval and f64 constant",
			prog: program.New([]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(5)), instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.CALL),
			}, program.WithConstants(upvalF64Const)),
			want: types.F64(8),
		},
		{
			name: "upval and local",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.CALL),
			}, program.WithConstants(upvalLocal)),
			want: types.I32(8),
		},
		{
			name: "global and i32 constant",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.I32_ADD),
			}, program.WithGlobals(types.TypeI32)),
			want: types.I32(8),
		},
		{
			name: "two globals",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.GLOBAL_SET, 1),
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.I32_ADD),
			}, program.WithGlobals(types.TypeI32, types.TypeI32)),
			want: types.I32(8),
		},
		{
			name: "global and upval",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(5)), instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.I64_CONST, i64operand(3)), instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.CALL),
			}, program.WithConstants(globalUpval), program.WithGlobals(types.TypeI64)),
			want: types.I64(8),
		},
		{
			name: "two upvals",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.I32_CONST, i32operand(3)),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.CALL),
			}, program.WithConstants(upvalPair)),
			want: types.I32(8),
		},
	}
	for _, tt := range fusions {
		t.Run("fuses "+tt.name, func(t *testing.T) {
			i := New(tt.prog, WithThreshold(-1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	refs := []struct {
		name string
		prog *program.Program
		want types.Value
		refs int
	}{
		{
			name: "repeated local reads keep the local reference",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)), instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_ADD), instr.New(instr.DROP),
				instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_ADD),
			}, program.WithLocals(types.TypeI64)),
			want: types.I64(huge + 1),
			refs: 1,
		},
		{
			name: "mixed local reads keep the local reference",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)), instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.I64_ADD), instr.New(instr.DROP),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_ADD),
			}, program.WithLocals(types.TypeI64)),
			want: types.I64(2 * huge),
			refs: 1,
		},
		{
			name: "repeated global reads keep the global reference",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)), instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I64_ADD), instr.New(instr.DROP),
				instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I64_ADD),
			}, program.WithGlobals(types.TypeI64)),
			want: types.I64(huge + 1),
			refs: 1,
		},
		{
			name: "repeated upval reads preserve the captured value",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.CALL),
			}, program.WithConstants(upval)),
			want: types.I64(huge + 1),
			refs: 1,
		},
		{
			name: "paired global reads preserve the global reference",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)), instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I64_ADD), instr.New(instr.DROP),
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I64_ADD),
			}, program.WithGlobals(types.TypeI64)),
			want: types.I64(2 * huge),
			refs: 1,
		},
	}
	for _, tt := range refs {
		t.Run(tt.name, func(t *testing.T) {
			i := New(tt.prog, WithThreshold(-1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			live := 0
			for addr := 1; addr < i.HeapCap(); addr++ {
				count, rcErr := i.RefCount(addr)
				if rcErr != nil {
					continue
				}
				live += count
			}
			require.Equal(t, tt.refs, live)
		})
	}

	t.Run("global/upval pair fusion is disabled in exact mode and still computes correctly", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.GLOBAL_SET, 1),
			instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.I32_ADD),
		}, program.WithGlobals(types.TypeI32, types.TypeI32))
		i := New(prog, WithTick(1)) // exact: disables fusion, forcing the generic path
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(8), v)
	})

	t.Run("a BR landing on the byte offset a fused GLOBAL_GET pair consumed still executes the standalone opcodes correctly", func(t *testing.T) {
		// Mirrors the LOCAL_GET+CONST fuseLocalConst branch-target test: jumps
		// directly into the middle of the GLOBAL_GET;GLOBAL_GET;binop window,
		// landing on the second GLOBAL_GET's own byte offset that the fused
		// closure at the first GLOBAL_GET's start would otherwise have skipped.
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.GLOBAL_SET, 1),
			instr.New(instr.I32_CONST, i32operand(10)), // manual lhs for the branched-in, unfused I32_ADD
			instr.New(instr.BR, 3),                     // jumps to the GLOBAL_GET 1 below, skipping the fused GLOBAL_GET 0 window
			instr.New(instr.GLOBAL_GET, 0),             // fused window start: never reached when BR is taken
			instr.New(instr.GLOBAL_GET, 1),             // BR's target: the offset the fused GLOBAL_GET 0 closure would have skipped
			instr.New(instr.I32_ADD),
		}, program.WithGlobals(types.TypeI32, types.TypeI32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(13), v) // 10 (manual lhs) + 3 (global 1), bypassing the fused global0+global1 path
	})

	t.Run("I32_EQ; BR_IF fuses without materializing a boxed bool (taken)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.NOP), // keeps fuseLocalLocal from absorbing I32_EQ, isolating the bare cmp+BR_IF fusion under test
			instr.New(instr.I32_EQ),
			instr.New(instr.BR_IF, 8),
			instr.New(instr.I32_CONST, i32operand(100)), instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, i32operand(200)),
		}, program.WithLocals(types.TypeI32, types.TypeI32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(200), v) // 3 == 3: branch taken
	})

	t.Run("I32_EQ; BR_IF fuses without materializing a boxed bool (not taken)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.I32_CONST, i32operand(4)), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.NOP),
			instr.New(instr.I32_EQ),
			instr.New(instr.BR_IF, 8),
			instr.New(instr.I32_CONST, i32operand(100)), instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, i32operand(200)),
		}, program.WithLocals(types.TypeI32, types.TypeI32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(100), v) // 3 != 4: branch not taken
	})

	t.Run("I64_EQ; BR_IF fuses without materializing a boxed bool (taken)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(3)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.I64_CONST, i64operand(3)), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.NOP), // keeps fuseLocalLocal from absorbing I64_EQ, isolating the bare cmp+BR_IF fusion under test
			instr.New(instr.I64_EQ),
			instr.New(instr.BR_IF, 8),
			instr.New(instr.I32_CONST, i32operand(100)), instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, i32operand(200)),
		}, program.WithLocals(types.TypeI64, types.TypeI64))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(200), v) // 3 == 3: branch taken
	})

	t.Run("I64_EQ; BR_IF fuses without materializing a boxed bool (not taken)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(3)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.I64_CONST, i64operand(4)), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.NOP),
			instr.New(instr.I64_EQ),
			instr.New(instr.BR_IF, 8),
			instr.New(instr.I32_CONST, i32operand(100)), instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, i32operand(200)),
		}, program.WithLocals(types.TypeI64, types.TypeI64))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(100), v) // 3 != 4: branch not taken
	})

	t.Run("I32_EQZ; BR_IF fuses without materializing a boxed bool (taken)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(0)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.NOP), // keeps any leftward fusion from absorbing I32_EQZ, isolating the bare cmp+BR_IF fusion under test
			instr.New(instr.I32_EQZ),
			instr.New(instr.BR_IF, 8),
			instr.New(instr.I32_CONST, i32operand(100)), instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, i32operand(200)),
		}, program.WithLocals(types.TypeI32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(200), v) // 0 == 0: branch taken
	})

	t.Run("I32_EQZ; BR_IF fuses without materializing a boxed bool (not taken)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.NOP),
			instr.New(instr.I32_EQZ),
			instr.New(instr.BR_IF, 8),
			instr.New(instr.I32_CONST, i32operand(100)), instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, i32operand(200)),
		}, program.WithLocals(types.TypeI32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(100), v) // 5 != 0: branch not taken
	})

	t.Run("I32_CONST; BR_IF fuses a compile-time-known branch condition (taken)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(1)),
			instr.New(instr.BR_IF, 8),
			instr.New(instr.I32_CONST, i32operand(100)), instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, i32operand(200)),
		})
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(200), v)
	})

	t.Run("I32_CONST; BR_IF fuses a compile-time-known branch condition (not taken)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(0)),
			instr.New(instr.BR_IF, 8),
			instr.New(instr.I32_CONST, i32operand(100)), instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, i32operand(200)),
		})
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(100), v)
	})

	t.Run("LOCAL_GET+CONST+cmp+BR_IF collapses into one fused dispatch (taken)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(2)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.I32_LT_S),
			instr.New(instr.BR_IF, 8),
			instr.New(instr.I32_CONST, i32operand(100)), instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, i32operand(200)),
		}, program.WithLocals(types.TypeI32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(200), v) // 2 < 5: branch taken
	})

	t.Run("LOCAL_GET+CONST+cmp+BR_IF collapses into one fused dispatch (not taken)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(10)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.I32_LT_S),
			instr.New(instr.BR_IF, 8),
			instr.New(instr.I32_CONST, i32operand(100)), instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, i32operand(200)),
		}, program.WithLocals(types.TypeI32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(100), v) // 10 < 5 is false: branch not taken
	})

	t.Run("a BR landing on the byte offset fuseLocalConst's CONST consumed still executes the standalone CONST+binop correctly", func(t *testing.T) {
		// Mirrors "fused LOCAL_GET+CONST binop computes correctly for i32" (the
		// fused fast-path case, (a)) but jumps directly into the middle of the
		// LOCAL_GET;CONST;binop window, landing exactly on the CONST's own byte
		// offset that the fused closure at LOCAL_GET's start would otherwise have
		// consumed at runtime -- proving (b): the compile loop still emitted a
		// correct, independent standalone closure there.
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(10)), // manual lhs for the branched-in, unfused I32_ADD
			instr.New(instr.BR, 2),                     // jumps past LOCAL_GET straight to the CONST below
			instr.New(instr.LOCAL_GET, 0),              // never executed at runtime; still compiled standalone
			instr.New(instr.I32_CONST, i32operand(3)),  // BR's target: the offset fuseLocalConst would have skipped
			instr.New(instr.I32_ADD),
		}, program.WithLocals(types.TypeI32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(13), v)
	})

	t.Run("a BR landing on the comparison opcode inside a LOCAL_GET+CONST+cmp+BR_IF window still executes correctly", func(t *testing.T) {
		// Jumps directly onto I32_LT_S's own byte offset, which the 4-instruction
		// LOCAL_GET+CONST+cmp+BR_IF composition (installed at LOCAL_GET's start)
		// would otherwise have consumed at runtime. Proves the standalone closure
		// the compile loop independently installs at that offset -- itself the new
		// bare cmp+BR_IF fusion, since BR_IF genuinely follows in the bytecode --
		// is correct even when reached without going through LOCAL_GET at all.
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(2)),  // manual lhs
			instr.New(instr.I32_CONST, i32operand(10)), // manual rhs
			instr.New(instr.BR, 7),                     // jumps past LOCAL_GET+CONST straight to I32_LT_S below
			instr.New(instr.LOCAL_GET, 0),              // never executed at runtime; still compiled standalone
			instr.New(instr.I32_CONST, i32operand(5)),  // never executed at runtime; still compiled standalone
			instr.New(instr.I32_LT_S),                  // BR's target
			instr.New(instr.BR_IF, 8),
			instr.New(instr.I32_CONST, i32operand(100)), instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, i32operand(200)),
		}, program.WithLocals(types.TypeI32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(200), v) // 2 < 10: branch taken
	})
}

func TestInterpreter_Marshal(t *testing.T) {
	// Marshal forwards to the installed codec, so the conversion contract is
	// owned by TestRegistry_Marshal and only the delegation is checked here.
	i := New(program.New(nil), WithCodec(upperCodec(0)))
	defer i.Close()

	got, err := i.Marshal("go")
	require.NoError(t, err)
	require.Equal(t, types.String("GO"), got)
}

func TestInterpreter_Unmarshal(t *testing.T) {
	i := New(program.New(nil), WithCodec(upperCodec(0)))
	defer i.Close()

	var dst string
	require.NoError(t, i.Unmarshal(types.String("GO"), &dst))
	require.Equal(t, "go", dst)
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
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 4), instr.New(instr.GLOBAL_SET, 0)}, program.WithGlobals(types.TypeI32))
	i := New(prog)
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	v, err := i.Global(0)
	require.NoError(t, err)
	require.Equal(t, types.BoxI32(4), v)
}

func TestInterpreter_SetGlobal(t *testing.T) {
	t.Run("sets scalar", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.GLOBAL_SET, 0)}, program.WithGlobals(types.TypeI32))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.NoError(t, i.SetGlobal(0, types.BoxI32(8)))
		v, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(8), v)
	})

	t.Run("rejects incompatible type", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeI32))
		i := New(prog)
		defer i.Close()

		require.ErrorIs(t, i.SetGlobal(0, types.BoxF32(1)), ErrTypeMismatch)
	})

	t.Run("accepts dynamic ref value", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeAny))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.SetGlobal(0, types.BoxI32(8)))
	})

	t.Run("accepts heap backed i64", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeI64))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Push(types.I64(1<<60)))
		val, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.KindRef, val.Kind())
		require.NoError(t, i.SetGlobal(0, val))
	})

	t.Run("rejects incompatible concrete ref type", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.NewArrayType(types.TypeI32)))
		i := New(prog)
		defer i.Close()

		matching, err := i.Alloc(types.TypedArray[int32]{1})
		require.NoError(t, err)
		require.NoError(t, i.SetGlobal(0, types.BoxRef(matching)))

		mismatching, err := i.Alloc(types.TypedArray[float32]{1})
		require.NoError(t, err)
		require.ErrorIs(t, i.SetGlobal(0, types.BoxRef(mismatching)), ErrTypeMismatch)
		require.NoError(t, i.Release(mismatching))
	})

	t.Run("preserves same reference", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeAny))
		i := New(prog)
		defer i.Close()

		addr, err := i.Alloc(types.String("value"))
		require.NoError(t, err)
		require.NoError(t, i.SetGlobal(0, types.BoxRef(addr)))
		require.NoError(t, i.SetGlobal(0, types.BoxRef(addr)))
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.String("value"), v)
	})

	t.Run("rejects invalid reference", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeAny))
		i := New(prog)
		defer i.Close()

		before, err := i.Global(0)
		require.NoError(t, err)
		require.ErrorIs(t, i.SetGlobal(0, types.BoxRef(9999)), ErrSegmentationFault)
		after, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, before, after)
	})
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
	t.Run("sets scalar", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.YIELD)}, program.WithLocals(types.TypeI32))
		i := New(prog)
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrYield)
		require.NoError(t, i.SetLocal(0, types.BoxI32(3)))
		v, err := i.Local(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(3), v)
	})

	t.Run("preserves same reference", func(t *testing.T) {
		prog := program.New(nil, program.WithLocals(types.TypeAny))
		i := New(prog)
		defer i.Close()

		addr, err := i.Alloc(types.String("value"))
		require.NoError(t, err)
		require.NoError(t, i.SetLocal(0, types.BoxRef(addr)))
		require.NoError(t, i.SetLocal(0, types.BoxRef(addr)))
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.String("value"), v)
	})

	t.Run("rejects invalid reference", func(t *testing.T) {
		prog := program.New(nil, program.WithLocals(types.TypeAny))
		i := New(prog)
		defer i.Close()

		before, err := i.Local(0)
		require.NoError(t, err)
		require.ErrorIs(t, i.SetLocal(0, types.BoxRef(9999)), ErrSegmentationFault)
		after, err := i.Local(0)
		require.NoError(t, err)
		require.Equal(t, before, after)
	})
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
	t.Run("replaces scalar", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)
		require.NoError(t, i.Store(addr, types.BoxI32(9)))
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.I32(9), v)
	})

	t.Run("finalizes replaced value", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		old := &trackedValue{}
		addr, err := i.Alloc(old)
		require.NoError(t, err)
		require.NoError(t, i.Store(addr, types.I32(9)))
		require.Equal(t, 1, old.closed)
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.I32(9), v)
	})

	t.Run("releases replaced child", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		child, err := i.Alloc(types.String("child"))
		require.NoError(t, err)
		_, err = i.Retain(child)
		require.NoError(t, err)
		parent := &trackedValue{refs: []types.Ref{types.Ref(child)}}
		addr, err := i.Alloc(parent)
		require.NoError(t, err)
		require.NoError(t, i.Release(child))
		require.NoError(t, i.Store(addr, types.I32(9)))
		_, err = i.Load(child)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})

	t.Run("ignores same-address reference", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		require.NoError(t, i.Store(addr, types.BoxRef(addr)))
		require.Equal(t, 0, value.closed)
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Same(t, value, v)
	})

	t.Run("ignores identical value", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		loaded, err := i.Load(addr)
		require.NoError(t, err)
		require.NoError(t, i.Store(addr, loaded))
		require.Equal(t, 0, value.closed)
	})

	t.Run("rejects different-address reference", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		source := &trackedValue{}
		sourceAddr, err := i.Alloc(source)
		require.NoError(t, err)
		targetAddr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)

		require.ErrorIs(t, i.Store(targetAddr, types.BoxRef(sourceAddr)), ErrTypeMismatch)
		require.Equal(t, 0, source.closed)
		v, err := i.Load(targetAddr)
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})

	t.Run("rejects owned pointer", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		source := &trackedValue{}
		_, err := i.Alloc(source)
		require.NoError(t, err)
		targetAddr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)

		require.ErrorIs(t, i.Store(targetAddr, source), ErrTypeMismatch)
		require.Equal(t, 0, source.closed)
		v, err := i.Load(targetAddr)
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})

	t.Run("ignores same-address ref", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		require.NoError(t, i.Store(addr, types.Ref(addr)))
		require.Equal(t, 0, value.closed)
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Same(t, value, v)
	})

	t.Run("rejects different-address ref", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		sourceAddr, err := i.Alloc(types.I32(7))
		require.NoError(t, err)
		targetAddr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)

		require.ErrorIs(t, i.Store(targetAddr, types.Ref(sourceAddr)), ErrTypeMismatch)
		v, err := i.Load(targetAddr)
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})

	t.Run("rejects invalid ref", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)
		require.ErrorIs(t, i.Store(addr, types.Ref(9999)), ErrSegmentationFault)
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})

	t.Run("rejects invalid boxed ref", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)
		require.ErrorIs(t, i.Store(addr, types.BoxRef(9999)), ErrSegmentationFault)
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})
}

func TestInterpreter_Alloc(t *testing.T) {
	t.Run("allocates value", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.String("hi"), v)
	})

	t.Run("copies boxed reference ownership", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		copyAddr, err := i.Alloc(types.BoxRef(addr))
		require.NoError(t, err)
		require.Equal(t, addr, copyAddr)
		require.NoError(t, i.Release(addr))
		v, err := i.Load(copyAddr)
		require.NoError(t, err)
		require.Equal(t, types.String("hi"), v)
		require.NoError(t, i.Release(copyAddr))
	})

	t.Run("copies reference ownership", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		copyAddr, err := i.Alloc(types.Ref(addr))
		require.NoError(t, err)
		require.Equal(t, addr, copyAddr)
		require.NoError(t, i.Release(addr))
		v, err := i.Load(copyAddr)
		require.NoError(t, err)
		require.Equal(t, types.String("hi"), v)
		require.NoError(t, i.Release(copyAddr))
	})

	t.Run("rejects owned pointer", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		_, err = i.Alloc(value)
		require.ErrorIs(t, err, ErrTypeMismatch)
		loaded, err := i.Load(addr)
		require.NoError(t, err)
		require.Same(t, value, loaded)
		require.Equal(t, 0, value.closed)
	})

	t.Run("rejects pointer read back out of the heap", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(&trackedValue{})
		require.NoError(t, err)
		for range 4 * heapRunway {
			_, err := i.Alloc(&trackedValue{})
			require.NoError(t, err)
		}

		loaded, err := i.Load(addr)
		require.NoError(t, err)
		_, err = i.Alloc(loaded)
		require.ErrorIs(t, err, ErrTypeMismatch)
	})

	t.Run("accepts pointer whose slot was released", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))

		reused, err := i.Alloc(value)
		require.NoError(t, err)
		require.NotEqual(t, 0, reused)
	})
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

func TestInterpreter_RefCount(t *testing.T) {
	t.Run("counts a fresh allocation", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		count, err := i.RefCount(addr)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})

	t.Run("tracks retain and release", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		_, err = i.Retain(addr)
		require.NoError(t, err)
		count, err := i.RefCount(addr)
		require.NoError(t, err)
		require.Equal(t, 2, count)

		require.NoError(t, i.Release(addr))
		count, err = i.RefCount(addr)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})

	t.Run("rejects a dead address", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))

		_, err = i.RefCount(addr)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})
}

func TestInterpreter_HeapCap(t *testing.T) {
	t.Run("grows to cover a new allocation", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		before := i.HeapCap()
		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		require.GreaterOrEqual(t, i.HeapCap(), before)
		require.Less(t, addr, i.HeapCap())
	})

	t.Run("bounds a scan over live addresses", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		first, err := i.Alloc(types.String("one"))
		require.NoError(t, err)
		second, err := i.Alloc(types.String("two"))
		require.NoError(t, err)

		live := map[int]int{}
		for addr := 1; addr < i.HeapCap(); addr++ {
			count, rcErr := i.RefCount(addr)
			if rcErr != nil {
				continue
			}
			live[addr] = count
		}
		require.Equal(t, map[int]int{first: 1, second: 1}, live)
	})

	t.Run("keeps a released slot in range until it is reused", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))

		require.Less(t, addr, i.HeapCap())
		_, err = i.RefCount(addr)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})
}

func TestInterpreter_Push(t *testing.T) {
	t.Run("pushes scalar", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(4)))
		require.Equal(t, 1, i.Len())
	})

	t.Run("rejects owned pointer", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		value := &trackedValue{}
		_, err := i.Alloc(value)
		require.NoError(t, err)
		require.ErrorIs(t, i.Push(value), ErrTypeMismatch)
		require.Equal(t, 0, i.Len())
		require.Equal(t, 0, value.closed)
	})
}

func TestInterpreter_Pop(t *testing.T) {
	t.Run("scalar", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(4)))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(4), value)
	})

	t.Run("reference value releases its heap ownership", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.String("value")))
		boxed, err := i.Peek(0)
		require.NoError(t, err)
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.String("value"), value)
		_, err = i.Load(boxed.Ref())
		require.ErrorIs(t, err, ErrSegmentationFault)
	})

	t.Run("stack underflow", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := i.Pop()
		require.ErrorIs(t, err, ErrStackUnderflow)
	})
}

func TestInterpreter_PopBoxed(t *testing.T) {
	t.Run("scalar f64 returns raw box without allocation", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.F64(3.5)))
		v, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.KindF64, v.Kind())
		require.Equal(t, 3.5, v.F64())
		require.Equal(t, 0, i.Len())
	})

	t.Run("ref kind transfers the reference to the caller", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.String("hello")))
		v, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.KindRef, v.Kind())
		require.Equal(t, 0, i.Len())

		val, err := i.Load(v.Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("hello"), val)
		require.NoError(t, i.Release(v.Ref()))
	})

	t.Run("stack underflow", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := i.PopBoxed()
		require.ErrorIs(t, err, ErrStackUnderflow)
	})
}

func TestInterpreter_Peek(t *testing.T) {
	t.Run("leaves value on stack", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(4)))
		value, err := i.Peek(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(4), value)
		require.Equal(t, 1, i.Len())
	})

	t.Run("keeps reference owned by stack", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.String("value")))
		value, err := i.Peek(0)
		require.NoError(t, err)
		loaded, err := i.Load(value.Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("value"), loaded)
		require.Equal(t, 1, i.Len())
	})

	t.Run("invalid depth", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := i.Peek(0)
		require.ErrorIs(t, err, ErrStackUnderflow)
	})
}

func TestInterpreter_Len(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()

	require.Equal(t, 0, i.Len())
	require.NoError(t, i.Push(types.I32(1)))
	require.Equal(t, 1, i.Len())
}

func TestInterpreter_Flush(t *testing.T) {
	t.Run("publishes samples without closing", func(t *testing.T) {
		p := prof.New()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD),
		})
		i := New(prog, WithProfiler(p), WithTick(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		i.Flush()

		total, ok := p.Metric("vm_samples_total")
		require.True(t, ok)
		require.Equal(t, float64(3), total)

		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(3), v)
	})

	t.Run("is a no-op without an attached profiler", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NotPanics(t, func() { i.Flush() })
	})
}

func TestInterpreter_Close(t *testing.T) {
	i := New(program.New(nil))
	value := &trackedValue{}
	_, err := i.Alloc(value)
	require.NoError(t, err)

	require.NoError(t, i.Close())
	require.Equal(t, 1, value.closed)
	require.NoError(t, i.Close())
	require.Equal(t, 1, value.closed)
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
			require.Equal(t, 1, i.FP())
			fn, ip, bp, err := i.Frame(0)
			require.NoError(t, err)
			require.Equal(t, 0, fn)
			require.Equal(t, 0, ip)
			require.Equal(t, 0, bp)
		}
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), v)
	})

	t.Run("restores declared-kind zero globals", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeI32, types.TypeAny))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.SetGlobal(0, types.BoxI32(9)))
		i.Reset()

		g, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(0), g)
		g, err = i.Global(1)
		require.NoError(t, err)
		require.Equal(t, types.BoxedNull, g)
	})

	t.Run("restores heap baseline after reset", func(t *testing.T) {
		prog := program.New(nil, program.WithConstants(types.Ref(42)))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Push(types.String("temporary")))
		boxed, err := i.Peek(0)
		require.NoError(t, err)
		addr := boxed.Ref()

		i.Reset()
		require.Equal(t, 0, i.Len())

		// A slot the heap reused after Reset proves the heap actually
		// returned to its baseline rather than merely growing further.
		reused, err := i.Alloc(types.String("temporary"))
		require.NoError(t, err)
		require.Equal(t, addr, reused)
	})

	t.Run("finalizes and clears dynamic values", func(t *testing.T) {
		i := New(program.New(nil), WithHeap(4))

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		require.GreaterOrEqual(t, addr, i.base)

		i.Reset()
		require.Equal(t, 1, value.closed)
		full := i.heap[:cap(i.heap)]
		for _, slot := range full[i.base:] {
			require.Nil(t, slot)
		}
		require.NoError(t, i.Close())
		require.Equal(t, 1, value.closed)
	})

	t.Run("preserves arrays detached by pop", func(t *testing.T) {
		typ := types.NewArrayType(types.TypeAny)
		prog := program.New(
			[]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0)},
			program.WithTypes(typ),
		)
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		value, err := i.Pop()
		require.NoError(t, err)
		first := value.(*types.Array)
		i.Reset()

		require.NoError(t, i.Run(context.Background()))
		value, err = i.Pop()
		require.NoError(t, err)
		second := value.(*types.Array)
		require.NotSame(t, first, second)
		require.Same(t, typ, first.Typ)
		require.Equal(t, []types.Boxed{types.BoxedNull}, first.Elems)
	})

	t.Run("preserves arrays reclaimed before reset", func(t *testing.T) {
		typ := types.NewArrayType(types.TypeAny)
		prog := program.New(
			[]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0)},
			program.WithTypes(typ),
		)
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		ref, err := i.PopBoxed()
		require.NoError(t, err)
		value, err := i.Load(ref.Ref())
		require.NoError(t, err)
		first := value.(*types.Array)
		require.NoError(t, i.Release(ref.Ref()))
		i.Reset()

		require.NoError(t, i.Run(context.Background()))
		value, err = i.Pop()
		require.NoError(t, err)
		second := value.(*types.Array)
		require.NotSame(t, first, second)
		require.Same(t, typ, first.Typ)
		require.Equal(t, []types.Boxed{types.BoxedNull}, first.Elems)
	})

}

func TestNew(t *testing.T) {
	t.Run("runs a program", func(t *testing.T) {
		i := New(program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5)}))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})

	t.Run("interns duplicate string constants", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_EQ),
		}, program.WithConstants(types.String("same"), types.String("same")))
		i := New(prog)
		defer i.Close()

		c0, err := i.Const(0)
		require.NoError(t, err)
		c1, err := i.Const(1)
		require.NoError(t, err)
		require.Equal(t, types.KindRef, c0.Kind())
		require.Equal(t, types.KindRef, c1.Kind())
		require.Equal(t, c0.Ref(), c1.Ref())
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I1(true), got)
	})
}

func TestWithHook(t *testing.T) {
	tests := []struct {
		name string
		prog *program.Program
		want types.Value
		ops  int
	}{
		{
			name: "arithmetic",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD),
			}),
			want: types.I32(3),
			ops:  3,
		},
		{
			name: "local arithmetic store",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 5), instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.LOCAL_GET, 0),
			}, program.WithLocals(types.TypeI32)),
			want: types.I32(8),
			ops:  7,
		},
		{
			name: "typed array load",
			prog: program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_GET),
			}, program.WithConstants(types.TypedArray[int32]{3, 5})),
			want: types.I32(5),
			ops:  3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			i := New(tt.prog, WithTick(1), WithThreshold(-1), WithHook(func(*Interpreter) error {
				calls++
				return nil
			}))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, tt.want, value)
			require.Equal(t, tt.ops, calls)
		})
	}
}

func TestWithCodec(t *testing.T) {
	i := New(program.New(nil), WithCodec(upperCodec(0)))
	defer i.Close()

	v, err := i.Marshal("go")
	require.NoError(t, err)
	require.Equal(t, types.String("GO"), v)

	var dst string
	require.NoError(t, i.Unmarshal(v, &dst))
	require.Equal(t, "go", dst)
}

func TestWithProfiler(t *testing.T) {
	t.Run("nil disables profiling", func(t *testing.T) {
		i := New(program.New(nil), WithProfiler(nil))
		defer i.Close()

		require.Nil(t, i.profiler)
	})

	t.Run("samples execution", func(t *testing.T) {
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
	})

	t.Run("records compilation and native entry", func(t *testing.T) {
		p := prof.New()
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		i := New(prog, WithProfiler(p), WithTick(1), WithThreshold(0))
		require.NoError(t, i.Run(context.Background()))
		if runtime.GOARCH == "arm64" {
			i.Reset()
			require.NoError(t, i.Run(context.Background()))
		}
		require.NoError(t, i.Close())

		if runtime.GOARCH == "arm64" {
			value, ok := p.Metric("vm_jit_compiles_total",
				prof.Label{Key: "func", Value: "0"}, prof.Label{Key: "ip", Value: "0"},
				prof.Label{Key: "trigger", Value: "hot"}, prof.Label{Key: "frontend", Value: "static"},
				prof.Label{Key: "outcome", Value: "emitted"}, prof.Label{Key: "reason", Value: "none"})
			require.True(t, ok)
			require.Equal(t, float64(1), value)
			value, ok = p.Metric("vm_jit_native_entries_total",
				prof.Label{Key: "func", Value: "0"}, prof.Label{Key: "ip", Value: "0"},
				prof.Label{Key: "kind", Value: "start"}, prof.Label{Key: "frontend", Value: "static"})
			require.True(t, ok)
			require.Equal(t, float64(2), value)
		} else {
			value, ok := p.Metric("vm_jit_compiles_total",
				prof.Label{Key: "func", Value: "0"}, prof.Label{Key: "ip", Value: "0"},
				prof.Label{Key: "trigger", Value: "hot"}, prof.Label{Key: "frontend", Value: "none"},
				prof.Label{Key: "outcome", Value: "rejected"}, prof.Label{Key: "reason", Value: "backend-unavailable"})
			require.True(t, ok)
			require.Equal(t, float64(1), value)
		}
	})

	t.Run("records a partial trace cut", func(t *testing.T) {
		code := make([]instr.Instruction, opLimit+1)
		for index := range code {
			code[index] = instr.New(instr.NOP)
		}
		p := prof.New()
		i := New(program.New(code), WithProfiler(p), WithTick(1), WithThreshold(0))
		require.NoError(t, i.Run(context.Background()))
		require.NoError(t, i.Close())

		value, ok := p.Metric("vm_jit_trace_captures_total",
			prof.Label{Key: "func", Value: "0"}, prof.Label{Key: "ip", Value: "0"},
			prof.Label{Key: "outcome", Value: "partial"}, prof.Label{Key: "reason", Value: "op-limit"})
		require.True(t, ok)
		require.Equal(t, float64(1), value)
	})

	t.Run("records a nested terminal rejection", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{}).Emit(
			instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_SET), instr.New(instr.RETURN),
		).MustBuild()
		p := prof.New()
		prog := program.New([]instr.Instruction{instr.New(instr.CONST_GET, 1), instr.New(instr.CALL)},
			program.WithConstants(types.TypedArray[int32]{0}, fn))
		i := New(prog, WithProfiler(p), WithTick(1), WithThreshold(0))
		require.NoError(t, i.Run(context.Background()))
		require.NoError(t, i.Close())

		value, ok := p.Metric("vm_jit_trace_captures_total",
			prof.Label{Key: "func", Value: "0"}, prof.Label{Key: "ip", Value: "0"},
			prof.Label{Key: "outcome", Value: "rejected"}, prof.Label{Key: "reason", Value: "nested-terminal"})
		require.True(t, ok)
		require.Equal(t, float64(1), value)
	})

	t.Run("records terminal native fallback", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		p := prof.New()
		prog := program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(5.5)),
			instr.New(instr.F64_CONST, math.Float64bits(2)),
			instr.New(instr.F64_REM),
		})
		i := New(prog, WithProfiler(p), WithTick(1), WithThreshold(0))
		require.NoError(t, i.Run(context.Background()))
		require.NoError(t, i.Close())

		value, ok := p.Metric("vm_jit_native_exits_total",
			prof.Label{Key: "func", Value: "0"}, prof.Label{Key: "ip", Value: "0"},
			prof.Label{Key: "kind", Value: "start"}, prof.Label{Key: "frontend", Value: "static"},
			prof.Label{Key: "reason", Value: "terminal-op"}, prof.Label{Key: "opcode", Value: "f64.rem"})
		require.True(t, ok)
		require.Equal(t, float64(1), value)
	})

	t.Run("stops repeating rejected trace captures once a function gives up", func(t *testing.T) {
		// A function neither frontend can compile must not keep paying for a
		// fresh capture attempt on every observation forever: cool should
		// make the capture-attempt count plateau instead of growing once per
		// run. Closure creation over a dynamically loaded function reference
		// is such a shape: the static planner only resolves CLOSURE_NEW's
		// capture count from a directly known constant function (see
		// applyStep in interp/jit_plan.go), and the tracer cannot record
		// CLOSURE_NEW at all (see tracer.reason), so
		// routing the reference through a local defeats both frontends.
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.DROP),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Captures(types.TypeI32).Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.RETURN)).MustBuild()),
			program.WithLocals(types.TypeAny))
		require.NoError(t, program.Verify(prog))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()

		captures := func() float64 {
			i.Flush()
			var total float64
			for _, metric := range p.Metrics() {
				if metric.Name == "vm_jit_trace_captures_total" {
					total += metric.Value
				}
			}
			return total
		}

		for range 4 {
			require.NoError(t, i.Run(context.Background()))
			i.Reset()
		}
		early := captures()
		require.Greater(t, early, float64(0))
		require.True(t, i.isCold(0), "the function should have given up after repeated unproductive observations")

		for range 16 {
			require.NoError(t, i.Run(context.Background()))
			i.Reset()
		}
		require.Equal(t, early, captures(), "capture attempts must not grow once the function is cold")
	})

	t.Run("stays correct through a genuine native trace-cut", func(t *testing.T) {
		// A module-level entry (addr 0) that contains any CALL never gets a
		// static plan at all (see staticPlan's declared/callFree gate in
		// interp/jit_plan.go), so once this driving loop goes hot it can only
		// ever compile through the trace frontend. Padding the loop body past
		// opLimit guarantees the recorded trace runs out of budget before a
		// single iteration completes, so the compiled entry hits a real,
		// unforced prof.ExitTraceCut - the tracer's own op-limit cutoff (see
		// tracer.capture) rather than a healthy loop-exit edge - on every
		// entry, mirroring the RecursiveFib/35 regression's dominant exit
		// reason without hand-writing any journal cell. Verify what the
		// public profiler surface can see: the exit reason really is
		// trace-cut, and results stay correct across many warm iterations
		// through the restored threaded handler.
		const iterations = int32(2000)
		const pad = opLimit + 6

		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Params(types.TypeI32).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN)).
			MustBuild()

		b := program.NewBuilder()
		idx := b.Const(fn)
		loop := b.Label()
		done := b.Label()
		b.Locals(types.TypeI32, types.TypeI32)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(iterations))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, uint64(idx)).Emit(instr.CALL).Emit(instr.LOCAL_SET, 1)
		for range pad {
			b.Emit(instr.NOP)
		}
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, program.Verify(prog))

		p := prof.New()
		i := New(prog, WithProfiler(p), WithTick(1), WithThreshold(0))
		defer i.Close()

		for range 4 {
			require.NoError(t, i.Run(context.Background()))
			got, err := i.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, types.BoxI32(iterations), got, "result must stay correct across a native trace-cut fallback")
			i.Reset()
		}

		i.Flush()
		cuts, ok := p.Metric("vm_jit_native_exits_total",
			prof.Label{Key: "func", Value: "0"}, prof.Label{Key: "ip", Value: "0"},
			prof.Label{Key: "kind", Value: "start"}, prof.Label{Key: "frontend", Value: "trace"},
			prof.Label{Key: "reason", Value: "trace-cut"}, prof.Label{Key: "opcode", Value: "none"})
		require.True(t, ok, "an op-limit-bound module entry must exit through a genuine trace-cut")
		require.Greater(t, cuts, float64(0))
	})
}

func TestWithFrame(t *testing.T) {
	t.Run("function call overflows once frames are exhausted", func(t *testing.T) {
		selfFn := types.NewFunctionBuilder(&types.FunctionType{}).Emit(
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(selfFn))
		i := New(prog, WithFrame(3))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrFrameOverflow)
	})

	t.Run("host call succeeds once frames are exhausted", func(t *testing.T) {
		hostFn := NewHostFunction(&types.FunctionType{Returns: []types.Type{types.TypeI32}},
			func(_ *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(1)}, nil
			})
		fillFn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.CONST_GET, 1), instr.New(instr.CALL), instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(fillFn, hostFn))
		i := New(prog, WithFrame(2))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1), v)
	})

	t.Run("native recursion respects reserved frame limit", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}

		b := types.NewFunctionBuilder(nil).Params(types.TypeI32).Returns(types.TypeI32)
		base := b.Label()
		b.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_EQZ)).
			BrIf(base).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 1)).
			Emit(instr.New(instr.I32_SUB)).
			Emit(instr.New(instr.CONST_GET, 0)).
			Emit(instr.New(instr.CALL)).
			Emit(instr.New(instr.I32_CONST, 1)).
			Emit(instr.New(instr.I32_ADD)).
			Emit(instr.New(instr.RETURN)).
			Bind(base).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.RETURN))
		recurse, err := b.Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, nativeFrameLimit),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(recurse))
		firstMetrics := prof.New()
		i := New(prog, WithFrame(nativeFrameLimit+2), WithTick(1), WithThreshold(0), WithProfiler(firstMetrics))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(nativeFrameLimit), v)
		i.Flush()
		firstEmits, ok := firstMetrics.Metric("vm_jit_emits_total")
		require.True(t, ok)
		require.GreaterOrEqual(t, firstEmits, float64(1))

		prog = program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, nativeFrameLimit+1),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(recurse))
		metrics := prof.New()
		i = New(prog, WithFrame(nativeFrameLimit+2), WithTick(1), WithThreshold(0), WithProfiler(metrics))

		require.ErrorIs(t, i.Run(context.Background()), ErrFrameOverflow)
		require.NoError(t, i.Close())
		emits, ok := metrics.Metric("vm_jit_emits_total")
		require.True(t, ok)
		require.GreaterOrEqual(t, emits, float64(1))
		hasEntry := false
		for _, metric := range metrics.Metrics() {
			switch metric.Name {
			case "vm_jit_native_entries_total":
				hasEntry = true
			case "vm_jit_native_exits_total", "vm_jit_native_yields_total":
				require.Failf(t, "unexpected native overflow metric", "metric=%s", metric.Name)
			}
		}
		require.True(t, hasEntry)
	})
}

func TestWithStack(t *testing.T) {
	t.Run("reports overflow", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 3),
		})
		i := New(prog, WithStack(2))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrStackOverflow)
	})

	t.Run("zero normalizes to one slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
		})
		i := New(prog, WithStack(0))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1), v)
	})
}

func TestWithHeap(t *testing.T) {
	t.Run("initial capacity grows", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW),
			instr.New(instr.I32_CONST, 2), instr.New(instr.REF_NEW),
			instr.New(instr.I32_CONST, 3), instr.New(instr.REF_NEW),
		})
		i := New(prog, WithHeap(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 3, i.Len())
	})

	t.Run("collects cycle at backing capacity", func(t *testing.T) {
		i := New(program.New(nil), WithHeap(2))
		defer i.Close()

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		value.refs = []types.Ref{types.Ref(addr)}
		_, err = i.Retain(addr)
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))

		reused, err := i.Alloc(types.I32(1))
		require.NoError(t, err)
		require.Equal(t, addr, reused)
		require.Equal(t, 1, value.closed)
	})

	t.Run("negative capacity normalizes", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW),
		})
		i := New(prog, WithHeap(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 1, i.Len())
	})

	t.Run("collects cycles at adaptive goal", func(t *testing.T) {
		const capacity = 2 * heapRunway

		i := New(program.New(nil), WithHeap(capacity), WithHeapLimit(capacity))
		defer i.Close()

		_, err := i.Alloc(types.I32(1))
		require.NoError(t, err)
		for range capacity - 2 {
			value := &trackedValue{}
			addr, err := i.Alloc(value)
			require.NoError(t, err)
			value.refs = []types.Ref{types.Ref(addr)}
			_, err = i.Retain(addr)
			require.NoError(t, err)
			require.NoError(t, i.Release(addr))
		}

		_, err = i.Alloc(types.I32(2))
		require.NoError(t, err)

		cycle := &trackedValue{}
		addr, err := i.Alloc(cycle)
		require.NoError(t, err)
		cycle.refs = []types.Ref{types.Ref(addr)}
		_, err = i.Retain(addr)
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))

		// The first collection leaves two live slots, so pace sets goal to
		// 2+heapRunway. Reuse and the new cycle occupy two of that runway.
		for n := range heapRunway - 2 {
			_, err = i.Alloc(types.I32(n + 3))
			require.NoError(t, err)
		}
		require.Equal(t, 0, cycle.closed)

		_, err = i.Alloc(types.I32(heapRunway + 1))
		require.NoError(t, err)
		require.Equal(t, 1, cycle.closed)
	})

	t.Run("paces from live set", func(t *testing.T) {
		const capacity = 3 * heapRunway

		i := New(program.New(nil), WithHeap(capacity), WithHeapLimit(capacity))
		defer i.Close()

		for n := range heapRunway + 1 {
			_, err := i.Alloc(types.I32(n))
			require.NoError(t, err)
		}
		for range capacity - heapRunway - 2 {
			value := &trackedValue{}
			addr, err := i.Alloc(value)
			require.NoError(t, err)
			value.refs = []types.Ref{types.Ref(addr)}
			_, err = i.Retain(addr)
			require.NoError(t, err)
			require.NoError(t, i.Release(addr))
		}

		_, err := i.Alloc(types.I32(heapRunway + 1))
		require.NoError(t, err)

		cycle := &trackedValue{}
		addr, err := i.Alloc(cycle)
		require.NoError(t, err)
		cycle.refs = []types.Ref{types.Ref(addr)}
		_, err = i.Retain(addr)
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))

		// After the first collection, heapRunway+2 slots survive and the
		// dynamic live set adds heapRunway+1 slots of runway.
		for n := range heapRunway - 2 {
			_, err = i.Alloc(types.I32(n + heapRunway + 2))
			require.NoError(t, err)
		}
		require.Equal(t, 0, cycle.closed)

		_, err = i.Alloc(types.I32(2 * heapRunway))
		require.NoError(t, err)
		require.Equal(t, 0, cycle.closed)

		_, err = i.Alloc(types.I32(2*heapRunway + 1))
		require.NoError(t, err)
		require.Equal(t, 1, cycle.closed)
	})

	t.Run("resets adaptive goal", func(t *testing.T) {
		const capacity = 3 * heapRunway

		i := New(program.New(nil), WithHeap(capacity), WithHeapLimit(4*heapRunway))
		defer i.Close()

		for n := range capacity {
			_, err := i.Alloc(types.I32(n))
			require.NoError(t, err)
		}
		i.Reset()

		cycle := &trackedValue{}
		addr, err := i.Alloc(cycle)
		require.NoError(t, err)
		cycle.refs = []types.Ref{types.Ref(addr)}
		_, err = i.Retain(addr)
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))

		// Reset leaves only null, so the next goal is 1+heapRunway. The
		// cycle consumes the first dynamic slot.
		for n := range heapRunway - 1 {
			_, err = i.Alloc(types.I32(n))
			require.NoError(t, err)
		}
		require.Equal(t, 0, cycle.closed)

		_, err = i.Alloc(types.I32(heapRunway - 1))
		require.NoError(t, err)
		require.Equal(t, 1, cycle.closed)
	})
}

func TestWithHeapLimit(t *testing.T) {
	t.Run("rejects live heap at limit", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW)})
		i := New(prog, WithHeapLimit(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrHeapExhausted)
	})

	t.Run("preserves host-owned reference", func(t *testing.T) {
		i := New(program.New(nil), WithHeap(2), WithHeapLimit(2))
		defer i.Close()

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		_, err = i.Alloc(types.String("blocked"))
		require.ErrorIs(t, err, ErrHeapExhausted)
		got, err := i.Load(addr)
		require.NoError(t, err)
		require.Same(t, value, got)
		require.Equal(t, 0, value.closed)
		require.NoError(t, i.Release(addr))
		require.Equal(t, 1, value.closed)
	})

	t.Run("preserves duplicate nested constant edges", func(t *testing.T) {
		const leafAddr = 1
		const midAddr = 2

		leaf := &trackedValue{}
		mid := types.NewArray(types.NewArrayType(types.TypeAny), types.BoxRef(leafAddr))
		root := types.NewArray(types.NewArrayType(types.TypeAny), types.BoxRef(midAddr), types.BoxRef(midAddr))
		prog := program.New(nil, program.WithConstants(leaf, mid, root))
		i := New(prog, WithHeap(4), WithHeapLimit(4))
		defer i.Close()

		_, err := i.Alloc(types.String("blocked"))
		require.ErrorIs(t, err, ErrHeapExhausted)
		got, err := i.Load(leafAddr)
		require.NoError(t, err)
		require.Same(t, leaf, got)
		require.Equal(t, 0, leaf.closed)

		i.Reset()
		_, err = i.Alloc(types.String("blocked again"))
		require.ErrorIs(t, err, ErrHeapExhausted)
		got, err = i.Load(leafAddr)
		require.NoError(t, err)
		require.Same(t, leaf, got)
		require.Equal(t, 0, leaf.closed)
	})

	t.Run("collects unreachable cycle", func(t *testing.T) {
		i := New(program.New(nil), WithHeap(3), WithHeapLimit(3))
		defer i.Close()

		left := &trackedValue{}
		leftAddr, err := i.Alloc(left)
		require.NoError(t, err)
		right := &trackedValue{}
		rightAddr, err := i.Alloc(right)
		require.NoError(t, err)
		left.refs = []types.Ref{types.Ref(rightAddr)}
		right.refs = []types.Ref{types.Ref(leftAddr)}
		_, err = i.Retain(rightAddr)
		require.NoError(t, err)
		_, err = i.Retain(leftAddr)
		require.NoError(t, err)
		require.NoError(t, i.Release(leftAddr))
		require.NoError(t, i.Release(rightAddr))

		addr, err := i.Alloc(types.String("reused"))
		require.NoError(t, err)
		require.Equal(t, 1, left.closed)
		require.Equal(t, 1, right.closed)
		require.NoError(t, i.Release(addr))
	})

	t.Run("preserves host-rooted cycle", func(t *testing.T) {
		i := New(program.New(nil), WithHeap(3), WithHeapLimit(3))
		defer i.Close()

		left := &trackedValue{}
		leftAddr, err := i.Alloc(left)
		require.NoError(t, err)
		right := &trackedValue{}
		rightAddr, err := i.Alloc(right)
		require.NoError(t, err)
		left.refs = []types.Ref{types.Ref(rightAddr)}
		right.refs = []types.Ref{types.Ref(leftAddr)}
		_, err = i.Retain(rightAddr)
		require.NoError(t, err)
		_, err = i.Retain(leftAddr)
		require.NoError(t, err)
		require.NoError(t, i.Release(rightAddr))

		_, err = i.Alloc(types.String("blocked"))
		require.ErrorIs(t, err, ErrHeapExhausted)
		got, err := i.Load(leftAddr)
		require.NoError(t, err)
		require.Same(t, left, got)
		got, err = i.Load(rightAddr)
		require.NoError(t, err)
		require.Same(t, right, got)
		require.Equal(t, 0, left.closed)
		require.Equal(t, 0, right.closed)

		require.NoError(t, i.Release(leftAddr))
		addr, err := i.Alloc(types.String("reused"))
		require.NoError(t, err)
		require.Equal(t, 1, left.closed)
		require.Equal(t, 1, right.closed)
		require.NoError(t, i.Release(addr))
	})

	t.Run("collects self cycle", func(t *testing.T) {
		i := New(program.New(nil), WithHeap(2), WithHeapLimit(2))
		defer i.Close()

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		value.refs = []types.Ref{types.Ref(addr)}
		_, err = i.Retain(addr)
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))

		reused, err := i.Alloc(types.String("reused"))
		require.NoError(t, err)
		require.Equal(t, 1, value.closed)
		require.NoError(t, i.Release(reused))
	})

	t.Run("collects duplicate cycle edges", func(t *testing.T) {
		i := New(program.New(nil), WithHeap(3), WithHeapLimit(3))
		defer i.Close()

		left := &trackedValue{}
		leftAddr, err := i.Alloc(left)
		require.NoError(t, err)
		right := &trackedValue{}
		rightAddr, err := i.Alloc(right)
		require.NoError(t, err)
		left.refs = []types.Ref{types.Ref(rightAddr), types.Ref(rightAddr)}
		right.refs = []types.Ref{types.Ref(leftAddr)}
		_, err = i.Retain(rightAddr)
		require.NoError(t, err)
		_, err = i.Retain(rightAddr)
		require.NoError(t, err)
		_, err = i.Retain(leftAddr)
		require.NoError(t, err)
		require.NoError(t, i.Release(leftAddr))
		require.NoError(t, i.Release(rightAddr))

		reused, err := i.Alloc(types.String("reused"))
		require.NoError(t, err)
		require.Equal(t, 1, left.closed)
		require.Equal(t, 1, right.closed)
		require.NoError(t, i.Release(reused))
	})

	t.Run("settles dead edges to live object", func(t *testing.T) {
		i := New(program.New(nil), WithHeap(4), WithHeapLimit(4))
		defer i.Close()

		left := &trackedValue{}
		leftAddr, err := i.Alloc(left)
		require.NoError(t, err)
		right := &trackedValue{}
		rightAddr, err := i.Alloc(right)
		require.NoError(t, err)
		live := &trackedValue{}
		liveAddr, err := i.Alloc(live)
		require.NoError(t, err)
		left.refs = []types.Ref{types.Ref(rightAddr), types.Ref(liveAddr)}
		right.refs = []types.Ref{types.Ref(leftAddr)}
		_, err = i.Retain(rightAddr)
		require.NoError(t, err)
		_, err = i.Retain(liveAddr)
		require.NoError(t, err)
		_, err = i.Retain(leftAddr)
		require.NoError(t, err)
		require.NoError(t, i.Release(leftAddr))
		require.NoError(t, i.Release(rightAddr))

		reused, err := i.Alloc(types.String("reused"))
		require.NoError(t, err)
		require.Equal(t, 1, left.closed)
		require.Equal(t, 1, right.closed)
		got, err := i.Load(liveAddr)
		require.NoError(t, err)
		require.IsType(t, live, got)
		require.Same(t, live, got)
		require.Equal(t, 0, live.closed)
		require.NoError(t, i.Release(liveAddr))
		require.Equal(t, 1, live.closed)
		require.NoError(t, i.Release(reused))
	})
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

// jitLabel returns labels' value for key, or "" if key is absent.
func jitLabel(labels []prof.Label, key string) string {
	for _, l := range labels {
		if l.Key == key {
			return l.Value
		}
	}
	return ""
}

// jitMetricSum sums every sample of name whose labels satisfy match.
// jitMetricSum flushes i's pending samples into p, then sums every sample of
// name whose labels satisfy match. Flushing here (rather than trusting every
// call site to remember it) is what makes the helpers below safe to call
// right after a Run.
func jitMetricSum(i *Interpreter, p *prof.Profiler, name string, match func(labels []prof.Label) bool) float64 {
	i.Flush()
	var total float64
	for _, m := range p.Metrics() {
		if m.Name == name && match(m.Labels) {
			total += m.Value
		}
	}
	return total
}

// jitCompiledAt reports whether fn compiled and emitted native code at ip (or
// at any ip, when ip is negative). It is the public projection of the
// private i.exits map the tests below used to gate on tiering having reached
// a specific entry.
func jitCompiledAt(i *Interpreter, p *prof.Profiler, fn, ip int) bool {
	want := strconv.Itoa(fn)
	return jitMetricSum(i, p, "vm_jit_compiles_total", func(labels []prof.Label) bool {
		if jitLabel(labels, "func") != want || jitLabel(labels, "outcome") != "emitted" {
			return false
		}
		return ip < 0 || jitLabel(labels, "ip") == strconv.Itoa(ip)
	}) > 0
}

// jitSideExitCompiles sums how many side-exit compiles emitted native code
// for fn (at any ip): the public signal that a learned branch continuation,
// recorded in the private tracer's branch tree, became native.
func jitSideExitCompiles(i *Interpreter, p *prof.Profiler, fn int) float64 {
	want := strconv.Itoa(fn)
	return jitMetricSum(i, p, "vm_jit_compiles_total", func(labels []prof.Label) bool {
		return jitLabel(labels, "func") == want &&
			jitLabel(labels, "trigger") == "side-exit" &&
			jitLabel(labels, "outcome") == "emitted"
	})
}

// jitNativeExits sums how many times fn's native code exited back to the
// interpreter for any reason (at any ip): the public signal behind the
// private tracer tree's per-branch hit counters.
func jitNativeExits(i *Interpreter, p *prof.Profiler, fn int) float64 {
	want := strconv.Itoa(fn)
	return jitMetricSum(i, p, "vm_jit_native_exits_total", func(labels []prof.Label) bool {
		return jitLabel(labels, "func") == want
	})
}

// jitCompileAttempts sums every compile attempt recorded for fn at an ip
// satisfying ipMatch, regardless of outcome, trigger, or reason: the public
// signal behind the private i.tried map used to gate on a specific anchor
// having been offered to the compiler at all.
func jitCompileAttempts(i *Interpreter, p *prof.Profiler, fn int, ipMatch func(ip string) bool) float64 {
	want := strconv.Itoa(fn)
	return jitMetricSum(i, p, "vm_jit_compiles_total", func(labels []prof.Label) bool {
		return jitLabel(labels, "func") == want && ipMatch(jitLabel(labels, "ip"))
	})
}

func TestWithThreshold(t *testing.T) {
	// This used to fabricate an entries[0]/trigger state at math.MaxUint64 and
	// call the private hit() directly to prove its counter increment saturates
	// instead of wrapping. Neither the entry counter nor hit() is reachable
	// through the public API, and no realistic run reaches MaxUint64 entries,
	// so that overflow-safety invariant is no longer expressible here.
	t.Run("entry counter saturates", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		i := New(prog, WithThreshold(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
	})

	t.Run("disabled", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 7)})
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), v)
	})

	// This used to drive compile() directly at a fabricated frame ip and
	// inspect the private tracer's tree map to prove a tree is recorded only
	// from the actual entry ip (0), not from an arbitrary one, then call the
	// private entered() to prove the real entry path does record one. Neither
	// compile()/entered() nor a frame's ip is reachable through the public
	// API, so that invariant is no longer expressible here; only the ordinary
	// entry path's absence of error survives.
	t.Run("records entry only from actual entry state", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.NOP),
			instr.New(instr.NOP),
		})
		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
	})

	t.Run("jits top-level entry", func(t *testing.T) {
		p := prof.New()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		})
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(3), v)
		if runtime.GOARCH != "arm64" {
			return
		}
		require.True(t, jitCompiledAt(i, p, 0, 0))
		i.Flush()
		emits, _ := p.Metric("vm_jit_emits_total")
		require.Equal(t, float64(1), emits)
	})

	t.Run("jits select with comparison condition", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		p := prof.New()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10),
			instr.New(instr.I32_CONST, 20),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_LT_S),
			instr.New(instr.SELECT),
		})
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(10), v)
		i.Flush()
		emits, _ := p.Metric("vm_jit_emits_total")
		require.Equal(t, float64(1), emits)
	})

	t.Run("jits oversized top-level code in bounded segments", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		p := prof.New()
		code := make([]instr.Instruction, 0, opLimit+3)
		for range opLimit/2 + 1 {
			code = append(code, instr.New(instr.I32_CONST, 1), instr.New(instr.DROP))
		}
		code = append(code, instr.New(instr.I32_CONST, 7))
		i := New(program.New(code), WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()

		for range exitThreshold + 3 {
			i.Reset()
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(7), v)
		}
		i.Flush()
		emits, _ := p.Metric("vm_jit_emits_total")
		require.GreaterOrEqual(t, emits, float64(1))
	})

	t.Run("keeps a learned nested loop resumable", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		b := program.NewBuilder()
		loop := b.Label()
		b.Locals(types.TypeI32, types.TypeF64).
			Emit(instr.I32_CONST, 0).
			Emit(instr.LOCAL_SET, 0).
			Emit(instr.F64_CONST, 0).
			Emit(instr.LOCAL_SET, 1).
			Bind(loop).
			Emit(instr.LOCAL_GET, 1).
			Emit(instr.F64_CONST, math.Float64bits(1)).
			Emit(instr.F64_ADD).
			Emit(instr.LOCAL_SET, 1).
			Emit(instr.LOCAL_GET, 0).
			Emit(instr.I32_CONST, 1).
			Emit(instr.I32_ADD).
			Emit(instr.LOCAL_TEE, 0).
			Emit(instr.I32_CONST, 4).
			Emit(instr.I32_LT_S).
			BrIf(loop).
			Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)
		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()

		for round := range exitThreshold + 8 {
			i.Reset()
			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(4), value, "round %d", round)
		}
	})

	t.Run("warm entry skips sampling", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval, err := types.NewFunctionBuilder(nil).Params(types.TypeI32).Returns(types.TypeI32).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 1)).
			Emit(instr.New(instr.I32_ADD)).
			Emit(instr.New(instr.RETURN)).
			Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(eval))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		fn, err := i.Const(0)
		require.NoError(t, err)
		addr := fn.Ref()

		// Warm the callee entry: run until its native entry installs.
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(41)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(42), v)
			if jitCompiledAt(i, p, addr, 0) {
				break
			}
		}
		require.True(t, jitCompiledAt(i, p, addr, 0), "callee entry never warmed")

		// This used to also assert that i.samples.Samples(addr) stopped
		// growing once warm, on the theory that a native entry stops the
		// threaded safepoint from sampling it. That assertion was vacuous as
		// originally written: it never attached a profiler, and sample() is a
		// no-op without one (see the profiler-gated call in tick), so both
		// sides of the comparison were always 0. Attaching the profiler this
		// public rewrite requires activates that sampling for real, and
		// measurement (vm_func_samples_total, vm_jit_native_entries_total)
		// shows the callee keeps recording exactly one safepoint sample per
		// call indefinitely, with no native entry ever registered for it: a
		// CALL from a non-native frame does not link into the callee's
		// compiled code (see the trace-linking limitation noted in
		// interp/jit.go). So "the threaded safepoint no longer samples a warm
		// callee" does not hold for this call shape, and is dropped rather
		// than asserted incorrectly.
		for range 4 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(41)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(42), v)
		}
	})

	t.Run("jits prefix before f64 rem terminal", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		p := prof.New()
		prog := program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(7.5)),
			instr.New(instr.F64_CONST, math.Float64bits(2)),
			instr.New(instr.F64_REM),
		})
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.F64(1.5), got)
		i.Flush()
		emits, _ := p.Metric("vm_jit_emits_total")
		require.GreaterOrEqual(t, emits, float64(1))
	})

	t.Run("jits prefix before string read terminal", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		p := prof.New()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.STRING_LEN),
		}, program.WithConstants(types.String("hello")))
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(5), got)
		i.Flush()
		emits, _ := p.Metric("vm_jit_emits_total")
		require.GreaterOrEqual(t, emits, float64(1))
	})

	t.Run("jits top-level loop", func(t *testing.T) {
		p := prof.New()
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
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1100), v)
		if runtime.GOARCH != "arm64" {
			return
		}
		i.Flush()
		emits, _ := p.Metric("vm_jit_emits_total")
		require.GreaterOrEqual(t, emits, float64(1))
	})

	// A do-while loop closes on a backward BR_IF, and a jump table can close one
	// on a backward case. Neither shape appears in the benchmark suite, where
	// every header is closed by an unconditional BR, so nothing else here would
	// notice if those two handlers stopped reporting their back edges.
	t.Run("tiers up a loop closed by a backward br_if", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		p := prof.New()
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
		i := New(prog, WithThreshold(3), WithProfiler(p))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1100), v)

		i.Flush()
		attempted := jitCompileAttempts(i, p, 0, func(ip string) bool { return ip != "0" })
		require.Greater(t, attempted, float64(0), "the backward br_if never reported its header")
	})

	t.Run("tiers up a loop closed by a backward br_table case", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		p := prof.New()
		b := program.NewBuilder()
		loop := b.Label()
		done := b.Label()
		b.Locals(types.TypeI32).
			Emit(instr.I32_CONST, 0).
			Emit(instr.LOCAL_SET, 0).
			Bind(loop).
			Emit(instr.LOCAL_GET, 0).
			Emit(instr.I32_CONST, 1).
			Emit(instr.I32_ADD).
			Emit(instr.LOCAL_TEE, 0).
			Emit(instr.I32_CONST, 1100).
			Emit(instr.I32_GE_S).
			BrTable(done, loop).
			Bind(done).
			Emit(instr.LOCAL_GET, 0)
		prog, err := b.Build()
		require.NoError(t, err)
		i := New(prog, WithThreshold(3), WithProfiler(p))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1100), v)

		i.Flush()
		attempted := jitCompileAttempts(i, p, 0, func(ip string) bool { return ip != "0" })
		require.Greater(t, attempted, float64(0), "the backward br_table case never reported its header")
	})

	// RESUME builds its frame by hand instead of going through the shared call
	// helper, so a coroutine body is the one entry the generated hook can miss.
	t.Run("counts entries into a resumed coroutine", func(t *testing.T) {
		body := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.RETURN)).
			MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.RESUME),
			instr.New(instr.CORO_VALUE),
		}, program.WithConstants(body))
		i := New(prog, WithThreshold(1<<20))
		defer i.Close()

		for range 4 {
			require.NoError(t, i.Run(context.Background()))
			_, err := i.Pop()
			require.NoError(t, err)
			i.Reset()
		}
		require.Equal(t, uint64(8), i.entries[1], "CALL and RESUME each enter the coroutine once per run")
	})

	// A function only ever reached from a host callback is entered through the
	// one-instruction trampoline invoke installs, not through the module's own
	// table, so it counts only if that trampoline carries the hook too.
	t.Run("counts entries made through a host callback", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32},
		}).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN)).MustBuild()
		prog := program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0)}, program.WithConstants(fn))
		i := New(prog, WithThreshold(1<<20))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		ref, err := i.PopBoxed()
		require.NoError(t, err)

		var add func(int32) (int32, error)
		require.NoError(t, i.Unmarshal(ref, &add))
		for n := range 4 {
			got, err := add(int32(n))
			require.NoError(t, err)
			require.Equal(t, int32(n)+1, got)
		}
		require.Equal(t, uint64(4), i.entries[ref.Ref()], "the invoke trampoline never counted its callee")
	})

	t.Run("jits top-level loop-free branch tree over constant f64 array", func(t *testing.T) {
		p := prof.New()
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
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()

		for range 4 {
			i.Reset()
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.InDelta(t, 0.16, float64(got.(types.F64)), 1e-9)
		}
		if runtime.GOARCH != "arm64" {
			return
		}
		require.True(t, jitCompiledAt(i, p, 0, 0))
		i.Flush()
		attempts, _ := p.Metric("vm_jit_attempts_total")
		require.GreaterOrEqual(t, attempts, float64(1))
		emits, _ := p.Metric("vm_jit_emits_total")
		require.GreaterOrEqual(t, emits, float64(1))
	})

	t.Run("jits called loop-free branch tree over constant f64 array", func(t *testing.T) {
		p := prof.New()
		row := make([]float64, 8)
		b := program.NewBuilder()
		featIdx := b.Const(types.TypedArray[float64](row))
		fb := types.NewFunctionBuilder(nil).Returns(types.TypeF64)
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
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()

		for range 4 {
			i.Reset()
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.InDelta(t, 0.16, float64(got.(types.F64)), 1e-9)
		}
		if runtime.GOARCH != "arm64" {
			return
		}
		require.True(t, jitCompiledAt(i, p, 0, 0))
		i.Flush()
		attempts, _ := p.Metric("vm_jit_attempts_total")
		require.GreaterOrEqual(t, attempts, float64(1))
		emits, _ := p.Metric("vm_jit_emits_total")
		require.GreaterOrEqual(t, emits, float64(1))
	})

	t.Run("jits top-level accumulator over many scalar calls", func(t *testing.T) {
		p := prof.New()
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
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()

		for range 4 {
			i.Reset()
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(want), got)
		}
		if runtime.GOARCH != "arm64" {
			return
		}
		require.True(t, jitCompiledAt(i, p, 0, 0))
		i.Flush()
		attempts, _ := p.Metric("vm_jit_attempts_total")
		require.GreaterOrEqual(t, attempts, float64(1))
		emits, _ := p.Metric("vm_jit_emits_total")
		require.GreaterOrEqual(t, emits, float64(1))
	})

	if runtime.GOARCH == "arm64" {
		rowsI1 := make([]bool, 8)
		rowsI8 := make([]int8, 8)
		rowsI32 := make([]int32, 8)
		rowsI64 := make([]int64, 8)
		rowsF32 := make([]float32, 8)
		rowsF64 := make([]float64, 8)
		arrays := []struct {
			name  string
			typ   types.Type
			ret   types.Type
			zero  instr.Instruction
			add   instr.Opcode
			array types.Value
			fill  func(int) float64
			delta float64
		}{
			{
				name:  "i1 array",
				typ:   types.TypeI1Array,
				ret:   types.TypeI32,
				zero:  instr.New(instr.I32_CONST, 0),
				add:   instr.I32_ADD,
				array: types.TypedArray[bool](rowsI1),
				fill: func(n int) float64 {
					var sum int32
					for idx := range rowsI1 {
						rowsI1[idx] = (n+idx)%3 == 0
						if rowsI1[idx] {
							sum++
						}
					}
					return float64(sum)
				},
			},
			{
				name:  "i8 array",
				typ:   types.TypeI8Array,
				ret:   types.TypeI32,
				zero:  instr.New(instr.I32_CONST, 0),
				add:   instr.I32_ADD,
				array: types.TypedArray[int8](rowsI8),
				fill: func(n int) float64 {
					var sum int32
					for idx := range rowsI8 {
						rowsI8[idx] = int8((n+idx)%9 - 4)
						sum += int32(rowsI8[idx])
					}
					return float64(sum)
				},
			},
			{
				name:  "i32 array",
				typ:   types.TypeI32Array,
				ret:   types.TypeI32,
				zero:  instr.New(instr.I32_CONST, 0),
				add:   instr.I32_ADD,
				array: types.TypedArray[int32](rowsI32),
				fill: func(n int) float64 {
					var sum int32
					for idx := range rowsI32 {
						rowsI32[idx] = int32((n+idx)%17 - 8)
						sum += rowsI32[idx]
					}
					return float64(sum)
				},
			},
			{
				name:  "i64 array",
				typ:   types.TypeI64Array,
				ret:   types.TypeI64,
				zero:  instr.New(instr.I64_CONST, 0),
				add:   instr.I64_ADD,
				array: types.TypedArray[int64](rowsI64),
				fill: func(n int) float64 {
					var sum int64
					for idx := range rowsI64 {
						rowsI64[idx] = int64((n+idx)%17 - 8)
						sum += rowsI64[idx]
					}
					return float64(sum)
				},
			},
			{
				name:  "f32 array",
				typ:   types.TypeF32Array,
				ret:   types.TypeF32,
				zero:  instr.New(instr.F32_CONST, uint64(math.Float32bits(0))),
				add:   instr.F32_ADD,
				array: types.TypedArray[float32](rowsF32),
				fill: func(n int) float64 {
					var sum float64
					for idx := range rowsF32 {
						rowsF32[idx] = float32((n+idx)%10) / 10
						sum += float64(rowsF32[idx])
					}
					return sum
				},
				delta: 1e-5,
			},
			{
				name:  "f64 array",
				typ:   types.TypeF64Array,
				ret:   types.TypeF64,
				zero:  instr.New(instr.F64_CONST, math.Float64bits(0)),
				add:   instr.F64_ADD,
				array: types.TypedArray[float64](rowsF64),
				fill: func(n int) float64 {
					var sum float64
					for idx := range rowsF64 {
						rowsF64[idx] = float64((n+idx)%10) / 10
						sum += rowsF64[idx]
					}
					return sum
				},
				delta: 1e-9,
			},
		}
		for _, tt := range arrays {
			t.Run("jits array get from host-pushed "+tt.name, func(t *testing.T) {
				eval := types.NewFunctionBuilder(nil).Params(tt.typ).Returns(tt.ret)
				eval.Emit(tt.zero)
				for idx := range 64 {
					eval.Emit(instr.New(instr.LOCAL_GET, 0)).
						Emit(instr.New(instr.I32_CONST, uint64(uint32(idx%8)))).
						Emit(instr.New(instr.ARRAY_GET)).
						Emit(instr.New(tt.add))
				}
				fn, err := eval.Emit(instr.New(instr.RETURN)).Build()
				require.NoError(t, err)
				prog := program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				}, program.WithConstants(fn))

				p := prof.New()
				i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
				defer i.Close()
				for n := range 4 {
					i.Reset()
					want := 8 * tt.fill(n)
					require.NoError(t, i.Push(tt.array))
					require.NoError(t, i.Run(context.Background()))
					value, err := i.Pop()
					require.NoError(t, err)
					var got float64
					switch value := value.(type) {
					case types.I32:
						got = float64(value)
					case types.I64:
						got = float64(value)
					case types.F32:
						got = float64(value)
					case types.F64:
						got = float64(value)
					default:
						require.FailNow(t, "unexpected result type", "type %T", value)
					}
					require.InDelta(t, want, got, tt.delta)
				}
				i.Flush()
				emits, _ := p.Metric("vm_jit_emits_total")
				require.GreaterOrEqual(t, emits, float64(1))
			})
		}
	}

	if runtime.GOARCH == "arm64" {
		for _, tt := range []struct {
			typ   types.Type
			value types.Value
			array types.Value
		}{
			{
				typ:   types.TypeI1Array,
				value: types.I1(true),
				array: types.TypedArray[bool](make([]bool, 8)),
			},
			{
				typ:   types.TypeI8Array,
				value: types.I8(-3),
				array: types.TypedArray[int8](make([]int8, 8)),
			},
			{
				typ:   types.TypeI32Array,
				value: types.I32(-33),
				array: types.TypedArray[int32](make([]int32, 8)),
			},
			{
				typ:   types.TypeI64Array,
				value: types.I64(-55),
				array: types.TypedArray[int64](make([]int64, 8)),
			},
			{
				typ:   types.TypeF32Array,
				value: types.F32(1.25),
				array: types.TypedArray[float32](make([]float32, 8)),
			},
			{
				typ:   types.TypeF64Array,
				value: types.F64(2.5),
				array: types.TypedArray[float64](make([]float64, 8)),
			},
		} {
			t.Run("jits array set for host-pushed primitive array arguments "+tt.typ.String(), func(t *testing.T) {
				eval := types.NewFunctionBuilder(nil).
					Params(tt.typ).
					Returns(types.TypeI32)
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

				p := prof.New()
				i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
				defer i.Close()
				for range 4 {
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
				i.Flush()
				emits, _ := p.Metric("vm_jit_emits_total")
				require.GreaterOrEqual(t, emits, float64(1))
			})
		}
	}

	if runtime.GOARCH == "arm64" {
		typ := types.NewStructType(
			types.NewStructField(types.TypeI1),
			types.NewStructField(types.TypeI8),
			types.NewStructField(types.TypeI32),
			types.NewStructField(types.TypeI64),
			types.NewStructField(types.TypeF32),
			types.NewStructField(types.TypeF64),
		)
		for _, tt := range []struct {
			idx   uint32
			typ   types.Type
			value types.Boxed
			want  types.Value
		}{
			{idx: 0, typ: types.TypeI1, value: types.BoxI1(true), want: types.I1(true)},
			{idx: 1, typ: types.TypeI8, value: types.BoxI8(-3), want: types.I8(-3)},
			{idx: 2, typ: types.TypeI32, value: types.BoxI32(-33), want: types.I32(-33)},
			{idx: 3, typ: types.TypeI64, value: types.BoxI64(-55), want: types.I64(-55)},
			{idx: 4, typ: types.TypeF32, value: types.BoxF32(1.25), want: types.F32(1.25)},
			{idx: 5, typ: types.TypeF64, value: types.BoxF64(2.5), want: types.F64(2.5)},
		} {
			t.Run("jits struct get from host-pushed primitive struct argument "+tt.typ.String(), func(t *testing.T) {
				eval := types.NewFunctionBuilder(nil).
					Params(typ).
					Returns(tt.typ)
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

				p := prof.New()
				i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
				defer i.Close()
				s := types.NewStruct(typ)
				for range 4 {
					i.Reset()
					s.SetField(int(tt.idx), tt.value)
					require.NoError(t, i.Push(s))
					require.NoError(t, i.Run(context.Background()))
					got, err := i.Pop()
					require.NoError(t, err)
					require.Equal(t, tt.want, got)
				}
				i.Flush()
				emits, _ := p.Metric("vm_jit_emits_total")
				require.GreaterOrEqual(t, emits, float64(1))
			})
		}
	}

	if runtime.GOARCH == "arm64" {
		typ := types.NewStructType(
			types.NewStructField(types.TypeI1),
			types.NewStructField(types.TypeI8),
			types.NewStructField(types.TypeI32),
			types.NewStructField(types.TypeI64),
			types.NewStructField(types.TypeF32),
			types.NewStructField(types.TypeF64),
		)
		for _, tt := range []struct {
			idx   uint32
			value types.Value
			want  types.Boxed
		}{
			{idx: 0, value: types.I1(true), want: types.BoxI1(true)},
			{idx: 1, value: types.I8(-3), want: types.BoxI8(-3)},
			{idx: 2, value: types.I32(-33), want: types.BoxI32(-33)},
			{idx: 3, value: types.I64(-55), want: types.BoxI64(-55)},
			{idx: 4, value: types.F32(1.25), want: types.BoxF32(1.25)},
			{idx: 5, value: types.F64(2.5), want: types.BoxF64(2.5)},
		} {
			t.Run("jits struct set for host-pushed primitive struct argument "+tt.value.Type().String(), func(t *testing.T) {
				eval := types.NewFunctionBuilder(nil).
					Params(typ).
					Returns(types.TypeI32)
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

				p := prof.New()
				i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
				defer i.Close()
				s := types.NewStruct(typ)
				for range 4 {
					i.Reset()
					require.NoError(t, i.Push(s))
					require.NoError(t, i.Run(context.Background()))
					got, err := i.Pop()
					require.NoError(t, err)
					require.Equal(t, types.I32(7), got)
					require.Equal(t, tt.want, s.Field(int(tt.idx)))
				}
				i.Flush()
				emits, _ := p.Metric("vm_jit_emits_total")
				require.GreaterOrEqual(t, emits, float64(1))
			})
		}
	}

	t.Run("jits learned br_if continuations", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		b := types.NewFunctionBuilder(nil).Params(types.TypeI32).Returns(types.TypeI32)
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

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		fnConst, err := i.Const(0)
		require.NoError(t, err)
		fn := fnConst.Ref()

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

		// Warm the arg=3 side exit until its learned continuation compiles as a
		// native side-exit: the public signal that the branch returning
		// i32.const 0 was learned.
		compiled := false
		for range exitThreshold * 4 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(3)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(0), v)

			i.Flush()
			if jitSideExitCompiles(i, p, fn) > 0 {
				compiled = true
				break
			}
		}
		if !compiled {
			emits, _ := p.Metric("vm_jit_emits_total")
			require.Greater(t, emits, float64(0))
			return
		}

		for range 3 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(3)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(0), v)
		}
	})

	t.Run("jits learned br_if continuations over mutable f64 row", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		row := make([]float64, 2)
		b := types.NewFunctionBuilder(nil).
			Params(types.TypeF64Array).
			Returns(types.TypeF64)
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

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		const fn = 0

		row[0], row[1] = 0.8, 0.8
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.F64(2), v)

		// Warm each side exit until its learned continuation compiles as a
		// native side-exit: the public signal that the branch was learned. The
		// second branch is identified by a further increase in the side-exit
		// compile count, since both share the same root function.
		compiled := false
		for range exitThreshold * 4 {
			i.Reset()
			row[0], row[1] = 0.2, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(1), v)

			i.Flush()
			if jitSideExitCompiles(i, p, fn) > 0 {
				compiled = true
				break
			}
		}
		require.True(t, compiled, "no branch returning f64.const 1 was learned")
		first := jitSideExitCompiles(i, p, fn)

		compiled = false
		for range exitThreshold * 4 {
			i.Reset()
			row[0], row[1] = 0.2, 0.1
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(-3), v)

			i.Flush()
			if jitSideExitCompiles(i, p, fn) > first {
				compiled = true
				break
			}
		}
		require.True(t, compiled, "no branch returning f64.const -3 was learned")

		for range 3 {
			i.Reset()
			row[0], row[1] = 0.2, 0.1
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(-3), v)
		}
	})

	t.Run("jits learned br_if continuation over a live ref value", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		row := []float64{10, 20}
		// The array parameter is declared as a generic ref, not
		// types.TypeF64Array: a declared array type now lets the static
		// planner resolve ARRAY_GET's element kind on its own (see atyp and
		// arrayKind in interp/jit_plan.go), which would compile this whole
		// function statically before the tracer ever learns a branch
		// continuation. ARRAY_GET's own runtime type check is unaffected by
		// the declared parameter type, so this keeps the exact behavior this
		// test exercises: a live ref value carried across BR_IF in a trace
		// the static frontend cannot compile.
		b := types.NewFunctionBuilder(nil).
			Params(types.TypeI32, types.TypeAny).
			Returns(types.TypeF64)
		neg := b.Label()
		b.Emit(instr.New(instr.LOCAL_GET, 1)).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.I32_LT_S)).
			BrIf(neg).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.RETURN)).
			Bind(neg).
			Emit(instr.New(instr.I32_CONST, 1)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.RETURN))
		eval, err := b.Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
		}, program.WithConstants(types.TypedArray[float64](row), eval))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		fnConst, err := i.Const(1)
		require.NoError(t, err)
		fn := fnConst.Ref()

		// Record the root trace through both directions of the BR_IF before
		// warming the diverging (negative-cond) side. In both directions the
		// array ref pushed by LOCAL_GET 1 stays live on the operand stack across
		// the branch, so the diverging side can only become a learned pending
		// continuation if marked() lets an ordinary materialized ref through.
		i.Reset()
		require.NoError(t, i.Push(types.I32(1)))
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.F64(10), v)

		compiled := false
		for range exitThreshold * 4 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(-1)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(20), v)

			i.Flush()
			if jitSideExitCompiles(i, p, fn) > 0 {
				compiled = true
				break
			}
		}
		require.True(t, compiled, "no branch reading array index 1 was learned")

		for range 3 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(-1)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(20), v)
		}
	})

	t.Run("deopts array get on negative index", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		row := []float64{7}
		eval := types.NewFunctionBuilder(nil).
			Params(types.TypeI32, types.TypeF64Array).
			Returns(types.TypeF64)
		eval.Emit(instr.New(instr.LOCAL_GET, 1)).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.RETURN))
		fn, err := eval.Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
		}, program.WithConstants(types.TypedArray[float64](row), fn))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(0)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(7), v)
		}
		i.Flush()
		emits, _ := p.Metric("vm_jit_emits_total")
		require.GreaterOrEqual(t, emits, float64(1))

		i.Reset()
		require.NoError(t, i.Push(types.I32(-1)))
		require.ErrorIs(t, i.Run(context.Background()), ErrIndexOutOfRange)
	})

	if runtime.GOARCH == "arm64" {
		for _, tt := range []struct {
			typ   types.Type
			cnst  instr.Instruction
			div   instr.Opcode
			value types.Value
			want  types.Value
		}{
			{
				typ:   types.TypeI32,
				cnst:  instr.New(instr.I32_CONST, 3),
				div:   instr.I32_DIV_S,
				value: types.I32(90),
				want:  types.I32(30),
			},
			{
				typ:   types.TypeI64,
				cnst:  instr.New(instr.I64_CONST, 3),
				div:   instr.I64_DIV_S,
				value: types.I64(90),
				want:  types.I64(30),
			},
		} {
			t.Run("jits constant nonzero divisors "+tt.typ.String(), func(t *testing.T) {
				eval := types.NewFunctionBuilder(nil).
					Params(tt.typ).
					Returns(tt.typ)
				fn, err := eval.Emit(instr.New(instr.LOCAL_GET, 0)).
					Emit(tt.cnst).
					Emit(instr.New(tt.div)).
					Emit(instr.New(instr.RETURN)).
					Build()
				require.NoError(t, err)
				prog := program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				}, program.WithConstants(fn))

				p := prof.New()
				i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
				defer i.Close()
				for range 8 {
					i.Reset()
					require.NoError(t, i.Push(tt.value))
					require.NoError(t, i.Run(context.Background()))
					got, err := i.Pop()
					require.NoError(t, err)
					require.Equal(t, tt.want, got)
				}
				i.Flush()
				emits, _ := p.Metric("vm_jit_emits_total")
				require.GreaterOrEqual(t, emits, float64(1))
			})
		}
	}

	if runtime.GOARCH == "arm64" {
		for _, tt := range []struct {
			typ   types.Type
			div   instr.Opcode
			left  types.Value
			right types.Value
			want  types.Value
			alt   types.Value
			next  types.Value
			zero  types.Value
		}{
			{
				typ:   types.TypeI32,
				div:   instr.I32_DIV_S,
				left:  types.I32(90),
				right: types.I32(3),
				want:  types.I32(30),
				alt:   types.I32(5),
				next:  types.I32(18),
				zero:  types.I32(0),
			},
			{
				typ:   types.TypeI64,
				div:   instr.I64_DIV_S,
				left:  types.I64(90),
				right: types.I64(3),
				want:  types.I64(30),
				alt:   types.I64(5),
				next:  types.I64(18),
				zero:  types.I64(0),
			},
		} {
			t.Run("deopts variable zero divisors "+tt.typ.String(), func(t *testing.T) {
				eval := types.NewFunctionBuilder(nil).
					Params(tt.typ, tt.typ).
					Returns(tt.typ)
				fn, err := eval.Emit(instr.New(instr.LOCAL_GET, 0)).
					Emit(instr.New(instr.LOCAL_GET, 1)).
					Emit(instr.New(tt.div)).
					Emit(instr.New(instr.RETURN)).
					Build()
				require.NoError(t, err)
				prog := program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				}, program.WithConstants(fn))

				p := prof.New()
				i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
				defer i.Close()
				for range 8 {
					i.Reset()
					require.NoError(t, i.Push(tt.left))
					require.NoError(t, i.Push(tt.right))
					require.NoError(t, i.Run(context.Background()))
					got, err := i.Pop()
					require.NoError(t, err)
					require.Equal(t, tt.want, got)
				}
				i.Flush()
				emits, _ := p.Metric("vm_jit_emits_total")
				require.GreaterOrEqual(t, emits, float64(1))

				i.Reset()
				require.NoError(t, i.Push(tt.left))
				require.NoError(t, i.Push(tt.alt))
				require.NoError(t, i.Run(context.Background()))
				got, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, tt.next, got)

				i.Reset()
				require.NoError(t, i.Push(tt.left))
				require.NoError(t, i.Push(tt.zero))
				require.ErrorIs(t, i.Run(context.Background()), ErrDivideByZero)
			})
		}
	}

	t.Run("deopts array len on shape mismatch", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			Params(types.TypeAny).
			Returns(types.TypeI32)
		fn, err := eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.ARRAY_LEN)).
			Emit(instr.New(instr.RETURN)).
			Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		fnConst, err := i.Const(0)
		require.NoError(t, err)
		addr := fnConst.Ref()
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.TypedArray[int32]{1, 2}))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(2), got)
		}
		require.True(t, jitCompiledAt(i, p, addr, 0))

		i.Reset()
		require.NoError(t, i.Push(types.NewArray(types.NewArrayType(types.TypeI32), types.BoxI32(1), types.BoxI32(2), types.BoxI32(3))))
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(3), got)

		i.Flush()
		require.Greater(t, jitNativeExits(i, p, addr), float64(0))
	})

	t.Run("deopts struct get on type mismatch", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			Params(types.TypeAny).
			Returns(types.TypeI32)
		fn, err := eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.STRUCT_GET)).
			Emit(instr.New(instr.RETURN)).
			Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		fnConst, err := i.Const(0)
		require.NoError(t, err)
		addr := fnConst.Ref()
		first := types.NewStructType(types.NewStructField(types.TypeI32))
		second := types.NewStructType(types.NewStructField(types.TypeI32))
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.NewStruct(first, types.BoxI32(7))))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(7), got)
		}
		require.True(t, jitCompiledAt(i, p, addr, 0))

		i.Reset()
		require.NoError(t, i.Push(types.NewStruct(second, types.BoxI32(9))))
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(9), got)

		i.Flush()
		require.Greater(t, jitNativeExits(i, p, addr), float64(0))
	})

	t.Run("deopts string len on type mismatch", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			Params(types.TypeAny).
			Returns(types.TypeI32)
		fn, err := eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.STRING_LEN)).
			Emit(instr.New(instr.RETURN)).
			Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		fnConst, err := i.Const(0)
		require.NoError(t, err)
		addr := fnConst.Ref()
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.String("hello")))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(5), got)
		}
		require.True(t, jitCompiledAt(i, p, addr, 0))

		i.Reset()
		require.NoError(t, i.Push(types.NewArray(types.NewArrayType(types.TypeI32), types.BoxI32(1))))
		require.ErrorIs(t, i.Run(context.Background()), ErrTypeMismatch)
	})

	t.Run("jits array set for a ref-element array argument", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		arrTyp := types.NewArrayType(types.TypeString)
		eval := types.NewFunctionBuilder(nil).
			Params(arrTyp, types.TypeString).
			Returns(types.TypeI32)
		// Store the same host-pushed local into index 0 twice in a row: the
		// second ARRAY_SET observes old==val (both reads of LOCAL_GET 1 name
		// the same ref), exercising releaseBoxExcept's aliased-store path
		// natively within a single call. Read the slot back through ARRAY_GET
		// and STRING_LEN (rather than inspecting the interpreter's heap table
		// directly) so the check only relies on legitimate VM operations —
		// ARRAY_SET's own frame teardown releases the local params once the
		// call returns, so the ref's continued validity has to be proven
		// in-VM, before that release, not by peeking at heap state after.
		fn, err := eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.LOCAL_GET, 1)).
			Emit(instr.New(instr.ARRAY_SET)).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.LOCAL_GET, 1)).
			Emit(instr.New(instr.ARRAY_SET)).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.STRING_LEN)).
			Emit(instr.New(instr.RETURN)).
			Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		for range 4 {
			i.Reset()
			arr := types.NewArray(arrTyp, types.BoxedNull, types.BoxedNull)
			require.NoError(t, i.Push(arr))
			require.NoError(t, i.Push(types.String("stored")))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(len("stored")), got)
		}
		i.Flush()
		emits, _ := p.Metric("vm_jit_emits_total")
		require.GreaterOrEqual(t, emits, float64(1))
	})

	t.Run("jits struct set for a ref-field struct argument", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		typ := types.NewStructType(types.NewStructField(types.TypeString))
		eval := types.NewFunctionBuilder(nil).
			Params(typ, types.TypeString).
			Returns(types.TypeI32)
		// Same aliased-store exercise as the array-set case, applied to a
		// ref-kind struct field, verified via STRUCT_GET + STRING_LEN.
		fn, err := eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.LOCAL_GET, 1)).
			Emit(instr.New(instr.STRUCT_SET)).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.LOCAL_GET, 1)).
			Emit(instr.New(instr.STRUCT_SET)).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.STRUCT_GET)).
			Emit(instr.New(instr.STRING_LEN)).
			Emit(instr.New(instr.RETURN)).
			Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		for range 4 {
			i.Reset()
			s := types.NewStruct(typ, types.BoxedNull)
			require.NoError(t, i.Push(s))
			require.NoError(t, i.Push(types.String("stored")))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(len("stored")), got)
		}
		i.Flush()
		emits, _ := p.Metric("vm_jit_emits_total")
		require.GreaterOrEqual(t, emits, float64(1))
	})

	t.Run("continues i64 array get through arithmetic", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			Params(types.TypeI64Array).
			Returns(types.TypeI64)
		fn, err := eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.I64_CONST, 1)).
			Emit(instr.New(instr.I64_ADD)).
			Emit(instr.New(instr.RETURN)).
			Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		fnConst, err := i.Const(0)
		require.NoError(t, err)
		addr := fnConst.Ref()
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.TypedArray[int64]{41}))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I64(42), got)
		}
		require.True(t, jitCompiledAt(i, p, addr, 0))

		i.Flush()
		require.Greater(t, jitNativeExits(i, p, addr), float64(0))
	})

	t.Run("deopts after i64 array get with stack shape intact", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			Params(types.TypeI64Array).
			Returns(types.TypeI64)
		fn, err := eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.I64_CONST, 1)).
			Emit(instr.New(instr.I64_ADD)).
			Emit(instr.New(instr.RETURN)).
			Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		fnConst, err := i.Const(0)
		require.NoError(t, err)
		addr := fnConst.Ref()
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.TypedArray[int64]{1<<48 - 1}))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I64(1<<48), got)
		}
		require.True(t, jitCompiledAt(i, p, addr, 0))

		i.Flush()
		require.Greater(t, jitNativeExits(i, p, addr), float64(0))
	})

	t.Run("deopts nonboxable i64 array get", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			Params(types.TypeI64Array).
			Returns(types.TypeI64)
		fn, err := eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.RETURN)).
			Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		fnConst, err := i.Const(0)
		require.NoError(t, err)
		addr := fnConst.Ref()
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.TypedArray[int64]{41}))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I64(41), got)
		}
		require.True(t, jitCompiledAt(i, p, addr, 0))

		i.Reset()
		require.NoError(t, i.Push(types.TypedArray[int64]{1 << 48}))
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I64(1<<48), got)

		i.Flush()
		require.Greater(t, jitNativeExits(i, p, addr), float64(0))
	})

	t.Run("continues i64 struct get through arithmetic", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		typ := types.NewStructType(types.NewStructField(types.TypeI64))
		eval := types.NewFunctionBuilder(nil).
			Params(typ).
			Returns(types.TypeI64)
		fn, err := eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.STRUCT_GET)).
			Emit(instr.New(instr.I64_CONST, 1)).
			Emit(instr.New(instr.I64_ADD)).
			Emit(instr.New(instr.RETURN)).
			Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		fnConst, err := i.Const(0)
		require.NoError(t, err)
		addr := fnConst.Ref()
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.NewStruct(typ, types.BoxI64(41))))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I64(42), got)
		}
		require.True(t, jitCompiledAt(i, p, addr, 0))

		i.Flush()
		require.Greater(t, jitNativeExits(i, p, addr), float64(0))
	})

	t.Run("jits learned callee branch through caller tail", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		row := make([]float64, 2)
		first := types.NewFunctionBuilder(nil).
			Params(types.TypeF64Array).
			Returns(types.TypeF64)
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
			Params(types.TypeF64Array).
			Returns(types.TypeF64)
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
			Params(types.TypeF64Array).
			Returns(types.TypeF64)
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

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(1), WithProfiler(p))
		defer i.Close()
		const fn = 0

		var v types.Value
		for range 4 {
			i.Reset()
			row[0], row[1] = 0.8, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(22), v)
			if jitCompiledAt(i, p, fn, 0) {
				break
			}
		}
		require.True(t, jitCompiledAt(i, p, fn, 0))

		// Warm the inlined callee branch until its learned continuation
		// compiles as a native side-exit: the public signal that the branch
		// returning f64.const 1 was learned.
		compiled := false
		for range exitThreshold * 4 {
			i.Reset()
			row[0], row[1] = 0.2, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(21), v)

			i.Flush()
			if jitSideExitCompiles(i, p, fn) > 0 {
				compiled = true
				break
			}
		}
		require.True(t, compiled, "no first callee branch returning f64.const 1 was learned")

		for range 3 {
			i.Reset()
			row[0], row[1] = 0.2, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(21), v)
		}
	})

	t.Run("keeps inlined callee params across nested learned branch continuations", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		row := make([]float64, 2)
		first := types.NewFunctionBuilder(nil).
			Params(types.TypeF64Array).
			Returns(types.TypeF64)
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
			Params(types.TypeF64Array).
			Returns(types.TypeF64)
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
			Params(types.TypeF64Array).
			Returns(types.TypeF64)
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

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(1), WithProfiler(p))
		defer i.Close()
		const fn = 0

		var v types.Value
		for range 4 {
			i.Reset()
			row[0], row[1] = 0.8, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(22), v)
			if jitCompiledAt(i, p, fn, 0) {
				break
			}
		}
		require.True(t, jitCompiledAt(i, p, fn, 0))

		// Warm the outer callee branch, then the nested one, each identified by
		// a further increase in the side-exit compile count since both share
		// the same root function.
		compiled := false
		for range exitThreshold * 4 {
			i.Reset()
			row[0], row[1] = 0.2, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(31), v)

			i.Flush()
			if jitSideExitCompiles(i, p, fn) > 0 {
				compiled = true
				break
			}
		}
		require.True(t, compiled, "no first callee branch returning f64.const 1 was learned")
		outer := jitSideExitCompiles(i, p, fn)

		for range 3 {
			i.Reset()
			row[0], row[1] = 0.2, 0.8
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(31), v)
		}

		compiled = false
		for range exitThreshold * 4 {
			i.Reset()
			row[0], row[1] = 0.2, 0.1
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(-9), v)

			i.Flush()
			if jitSideExitCompiles(i, p, fn) > outer {
				compiled = true
				break
			}
		}
		require.True(t, compiled, "no nested callee branch returning f64.const -10 was learned")

		for range 3 {
			i.Reset()
			row[0], row[1] = 0.2, 0.1
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(-9), v)
		}
	})

	t.Run("jits learned br_table continuations", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		b := types.NewFunctionBuilder(nil).Params(types.TypeI32).Returns(types.TypeI32)
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

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		fnConst, err := i.Const(0)
		require.NoError(t, err)
		fn := fnConst.Ref()

		// Record the root trace through table index 0 before warming index 1.
		i.Reset()
		require.NoError(t, i.Push(types.I32(0)))
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(10), v)

		// Warm the index=1 side exit until its learned continuation compiles as
		// a native side-exit: the public signal that the branch returning
		// i32.const 11 was learned.
		compiled := false
		for range exitThreshold * 4 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(1)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(11), v)

			i.Flush()
			if jitSideExitCompiles(i, p, fn) > 0 {
				compiled = true
				break
			}
		}
		if !compiled {
			emits, _ := p.Metric("vm_jit_emits_total")
			require.Greater(t, emits, float64(0))
			return
		}

		for range 3 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(1)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(11), v)
		}

		// The default target still deopts correctly after index 1 is learned.
		i.Reset()
		require.NoError(t, i.Push(types.I32(4)))
		require.NoError(t, i.Run(context.Background()))
		v, err = i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(99), v)
	})

	t.Run("jits inlined br_table continuation through caller tail", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		choice := types.NewFunctionBuilder(nil).Params(types.TypeI32).Returns(types.TypeI32)
		zero := choice.Label()
		one := choice.Label()
		two := choice.Label()
		def := choice.Label()
		choice.Emit(instr.New(instr.LOCAL_GET, 0)).
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
		choiceFn, err := choice.Build()
		require.NoError(t, err)

		eval := types.NewFunctionBuilder(nil).Params(types.TypeI32).Returns(types.TypeI32)
		eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.CONST_GET, 0)).
			Emit(instr.New(instr.CALL)).
			Emit(instr.New(instr.I32_CONST, 100)).
			Emit(instr.New(instr.I32_ADD)).
			Emit(instr.New(instr.RETURN))
		evalFn, err := eval.Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
		}, program.WithConstants(choiceFn, evalFn))

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		fnConst, err := i.Const(1)
		require.NoError(t, err)
		fn := fnConst.Ref()

		i.Reset()
		require.NoError(t, i.Push(types.I32(0)))
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(110), v)

		// Warm the index=1 side exit until its learned continuation compiles as
		// a native side-exit: the public signal that the inlined br_table
		// branch returning i32.const 11 was learned.
		compiled := false
		for range exitThreshold * 4 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(1)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(111), v)

			i.Flush()
			if jitSideExitCompiles(i, p, fn) > 0 {
				compiled = true
				break
			}
		}
		if !compiled {
			emits, _ := p.Metric("vm_jit_emits_total")
			require.Greater(t, emits, float64(0))
			return
		}

		for range 3 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(1)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(111), v)
		}
	})

	t.Run("jits top-level typed-array loop as cfg", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}

		b := program.NewBuilder()
		b.Locals(types.TypeI32, types.TypeI32)
		values := b.Const(types.TypedArray[int32]{1, 2, 3, 4})
		loop := b.Label()
		b.Bind(loop)
		b.Emit(instr.CONST_GET, uint64(values))
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.ARRAY_GET)
		b.Emit(instr.LOCAL_GET, 1)
		b.Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.I32_CONST, 1)
		b.Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_TEE, 0)
		b.Emit(instr.I32_CONST, 4)
		b.Emit(instr.I32_LT_S)
		b.BrIf(loop)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		p := prof.New()
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(p))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		got, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, int32(10), got.I32())
		i.Flush()
		emits, _ := p.Metric("vm_jit_emits_total")
		require.Greater(t, emits, float64(0))

		valuesConst, err := i.Const(int(values))
		require.NoError(t, err)
		ref := valuesConst.Ref()
		require.NoError(t, i.Store(ref, types.TypedArray[int32]{10, 20, 30, 40}))
		i.Reset()
		require.NoError(t, i.Run(context.Background()))
		got, err = i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, int32(100), got.I32())
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

func BenchmarkNew(b *testing.B) {
	b.Run("Empty", func(b *testing.B) {
		prog := program.New(nil)
		var vm *Interpreter
		var closeErr error
		var elapsed time.Duration
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			start := time.Now()
			vm = New(prog)
			elapsed += time.Since(start)
			closeErr = vm.Close()
		}
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
		require.NoError(b, closeErr)
	})

	b.Run("Program", func(b *testing.B) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 42)})
		var vm *Interpreter
		var closeErr error
		var elapsed time.Duration
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			start := time.Now()
			vm = New(prog, WithThreshold(-1))
			elapsed += time.Since(start)
			closeErr = vm.Close()
		}
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
		require.NoError(b, closeErr)
	})

	b.Run("JITEnabled", func(b *testing.B) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 42)})
		var vm *Interpreter
		var closeErr error
		var elapsed time.Duration
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			start := time.Now()
			vm = New(prog, WithThreshold(0))
			elapsed += time.Since(start)
			closeErr = vm.Close()
		}
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
		require.NoError(b, closeErr)
	})
}

func BenchmarkInterpreter_Run(b *testing.B) {
	b.Run("ColdBackedge", func(b *testing.B) {
		builder := program.NewBuilder()
		loop := builder.Label()
		done := builder.Label()
		builder.Locals(types.TypeI32)
		builder.Emit(instr.I32_CONST, 0).
			Emit(instr.LOCAL_SET, 0).
			Bind(loop).
			Emit(instr.LOCAL_GET, 0).
			Emit(instr.I32_CONST, 256).
			Emit(instr.I32_GE_S).
			BrIf(done).
			Emit(instr.LOCAL_GET, 0).
			Emit(instr.I32_CONST, 1).
			Emit(instr.I32_ADD).
			Emit(instr.LOCAL_SET, 0).
			Br(loop).
			Bind(done).
			Emit(instr.LOCAL_GET, 0)
		prog, err := builder.Build()
		require.NoError(b, err)
		vm := New(prog, WithTick(1<<20), WithThreshold(1<<30))
		b.Cleanup(func() { require.NoError(b, vm.Close()) })
		ctx := context.Background()
		require.NoError(b, vm.Run(ctx))
		value, err := vm.PopBoxed()
		require.NoError(b, err)
		require.Equal(b, types.BoxI32(256), value)
		vm.Reset()

		var runErr error
		var elapsed time.Duration
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			start := time.Now()
			runErr = vm.Run(ctx)
			elapsed += time.Since(start)
			value, err = vm.PopBoxed()
			vm.Reset()
		}
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
		require.NoError(b, runErr)
		require.NoError(b, err)
		require.Equal(b, types.BoxI32(256), value)
	})

	for _, tt := range runTests {
		b.Run(runTestName(tt.program), func(b *testing.B) {
			modes := []struct {
				name string
				opts []Option
			}{
				{name: "Threaded", opts: []Option{WithTick(1), WithThreshold(-1)}},
				{name: "Fused", opts: []Option{WithThreshold(-1)}},
			}
			jit := runtime.GOARCH == "arm64"
			codes := [][]byte{tt.program.Code}
			for _, constant := range tt.program.Constants {
				if fn, ok := constant.(*types.Function); ok {
					codes = append(codes, fn.Code)
				}
			}
			for _, code := range codes {
				for ip := 0; jit && ip < len(code); {
					inst := instr.Instruction(code[ip:])
					if inst.Opcode() == instr.RETURN_CALL {
						jit = false
						break
					}
					ip += inst.Width()
				}
			}
			if jit {
				modes = append(modes, struct {
					name string
					opts []Option
				}{name: "JITWarm", opts: []Option{WithTick(1), WithThreshold(0)}})
			}

			for _, mode := range modes {
				b.Run(mode.name, func(b *testing.B) {
					ctx := context.Background()
					vm := New(tt.program, mode.opts...)
					b.Cleanup(func() { require.NoError(b, vm.Close()) })

					warmups := 1
					if mode.name == "JITWarm" {
						warmups = 16
					}
					for range warmups {
						err := vm.Run(ctx)
						if tt.err != nil {
							require.ErrorIs(b, err, tt.err)
						} else {
							require.NoError(b, err)
							for _, want := range tt.values {
								got, err := vm.Pop()
								require.NoError(b, err)
								require.Equal(b, want, got)
							}
						}
						vm.Reset()
						if mode.name == "JITWarm" && vm.stub(0) != nil {
							break
						}
					}

					if mode.name == "JITWarm" && vm.stub(0) == nil {
						b.Skip("entry does not compile")
					}

					var runErr error
					var elapsed time.Duration
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						start := time.Now()
						runErr = vm.Run(ctx)
						elapsed += time.Since(start)
						vm.Reset()
					}
					b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
					if tt.err != nil {
						require.ErrorIs(b, runErr, tt.err)
					} else {
						require.NoError(b, runErr)
					}
				})
			}
		})
	}

	// PerOp benchmarks every runTests case under the interpreter's default
	// options, one sub-benchmark per case, keyed by the same derived name the
	// test uses. Trapping cases are skipped: a trap always takes the same
	// short exit path, so timing it case by case is noise.
	b.Run("PerOp", func(b *testing.B) {
		for _, tt := range runTests {
			if tt.err != nil {
				continue
			}
			b.Run(runTestName(tt.program), func(b *testing.B) {
				ctx := context.Background()
				vm := New(tt.program)
				b.Cleanup(func() { require.NoError(b, vm.Close()) })

				require.NoError(b, vm.Run(ctx))
				for _, want := range tt.values {
					got, err := vm.Pop()
					require.NoError(b, err)
					require.Equal(b, want, got)
				}
				vm.Reset()

				var runErr error
				var elapsed time.Duration
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					start := time.Now()
					runErr = vm.Run(ctx)
					elapsed += time.Since(start)
					vm.Reset()
				}
				b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
				require.NoError(b, runErr)
			})
		}
	})
}

func BenchmarkInterpreter_Reset(b *testing.B) {
	var jitCode []instr.Instruction
	for range 64 {
		jitCode = append(jitCode,
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.DROP),
		)
	}
	jitCode = append(jitCode, instr.New(instr.I32_CONST, 42))

	tests := []struct {
		name string
		prog *program.Program
		opts []Option
	}{
		{
			name: "Scalar",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 42), instr.New(instr.GLOBAL_SET, 0),
			}, program.WithGlobals(types.TypeI32)),
			opts: []Option{WithThreshold(-1)},
		},
		{
			name: "Heap",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 8), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			}, program.WithTypes(types.NewArrayType(types.TypeAny))),
			opts: []Option{WithThreshold(-1)},
		},
		{
			name: "JITState",
			prog: program.New(jitCode),
			opts: []Option{WithThreshold(0)},
		},
	}

	for _, tt := range tests {
		if tt.name == "JITState" && runtime.GOARCH != "arm64" {
			continue
		}
		b.Run(tt.name, func(b *testing.B) {
			ctx := context.Background()
			vm := New(tt.prog, tt.opts...)
			defer vm.Close()
			require.NoError(b, vm.Run(ctx))
			if tt.name == "JITState" {
				for range 16 {
					if vm.stub(0) != nil {
						break
					}
					vm.Reset()
					require.NoError(b, vm.Run(ctx))
				}
				require.NotNil(b, vm.stub(0))
			}

			var runErr error
			var elapsed time.Duration
			var bytes, allocs uint64
			var sampled bool
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				var before runtime.MemStats
				if !sampled {
					runtime.ReadMemStats(&before)
				}
				start := time.Now()
				vm.Reset()
				elapsed += time.Since(start)
				if !sampled {
					var after runtime.MemStats
					runtime.ReadMemStats(&after)
					bytes = after.TotalAlloc - before.TotalAlloc
					allocs = after.Mallocs - before.Mallocs
					sampled = true
				}
				runErr = vm.Run(ctx)
			}
			b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
			b.ReportMetric(float64(bytes), "B/op")
			b.ReportMetric(float64(allocs), "allocs/op")
			require.NoError(b, runErr)
		})
	}
}

func BenchmarkInterpreter_Push(b *testing.B) {
	b.Run("Scalar", func(b *testing.B) {
		vm := New(program.New(nil))
		defer vm.Close()
		var pushErr error
		var elapsed time.Duration
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			start := time.Now()
			pushErr = vm.Push(types.I32(42))
			elapsed += time.Since(start)
			vm.Reset()
		}
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
		require.NoError(b, pushErr)
	})

	b.Run("Reference", func(b *testing.B) {
		vm := New(program.New(nil))
		defer vm.Close()
		var pushErr error
		var elapsed time.Duration
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			start := time.Now()
			pushErr = vm.Push(types.String("value"))
			elapsed += time.Since(start)
			vm.Reset()
		}
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
		require.NoError(b, pushErr)
	})
}

func BenchmarkInterpreter_Pop(b *testing.B) {
	vm := New(program.New(nil))
	defer vm.Close()
	var value types.Value
	var pushErr, popErr error
	var elapsed time.Duration
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		pushErr = vm.Push(types.I32(42))
		start := time.Now()
		value, popErr = vm.Pop()
		elapsed += time.Since(start)
	}
	b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
	require.NoError(b, pushErr)
	require.NoError(b, popErr)
	require.Equal(b, types.I32(42), value)
}

func BenchmarkInterpreter_PopBoxed(b *testing.B) {
	vm := New(program.New(nil))
	defer vm.Close()
	var value types.Boxed
	var pushErr, popErr error
	var elapsed time.Duration
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		pushErr = vm.Push(types.I32(42))
		start := time.Now()
		value, popErr = vm.PopBoxed()
		elapsed += time.Since(start)
	}
	b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
	require.NoError(b, pushErr)
	require.NoError(b, popErr)
	require.Equal(b, types.BoxI32(42), value)
}

func BenchmarkInterpreter_Peek(b *testing.B) {
	vm := New(program.New(nil))
	defer vm.Close()
	require.NoError(b, vm.Push(types.I32(42)))
	var value types.Boxed
	var err error
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		value, err = vm.Peek(0)
	}
	require.NoError(b, err)
	require.Equal(b, types.BoxI32(42), value)
}

func BenchmarkInterpreter_Alloc(b *testing.B) {
	vm := New(program.New(nil))
	defer vm.Close()
	var addr int
	var err error
	var elapsed time.Duration
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		start := time.Now()
		addr, err = vm.Alloc(types.String("value"))
		elapsed += time.Since(start)
		require.NoError(b, err)
		require.NoError(b, vm.Release(addr))
	}
	b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
}

func BenchmarkInterpreter_Retain(b *testing.B) {
	vm := New(program.New(nil))
	defer vm.Close()
	addr, err := vm.Alloc(types.String("value"))
	require.NoError(b, err)
	var elapsed time.Duration
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		start := time.Now()
		_, err = vm.Retain(addr)
		elapsed += time.Since(start)
		require.NoError(b, err)
		require.NoError(b, vm.Release(addr))
	}
	b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
	require.NoError(b, vm.Release(addr))
}

func BenchmarkInterpreter_Release(b *testing.B) {
	vm := New(program.New(nil))
	defer vm.Close()
	addr, err := vm.Alloc(types.String("value"))
	require.NoError(b, err)
	var releaseErr error
	var elapsed time.Duration
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err = vm.Retain(addr)
		require.NoError(b, err)
		start := time.Now()
		releaseErr = vm.Release(addr)
		elapsed += time.Since(start)
		require.NoError(b, releaseErr)
	}
	b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
	require.NoError(b, vm.Release(addr))
}

func BenchmarkInterpreter_StructGetLocalFusion(b *testing.B) {
	prog := structSumTree(12, 500)
	vm := New(prog, WithThreshold(-1)) // threaded + fused, no JIT
	b.Cleanup(func() { require.NoError(b, vm.Close()) })
	ctx := context.Background()

	require.NoError(b, vm.Run(ctx))
	want, err := vm.PopBoxed()
	require.NoError(b, err)
	vm.Reset()

	b.ReportAllocs()
	for b.Loop() {
		require.NoError(b, vm.Run(ctx))
		got, err := vm.PopBoxed()
		require.NoError(b, err)
		require.Equal(b, want, got)
		vm.Reset()
	}
}

// structSumTree builds a small binary-tree kernel shaped like
// benchmarks/memory_test.go's structTreeWalk, except sumFn's tree parameter
// is declared as the concrete node struct type instead of types.TypeAny, so
// every struct.get on it is a LOCAL_GET whose declared type is a concrete
// *types.StructType -- the shape interp/threaded.go's generated STRUCT_GET
// local-container fusion specializes.
func structSumTree(depth, repeats int32) *program.Program {
	nodeType := types.NewStructType(
		types.NewStructField(types.TypeI32, types.FieldWithName("value")),
		types.NewStructField(types.TypeAny, types.FieldWithName("left")),
		types.NewStructField(types.TypeAny, types.FieldWithName("right")),
	)

	// build locals: 0=d (param), 1=n
	buildBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny}}).
		Params(types.TypeI32).
		Locals(types.TypeAny)
	buildDone := buildBuilder.Label()
	buildFn := buildBuilder.
		Emit(
			instr.New(instr.STRUCT_NEW_DEFAULT, 0), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.STRUCT_SET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S),
		).
		BrIf(buildDone).
		Emit(
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.STRUCT_SET),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 2),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.STRUCT_SET),
		).
		Bind(buildDone).
		Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.RETURN)).
		MustBuild()

	// sum params: 0=t, declared as the concrete node struct type.
	sumBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Params(nodeType)
	nullCase := sumBuilder.Label()
	sumFn := sumBuilder.
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_IS_NULL)).
		BrIf(nullCase).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.STRUCT_GET),
			instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.STRUCT_GET),
			instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).
		Bind(nullCase).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
		MustBuild()

	// Top level: build the tree once, then call sum(tree) repeats times in a
	// loop, accumulating a checksum. This isolates sum's struct.get cost
	// from build's allocation cost, which would otherwise dominate a single
	// build+sum call and hide any per-access improvement.
	b := program.NewBuilder()
	buildIdx := b.Const(buildFn)
	sumIdx := b.Const(sumFn)
	b.Type(nodeType)
	b.Locals(types.TypeAny, types.TypeI32, types.TypeI32) // 0=tree, 1=counter, 2=checksum
	loop := b.Label()
	done := b.Label()
	b.Emit(instr.I32_CONST, uint64(uint32(depth))).
		Emit(instr.CONST_GET, uint64(buildIdx)).Emit(instr.CALL).
		Emit(instr.LOCAL_SET, 0).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2).
		Bind(loop).
		Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(repeats))).Emit(instr.I32_GE_S).
		BrIf(done).
		Emit(instr.LOCAL_GET, 2).
		Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, uint64(sumIdx)).Emit(instr.CALL).
		Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2).
		Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).
		Br(loop).
		Bind(done).
		Emit(instr.LOCAL_GET, 2)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

// BenchmarkInterpreter_ArrayGetContainerFusion measures ARRAY_GET fused onto
// a GLOBAL_GET and an UPVAL_GET container -- the two sources this change adds
// to the LOCAL_GET container fusion BenchmarkInterpreter_StructGetLocalFusion
// already covers. No canonical kernel in benchmarks/ holds an array or struct
// in a global or an upvalue (#176), so this is the only coverage of either
// path's runtime win; both sources share one benchmark function, run as
// subtests, because they exercise the identical sum-loop shape and differ
// only in where the container lives.
func BenchmarkInterpreter_ArrayGetContainerFusion(b *testing.B) {
	const size, repeats = 64, 4000
	for _, tt := range []struct {
		name string
		prog *program.Program
	}{
		{name: "global", prog: arraySumGlobal(size, repeats)},
		{name: "upvalue", prog: arraySumUpvalue(size, repeats)},
	} {
		b.Run(tt.name, func(b *testing.B) {
			vm := New(tt.prog, WithThreshold(-1)) // threaded + fused, no JIT
			b.Cleanup(func() { require.NoError(b, vm.Close()) })
			ctx := context.Background()

			require.NoError(b, vm.Run(ctx))
			want, err := vm.PopBoxed()
			require.NoError(b, err)
			vm.Reset()

			b.ReportAllocs()
			for b.Loop() {
				require.NoError(b, vm.Run(ctx))
				got, err := vm.PopBoxed()
				require.NoError(b, err)
				require.Equal(b, want, got)
				vm.Reset()
			}
		})
	}
}

// arraySumGlobal builds a kernel that holds a size-length int32 array in a
// declared GLOBAL_GET slot and sums its elements repeats times in a nested
// loop, so every array.get is a GLOBAL_GET whose declared type is a concrete
// *types.ArrayType -- the shape interp/threaded.go's generated ARRAY_GET
// global-container fusion specializes.
func arraySumGlobal(size, repeats int32) *program.Program {
	elems := make([]int32, size)
	for i := range elems {
		elems[i] = int32(i)
	}

	b := program.NewBuilder()
	b.Globals(types.TypeI32Array)
	b.Locals(types.TypeI32, types.TypeI32, types.TypeI32) // 0=outer, 1=inner, 2=sum
	outerLoop, outerDone := b.Label(), b.Label()
	innerLoop, innerDone := b.Label(), b.Label()
	b.ConstGet(types.TypedArray[int32](elems)).Emit(instr.GLOBAL_SET, 0).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0).
		Bind(outerLoop).
		Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(repeats))).Emit(instr.I32_GE_S).
		BrIf(outerDone).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1).
		Bind(innerLoop).
		Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).
		BrIf(innerDone).
		Emit(instr.LOCAL_GET, 2).
		Emit(instr.GLOBAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET).
		Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2).
		Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).
		Br(innerLoop).
		Bind(innerDone).
		Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).
		Br(outerLoop).
		Bind(outerDone).
		Emit(instr.LOCAL_GET, 2)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

// arraySumUpvalue builds the same kernel as arraySumGlobal, except the array
// is captured as a closure upvalue instead of stored in a global, so every
// array.get is a UPVAL_GET whose declared type is a concrete *types.ArrayType
// -- the shape interp/threaded.go's generated ARRAY_GET upvalue-container
// fusion specializes.
func arraySumUpvalue(size, repeats int32) *program.Program {
	elems := make([]int32, size)
	for i := range elems {
		elems[i] = int32(i)
	}

	sumBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Captures(types.TypeI32Array).
		Locals(types.TypeI32, types.TypeI32, types.TypeI32) // 0=outer, 1=inner, 2=sum
	outerLoop, outerDone := sumBuilder.Label(), sumBuilder.Label()
	innerLoop, innerDone := sumBuilder.Label(), sumBuilder.Label()
	sumFn := sumBuilder.
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 2)).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 0)).
		Bind(outerLoop).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, uint64(uint32(repeats))), instr.New(instr.I32_GE_S)).
		BrIf(outerDone).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 1)).
		Bind(innerLoop).
		Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, uint64(uint32(size))), instr.New(instr.I32_GE_S)).
		BrIf(innerDone).
		Emit(
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.UPVAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.ARRAY_GET),
			instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1),
		).
		Br(innerLoop).
		Bind(innerDone).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 0)).
		Br(outerLoop).
		Bind(outerDone).
		Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN)).
		MustBuild()

	b := program.NewBuilder()
	fnIdx := b.Const(sumFn)
	b.ConstGet(types.TypedArray[int32](elems))
	b.Emit(instr.CONST_GET, uint64(fnIdx)).Emit(instr.CLOSURE_NEW).Emit(instr.CALL)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

type structGetHostFields struct {
	Count  int32
	hidden int32
}

func (h *structGetHostFields) Bump(n int32) int32 {
	h.Count += n
	h.hidden++
	return h.Count
}

// BenchmarkInterpreter_StructGetHost measures STRUCT_GET against the value the
// reflection codec picks for a Go struct that carries a method and an
// unexported field. Field 0 is a plain i32, so the loop isolates per-access
// dispatch cost from any boxing or heap traffic. The marshal and reset work
// outside the inner loop is amortized over repeats field reads per run, the
// same way BenchmarkInterpreter_StructGetLocalFusion amortizes its tree build.
// The two rows separate the threaded read from the lowered one, which is the
// pair a change to hostGet has to report.
func BenchmarkInterpreter_StructGetHost(b *testing.B) {
	const repeats = 10000

	for _, tt := range []struct {
		name      string
		threshold int
	}{
		{name: "Threaded", threshold: -1},
		{name: "JIT", threshold: 0},
	} {
		b.Run(tt.name, func(b *testing.B) {
			prog := structGetHostLoop(repeats)
			vm := New(prog, WithThreshold(tt.threshold))
			b.Cleanup(func() { require.NoError(b, vm.Close()) })
			ctx := context.Background()

			run := func() types.Boxed {
				host, err := vm.Marshal(&structGetHostFields{Count: 1})
				require.NoError(b, err)
				require.NoError(b, vm.Push(host))
				require.NoError(b, vm.Run(ctx))
				got, err := vm.PopBoxed()
				require.NoError(b, err)
				vm.Reset()
				return got
			}

			want := run()

			b.ReportAllocs()
			for b.Loop() {
				require.Equal(b, want, run())
			}
		})
	}
}

// structGetHostLoop reads field 0 of a host-backed struct repeats times,
// accumulating the reads so nothing is optimized away.
func structGetHostLoop(repeats int32) *program.Program {
	b := program.NewBuilder()
	b.Locals(types.TypeAny, types.TypeI32, types.TypeI32) // 0=host, 1=counter, 2=sum
	loop := b.Label()
	done := b.Label()
	b.Emit(instr.LOCAL_SET, 0).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2).
		Bind(loop).
		Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(repeats))).Emit(instr.I32_GE_S).
		BrIf(done).
		Emit(instr.LOCAL_GET, 2).
		Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).
		Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2).
		Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).
		Br(loop).
		Bind(done).
		Emit(instr.LOCAL_GET, 2)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

type marshalBenchData struct {
	Count int32
	Ratio float64
	Name  string
	Flag  bool
}

type marshalBenchMethods struct {
	Count  int32
	hidden int32
}

func (v *marshalBenchMethods) Bump(n int32) int32 {
	v.Count += n
	v.hidden++
	return v.Count
}

// BenchmarkInterpreter_Marshal records the per-shape cost of the reflection
// codec. Every iteration resets the interpreter so the heap stays at one
// conversion's worth; that reset cost is identical across runs, so it does not
// disturb a before/after comparison.
func BenchmarkInterpreter_Marshal(b *testing.B) {
	elems := make([]int32, 64)
	for idx := range elems {
		elems[idx] = int32(idx)
	}
	entries := make(map[string]int32, 16)
	for idx := range 16 {
		entries[string(rune('a'+idx))] = int32(idx)
	}
	// Keys past the boxed payload, the shape a slot round trip allocates for.
	counters := make(map[int64]int32, 16)
	for idx := range 16 {
		counters[1<<50+int64(idx)] = int32(idx)
	}

	for _, tt := range []struct {
		name  string
		value any
	}{
		{name: "scalar", value: int32(7)},
		{name: "struct", value: marshalBenchData{Count: 7, Ratio: 2.5, Name: "x", Flag: true}},
		{name: "slice", value: elems},
		{name: "map", value: entries},
		{name: "map i64 key", value: counters},
		{name: "methods", value: &marshalBenchMethods{Count: 7}},
	} {
		b.Run(tt.name, func(b *testing.B) {
			i := New(program.New(nil))
			b.Cleanup(func() { require.NoError(b, i.Close()) })

			_, err := i.Marshal(tt.value)
			require.NoError(b, err)
			i.Reset()

			b.ReportAllocs()
			for b.Loop() {
				if _, err := i.Marshal(tt.value); err != nil {
					b.Fatal(err)
				}
				i.Reset()
			}
		})
	}
}

// BenchmarkInterpreter_Unmarshal records the reverse direction over the same
// shapes. The source value is marshaled once outside the loop, so only decode
// cost is measured; the destination is reused because Unmarshal overwrites it.
func BenchmarkInterpreter_Unmarshal(b *testing.B) {
	elems := make([]int32, 64)
	for idx := range elems {
		elems[idx] = int32(idx)
	}
	entries := make(map[string]int32, 16)
	for idx := range 16 {
		entries[string(rune('a'+idx))] = int32(idx)
	}

	for _, tt := range []struct {
		name  string
		value any
		dst   any
	}{
		{name: "scalar", value: int32(7), dst: new(int32)},
		{name: "struct", value: marshalBenchData{Count: 7, Ratio: 2.5, Name: "x", Flag: true}, dst: new(marshalBenchData)},
		{name: "slice", value: elems, dst: new([]int32)},
		{name: "map", value: entries, dst: new(map[string]int32)},
	} {
		b.Run(tt.name, func(b *testing.B) {
			i := New(program.New(nil))
			b.Cleanup(func() { require.NoError(b, i.Close()) })

			value, err := i.Marshal(tt.value)
			require.NoError(b, err)
			require.NoError(b, i.Unmarshal(value, tt.dst))

			b.ReportAllocs()
			for b.Loop() {
				if err := i.Unmarshal(value, tt.dst); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
