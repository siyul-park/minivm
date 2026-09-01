package interp_test

import (
	"context"
	"math"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// heapRunway mirrors the interpreter's unexported heapRunway. Keep in sync.
const heapRunway = 64

// exitThreshold mirrors the tracer's unexported exitThreshold. Keep in sync.
const exitThreshold = 8

// opLimit mirrors the tracer's unexported opLimit. Keep in sync.
const opLimit = 1024

// nativeFrameLimit mirrors the JIT's unexported nativeFrameLimit. Keep in sync.
const nativeFrameLimit = 128

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

func (upperCodec) Marshal(_ *interp.Interpreter, v any) (types.Value, error) {
	s, ok := v.(string)
	if !ok {
		return nil, interp.ErrUnsupportedMarshalType
	}
	return types.String(strings.ToUpper(s)), nil
}

func (upperCodec) Unmarshal(_ *interp.Interpreter, v types.Value, dst any) error {
	s, ok := v.(types.String)
	if !ok {
		return interp.ErrInvalidUnmarshalTarget
	}
	p, ok := dst.(*string)
	if !ok {
		return interp.ErrInvalidUnmarshalTarget
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
		err:     interp.ErrUnreachableExecuted,
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
		err:     interp.ErrYield,
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
		err: interp.ErrTypeMismatch,
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
		err: interp.ErrDivideByZero,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1))), instr.New(instr.F32_CONST, 0), instr.New(instr.F32_MOD),
		}),
		err: interp.ErrDivideByZero,
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
		err: interp.ErrDivideByZero,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(1)), instr.New(instr.F64_CONST, 0), instr.New(instr.F64_MOD),
		}),
		err: interp.ErrDivideByZero,
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
		err: interp.ErrTypeMismatch,
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
			instr.New(instr.STRUCT_NEW_DEFAULT, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
			instr.New(instr.REF_IS_NULL),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeAny)))),
		values: []types.Value{types.I1(true)},
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
		err: interp.ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
		}, program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32)))),
		err: interp.ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.REF_NULL), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
		}, program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32)))),
		err: interp.ErrTypeMismatch,
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
		err: interp.ErrSegmentationFault,
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
		err: interp.ErrSegmentationFault,
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
		err: interp.ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
		}, program.WithGlobals(types.NewStructType(types.NewStructField(types.TypeI32)))),
		err: interp.ErrTypeMismatch,
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
		err: interp.ErrTypeMismatch,
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
		err: interp.ErrTypeMismatch,
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
		}, program.WithConstants(interp.NewHostFunction(
			&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			func(_ *interp.Interpreter, args []types.Boxed) ([]types.Boxed, error) {
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
		err: interp.ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 9), instr.New(instr.ARRAY_SET),
		}, program.WithTypes(types.TypeI32Array)),
		err: interp.ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 7), instr.New(instr.I32_CONST, 5), instr.New(instr.ARRAY_FILL),
		}, program.WithTypes(types.TypeI32Array)),
		err: interp.ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 5), instr.New(instr.ARRAY_DELETE),
		}, program.WithTypes(types.TypeI32Array)),
		err: interp.ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 5),
			instr.New(instr.ARRAY_COPY),
		}, program.WithTypes(types.TypeI32Array)),
		err: interp.ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_CONST, 4), instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 6), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, uint64(^uint32(0))),
			instr.New(instr.ARRAY_COPY),
		}, program.WithTypes(types.TypeI32Array)),
		err: interp.ErrIndexOutOfRange,
	},
	{
		// Regression: array.set fused through a CONST_GET typed-array
		// constant container previously did i.sp -= 3 after the write, but a
		// fused sequence never pushes its container, index, or value onto the
		// operand stack, so its net stack effect must be zero. The stray
		// decrement corrupted the stack pointer and crashed the next stack
		// access (interp.Run panicked "index out of range [-3]").
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 42), instr.New(instr.ARRAY_SET),
			instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_GET),
		}, program.WithConstants(types.TypedArray[int32]{1, 2, 3})),
		values: []types.Value{types.I32(42)},
	},
	{
		// array.set fused onto a LOCAL_GET whose declared slot type is a
		// concrete typed array specializes directly: the runtime value's
		// representation matches the declared kind, so no fallback is
		// needed.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 42), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI32Array),
			program.WithLocals(types.TypeI32Array),
		),
		values: []types.Value{types.I32(42)},
	},
	{
		// Mirrors the LOCAL_GET case above, but the container is a module
		// global instead of a local.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 42), instr.New(instr.ARRAY_SET),
			instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI32Array),
			program.WithGlobals(types.TypeI32Array),
		),
		values: []types.Value{types.I32(42)},
	},
	{
		// Mirrors the LOCAL_GET case above, but the container is a
		// closure's captured upvalue instead of a local.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		},
			program.WithTypes(types.TypeI32Array),
			program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Captures(types.TypeI32Array).
				Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 42), instr.New(instr.ARRAY_SET),
					instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_GET), instr.New(instr.RETURN)).
				MustBuild()),
		),
		values: []types.Value{types.I32(42)},
	},
	{
		// array.set's fused LOCAL_GET path over the i1 (bool) element kind.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI1Array),
			program.WithLocals(types.TypeI1Array),
		),
		values: []types.Value{types.I1(true)},
	},
	{
		// array.set's fused LOCAL_GET path over the i8 element kind.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 7), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI8Array),
			program.WithLocals(types.TypeI8Array),
		),
		values: []types.Value{types.I8(7)},
	},
	{
		// array.set's fused LOCAL_GET path over the i32 element kind.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 42), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI32Array),
			program.WithLocals(types.TypeI32Array),
		),
		values: []types.Value{types.I32(42)},
	},
	{
		// array.set's fused LOCAL_GET path over the i64 element kind.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I64_CONST, i64operand(42)), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI64Array),
			program.WithLocals(types.TypeI64Array),
		),
		values: []types.Value{types.I64(42)},
	},
	{
		// array.set's fused LOCAL_GET path over the f32 element kind.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeF32Array),
			program.WithLocals(types.TypeF32Array),
		),
		values: []types.Value{types.F32(1.5)},
	},
	{
		// array.set's fused LOCAL_GET path over the f64 element kind.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeF64Array),
			program.WithLocals(types.TypeF64Array),
		),
		values: []types.Value{types.F64(2.5)},
	},
	{
		// Mirrors the LOCAL_GET parity case for array.get: array.new_default's
		// type index names a ref-element array type, so it allocates the
		// generic *types.Array boxed-element representation (never
		// TypedArray[int32]), while the local it is stored into is declared
		// types.TypeI32Array. array.set's fused LOCAL_GET path proves only
		// the local's declared element kind at threading time, so a miss on
		// its specialized TypedArray[int32] assertion must fall back to
		// (*Interpreter).arraySet, which stores the boxed value as-is,
		// instead of trapping.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 42), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		},
			program.WithTypes(types.NewArrayType(types.TypeAny)),
			program.WithLocals(types.TypeI32Array),
		),
		values: []types.Value{types.I32(42)},
	},
	{
		// Mirrors the LOCAL_GET parity case for array.get: LOCAL_SET does
		// not recheck the declared type of the slot it writes into, so a
		// local declared as a concrete i32-element array can still hold a
		// different concrete element kind (here f32) at runtime. array.set's
		// fused LOCAL_GET path proves only the local's declared element kind
		// at threading time, so a miss on its specialized TypedArray[int32]
		// assertion must fall back through every other concrete
		// TypedArray[_] representation, not just *types.Array, instead of
		// trapping a case the unfused handler accepts. The fallback stores
		// through val.F32(), which reinterprets the fused I32_CONST
		// payload's raw bits rather than numerically converting it, so
		// I32_CONST 0 lands as float32(0) -- distinct from the constant's
		// original 1.5, proving the write actually happened.
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		},
			program.WithConstants(types.TypedArray[float32]{1.5}),
			program.WithLocals(types.TypeI32Array),
		),
		values: []types.Value{types.F32(0)},
	},
	{
		// array.set's fused LOCAL_GET path still bounds-checks: an
		// out-of-range index traps the same as the unfused handler.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 9), instr.New(instr.ARRAY_SET),
		},
			program.WithTypes(types.TypeI32Array),
			program.WithLocals(types.TypeI32Array),
		),
		err: interp.ErrIndexOutOfRange,
	},
	{
		// A local declared as a typed array can still hold a non-ref value
		// at runtime (LOCAL_SET does not recheck the declared type).
		// array.set's fused LOCAL_GET path traps type mismatch the same as
		// the unfused handler.
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 9), instr.New(instr.ARRAY_SET),
		}, program.WithLocals(types.TypeI32Array)),
		err: interp.ErrTypeMismatch,
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

		i := interp.New(prog, interp.WithHeapLimit(heapRunway))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
	})

	t.Run("string.concat reads the result after releasing both last operand references", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.STRING_CONCAT)})
		i := interp.New(prog, interp.WithThreshold(-1))
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

		i := interp.New(prog, interp.WithHeapLimit(heapRunway))
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
		i := interp.New(prog, interp.WithThreshold(-1))
		defer i.Close()

		err := i.Run(context.Background())
		require.ErrorIs(t, err, interp.ErrTypeMismatch)
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
			i := interp.New(prog, interp.WithThreshold(-1))
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

		p := prof.New()
		i := interp.New(prog, interp.WithTick(1<<20), interp.WithThreshold(1<<30), interp.WithProfiler(p))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		value, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(64), value)

		i.Flush()
		attempts, _ := p.Metric("vm_jit_attempts_total")
		require.Zero(t, attempts, "an unreachable threshold must never trigger a compile attempt")
		// The very first entry always records one incidental, never-completed
		// capture (outcome "partial") regardless of the threshold - that is
		// ordinary entry-warmup instrumentation, not loop discovery. The public
		// projection of i.tracer.loops staying empty is that no capture ever
		// reaches "published": an unreachable threshold gates backedge() from
		// ever calling i.trace, which is the only path that discovers and
		// publishes a loop.
		published := jitMetricSum(i, p, "vm_jit_trace_captures_total", func(labels []prof.Label) bool {
			return jitLabel(labels, "outcome") == "published"
		})
		require.Zero(t, published, "an unreachable threshold must never publish a discovered loop trace")
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

			p := prof.New()
			i := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(p))
			defer i.Close()

			attempts := func() float64 {
				i.Flush()
				v, _ := p.Metric("vm_jit_attempts_total")
				return v
			}
			nativeEntries := func() float64 {
				return jitMetricSum(i, p, "vm_jit_native_entries_total", func([]prof.Label) bool { return true })
			}

			// Drive the loop well past the point every root has been attempted
			// and cooled: docs/profile.md's contract is that cooling removes
			// further hotness instrumentation and capture overhead while
			// leaving any installed native code active.
			for range 8 {
				require.NoError(t, i.Run(context.Background()))
				v, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I32(2), v)
				i.Reset()
			}
			settledAttempts := attempts()
			settledEntries := nativeEntries()
			require.Greater(t, settledEntries, float64(0), "the loop must have installed native code before cooling")

			for range 8 {
				require.NoError(t, i.Run(context.Background()))
				v, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I32(2), v)
				i.Reset()
			}
			require.Equal(t, settledAttempts, attempts(), "cooling must stop further compile attempts")
			require.Greater(t, nativeEntries(), settledEntries, "cooling must not stop the installed native handler from running")
		})
	}

	if runtime.GOARCH == "arm64" {
		t.Run("retires a slower function entry", func(t *testing.T) {
			fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN)).
				MustBuild()

			builder := program.NewBuilder()
			builder.Const(fn)
			const calls = 1024
			for index := 0; index < calls; index++ {
				builder.ConstGet(fn).Emit(instr.CALL)
				if index+1 < calls {
					builder.Emit(instr.DROP)
				}
			}
			prog, err := builder.Build()
			require.NoError(t, err)

			profile := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profile))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			value, err := vm.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, types.BoxI32(1), value)
			vm.Flush()

			retirements, ok := profile.Metric("vm_jit_retirements_total",
				prof.Label{Key: "func", Value: "1"},
				prof.Label{Key: "ip", Value: "0"},
				prof.Label{Key: "kind", Value: "call"},
				prof.Label{Key: "frontend", Value: "static"})
			require.True(t, ok)
			require.Equal(t, float64(1), retirements)

			entries, ok := profile.Metric("vm_jit_native_entries_total",
				prof.Label{Key: "func", Value: "1"},
				prof.Label{Key: "ip", Value: "0"},
				prof.Label{Key: "kind", Value: "call"},
				prof.Label{Key: "frontend", Value: "static"})
			require.True(t, ok)
			require.Less(t, entries, float64(4096), "probe should retire within its bounded adaptive budget")
		})

		t.Run("keeps a faster function entry", func(t *testing.T) {
			const ops = 128
			body := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Locals(types.TypeI32)
			body.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 0))
			for range ops {
				body.Emit(
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_ADD),
					instr.New(instr.LOCAL_SET, 0),
				)
			}
			body.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
			fn := body.MustBuild()

			builder := program.NewBuilder()
			builder.Const(fn)
			const calls = 128
			for index := 0; index < calls; index++ {
				builder.ConstGet(fn).Emit(instr.CALL)
				if index+1 < calls {
					builder.Emit(instr.DROP)
				}
			}
			prog, err := builder.Build()
			require.NoError(t, err)

			profile := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profile))
			defer vm.Close()
			const want int32 = ops
			for range 3 {
				require.NoError(t, vm.Run(context.Background()))
				value, err := vm.PopBoxed()
				require.NoError(t, err)
				require.Equal(t, types.BoxI32(want), value)
				vm.Reset()
			}
			vm.Flush()

			_, retired := profile.Metric("vm_jit_retirements_total",
				prof.Label{Key: "func", Value: "1"},
				prof.Label{Key: "ip", Value: "0"},
				prof.Label{Key: "kind", Value: "call"},
				prof.Label{Key: "frontend", Value: "static"})
			require.False(t, retired, "a faster native function entry must not retire")
		})
	}

	if runtime.GOARCH == "arm64" {
		t.Run("ARM64 in-loop branch rejoins the header natively", func(t *testing.T) {
			const size = int32(64)
			b := program.NewBuilder()
			b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
			loop := b.Label()
			odd := b.Label()
			advance := b.Label()
			done := b.Label()
			values := make(types.TypedArray[int32], size)
			for index := range values {
				values[index] = int32(index)
			}
			b.ConstGet(values).Emit(instr.LOCAL_SET, 0)
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
				jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
				threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
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
					require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
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
			traces := float64(0)
			for _, metric := range profile.Metrics() {
				if metric.Name != "vm_jit_compiles_total" {
					continue
				}
				if jitLabel(metric.Labels, "func") == "0" &&
					jitLabel(metric.Labels, "frontend") == "trace" &&
					jitLabel(metric.Labels, "outcome") == "emitted" &&
					jitLabel(metric.Labels, "ip") != "0" {
					traces += metric.Value
				}
			}
			require.Greater(t, traces, float64(0), "static entry must yield a hot loop to the trace frontend")
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

			jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
			threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
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
				require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
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

			jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
			threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
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
				require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
				jit.Reset()
				threaded.Reset()
			}
		})

		t.Run("STRUCT_NEW_DEFAULT's default ref field agrees between threaded and JIT", func(t *testing.T) {
			// A struct's unset ref field is a raw zero word, not the
			// BoxedNull bit pattern, and ref.is_null must report both as
			// null. The tree's leaves reach ref.is_null through exactly that
			// field, so a JIT lowering that only matched BoxedNull fell
			// through into the ref.cast below and trapped.
			const listing = `
.types
struct {value: i64; left: any; right: any}
.constants
func(i32) struct {value: i64; left: any; right: any}
	struct {value: i64; left: any; right: any}
	struct.new_default 0
	local.set 1
	local.get 1
	i32.const 0
	local.get 0
	i32.to_i64_s
	struct.set
	local.get 0
	i32.const 0
	i32.le_s
	br_if buildDone
	local.get 1
	i32.const 1
	local.get 0
	i32.const 1
	i32.sub
	const.get 0
	call
	struct.set
	local.get 1
	i32.const 2
	local.get 0
	i32.const 1
	i32.sub
	const.get 0
	call
	struct.set
	buildDone:
	local.get 1
	return
func(struct {value: i64; left: any; right: any}) i32
	local.get 0
	ref.is_null
	br_if nullCase
	local.get 0
	ref.cast 0
	i32.const 1
	struct.get
	const.get 1
	call
	local.get 0
	i32.const 2
	struct.get
	const.get 1
	call
	i32.add
	i32.const 1
	i32.add
	return
	nullCase:
	i32.const 0
	return
.code
	i32.const 9
	const.get 0
	call
	const.get 1
	call
`
			prog, err := program.Parse(strings.NewReader(listing))
			require.NoError(t, err)

			threaded := interp.New(prog, interp.WithThreshold(-1))
			t.Cleanup(func() { require.NoError(t, threaded.Close()) })
			require.NoError(t, threaded.Run(context.Background()))
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, types.BoxI32(1023), want)

			jit := interp.New(prog, interp.WithThreshold(0))
			t.Cleanup(func() { require.NoError(t, jit.Close()) })
			require.NoError(t, jit.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded")
		})
	}
	modes := []struct {
		name string
		opts []interp.Option
	}{
		{name: "standalone", opts: []interp.Option{interp.WithTick(1), interp.WithThreshold(-1)}},
		{name: "fused", opts: []interp.Option{interp.WithThreshold(-1)}},
	}
	for _, tt := range runTests {
		name := runTestName(tt.program)
		for _, mode := range modes {
			t.Run(name+"/"+mode.name, func(t *testing.T) {
				i := interp.New(tt.program, mode.opts...)
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
			vm := interp.New(program.New(benchmarkNumeric), interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
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
			vm := interp.New(arrayExit, interp.WithProfiler(profile), interp.WithTick(1), interp.WithThreshold(0))
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
			require.ErrorIs(t, vm.Run(context.Background()), interp.ErrIndexOutOfRange)
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
			vm := interp.New(divide, interp.WithProfiler(profile), interp.WithTick(1), interp.WithThreshold(0))
			for range 8 {
				vm.Reset()
				require.NoError(t, vm.Push(types.I32(90)))
				require.NoError(t, vm.Push(types.I32(3)))
				require.NoError(t, vm.Run(context.Background()))
				value, err := vm.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I32(30), value)
			}
			require.True(t, jitCompiledAt(vm, profile, 1, 0))

			vm.Reset()
			require.NoError(t, vm.Push(types.I32(90)))
			require.NoError(t, vm.Push(types.I32(0)))
			require.ErrorIs(t, vm.Run(context.Background()), interp.ErrDivideByZero)
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
	run := func(t *testing.T, prog *program.Program, opts ...interp.Option) outcome {
		t.Helper()
		i := interp.New(prog, opts...)
		defer i.Close()
		err := i.Run(context.Background())
		result := outcome{code: interp.ErrorCode(err)}
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
		oracle := run(t, tt.prog, interp.WithTick(1), interp.WithThreshold(-1))
		t.Run("parity/"+tt.name+"/fused", func(t *testing.T) {
			require.Equal(t, oracle, run(t, tt.prog, interp.WithThreshold(-1)))
		})
		if runtime.GOARCH == "arm64" {
			t.Run("parity/"+tt.name+"/jit warm", func(t *testing.T) {
				i := interp.New(tt.prog, interp.WithThreshold(0))
				defer i.Close()
				require.Equal(t, oracle.code, interp.ErrorCode(i.Run(context.Background())))
				i.Reset()

				err := i.Run(context.Background())
				result := outcome{code: interp.ErrorCode(err)}
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
		runHost := func(opts ...interp.Option) (types.Value, int) {
			calls := 0
			host := interp.NewHostFunction(
				&types.FunctionType{Returns: []types.Type{types.TypeI32}},
				func(_ *interp.Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
					calls++
					return []types.Boxed{types.BoxI32(42)}, nil
				},
			)
			prog := program.New(
				[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
				program.WithConstants(host),
			)
			i := interp.New(prog, opts...)
			defer i.Close()
			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			return value, calls
		}

		want, calls := runHost(interp.WithTick(1), interp.WithThreshold(-1))
		require.Equal(t, 1, calls)
		got, calls := runHost(interp.WithThreshold(-1))
		require.Equal(t, want, got)
		require.Equal(t, 1, calls)
		if runtime.GOARCH == "arm64" {
			got, calls = runHost(interp.WithThreshold(0))
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

		runHost := func(opts ...interp.Option) (types.Value, int32) {
			setup := interp.New(program.New(nil))
			defer setup.Close()
			src := &counter{Count: 7}
			host, err := interp.NewRegistry().Marshal(setup, src)
			require.NoError(t, err)

			prog := program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.DUP),
				instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 99), instr.New(instr.STRUCT_SET),
				instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
			}, program.WithConstants(host))
			i := interp.New(prog, opts...)
			defer i.Close()
			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			// The Go value carries the write, so a method reading it agrees
			// with what the guest just stored.
			return value, bump(src)
		}

		want, seen := runHost(interp.WithTick(1), interp.WithThreshold(-1))
		require.Equal(t, types.I32(99), want)
		require.Equal(t, int32(99), seen)
		got, seen := runHost(interp.WithThreshold(-1))
		require.Equal(t, want, got)
		require.Equal(t, int32(99), seen)
		if runtime.GOARCH == "arm64" {
			got, seen = runHost(interp.WithThreshold(0))
			require.Equal(t, want, got)
			require.Equal(t, int32(99), seen)
		}
	})

	t.Run("parity/host container writes reach the Go value", func(t *testing.T) {
		runHost := func(value any, code []instr.Instruction, opts ...interp.Option) types.Value {
			setup := interp.New(program.New(nil))
			defer setup.Close()
			host, err := interp.NewRegistry().Marshal(setup, value)
			require.NoError(t, err)

			i := interp.New(program.New(code, program.WithConstants(host)), opts...)
			defer i.Close()
			require.NoError(t, i.Run(context.Background()))
			out, err := i.Pop()
			require.NoError(t, err)
			return out
		}

		for _, tt := range []struct {
			name string
			run  func(opts ...interp.Option) (types.Value, any)
			want any
		}{
			{
				name: "array element",
				run: func(opts ...interp.Option) (types.Value, any) {
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
				run: func(opts ...interp.Option) (types.Value, any) {
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
				value, src := tt.run(interp.WithTick(1), interp.WithThreshold(-1))
				require.Equal(t, types.I32(99), value)
				require.Equal(t, tt.want, src)

				value, src = tt.run(interp.WithThreshold(-1))
				require.Equal(t, types.I32(99), value)
				require.Equal(t, tt.want, src)

				if runtime.GOARCH == "arm64" {
					value, src = tt.run(interp.WithThreshold(0))
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
		i := interp.New(prog)
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
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
		i := interp.New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		top, err := i.Peek(0)
		require.NoError(t, err)
		require.Equal(t, 1, top.Ref())
		rc1, err := i.RefCount(1)
		require.NoError(t, err)
		require.Equal(t, 1, rc1) // selected ref survives on the stack
		_, err = i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault) // discarded ref released to zero
	})

	t.Run("GLOBAL_TEE retains the ref stored into the global slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.GLOBAL_TEE, 0), // duplicates ownership: stack + global
			instr.New(instr.DROP),          // drop stack copy; global still owns
		}, program.WithGlobals(types.TypeAny))
		i := interp.New(prog)
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
		i := interp.New(prog)
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
		i := interp.New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		_, err := i.RefCount(1)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
		_, err = i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})

	t.Run("REF_NE releases both consumed refs", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.I32_CONST, 2), instr.New(instr.REF_NEW), // heap[2]
			instr.New(instr.REF_NE),
		})
		i := interp.New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		_, err := i.RefCount(1)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
		_, err = i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})

	t.Run("REF_TEST releases the consumed ref", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.REF_TEST, 0),
		}, program.WithTypes(types.TypeI32))
		i := interp.New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		_, err := i.RefCount(1)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})

	t.Run("REF_IS_NULL releases the consumed ref", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.REF_IS_NULL),
		})
		i := interp.New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		_, err := i.RefCount(1)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})

	t.Run("fused trapping sources use the remaining stack slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(8))),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2))),
			instr.New(instr.F32_DIV),
		}, program.WithGlobals(types.TypeF32))
		i := interp.New(prog, interp.WithStack(2), interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithStack(1), interp.WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrStackOverflow)
		require.Equal(t, 1, i.Len())
	})

	t.Run("CONST_GET reports overflow before retaining a ref", func(t *testing.T) {
		fn := types.NewFunction(&types.FunctionType{}, nil, nil)
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.CONST_GET, 0),
		}, program.WithConstants(fn))
		i := interp.New(prog, interp.WithStack(1), interp.WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrStackOverflow)
		require.Equal(t, 1, i.Len())
	})

	t.Run("fused REF_IS_NULL reports overflow before pushing", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.REF_IS_NULL),
		}, program.WithGlobals(types.TypeAny))
		i := interp.New(prog, interp.WithStack(1), interp.WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrStackOverflow)
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
		i := interp.New(prog, interp.WithStack(1), interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithStack(1), interp.WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrStackOverflow)
		require.Equal(t, 1, i.Len())
	})

	t.Run("STRUCT_NEW_DEFAULT reports stack overflow before mutating sp", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.STRUCT_NEW_DEFAULT, 0),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32))))
		i := interp.New(prog, interp.WithStack(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrStackOverflow)
		require.Equal(t, 1, i.Len())
	})

	t.Run("LOCAL_GET rejects one-past-current local slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.DROP),
			instr.New(instr.LOCAL_GET, 0),
		}, program.WithLocals(types.TypeI32))
		i := interp.New(prog, interp.WithTick(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrSegmentationFault)
	})

	t.Run("LOCAL_GET rejects undeclared metadata without panicking during threading", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.LOCAL_GET, 0)})
		i := interp.New(prog, interp.WithTick(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrSegmentationFault)
	})

	t.Run("LOCAL_SET rejects one-past-current local slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.DROP),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.LOCAL_SET, 1),
		}, program.WithLocals(types.TypeI32, types.TypeI32))
		i := interp.New(prog, interp.WithTick(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrSegmentationFault)
	})

	t.Run("LOCAL_TEE rejects one-past-current local slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.DROP),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.LOCAL_TEE, 1),
		}, program.WithLocals(types.TypeI32, types.TypeI32))
		i := interp.New(prog, interp.WithTick(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrSegmentationFault)
	})

	t.Run("fused LOCAL_GET rejects one-past-current local slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.DROP),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, i32operand(1)),
			instr.New(instr.I32_ADD),
		}, program.WithLocals(types.TypeI32))
		i := interp.New(prog, interp.WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrSegmentationFault)
	})

	t.Run("GLOBAL_SET rejects an undeclared global slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.GLOBAL_SET, 0),
		})
		i := interp.New(prog)
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrSegmentationFault)
	})

	t.Run("GLOBAL_TEE rejects an undeclared global slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.GLOBAL_TEE, 0),
		})
		i := interp.New(prog)
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrSegmentationFault)
	})

	t.Run("unseeded declared globals read kind-correct zeros", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.I32_CONST, i32operand(2)),
			instr.New(instr.I32_ADD), // fuses without any prior GLOBAL_SET/SetGlobal
			instr.New(instr.GLOBAL_GET, 1),
			instr.New(instr.GLOBAL_GET, 2),
		}, program.WithGlobals(types.TypeI32, types.TypeF64, types.TypeAny))
		i := interp.New(prog)
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
		i := interp.New(prog)
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
		i := interp.New(prog)
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
		i := interp.New(prog, interp.WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrTypeMismatch)
	})

	t.Run("ARRAY_NEW_DEFAULT rejects negative size with VM error", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-1)),
			instr.New(instr.ARRAY_NEW_DEFAULT, 0),
		}, program.WithTypes(types.TypeI32Array))
		i := interp.New(prog)
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrSegmentationFault)
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
		i := interp.New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		_, err := i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault) // every overwritten element is released,
		_, err = i.RefCount(3)
		require.ErrorIs(t, err, interp.ErrSegmentationFault) // not just the first one
		_, err = i.RefCount(4)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
		rc5, err := i.RefCount(5)
		require.NoError(t, err)
		require.Equal(t, 3, rc5) // fill value owned once per filled slot
	})

	t.Run("host call with an all-scalar signature works through the generic path (exact, fusion disabled)", func(t *testing.T) {
		hostFn := interp.NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			func(_ *interp.Interpreter, args []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(args[0].I32() * args[1].I32())}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 6), instr.New(instr.I32_CONST, 7),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(hostFn))
		i := interp.New(prog, interp.WithTick(1)) // exact: disables fusion, forcing the generic callHost path
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), v)
	})

	t.Run("host call releases a ref param the callee does not return (fused)", func(t *testing.T) {
		hostFn := interp.NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeAny}, Returns: []types.Type{types.TypeI32}},
			func(_ *interp.Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(1)}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 9), instr.New(instr.REF_NEW), // heap[1] is hostFn; heap[2] is this ref
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(hostFn))
		i := interp.New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault) // arg not returned: host cleanup released it
	})

	t.Run("host call releases a ref param the callee does not return (generic, exact)", func(t *testing.T) {
		hostFn := interp.NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeAny}, Returns: []types.Type{types.TypeI32}},
			func(_ *interp.Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(1)}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 9), instr.New(instr.REF_NEW),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(hostFn))
		i := interp.New(prog, interp.WithTick(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})

	for _, tt := range []struct {
		name string
		opts []interp.Option
	}{
		{name: "fused"},
		{name: "generic", opts: []interp.Option{interp.WithTick(1)}},
	} {
		t.Run("host call releases the consumed callable ref on fused and generic paths "+tt.name, func(t *testing.T) {
			hostFn := interp.NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
				func(_ *interp.Interpreter, args []types.Boxed) ([]types.Boxed, error) {
					return []types.Boxed{args[0]}, nil
				})
			prog := program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 9),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			}, program.WithConstants(hostFn))
			i := interp.New(prog, tt.opts...)
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			rc, err := i.RefCount(1)
			require.NoError(t, err)
			require.Equal(t, 1, rc)
		})
	}

	t.Run("generic host call can return the consumed callable ref", func(t *testing.T) {
		hostFn := interp.NewHostFunction(&types.FunctionType{Returns: []types.Type{types.TypeAny}},
			func(i *interp.Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				v, peekErr := i.Peek(0)
				if peekErr != nil {
					return nil, peekErr
				}
				return []types.Boxed{v}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(hostFn))
		i := interp.New(prog, interp.WithTick(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		rc, err := i.RefCount(1)
		require.NoError(t, err)
		require.Equal(t, 2, rc)
	})

	t.Run("host call releases a promoted i64 param even though I64 is declared (not the scalar fast path)", func(t *testing.T) {
		huge := int64(1) << 50
		hostFn := interp.NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI32}},
			func(_ *interp.Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(1)}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(huge)), // heap[1] is hostFn; heap[2] is this promoted i64
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(hostFn))
		i := interp.New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault) // promoted i64 arg released: I64 params keep the generic scanning path
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
		i := interp.New(prog, interp.WithTick(1), interp.WithHook(func(i *interp.Interpreter) error {
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
		i := interp.New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault) // old ref capture released on overwrite
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
		i := interp.New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault) // old promoted capture released: I64 captures keep the generic ref-aware path
	})

	t.Run("fused LOCAL_GET+CONST binop computes correctly for i32 (interp-only)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.I32_ADD),
		}, program.WithLocals(types.TypeI32))
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
			err: interp.ErrDivideByZero,
		},
		{
			name: "promoted i64 divide by zero preserves trap state",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)),
				instr.New(instr.I64_CONST, 0),
				instr.New(instr.I64_DIV_S),
			}),
			err: interp.ErrDivideByZero,
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
			err: interp.ErrDivideByZero,
		},
	}
	for _, tt := range parity {
		t.Run(tt.name, func(t *testing.T) {
			states := make([]parityState, 0, 2)
			for _, opts := range [][]interp.Option{
				{interp.WithTick(1)},
				{interp.WithThreshold(-1)},
			} {
				i := interp.New(tt.prog, opts...)
				err := i.Run(context.Background())
				if tt.err == nil {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, tt.err)
				}

				state := parityState{
					code: interp.ErrorCode(err),
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
				for addr := 1; addr < i.HeapLen(); addr++ {
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
			i := interp.New(tt.prog, interp.WithThreshold(-1))
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
			i := interp.New(tt.prog, interp.WithThreshold(-1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			live := 0
			for addr := 1; addr < i.HeapLen(); addr++ {
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
		i := interp.New(prog, interp.WithTick(1)) // exact: disables fusion, forcing the generic path
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(200), v) // 2 < 10: branch taken
	})
}

// refCountAt reads one address's reference count through the public API. The
// callers below assert on an address they just observed live, so a lookup
// error is a test failure rather than a case to handle.
func refCountAt(t *testing.T, i *interp.Interpreter, addr int) int {
	t.Helper()
	count, err := i.RefCount(addr)
	require.NoError(t, err)
	return count
}

// ArraySetAfterNestedCalls protects compiled stack materialization across
// a SIGSEGV in generated ARM64 code: an outer row loop whose body inlines
// branchy F64 tree calls and ends each iteration with ARRAY_SET. Register
// pressure used to spill inside the terminal mutation trace, letting a branch
// skip spill-frame work and corrupt the Go stack.
func TestARM64_ArraySetAfterNestedCalls(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	const trees = 2
	const rows = 8
	row := make([]float64, rows)
	out := make([]float64, rows)
	rowArr := types.TypedArray[float64](row)
	outArr := types.TypedArray[float64](out)

	fn := types.NewFunctionBuilder(nil).
		Params(types.TypeF64Array).
		Returns(types.TypeF64)
	left := fn.Label()
	fn.Emit(instr.New(instr.LOCAL_GET, 0)).
		Emit(instr.New(instr.I32_CONST, 0)).
		Emit(instr.New(instr.ARRAY_GET)).
		Emit(instr.New(instr.F64_CONST, math.Float64bits(0.5))).
		Emit(instr.New(instr.F64_LE)).
		BrIf(left).
		Emit(instr.New(instr.F64_CONST, math.Float64bits(-0.01))).
		Emit(instr.New(instr.RETURN)).
		Bind(left).
		Emit(instr.New(instr.F64_CONST, math.Float64bits(0.01))).
		Emit(instr.New(instr.RETURN))
	tree, err := fn.Build()
	require.NoError(t, err)

	b := program.NewBuilder()
	b.Locals(types.TypeI32, types.TypeF64)
	b.Const(rowArr)
	b.Const(outArr)
	b.Const(tree)

	loop := b.Label()
	b.Emit(instr.I32_CONST, 0).
		Emit(instr.LOCAL_SET, 0).
		Bind(loop).
		Emit(instr.F64_CONST, 0).
		Emit(instr.LOCAL_SET, 1)
	for range trees {
		b.Emit(instr.LOCAL_GET, 1).
			ConstGet(rowArr).
			ConstGet(tree).
			Emit(instr.CALL).
			Emit(instr.F64_ADD).
			Emit(instr.LOCAL_SET, 1)
	}
	b.ConstGet(outArr).
		Emit(instr.LOCAL_GET, 0).
		Emit(instr.LOCAL_GET, 1).
		Emit(instr.ARRAY_SET).
		Emit(instr.LOCAL_GET, 0).
		Emit(instr.I32_CONST, 1).
		Emit(instr.I32_ADD).
		Emit(instr.LOCAL_TEE, 0).
		Emit(instr.I32_CONST, uint64(uint32(rows))).
		Emit(instr.I32_LT_S).
		BrIf(loop)

	prog, err := b.Build()
	require.NoError(t, err)

	i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(1))
	defer i.Close()

	for n := 0; n < 256; n++ {
		for idx := range row {
			row[idx] = float64((n*13+idx*7)%19) / 19
		}
		require.NoError(t, i.Run(context.Background()))
		i.Reset()
	}

	// The JIT result must match the pure interpreter on the same program:
	// a spill-path bug would corrupt the accumulated sum.
	jitOut := make([]float64, len(out))
	copy(jitOut, out)

	ref := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
	defer ref.Close()
	for n := 0; n < 256; n++ {
		for idx := range row {
			row[idx] = float64((n*13+idx*7)%19) / 19
		}
		require.NoError(t, ref.Run(context.Background()))
		ref.Reset()
	}
	require.Equal(t, jitOut, out)
}

func TestARM64_LoopCarriedLocals(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	t.Run("folded side exits preserve accumulators", func(t *testing.T) {
		const size = int32(16)
		b := program.NewBuilder()
		b.Locals(types.TypeI32, types.TypeI32)
		loop := b.Label()
		odd := b.Label()
		advance := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(odd)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(advance)
		b.Bind(odd)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Bind(advance)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for iteration := 0; iteration < 32; iteration++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, types.BoxI32(size+size/2), got)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())

		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	})

	t.Run("yield commits before WithTick one safepoint", func(t *testing.T) {
		// loopSafepointBudget mirrors the native loop back-edge budget interp
		// spends between safepoints. The loop must run past it for a native
		// yield to be observable, and the budget is not exported, so raising
		// it in interp without raising this copy silently stops covering the
		// safepoint commit.
		const loopSafepointBudget = 1 << 13
		const limit = int32(loopSafepointBudget + 3)
		b := program.NewBuilder()
		b.Locals(types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(limit))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		for iteration := 0; iteration < 12; iteration++ {
			require.NoError(t, jit.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, types.BoxI32(limit), got)
			jit.Reset()
		}
		require.NoError(t, jit.Close())

		var yields float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_yields_total" {
				yields += metric.Value
			}
		}
		require.Greater(t, yields, float64(0))
	})
}

// AbortedSideExitDoesNotComplete protects partial unsupported traces from
// miscompile where a captured side-exit fragment that recorded a few
// supported opcodes and then aborted on an unsupported one (MAP_NEW_DEFAULT
// is not recordable) could be mistaken for a normal top-level completion:
// lowering a learned continuation used to check the entry root rather than
// the current block, so
// an aborted fragment whose ops simply ran out could fall through as if it
// had returned normally. The x>0 path (taken while warming up) compiles as
// the JIT entry trace; the x<=0 path is hit often enough at runtime to cross
// exitThreshold and force the tracer to capture — and abort on — the
// MAP_NEW_DEFAULT side exit. The JIT-enabled run must match a pure
// interpreter run (WithThreshold(-1)) on every input.
func TestARM64_AbortedSideExitDoesNotComplete(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	b := program.NewBuilder()
	b.Globals(types.TypeI32, types.TypeI32) // 0: x (in), 1: result (out)
	mapIdx := b.Type(types.NewMapType(types.TypeI32, types.TypeI32))
	pathA := b.Label()
	done := b.Label()
	b.Emit(instr.GLOBAL_GET, 0).
		Emit(instr.I32_CONST, 0).
		Emit(instr.I32_GT_S).
		BrIf(pathA).
		Emit(instr.I32_CONST, 4).
		Emit(instr.MAP_NEW_DEFAULT, uint64(mapIdx)).
		Emit(instr.MAP_LEN).
		Emit(instr.I32_CONST, 77).
		Emit(instr.I32_ADD).
		Emit(instr.GLOBAL_SET, 1).
		Br(done).
		Bind(pathA).
		Emit(instr.I32_CONST, 1).
		Emit(instr.GLOBAL_SET, 1).
		Bind(done)
	prog, err := b.Build()
	require.NoError(t, err)

	// Mostly positive inputs (compile and exercise the JIT-native path A),
	// with a non-positive input every 4th call starting after warm-up (path
	// B) so the side exit's hit count reaches exitThreshold within the run.
	// The first several calls stay positive so the entry trace itself
	// records path A, not path B.
	inputs := make([]int32, 40)
	for n := range inputs {
		if n >= 4 && n%4 == 0 {
			inputs[n] = -1
		} else {
			inputs[n] = 5
		}
	}

	jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(1))
	defer jit.Close()
	threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
	defer threaded.Close()
	for _, input := range inputs {
		require.NoError(t, jit.SetGlobal(0, types.BoxI32(input)))
		require.NoError(t, threaded.SetGlobal(0, types.BoxI32(input)))
		require.NoError(t, jit.Run(context.Background()))
		require.NoError(t, threaded.Run(context.Background()))
		got, err := jit.Global(1)
		require.NoError(t, err)
		want, err := threaded.Global(1)
		require.NoError(t, err)
		require.Equal(t, want, got)
		jit.Reset()
		threaded.Reset()
	}
}

// TestARM64_SelfCallFromInlinedFrame protects selfCall's live-frame
// precondition. selfCall lowers recursion as a BL back to ctx.head, which
// re-enters the plan's entry prologue - correct only when the live frame is
// that plan's own. It used to check neither ctx.kind nor len(ctx.frames), and
// the trace frontend reaches it with a foreign frame live: any CALL in a
// function-entry-anchored trace whose callee ref matches the anchor is recorded
// as an ordinary self CALL, including one nested inside an already-inlined
// callee's body. So A inlining B (frames 1 -> 2) and B calling back into A
// arrives at selfCall with two frames, and the BL lays A's parameter prologue
// over B's activation.
//
// The mutual recursion below reaches that state by passing both callees as ref
// parameters, so no callee is a static constant, A's static plan is rejected
// outright, and the trace frontend is the only one left. B carries three locals
// A does not, so the two frame shapes differ; with matching shapes the
// corruption happens to be survivable, and with differing ones the process
// segfaults inside the Go runtime on main.
func TestARM64_SelfCallFromInlinedFrame(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	// A(n, selfA, selfB): if n <= 0 return 0; return 1 + selfB(n-1, selfA, selfB)
	aBuilder := types.NewFunctionBuilder(nil).
		Params(types.TypeI32, types.TypeAny, types.TypeAny).
		Returns(types.TypeI32)
	baseA := aBuilder.Label()
	aFn := aBuilder.
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S)).
		BrIf(baseA).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.CALL),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		).
		Bind(baseA).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
		MustBuild()

	// B(n, selfA, selfB): three extra locals widen B's frame past A's, then it
	// calls back into A - the self call that reaches selfCall with B's frame
	// live. Local 5 stays live across that call so the frame cannot be elided.
	bBuilder := types.NewFunctionBuilder(nil).
		Params(types.TypeI32, types.TypeAny, types.TypeAny).
		Locals(types.TypeI32, types.TypeI32, types.TypeI32).
		Returns(types.TypeI32)
	baseB := bBuilder.Label()
	bFn := bBuilder.
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S)).
		BrIf(baseB).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 3),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.I32_CONST, 100), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 4),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_CONST, 100), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 5),
			instr.New(instr.LOCAL_GET, 3),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.CALL),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 5), instr.New(instr.I32_SUB), instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).
		Bind(baseB).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
		MustBuild()

	// Depth is varied so the trace tree keeps rebuilding and the self call is
	// reached from inlined frames at many recursion depths.
	for depth := 1; depth <= 16; depth++ {
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, uint64(depth)),
				instr.New(instr.CONST_GET, 0), // selfA
				instr.New(instr.CONST_GET, 1), // selfB
				instr.New(instr.CONST_GET, 0), // callee
				instr.New(instr.CALL),
			},
			program.WithConstants(aFn, bFn),
		)

		jit := interp.New(prog, interp.WithThreshold(0), interp.WithTick(1))
		threaded := interp.New(prog, interp.WithThreshold(-1))
		for iter := 0; iter < 125; iter++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "depth %d iteration %d", depth, iter)
			require.Equal(t, refCounts(threaded), refCounts(jit), "depth %d iteration %d", depth, iter)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
	}
}

// TestARM64_CalleeLocals covers the callee frame's non-parameter locals. A frame
// opens on stack space an earlier frame may have left populated, and threaded
// CALL clears that range before transferring control, so native code must too.
// Neither native call path did it reliably: selfCall emitted no clear at all,
// and directCall's clear was computed against the wrong frame base. A callee
// therefore started with stale boxed words - its first LOCAL_SET released a ref
// it never owned, and RETURN teardown released the rest.
//
// Both sub-cases read the local BEFORE writing it and fold the answer into the
// result, so a stale slot shows up as a wrong value rather than only as a
// refcount drift. The control has no non-parameter local, which is the shape
// every pre-existing self-call test used - len(Slots()) == params leaves the
// uninitialized range empty, which is why none of them caught this.
func TestARM64_CalleeLocals(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	// Each level contributes 1000 when its ref local reads back null, as the
	// threaded interpreter guarantees, and 0 when it reads stale stack data.
	const perLevel = 1000

	runParity := func(t *testing.T, prog *program.Program) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithThreshold(-1))
		for n := 0; n < 64; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())

		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0), "expected native code to be installed")
	}

	// probe reads ref local 1 before assigning it, scales the answer, and leaves
	// it on the stack for the caller to fold in.
	probe := []instr.Instruction{
		instr.New(instr.LOCAL_GET, 1), instr.New(instr.REF_IS_NULL),
		instr.New(instr.I32_CONST, perLevel), instr.New(instr.I32_MUL),
		instr.New(instr.CONST_GET, 2), instr.New(instr.LOCAL_SET, 1),
	}

	t.Run("self-recursion through selfCall", func(t *testing.T) {
		b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Params(types.TypeI32).
			Locals(types.TypeAny)
		base := b.Label()
		body := append(append([]instr.Instruction{}, probe...),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.I32_ADD), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		)
		fn := b.
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).
			BrIf(base).
			Emit(body...).
			Bind(base).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
			MustBuild()

		runParity(t, program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 12),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			},
			program.WithConstants(fn, fn, types.String("s")),
		))
	})

	t.Run("mutual recursion through directCall", func(t *testing.T) {
		// Neither callee is the function being compiled, so each CALL lowers
		// through directCall's natives-slot BLR rather than selfCall's BL.
		build := func(other uint64) *types.Function {
			b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Params(types.TypeI32).
				Locals(types.TypeAny)
			base := b.Label()
			body := append(append([]instr.Instruction{}, probe...),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, other), instr.New(instr.CALL),
				instr.New(instr.I32_ADD), instr.New(instr.RETURN),
			)
			return b.
				Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S)).
				BrIf(base).
				Emit(body...).
				Bind(base).
				Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
				MustBuild()
		}

		runParity(t, program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 20),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			},
			program.WithConstants(build(1), build(0), types.String("s")),
		))
	})
}

// SelfCallWithRefArg protects a self-recursive function that forwards its own
// callee ref as an argument. flush used to refuse a committing flush whenever
// any live operand was a KindRef, including a ref parameter merely passed
// through, so every such self-call failed to lower and rejected the whole
// compile. A rejected anchor is never retried, so the function stayed
// interpreted for the process lifetime while still returning the right value.
func TestARM64_SelfCallWithRefArg(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	b := types.NewFunctionBuilder(nil).
		Params(types.TypeI32, types.TypeAny).
		Returns(types.TypeI32)
	base := b.Label()
	fib := b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).
		BrIf(base).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL),
			instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		).
		Bind(base).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
		MustBuild()
	prog := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, 20),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		},
		program.WithConstants(fib),
	)

	profile := prof.New()
	jit := interp.New(prog, interp.WithProfiler(profile))
	threaded := interp.New(prog, interp.WithThreshold(-1))

	for range 64 {
		require.NoError(t, jit.Run(context.Background()))
		require.NoError(t, threaded.Run(context.Background()))
		got, err := jit.PopBoxed()
		require.NoError(t, err)
		want, err := threaded.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, types.BoxI32(6765), got)
		jit.Reset()
		threaded.Reset()
	}
	require.NoError(t, threaded.Close())
	require.NoError(t, jit.Close())

	var entries float64
	for _, metric := range profile.Metrics() {
		if metric.Name != "vm_jit_native_entries_total" {
			continue
		}
		labels := map[string]string{}
		for _, label := range metric.Labels {
			labels[label.Key] = label.Value
		}
		if labels["func"] == "1" {
			entries += metric.Value
		}
	}
	require.Greater(t, entries, float64(0), "self-recursive function must retain native coverage")
}

// TestARM64_SelfCallFrameLocals protects the frame teardown that follows a
// native self-call. The callee owns every allocatable register, so the caller's
// cached local registers do not survive the BL; the teardown has to read each
// ref local from its VM stack slot instead of boxing the register it used to
// live in. Reading the stale register released whatever the recursion left
// behind, which faulted inside the Go runtime rather than diverging quietly.
//
// The shape matters: the ref local is read before it is written, which is what
// gives it a cached register live across the call, and the recursion has to run
// deep enough to reach the entry compile.
func TestARM64_SelfCallFrameLocals(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Params(types.TypeI32).
		Locals(types.TypeAny)
	base := b.Label()
	fn := b.
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).
		BrIf(base).
		Emit(
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.REF_IS_NULL),
			instr.New(instr.I32_CONST, 1000), instr.New(instr.I32_MUL),
			instr.New(instr.CONST_GET, 1), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.I32_ADD), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		).
		Bind(base).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
		MustBuild()
	prog := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, 8),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		},
		program.WithConstants(fn, types.String("s")),
	)

	profile := prof.New()
	jit := interp.New(prog, interp.WithProfiler(profile), interp.WithTick(1))
	threaded := interp.New(prog, interp.WithThreshold(-1))
	for n := range 16 {
		require.NoError(t, jit.Run(context.Background()), "iteration %d", n)
		require.NoError(t, threaded.Run(context.Background()))
		got, err := jit.PopBoxed()
		require.NoError(t, err)
		want, err := threaded.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
		require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
		jit.Reset()
		threaded.Reset()
	}
	require.NoError(t, threaded.Close())
	require.NoError(t, jit.Close())

	var entries float64
	for _, metric := range profile.Metrics() {
		if metric.Name == "vm_jit_native_entries_total" {
			entries += metric.Value
		}
	}
	require.Greater(t, entries, float64(0), "expected native code to be installed")
}

// TestARM64_MutualEntries protects nested native entry frames.
func TestARM64_MutualEntries(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	build := func(other uint64) *types.Function {
		b := types.NewFunctionBuilder(nil).
			Params(types.TypeI32).
			Returns(types.TypeI32)
		base := b.Label()
		b.Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S),
		).BrIf(base)
		b.Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, other), instr.New(instr.CALL),
			instr.New(instr.RETURN),
		)
		b.Bind(base).
			Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN))
		return b.MustBuild()
	}

	const depth = int32(40)
	prog := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, uint64(uint32(depth))),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		},
		program.WithConstants(build(1), build(0)),
	)

	profile := prof.New()
	jit := interp.New(prog, interp.WithProfiler(profile), interp.WithTick(1), interp.WithThreshold(1))
	threaded := interp.New(prog, interp.WithThreshold(-1))
	for range 16 {
		require.NoError(t, jit.Run(context.Background()))
		require.NoError(t, threaded.Run(context.Background()))
		got, err := jit.PopBoxed()
		require.NoError(t, err)
		want, err := threaded.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, types.BoxI32(0), got)
		require.Equal(t, refCounts(threaded), refCounts(jit))
		jit.Reset()
		threaded.Reset()
	}
	require.NoError(t, threaded.Close())
	require.NoError(t, jit.Close())

	entries := map[string]float64{}
	for _, metric := range profile.Metrics() {
		if metric.Name != "vm_jit_native_entries_total" {
			continue
		}
		labels := map[string]string{}
		for _, label := range metric.Labels {
			labels[label.Key] = label.Value
		}
		entries[labels["func"]] += metric.Value
	}
	require.Greater(t, entries["1"], float64(0), "function A must install a native entry")
	require.Greater(t, entries["2"], float64(0), "function B must install a native entry")
}

// TestARM64_RefReturn protects the retain ordering at a native entry frame's
// RETURN. ret took the return value's retain after guardFrame had already read
// the backing local's refcount, and guardFrame deopts whenever rc <= pending.
// A function that allocates a node into a local and returns it sits exactly at
// that boundary - rc == pending == 1, the node being held only by that local -
// so every such RETURN deopted: 512 guard-value exits against 1024 native
// entries for the program below, which is the shape benchmarks/memory_test.go's
// structTreeWalk and binaryTrees builders use. Taking the retain first raises rc
// above pending, and the following releaseFrame brings it back down to the one
// live reference the caller now owns.
//
// A spurious deopt is invisible to a value or refcount oracle, because the
// interpreter finishes the RETURN correctly either way. The guard-value exit
// count is therefore the assertion that carries this test; the parity checks
// alongside it only rule out the retain leaking or freeing.
func TestARM64_RefReturn(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	runParity := func(t *testing.T, prog *program.Program, want types.Boxed) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithThreshold(-1))
		for n := 0; n < 64; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			ref, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, ref, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, want, got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())

		var entries, guardValueExits float64
		for _, metric := range profile.Metrics() {
			switch metric.Name {
			case "vm_jit_native_entries_total":
				entries += metric.Value
			case "vm_jit_native_exits_total":
				for _, label := range metric.Labels {
					if label.Key == "reason" && label.Value == "guard-value" {
						guardValueExits += metric.Value
					}
				}
			}
		}
		require.Greater(t, entries, float64(0), "expected native code to be installed")
		require.Zero(t, guardValueExits, "guardFrame spuriously deopted a RETURN of a singly-owned frame local")
	}

	t.Run("entry frame returns a singly-owned frame local", func(t *testing.T) {
		const iterations = int32(512)
		nodeType := types.NewStructType(types.NewStructField(types.TypeI32, types.FieldWithName("val")))
		// build(v): n = STRUCT_NEW_DEFAULT; n.val = v; return n. Local 1 is the
		// only holder of n at RETURN, so its refcount equals the frame's pending
		// count and guardFrame's rc <= pending check is exactly straddled.
		buildFn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny}}).
			Params(types.TypeI32).
			Locals(types.TypeAny).
			Emit(
				instr.New(instr.STRUCT_NEW_DEFAULT, 0), instr.New(instr.LOCAL_SET, 1),
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.STRUCT_SET),
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.RETURN),
			).
			MustBuild()

		// The module driver calls build in a loop so build's own entry warms up
		// and compiles within a single Run.
		b := program.NewBuilder()
		b.Type(nodeType)
		b.Const(buildFn)
		b.Locals(types.TypeI32, types.TypeI32) // 0=i, 1=sum
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(iterations))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1)
		b.Emit(instr.LOCAL_GET, 0).ConstGet(buildFn).Emit(instr.CALL)
		b.Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		runParity(t, prog, types.BoxI32(iterations*(iterations-1)/2))
	})

	t.Run("self-recursive ref return through selfCall", func(t *testing.T) {
		nodeType := types.NewStructType(
			types.NewStructField(types.TypeI32, types.FieldWithName("val")),
			types.NewStructField(types.TypeAny, types.FieldWithName("next")),
		)
		// chain(d): n = STRUCT_NEW_DEFAULT; n.val = d; if d > 0, n.next =
		// chain(d-1); return n. Every level's RETURN hands back a node whose
		// refcount is exactly 1 (held only by local 1), so the self-recursive
		// call inside the body (CONST_GET 0; CALL, where 0 is this function's
		// own constant index) must lower through selfCall - which rejected any
		// ref-returning target before checkReturns admitted KindRef.
		chainBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny}}).
			Params(types.TypeI32).
			Locals(types.TypeAny)
		done := chainBuilder.Label()
		chainFn := chainBuilder.
			Emit(
				instr.New(instr.STRUCT_NEW_DEFAULT, 0), instr.New(instr.LOCAL_SET, 1),
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.STRUCT_SET),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S),
			).
			BrIf(done).
			Emit(
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
				instr.New(instr.STRUCT_SET),
			).
			Bind(done).
			Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.RETURN)).
			MustBuild()

		const depth = int32(16)
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, uint64(uint32(depth))),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
				instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
			},
			program.WithConstants(chainFn),
			program.WithTypes(nodeType),
		)
		runParity(t, prog, types.BoxI32(depth))
	})
}

// TestARM64_DirectSelfCall covers the fused constant-marker form of
// recursion, `const.get fn; call` where fn is the function being compiled.
// It is the shape every direct recursive call takes, and lowering it is what
// lets the static frontend plan a recursive function at all: while it was
// rejected, such a function had no whole-function plan and depended on a
// recorded trace, whose coverage varies with how much of the recursion the
// recording happened to reach. The sub-cases therefore assert both the value
// and that the emitted entry came from the static frontend.
func TestARM64_DirectSelfCall(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	build := func(n int32) *program.Program {
		b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Params(types.TypeI32)
		base := b.Label()
		fn := b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).
			BrIf(base).
			Emit(
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
				instr.New(instr.I32_ADD), instr.New(instr.RETURN),
			).
			Bind(base).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
			MustBuild()
		return program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, uint64(uint32(n))),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn),
		)
	}

	for _, tc := range []struct {
		name string
		n    int32
		want int32
	}{
		{name: "shallow recursion", n: 12, want: 144},
		{name: "recursion deeper than one trace can record", n: 24, want: 46368},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog := build(tc.n)
			profile := prof.New()
			jit := interp.New(prog, interp.WithProfiler(profile))
			threaded := interp.New(prog, interp.WithThreshold(-1))

			for range 8 {
				require.NoError(t, jit.Run(context.Background()))
				require.NoError(t, threaded.Run(context.Background()))
				got, err := jit.PopBoxed()
				require.NoError(t, err)
				want, err := threaded.PopBoxed()
				require.NoError(t, err)
				require.Equal(t, want, got)
				require.Equal(t, types.BoxI32(tc.want), got)
				jit.Reset()
				threaded.Reset()
			}
			require.NoError(t, threaded.Close())
			require.NoError(t, jit.Close())

			var static, entries float64
			for _, metric := range profile.Metrics() {
				switch metric.Name {
				case "vm_jit_entry_emits_total":
					var frontend, fn string
					for _, label := range metric.Labels {
						switch label.Key {
						case "frontend":
							frontend = label.Value
						case "func":
							fn = label.Value
						}
					}
					if frontend == "static" && fn == "1" {
						static += metric.Value
					}
				case "vm_jit_native_entries_total":
					entries += metric.Value
				}
			}
			require.Greater(t, static, float64(0), "recursive function must get a static whole-function entry")
			require.Greater(t, entries, float64(0))
		})
	}
}

// DeferredRefElision protects Phase 3 of the JIT refcount-elision work:
// LOCAL_GET/GLOBAL_GET/UPVAL_GET of a ref defers its retain to the backing
// slot instead of taking one immediately, and ARRAY_GET/ARRAY_SET elide their
// matching container release when the operand is still deferred. Every
// sub-case asserts both the computed result and the exact heap refcount
// survive repeated JIT warmup, so a missed retain (use-after-free) or a
// missed release (leak) would show up as a wrong value or a wrong count.
// Coverage of a deferred value staying live across a learned exit/continuation
// boundary — the other half of emitExits' retain-on-reload path — is
// exercised separately by "jits learned br_if continuation over a live ref
// value" in interp_test.go, which already keeps a LOCAL_GET-deferred array
// live across a BR_IF and asserts both the result and stable exit counts.
func TestARM64_DeferredRefElision(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	t.Run("local-backed ref stays live across a loop back-edge", func(t *testing.T) {
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.ARRAY_LEN)
		prog, err := b.Build()
		require.NoError(t, err)

		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(1), got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
	})

	t.Run("sieve-shaped kernel keeps the local-backed array refcount exact", func(t *testing.T) {
		const size = int32(24)
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
		fill := b.Label()
		scan := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(fill)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(scan)
		// arr[i] = 1 — LOCAL_GET 0 pushes the array deferred (backingLocal);
		// ARRAY_SET must elide the container release to match.
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(fill)
		b.Bind(scan)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		loop := b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// sum += arr[i] — the same deferred array feeds ARRAY_GET.
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		var ref int
		for n := 0; n < 32; n++ {
			require.NoError(t, i.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			v, err := i.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, v, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(size), v)

			l, err := i.Local(0)
			require.NoError(t, err)
			ref = l.Ref()
			require.Equal(t, 1, refCountAt(t, i, ref)) // the local slot's own retain, never doubled or dropped
			require.Equal(t, refCounts(threaded), refCounts(i), "refcount diverged from threaded on iteration %d", n)
			i.Reset()
			threaded.Reset()
		}
		require.NoError(t, i.Close())
		require.NoError(t, threaded.Close())
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	})

	t.Run("global-backed variant elides the container release", func(t *testing.T) {
		const size = int32(8)
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Globals(types.TypeI32Array)
		b.Locals(types.TypeI32, types.TypeI32)
		fill := b.Label()
		scan := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.GLOBAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(fill)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(scan)
		// GLOBAL_GET pushes the array deferred (backingGlobal).
		b.Emit(instr.GLOBAL_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(fill)
		b.Bind(scan)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		loop := b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.GLOBAL_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, i.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			v, err := i.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, v, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(2*size), v)

			g, err := i.Global(0)
			require.NoError(t, err)
			require.Equal(t, 1, refCountAt(t, i, g.Ref())) // the global slot's own retain, never doubled or dropped
			require.Equal(t, refCounts(threaded), refCounts(i), "refcount diverged from threaded on iteration %d", n)
			i.Reset()
			threaded.Reset()
		}
		require.NoError(t, i.Close())
		require.NoError(t, threaded.Close())
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	})

	t.Run("upval-backed variant elides the container release", func(t *testing.T) {
		const size = int32(8)
		body := types.NewFunctionBuilder(nil).Captures(types.TypeI32Array).Returns(types.TypeI32)
		body.Locals(types.TypeI32, types.TypeI32)
		fill := body.Label()
		scan := body.Label()
		done := body.Label()
		body.Emit(instr.New(instr.I32_CONST, 0)).Emit(instr.New(instr.LOCAL_SET, 0))
		body.Bind(fill)
		body.Emit(instr.New(instr.LOCAL_GET, 0)).Emit(instr.New(instr.I32_CONST, uint64(uint32(size)))).Emit(instr.New(instr.I32_GE_S)).BrIf(scan)
		// UPVAL_GET pushes the captured array deferred (backingUpval).
		body.Emit(instr.New(instr.UPVAL_GET, 0)).Emit(instr.New(instr.LOCAL_GET, 0)).Emit(instr.New(instr.I32_CONST, 3)).Emit(instr.New(instr.ARRAY_SET))
		body.Emit(instr.New(instr.LOCAL_GET, 0)).Emit(instr.New(instr.I32_CONST, 1)).Emit(instr.New(instr.I32_ADD)).Emit(instr.New(instr.LOCAL_SET, 0))
		body.Br(fill)
		body.Bind(scan)
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

		arrayTyp := 0
		b := program.NewBuilder()
		arrayTyp = b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp))
		b.ConstGet(fn).Emit(instr.CLOSURE_NEW).Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, i.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			v, err := i.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, v, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(3*size), v)
			require.Equal(t, refCounts(threaded), refCounts(i), "refcount diverged from threaded on iteration %d", n)
			i.Reset()
			threaded.Reset()
		}
		require.NoError(t, i.Close())
		require.NoError(t, threaded.Close())
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	})

	t.Run("dup of a deferred ref consumed twice keeps both container releases elided", func(t *testing.T) {
		const size = int32(4)
		use := types.NewFunctionBuilder(nil).
			Params(types.TypeI32Array).
			Returns(types.TypeI32)
		// DUP of a deferred LOCAL_GET must stay deferred. Both ARRAY_LEN
		// consumers box their copies without retain/release churn.
		fn, err := use.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.DUP),
			instr.New(instr.ARRAY_LEN),
			instr.New(instr.SWAP),
			instr.New(instr.ARRAY_LEN),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		arrayType := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(types.TypeI32Array)
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).ConstGet(fn).Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, i.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			v, err := i.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, v, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(2*size), v)

			l, err := i.Local(0)
			require.NoError(t, err)
			wantLocal, err := threaded.Local(0)
			require.NoError(t, err)
			require.Equal(t, refCountAt(t, threaded, wantLocal.Ref()), refCountAt(t, i, l.Ref()))
			require.Equal(t, refCounts(threaded), refCounts(i), "refcount diverged from threaded on iteration %d", n)
			i.Reset()
			threaded.Reset()
		}
		require.NoError(t, i.Close())
		require.NoError(t, threaded.Close())
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	})

	t.Run("backing slot overwrite preserves a live deferred reader", func(t *testing.T) {
		replace := types.NewFunctionBuilder(nil).
			Params(types.TypeI32Array, types.TypeI32Array).
			Returns(types.TypeI32)
		// LOCAL_GET 0 pushes the first array deferred. LOCAL_SET 0 then replaces
		// that backing slot with parameter 1, so detach must own the stale reader
		// before its original backing slot changes.
		fn, err := replace.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 9),
			instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.ARRAY_LEN),
			instr.New(instr.RETURN),
		).Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		arrayType := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(types.TypeI32Array, types.TypeI32Array)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 2).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).ConstGet(fn).Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, i.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			v, err := i.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, v, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(2), v)

			l, err := i.Local(1)
			require.NoError(t, err)
			wantLocal, err := threaded.Local(1)
			require.NoError(t, err)
			require.Equal(t, refCountAt(t, threaded, wantLocal.Ref()), refCountAt(t, i, l.Ref()))
			require.Equal(t, refCounts(threaded), refCounts(i), "refcount diverged from threaded on iteration %d", n)
			i.Reset()
			threaded.Reset()
		}
		require.NoError(t, i.Close())
		require.NoError(t, threaded.Close())
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	})

	// balanced runs prog under the JIT and the pure interpreter in lockstep and
	// asserts, on every iteration, that the popped result and every heap
	// refcount agree with the threaded reference. A missed retain leaves an rc
	// below threaded (and corrupts under -race via premature reuse); a missed
	// release leaves one above threaded. It is path-agnostic: whichever cold path
	// (terminal trap, direct call, module completion, or a threaded fallback)
	// the trace takes, the interpreter's own bookkeeping is the oracle. Heap
	// index 0 is the permanent Null cell whose count never gates a free, so its
	// bookkeeping is excluded.
	requireRefParity := func(t *testing.T, prog *program.Program) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		ref := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 48; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, ref.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := ref.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(ref), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			ref.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, ref.Close())
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	}

	t.Run("terminal fallback preserves a live deferred ref", func(t *testing.T) {
		const size = int32(6)
		// A ref-element ARRAY_SET lowers as an unconditional terminal trap. Put it
		// in a compiled leaf function so the trap fires on every call, with an
		// extra deferred copy of the array live below the store: trap() must
		// retainDeferred that copy's retain before the threaded resume (ARRAY_LEN) reads
		// and then releases it. Without retainDeferred the copy is flushed unretained and
		// the interpreter frees the array one reference early.
		store := types.NewFunctionBuilder(nil).Params(types.NewArrayType(types.TypeAny), types.TypeI32).Returns(types.TypeI32)
		store.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.REF_NULL), instr.New(instr.ARRAY_SET),
			instr.New(instr.ARRAY_LEN),
			instr.New(instr.RETURN),
		)
		fn, err := store.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		refArrTyp := b.Type(types.NewArrayType(types.TypeAny))
		b.Const(fn)
		b.Locals(types.NewArrayType(types.TypeAny), types.TypeI32, types.TypeI32)
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(refArrTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		loop := b.Label()
		done := b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).ConstGet(fn).Emit(instr.CALL) // store(arr, i) -> size
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)
		requireRefParity(t, prog)
	})

	t.Run("ref array store owns a deferred element before transfer", func(t *testing.T) {
		refArray := types.NewArrayType(types.TypeAny)
		store := types.NewFunctionBuilder(nil).
			Params(refArray, types.TypeI32Array).
			Returns(types.TypeI32)
		store.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.ARRAY_SET),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.RETURN),
		)
		fn, err := store.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		refArrayType := b.Type(refArray)
		valueArrayType := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(refArray, types.TypeI32Array)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(refArrayType)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(valueArrayType)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).ConstGet(fn).Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)
		requireRefParity(t, prog)
	})

	t.Run("ref struct store owns a deferred field before transfer", func(t *testing.T) {
		structure := types.NewStructType(types.NewStructField(types.TypeI32Array))
		store := types.NewFunctionBuilder(nil).
			Params(structure, types.TypeI32Array).
			Returns(types.TypeI32)
		store.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.STRUCT_SET),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.RETURN),
		)
		fn, err := store.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		structType := b.Type(structure)
		valueArrayType := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(structure, types.TypeI32Array)
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(structType)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(valueArrayType)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).ConstGet(fn).Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)
		requireRefParity(t, prog)
	})

	t.Run("deferred ref passed as a call argument stays balanced", func(t *testing.T) {
		const size = int32(6)
		sink := types.NewFunctionBuilder(nil).Params(types.TypeI32Array).Returns(types.TypeI32)
		sink.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.ARRAY_LEN), instr.New(instr.RETURN))
		fn, err := sink.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		loop := b.Label()
		done := b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// Pass the array as a deferred (backingLocal) ref argument: the call must
		// own it before handing it to the callee, which releases it on RETURN.
		b.Emit(instr.LOCAL_GET, 0).ConstGet(fn).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2) // acc += sink(arr)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)
		requireRefParity(t, prog)
	})

	t.Run("deferred ref forwarded through a self tail call stays balanced", func(t *testing.T) {
		const size = int32(6)
		// fill(arr, i, self): arr[i] = 7; i < 0 ? 0 : self(arr, i-1, self). Each
		// LOCAL_GET of arr defers, and the tail call commits its frame; the tail
		// dispatch must own every deferred ref before the committing flush (which
		// now rejects any deferred it still sees).
		fill := types.NewFunctionBuilder(nil).Params(types.TypeI32Array, types.TypeI32, types.TypeAny).Returns(types.TypeI32)
		base := fill.Label()
		fill.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LT_S)).
			BrIf(base).
			Emit(
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 7), instr.New(instr.ARRAY_SET),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
				instr.New(instr.LOCAL_GET, 2),
				instr.New(instr.LOCAL_GET, 2),
				instr.New(instr.RETURN_CALL),
			).
			Bind(base).
			Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN))
		fn, err := fill.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(types.TypeI32Array)
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size-1)))
		b.ConstGet(fn).ConstGet(fn).Emit(instr.CALL) // fill(arr, size-1, fill)
		prog, err := b.Build()
		require.NoError(t, err)
		requireRefParity(t, prog)
	})

	t.Run("module completion preserves a live deferred ref", func(t *testing.T) {
		tarr := types.TypedArray[int32]{3, 5, 7}
		b := program.NewBuilder()
		b.Const(tarr)
		// A typed-array constant used as an ARRAY_GET container is a deferred
		// (backingConst) marker. Leave one live on the operand stack at module end:
		// complete() flushes it to the top-level stack the wrapper preserves, so
		// retainDeferred must re-take its retain the way the threaded CONST_GET would.
		b.ConstGet(tarr)
		b.ConstGet(tarr).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_GET).Emit(instr.DROP)
		// [A] is left live on the stack; the module returns it at completion.
		prog, err := b.Build()
		require.NoError(t, err)
		requireRefParity(t, prog)
	})
}

// HoistedContainerLoop protects the loop-invariant container hoisting of
// issue #153: an entryLoop plan whose body is call-free and never writes the
// container local derives the heap cell, shape guard, and slice header once
// per native entry, so accesses keep only the bounds check and element op.
// Every sub-case diffs results and exact refcounts against a threaded twin.
func TestARM64_StaticLoopEntry(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	// A loop header compiled by the static frontend must emit only the blocks
	// its own root reaches, not every block of the function it belongs to (see
	// prune). Two loop headers share one whole-function block list, so an
	// unpruned header emits the whole function again and its entry ends up at
	// least as large as the whole-function entry.
	const size = int32(4096)
	b := program.NewBuilder()
	arrayTyp := b.Type(types.TypeI32Array)
	b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
	fill := b.Label()
	scan := b.Label()
	loop := b.Label()
	done := b.Label()
	b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Bind(fill)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(scan)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(fill)
	b.Bind(scan)
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

	profile := prof.New()
	jit := interp.New(prog, interp.WithProfiler(profile))
	threaded := interp.New(prog, interp.WithThreshold(-1))
	for n := 0; n < 16; n++ {
		require.NoError(t, jit.Run(context.Background()))
		require.NoError(t, threaded.Run(context.Background()))
		got, err := jit.PopBoxed()
		require.NoError(t, err)
		want, err := threaded.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
		require.Equal(t, types.BoxI32(size), got)
		require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
		jit.Reset()
		threaded.Reset()
	}
	require.NoError(t, jit.Close())
	require.NoError(t, threaded.Close())

	var entry, header float64
	var entered bool
	for _, metric := range profile.Metrics() {
		kind, frontend := "", ""
		for _, label := range metric.Labels {
			switch label.Key {
			case "kind":
				kind = label.Value
			case "frontend":
				frontend = label.Value
			}
		}
		if frontend != "static" {
			continue
		}
		switch {
		case metric.Name == "vm_jit_entry_bytes_total" && kind == "start":
			entry = metric.Value
		case metric.Name == "vm_jit_entry_bytes_total" && kind == "loop":
			header = metric.Value
		case metric.Name == "vm_jit_native_entries_total" && kind == "loop":
			entered = metric.Value > 0
		}
	}
	require.NotZero(t, entry, "expected a whole-module static entry")
	require.NotZero(t, header, "expected a static loop-header entry")
	require.True(t, entered, "the static loop entry was never invoked")
	require.Less(t, header, entry, "the loop header emitted blocks its own root cannot reach")
}

func TestARM64_HoistedContainerLoop(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	t.Run("sieve-shaped loops run native without per-access exits", func(t *testing.T) {
		const size = int32(24)
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
		fill := b.Label()
		scan := b.Label()
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(fill)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(scan)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(fill)
		b.Bind(scan)
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

		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(size), got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())

		// Hoisted loops must not pay per-access deopts: the only native exits
		// are the loops' own cold branches. ARRAY_NEW_DEFAULT's bridge (see
		// bridgeable in internal/jit) combined with ARRAY_GET's
		// declared-array-type resolution (see arrayKind) now let the static
		// planner cover this whole module - fill loop, scan loop, and all -
		// as one flat entry with no exits at all, which the compiler tries
		// before the trace frontend's loop-anchored hoist path; either
		// frontend winning satisfies "no per-access exits".
		var entries float64
		var sawBytes bool
		for _, metric := range profile.Metrics() {
			switch metric.Name {
			case "vm_jit_native_entries_total":
				entries += metric.Value
			case "vm_jit_entry_bytes_total":
				sawBytes = true
				require.Less(t, metric.Value, float64(16<<10), "loop body was duplicated instead of using a back-edge")
			case "vm_jit_native_exits_total":
				for _, label := range metric.Labels {
					if label.Key == "reason" {
						require.Equal(t, "loop-exit", label.Value)
					}
				}
			}
		}
		require.Greater(t, entries, float64(0))
		require.True(t, sawBytes, "expected a native entry byte metric")
	})

	t.Run("the prologue shape guard deopts to the header", func(t *testing.T) {
		// The container local alternates between an i32 array and null across
		// loop entries: entries with the array run native, entries with null
		// fail the prologue tag guard and fall back to threaded execution
		// with an empty operand stack and untouched refcounts.
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32, types.TypeI32)
		outer := b.Label()
		odd := b.Label()
		enter := b.Label()
		inner := b.Label()
		next := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(outer)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(odd)
		b.Emit(instr.I32_CONST, 8).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 8).Emit(instr.LOCAL_SET, 3)
		b.Br(enter)
		b.Bind(odd)
		b.Emit(instr.REF_NULL).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(enter)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_GE_S).BrIf(next)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(inner)
		b.Bind(next)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(outer)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())

		// The null-container iterations must leave native execution through a
		// guard rather than running the access. Which guard owns that exit
		// depends on the frontend that compiled the entry: the trace loop
		// plan's hoist prologue guards the container shape once per entry,
		// while a static whole-function plan guards the value where it is
		// used. Both are correct; the contract asserted here is that a null
		// container always deopts, never executes natively.
		var guards float64
		for _, metric := range profile.Metrics() {
			if metric.Name != "vm_jit_native_exits_total" {
				continue
			}
			for _, label := range metric.Labels {
				if label.Key == "reason" && strings.HasPrefix(label.Value, "guard-") {
					guards += metric.Value
				}
			}
		}
		require.Greater(t, guards, float64(0), "null entries must deopt through a guard")
	})

	t.Run("a bounds deopt inside the loop matches threaded", func(t *testing.T) {
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 8).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 12).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			gotErr := jit.Run(context.Background())
			wantErr := threaded.Run(context.Background())
			require.Error(t, wantErr)
			require.Error(t, gotErr)
			require.Equal(t, wantErr.Error(), gotErr.Error(), "error diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
	})

	t.Run("a write to the container local keeps the slow path exact", func(t *testing.T) {
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32Array, types.TypeI32Array, types.TypeI32)
		loop := b.Label()
		odd := b.Label()
		write := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 8).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 8).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(odd)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_SET, 0)
		b.Br(write)
		b.Bind(odd)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_SET, 0)
		b.Bind(write)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 3)
		prog, err := b.Build()
		require.NoError(t, err)

		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
	})

	t.Run("a second container shares the loop via the slow path", func(t *testing.T) {
		const size = int32(8)
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32Array, types.TypeI32, types.TypeI32)
		fill := b.Label()
		scan := b.Label()
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(fill)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(scan)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 2).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(fill)
		b.Bind(scan)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 3)
		prog, err := b.Build()
		require.NoError(t, err)

		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(3*size), got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
	})
}

// StructSetLoop protects the continuable scalar STRUCT_SET path: a loop whose
// body stores a scalar field keeps executing natively past the store instead
// of deopting at a terminal boundary every iteration. Every sub-case diffs
// results and exact refcounts against a threaded twin.
func TestARM64_StructSetLoop(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	runParity := func(t *testing.T, prog *program.Program, want types.Boxed) *prof.Profiler {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		closed := false
		t.Cleanup(func() {
			if !closed {
				require.NoError(t, jit.Close())
				require.NoError(t, threaded.Close())
			}
		})
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			ref, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, ref, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, want, got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
		closed = true
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
		return profile
	}

	storeTests := []struct {
		name  string
		typ   *types.StructType
		field uint64
		steps []instr.Instruction
		want  types.Boxed
	}{
		{
			name:  "i32 field store loop stays native",
			typ:   types.NewStructType(types.NewStructField(types.TypeI32)),
			steps: []instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD)},
			want:  types.BoxI32(24),
		},
		{
			name:  "i64 field store loop",
			typ:   types.NewStructType(types.NewStructField(types.TypeI64)),
			steps: []instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET), instr.New(instr.I64_CONST, 1), instr.New(instr.I64_ADD)},
			want:  types.BoxI64(24),
		},
		{
			name:  "f32 field store loop masks the stored lane",
			typ:   types.NewStructType(types.NewStructField(types.TypeF32)),
			steps: []instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET), instr.New(instr.F32_CONST, uint64(math.Float32bits(1))), instr.New(instr.F32_ADD)},
			want:  types.BoxF32(24),
		},
		{
			name:  "f64 field store loop",
			typ:   types.NewStructType(types.NewStructField(types.TypeF64)),
			steps: []instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET), instr.New(instr.F64_CONST, math.Float64bits(1)), instr.New(instr.F64_ADD)},
			want:  types.BoxF64(24),
		},
		{
			name:  "i1 field store loop",
			typ:   types.NewStructType(types.NewStructField(types.TypeI1)),
			steps: []instr.Instruction{instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_EQZ)},
			want:  types.BoxI1(false),
		},
		{
			name: "heap-backed data past the inline fields",
			typ: types.NewStructType(
				types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI32),
				types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI32),
			),
			field: 4,
			steps: []instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 4), instr.New(instr.STRUCT_GET), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD)},
			want:  types.BoxI32(24),
		},
	}
	for _, tt := range storeTests {
		t.Run(tt.name, func(t *testing.T) {
			const size = int32(24)
			b := program.NewBuilder()
			typ := b.Type(tt.typ)
			b.Locals(tt.typ, types.TypeI32)
			loop := b.Label()
			done := b.Label()
			b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(typ)).Emit(instr.LOCAL_SET, 0)
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, tt.field)
			for _, step := range tt.steps {
				b.Emit(step.Opcode(), step.Operands()...)
			}
			b.Emit(instr.STRUCT_SET)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Br(loop)
			b.Bind(done)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, tt.field).Emit(instr.STRUCT_GET)
			prog, err := b.Build()
			require.NoError(t, err)
			// STRUCT_NEW_DEFAULT now bridges (see bridgeable in
			// internal/jit), so the static planner covers this whole
			// module - loop included - as one flat native entry instead of
			// falling back to a trace-compiled loop anchor with its own
			// cold loop-exit branch; runParity already asserts native
			// entries were emitted.
			runParity(t, prog, tt.want)
		})
	}

	t.Run("owned container from a nested struct get", func(t *testing.T) {
		const size = int32(24)
		inner := types.NewStructType(types.NewStructField(types.TypeI32))
		outer := types.NewStructType(types.NewStructField(inner))
		b := program.NewBuilder()
		innerTyp := b.Type(inner)
		outerTyp := b.Type(outer)
		b.Locals(outer, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(outerTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_NEW_DEFAULT, uint64(innerTyp)).Emit(instr.STRUCT_SET)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// The container operand is the owned (retained) result of STRUCT_GET,
		// so the native store must take the rc guard and release it in place.
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.I32_CONST, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(size))
	})

	t.Run("polymorphic struct types deopt and stay balanced", func(t *testing.T) {
		// The container local alternates between two struct types whose field 0
		// is i32: iterations against the type the trace recorded run native,
		// the other type deopts on the shape or kind guard and falls back to
		// the threaded handler with identical results and refcounts.
		narrow := types.NewStructType(types.NewStructField(types.TypeI32))
		wide := types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI64))
		b := program.NewBuilder()
		narrowTyp := b.Type(narrow)
		wideTyp := b.Type(wide)
		b.Locals(types.TypeAny, types.TypeI32, types.TypeI32, types.TypeI32)
		outer := b.Label()
		odd := b.Label()
		enter := b.Label()
		inner := b.Label()
		next := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(outer)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(odd)
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(narrowTyp)).Emit(instr.LOCAL_SET, 0)
		b.Br(enter)
		b.Bind(odd)
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(wideTyp)).Emit(instr.LOCAL_SET, 0)
		b.Bind(enter)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 6).Emit(instr.I32_GE_S).BrIf(next)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(inner)
		b.Bind(next)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(outer)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 3)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(48))
	})

	t.Run("store after an inlined call stays terminal and balanced", func(t *testing.T) {
		const size = int32(8)
		id := types.NewFunctionBuilder(nil).Params(types.TypeI32).Returns(types.TypeI32)
		id.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
		fn, err := id.Build()
		require.NoError(t, err)

		structTyp := types.NewStructType(types.NewStructField(types.TypeI32))
		b := program.NewBuilder()
		typ := b.Type(structTyp)
		b.Const(fn)
		b.Locals(structTyp, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(typ)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0)
		b.Emit(instr.LOCAL_GET, 1).ConstGet(fn).Emit(instr.CALL)
		b.Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(size-1))
	})
}

// RefEqLoop protects the native boxed-word equality for REF_EQ/REF_NE.
// Every sub-case diffs results and exact refcounts against a threaded twin.
func TestARM64_RefEqLoop(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	runParity := func(t *testing.T, prog *program.Program, want types.Boxed) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		closed := false
		t.Cleanup(func() {
			if !closed {
				require.NoError(t, jit.Close())
				require.NoError(t, threaded.Close())
			}
		})
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			ref, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, ref, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, want, got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
		closed = true
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	}

	// eqLoop counts, over size iterations, how often the two operands the
	// build callback pushes compare equal under op.
	eqLoop := func(setup func(b *program.Builder), operands func(b *program.Builder), op instr.Opcode) *program.Program {
		const size = int32(24)
		b := program.NewBuilder()
		setup(b)
		loop := b.Label()
		hit := b.Label()
		skip := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		operands(b)
		b.Emit(op).BrIf(hit)
		b.Br(skip)
		b.Bind(hit)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Bind(skip)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)
		return prog
	}

	t.Run("deferred ref equality stays native", func(t *testing.T) {
		prog := eqLoop(func(b *program.Builder) {
			arrTyp := b.Type(types.TypeI32Array)
			b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32, types.TypeI32Array)
			b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 0)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_SET, 3)
		}, func(b *program.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 3)
		}, instr.REF_EQ)
		runParity(t, prog, types.BoxI32(24))
	})

	t.Run("deferred ref inequality stays native", func(t *testing.T) {
		prog := eqLoop(func(b *program.Builder) {
			arrTyp := b.Type(types.TypeI32Array)
			b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32, types.TypeI32Array)
			b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 0)
			b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 3)
		}, func(b *program.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 3)
		}, instr.REF_NE)
		runParity(t, prog, types.BoxI32(24))
	})
}

// TerminalMutationLoop protects the abort-to-terminal reclassification of bulk
// mutations: a loop containing ARRAY_FILL or MAP_SET compiles its prefix
// natively and deopts at the boundary instead of rejecting the whole trace.
// Every sub-case diffs results and exact refcounts against a threaded twin.
func TestARM64_TerminalMutationLoop(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	runParity := func(t *testing.T, prog *program.Program, want types.Boxed) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		closed := false
		t.Cleanup(func() {
			if !closed {
				require.NoError(t, jit.Close())
				require.NoError(t, threaded.Close())
			}
		})
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			ref, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, ref, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, want, got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
		closed = true
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	}

	t.Run("array fill loop keeps its prefix native", func(t *testing.T) {
		const size = int32(24)
		b := program.NewBuilder()
		arrTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_FILL)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(size*(size-1)/2+1))
	})

	t.Run("array copy loop keeps its prefix native", func(t *testing.T) {
		const size = int32(24)
		b := program.NewBuilder()
		arrTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32Array, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 7).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW, uint64(arrTyp)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_COPY)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(size*(size-1)/2+7))
	})

	t.Run("array append loop keeps its prefix native", func(t *testing.T) {
		const size = int32(24)
		b := program.NewBuilder()
		arrTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_APPEND).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_LEN).Emit(instr.I32_ADD)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(size*(size-1)/2+size))
	})

	t.Run("map set loop keeps its prefix native", func(t *testing.T) {
		const size = int32(24)
		b := program.NewBuilder()
		mapTyp := b.Type(types.NewMapType(types.TypeI32, types.TypeI32))
		b.Locals(types.TypeAny, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 4).Emit(instr.MAP_NEW_DEFAULT, uint64(mapTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 5).Emit(instr.MAP_GET).Emit(instr.I32_ADD)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(size*(size-1)/2+5))
	})
}

// RefContainerStore covers a ref-kind ARRAY_SET/STRUCT_SET whose native
// continuation is nested inside a native self-recursive call. Both stores used
// to exit to the interpreter unconditionally on a ref element or field, because
// letting them continue drove refcounts negative against a threaded twin from
// recursion depth two upward. That was never a defect in either store: the
// cause was selfCall and directCall handing a callee a frame whose
// non-parameter locals were never cleared (see zeroLocals), so the callee's
// first LOCAL_SET released a stale boxed word it never owned. Lifting the store
// rule is simply what first admitted a function holding a ref local into native
// lowering, which is why the two looked connected.
//
// Each sub-case allocates a container, stores a recursive call's result into it
// so the store is never the block's last step, and returns the container.
// Depths zero and one passed even with the defect present, so the depths that
// diverged are the ones that matter here.
func TestARM64_RefContainerStore(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	depths := []int32{0, 1, 2, 3, 5, 8}

	t.Run("ref-element ARRAY_SET nested in self-recursion", func(t *testing.T) {
		// build() is self-recursive (it CONST_GETs and CALLs its own constant
		// index), so its entry compiles through the static frontend before any
		// trace could exist. arr[0]'s ARRAY_SET is immediately followed by
		// arr[1]'s ARRAY_SET in the same block, so neither is the block's last
		// step.
		build := func(t *testing.T, depth int32) *program.Program {
			t.Helper()
			arrTyp := types.NewArrayType(types.TypeAny)
			buildBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny}}).
				Params(types.TypeI32).
				Locals(types.TypeAny)
			buildDone := buildBuilder.Label()
			buildFn := buildBuilder.
				Emit(
					instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
					instr.New(instr.LOCAL_SET, 1),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S),
				).
				BrIf(buildDone).
				Emit(
					// arr[0] = build(d-1)
					instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
					instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
					instr.New(instr.ARRAY_SET),
					// arr[1] = build(d-1)
					instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
					instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
					instr.New(instr.ARRAY_SET),
				).
				Bind(buildDone).
				Emit(
					instr.New(instr.LOCAL_GET, 1),
					instr.New(instr.RETURN),
				).
				MustBuild()

			checkBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Params(types.TypeAny)
			nullCase := checkBuilder.Label()
			checkFn := checkBuilder.
				Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_IS_NULL)).
				BrIf(nullCase).
				Emit(
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_CAST, 0),
					instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
					instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_CAST, 0),
					instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_GET),
					instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
					instr.New(instr.I32_ADD), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
					instr.New(instr.RETURN),
				).
				Bind(nullCase).
				Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
				MustBuild()

			return program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, uint64(uint32(depth))),
					instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
					instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
				},
				program.WithConstants(buildFn, checkFn),
				program.WithTypes(arrTyp),
			)
		}

		for _, depth := range depths {
			prog := build(t, depth)
			want := types.BoxI32(int32(1)<<uint(depth+1) - 1)

			jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
			threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
			for n := 0; n < 8; n++ {
				require.NoError(t, jit.Run(context.Background()))
				require.NoError(t, threaded.Run(context.Background()))
				got, err := jit.PopBoxed()
				require.NoError(t, err)
				ref, err := threaded.PopBoxed()
				require.NoError(t, err)
				require.Equal(t, ref, got, "result diverged from threaded at depth %d iteration %d", depth, n)
				require.Equal(t, want, got, "result diverged from expected node count at depth %d", depth)
				require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded at depth %d iteration %d", depth, n)
				jit.Reset()
				threaded.Reset()
			}
			require.NoError(t, jit.Close())
			require.NoError(t, threaded.Close())
		}
	})

	t.Run("ref-field STRUCT_SET nested in self-recursion", func(t *testing.T) {
		// build() is self-recursive (it CONST_GETs and CALLs its own constant
		// index), so its entry compiles through the static frontend before any
		// trace could exist. n.left's STRUCT_SET is immediately followed by
		// n.right's STRUCT_SET in the same block, so neither is the block's
		// last step.
		build := func(t *testing.T, depth int32) *program.Program {
			t.Helper()
			nodeType := types.NewStructType(
				types.NewStructField(types.TypeI32, types.FieldWithName("value")),
				types.NewStructField(types.TypeAny, types.FieldWithName("left")),
				types.NewStructField(types.TypeAny, types.FieldWithName("right")),
			)
			buildBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny}}).
				Params(types.TypeI32).
				Locals(types.TypeAny)
			buildDone := buildBuilder.Label()
			buildFn := buildBuilder.
				Emit(
					instr.New(instr.STRUCT_NEW_DEFAULT, 0), instr.New(instr.LOCAL_SET, 1),
					instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.STRUCT_SET),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S),
				).
				BrIf(buildDone).
				Emit(
					// n.left = build(d-1)
					instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
					instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
					instr.New(instr.STRUCT_SET),
					// n.right = build(d-1)
					instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 2),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
					instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
					instr.New(instr.STRUCT_SET),
				).
				Bind(buildDone).
				Emit(
					instr.New(instr.LOCAL_GET, 1),
					instr.New(instr.RETURN),
				).
				MustBuild()

			checkBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Params(types.TypeAny)
			nullCase := checkBuilder.Label()
			checkFn := checkBuilder.
				Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_IS_NULL)).
				BrIf(nullCase).
				Emit(
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_CAST, 0),
					instr.New(instr.I32_CONST, 1), instr.New(instr.STRUCT_GET),
					instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.STRUCT_GET),
					instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
					instr.New(instr.I32_ADD), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
					instr.New(instr.RETURN),
				).
				Bind(nullCase).
				Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
				MustBuild()

			return program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, uint64(uint32(depth))),
					instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
					instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
				},
				program.WithConstants(buildFn, checkFn),
				program.WithTypes(nodeType),
			)
		}

		for _, depth := range depths {
			prog := build(t, depth)
			want := types.BoxI32(int32(1)<<uint(depth+1) - 1)

			jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
			threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
			for n := 0; n < 8; n++ {
				require.NoError(t, jit.Run(context.Background()))
				require.NoError(t, threaded.Run(context.Background()))
				got, err := jit.PopBoxed()
				require.NoError(t, err)
				ref, err := threaded.PopBoxed()
				require.NoError(t, err)
				require.Equal(t, ref, got, "result diverged from threaded at depth %d iteration %d", depth, n)
				require.Equal(t, want, got, "result diverged from expected node count at depth %d", depth)
				require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded at depth %d iteration %d", depth, n)
				jit.Reset()
				threaded.Reset()
			}
			require.NoError(t, jit.Close())
			require.NoError(t, threaded.Close())
		}
	})
}

// StructGetStaticPlan protects the static frontend's STRUCT_GET support: a
// function whose struct-typed parameter is read with a constant field index
// compiles through the static planner without trace warmup, for scalar and
// ref fields alike, and matches the threaded twin including refcounts.
func TestARM64_StructGetStaticPlan(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	runStatic := func(t *testing.T, prog *program.Program, want types.Boxed) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		closed := false
		t.Cleanup(func() {
			if !closed {
				require.NoError(t, jit.Close())
				require.NoError(t, threaded.Close())
			}
		})
		for n := 0; n < 8; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			ref, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, ref, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, want, got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
		closed = true
		static := false
		for _, metric := range profile.Metrics() {
			if metric.Name != "vm_jit_compiles_total" {
				continue
			}
			frontend, outcome := "", ""
			for _, label := range metric.Labels {
				switch label.Key {
				case "frontend":
					frontend = label.Value
				case "outcome":
					outcome = label.Value
				}
			}
			if frontend == "static" && outcome == "emitted" && metric.Value > 0 {
				static = true
			}
		}
		require.True(t, static, "expected a static-frontend compile")
	}

	t.Run("scalar field", func(t *testing.T) {
		structTyp := types.NewStructType(types.NewStructField(types.TypeI32))
		get := types.NewFunctionBuilder(nil).Params(structTyp).Returns(types.TypeI32)
		get.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.STRUCT_GET),
			instr.New(instr.RETURN),
		)
		fn, err := get.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		typ := b.Type(structTyp)
		b.Const(fn)
		b.Locals(structTyp)
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(typ)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 9).Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 0).ConstGet(fn).Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)
		runStatic(t, prog, types.BoxI32(9))
	})

	t.Run("ref field retains its result", func(t *testing.T) {
		structTyp := types.NewStructType(types.NewStructField(types.TypeI32Array))
		get := types.NewFunctionBuilder(nil).Params(structTyp).Returns(types.TypeI32Array)
		get.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.STRUCT_GET),
			instr.New(instr.RETURN),
		)
		fn, err := get.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		typ := b.Type(structTyp)
		arrTyp := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(structTyp)
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(typ)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 6).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 0).ConstGet(fn).Emit(instr.CALL).Emit(instr.ARRAY_LEN)
		prog, err := b.Build()
		require.NoError(t, err)
		runStatic(t, prog, types.BoxI32(6))
	})
}

// TestARM64_BridgedOpcodes protects the generalized bridge mechanism
// (bridgeable in internal/jit): every opcode the ARM64 backend cannot
// lower now ends its block as a bridge instead of rejecting the whole
// function, so the static planner can compile object-shaped code that used
// to stay fully threaded. Each subtest exercises one bridgeable opcode
// family inside a hot loop and diffs the JIT run against a threaded twin
// across repeated Run+Reset cycles, on both result and exact refcount.
func TestARM64_BridgedOpcodes(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	runParity := func(t *testing.T, prog *program.Program) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		closed := false
		t.Cleanup(func() {
			if !closed {
				require.NoError(t, jit.Close())
				require.NoError(t, threaded.Close())
			}
		})
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, gotErr := jit.PopBoxed()
			want, wantErr := threaded.PopBoxed()
			require.NoError(t, gotErr)
			require.NoError(t, wantErr)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		// Close flushes each interpreter's private sample collector into the
		// shared profiler (see Interpreter.Close), so entries must be read
		// only after both are closed.
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
		closed = true
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0), "expected native code to be installed")
	}

	runParityErr := func(t *testing.T, prog *program.Program) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		closed := false
		t.Cleanup(func() {
			if !closed {
				require.NoError(t, jit.Close())
				require.NoError(t, threaded.Close())
			}
		})
		for n := 0; n < 32; n++ {
			gotErr := jit.Run(context.Background())
			wantErr := threaded.Run(context.Background())
			require.Error(t, wantErr)
			require.Error(t, gotErr)
			require.Equal(t, wantErr.Error(), gotErr.Error(), "error diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
		closed = true
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0), "expected native code to be installed")
	}

	t.Run("allocation family: struct.new, array.new, closure.new, and ref.new", func(t *testing.T) {
		const size = int32(16)
		structTyp := types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI32))
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Captures(types.TypeI32).
			Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.RETURN)).
			MustBuild()

		b := program.NewBuilder()
		arrTyp := b.Type(types.TypeI32Array)
		structIdx := b.Type(structTyp)
		fnIdx := b.Const(fn)
		b.Locals(types.TypeI32Array, structTyp, types.TypeAny, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 4)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// arr = array.new([i, i+1], count=2)
		b.Emit(instr.LOCAL_GET, 3)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.I32_CONST, 2)
		b.Emit(instr.ARRAY_NEW, uint64(arrTyp))
		b.Emit(instr.LOCAL_SET, 0)
		// s = struct.new(field0=i, field1=i+1)
		b.Emit(instr.LOCAL_GET, 3)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.STRUCT_NEW, uint64(structIdx))
		b.Emit(instr.LOCAL_SET, 1)
		// c = closure.new(capture=i, fn) - created and released every iteration
		b.Emit(instr.LOCAL_GET, 3)
		b.Emit(instr.CONST_GET, uint64(fnIdx))
		b.Emit(instr.CLOSURE_NEW)
		b.Emit(instr.LOCAL_SET, 2)
		// ref.new(i), then drop it - exercises create-then-release
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.REF_NEW).Emit(instr.DROP)
		// sum += struct.get(s, 0). arr is deliberately left unread: both
		// ARRAY_GET and ARRAY_LEN only lower natively for a known constant
		// container (see arrayKind in internal/jit and arrayLen in
		// internal/jit/arm64, which reads the trace-only op.shape a static
		// plan never populates), so arr's local-backed value is exercised
		// only through create-and-release across LOCAL_SET each iteration.
		b.Emit(instr.LOCAL_GET, 4)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 4)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("allocation family: ref.test and ref.cast against a declared struct type", func(t *testing.T) {
		const size = int32(16)
		structTyp := types.NewStructType(types.NewStructField(types.TypeI32))

		b := program.NewBuilder()
		typIdx := b.Type(structTyp)
		b.Locals(types.TypeAny, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(typIdx)).Emit(instr.LOCAL_SET, 0)
		// ref.test[structTyp](s) is always true here; drop it, only the bridge
		// and its release matter.
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.REF_TEST, uint64(typIdx)).Emit(instr.DROP)
		// ref.cast[structTyp](s) always succeeds against its own declared
		// type; the field-0 read afterward proves the cast value is intact.
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.REF_CAST, uint64(typIdx))
		b.Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("map family: map.new, map.set, map.len, map.delete, and map.clear", func(t *testing.T) {
		const size = int32(16)
		mapTyp := types.NewMapType(types.TypeI32, types.TypeI32)

		b := program.NewBuilder()
		typIdx := b.Type(mapTyp)
		b.Locals(types.TypeAny, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// m = map.new({i: i*2}, count=1)
		b.Emit(instr.LOCAL_GET, 1)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 2).Emit(instr.I32_MUL)
		b.Emit(instr.I32_CONST, 1)
		b.Emit(instr.MAP_NEW, uint64(typIdx))
		b.Emit(instr.LOCAL_SET, 0)
		// map.set(m, key=i+100, value=i+1)
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 100).Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.MAP_SET)
		// sum += map.len(m)
		b.Emit(instr.LOCAL_GET, 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_LEN)
		b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		// map.delete(m, key=i+100)
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 100).Emit(instr.I32_ADD)
		b.Emit(instr.MAP_DELETE)
		// map.clear(m)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_CLEAR)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("string family: string.new_utf32, string.encode_utf32, and string.iter", func(t *testing.T) {
		const size = int32(16)
		b := program.NewBuilder()
		charTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeAny, types.TypeI32Array, types.TypeAny, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 5)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// arr = new i32[1]; arr[0] = 65+i
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(charTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 65).Emit(instr.I32_ADD)
		b.Emit(instr.ARRAY_SET)
		// str = string.new_utf32(arr)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.STRING_NEW_UTF32).Emit(instr.LOCAL_SET, 1)
		// codepoints = string.encode_utf32(str)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_ENCODE_UTF32).Emit(instr.LOCAL_SET, 2)
		// iter = string.iter(str) - created and released every iteration
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_ITER).Emit(instr.LOCAL_SET, 3)
		// sum += string.len(str). codepoints is deliberately left unread:
		// ARRAY_LEN and ARRAY_GET only lower natively for a known constant
		// container (see arrayKind in internal/jit and arrayLen in
		// internal/jit/arm64), so its local-backed value is exercised only
		// through create-and-release across LOCAL_SET each iteration.
		// string.len needs no such container-shape hint: it guards against
		// the fixed string itab directly (see stringLen in
		// internal/jit/arm64).
		b.Emit(instr.LOCAL_GET, 5)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_LEN)
		b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 5)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 5)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("bulk array family: array.fill, array.append, array.copy, and array.slice", func(t *testing.T) {
		const size = int32(16)
		b := program.NewBuilder()
		arrTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32Array, types.TypeI32Array, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 4)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// arr = new i32[4]; arr.fill(offset=0, value=i, count=4)
		b.Emit(instr.I32_CONST, 4).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 4)
		b.Emit(instr.ARRAY_FILL)
		// arr.append([i+1], count=1); ARRAY_APPEND leaves the array ref on the
		// stack for chaining (see arrayAppend in
		// internal/cmd/geninterp/lower.go), so drop it here since arr is
		// already reachable through local 0.
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.I32_CONST, 1)
		b.Emit(instr.ARRAY_APPEND)
		b.Emit(instr.DROP)
		// dst = new i32[5]; array.copy(dst, 0, arr, 0, 4)
		b.Emit(instr.I32_CONST, 5).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0)
		b.Emit(instr.I32_CONST, 4)
		b.Emit(instr.ARRAY_COPY)
		// slice = arr[0:2]
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 2).Emit(instr.ARRAY_SLICE).Emit(instr.LOCAL_SET, 2)
		// sum += i. dst and slice are deliberately left unread: ARRAY_GET
		// and ARRAY_LEN only lower natively for a known constant container
		// (see arrayKind in internal/jit and arrayLen in
		// internal/jit/arm64), so their local-backed values are exercised
		// only through create-and-release across LOCAL_SET each iteration.
		b.Emit(instr.LOCAL_GET, 4)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 4)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("bulk array family: array.delete on a known constant container", func(t *testing.T) {
		// array.delete only resolves its removed-element kind statically for
		// a known constant container (see arrayKind in
		// internal/jit), the same restriction ARRAY_GET has always
		// had; this exercises the bridge with that supported shape.
		//
		// array.delete shrinks and shifts its container in place, and the
		// jit and threaded interpreters below are both built from the same
		// prog, whose constant array a fresh *Interpreter does not deep-copy
		// per instance: every element is the same value so the result stays
		// independent of which interpreter's prior runs already shifted the
		// shared backing array, and the array is sized well beyond every
		// run's total deletions so it never underflows across Reset cycles.
		const size = int32(8)
		values := make(types.TypedArray[int32], size*256)
		for i := range values {
			values[i] = 7
		}
		b := program.NewBuilder()
		arr := b.Const(values)
		b.Locals(types.TypeI32, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, uint64(arr)).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_DELETE).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("error family: error.new and error.code", func(t *testing.T) {
		const size = int32(16)
		b := program.NewBuilder()
		b.Locals(types.TypeAny, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// err = error.new(code=42, payload=i)
		b.Emit(instr.LOCAL_GET, 1)
		b.Emit(instr.I32_CONST, 42)
		b.Emit(instr.ERROR_NEW)
		b.Emit(instr.LOCAL_SET, 0)
		// sum += error.code(err)
		b.Emit(instr.LOCAL_GET, 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.ERROR_CODE)
		b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("error family: throw after a bridged allocation stays uncaught", func(t *testing.T) {
		const size = int32(8)
		b := program.NewBuilder()
		b.Locals(types.TypeI32)
		loop := b.Label()
		throwPoint := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(throwPoint)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(throwPoint)
		// error.new(code=99, payload=count); throw - two adjacent bridgeable
		// opcodes in a row exercise the bridge-of-a-bridge resume path.
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.I32_CONST, 99)
		b.Emit(instr.ERROR_NEW)
		b.Emit(instr.THROW)
		prog, err := b.Build()
		require.NoError(t, err)
		runParityErr(t, prog)
	})
}

// hostLoopFields is the Go struct TestARM64_HostStructLoop reads and writes
// through. It carries an unexported field so the codec picks a live view, one
// exported field per Go kind the lowerer has a row for, a string field it has
// none for, and an int64 field holding more than a box payload fits.
type hostLoopFields struct {
	Flag   bool
	I8     int8
	I16    int16
	I32    int32
	Int    int
	I64    int64
	U8     uint8
	U16    uint16
	U32    uint32
	U64    uint64
	F32    float32
	F64    float64
	Text   string
	Big    int64
	hidden int32
}

func (h *hostLoopFields) Hidden() int32 { return h.hidden }

// hostNarrowField and hostWideField hold one field of the same VM kind in two
// Go widths, which is what makes a lowered read of one wrong for the other.
type hostNarrowField struct {
	V      int16
	hidden int32
}

func (h *hostNarrowField) Hidden() int32 { return h.hidden }

type hostWideField struct {
	V      int32
	hidden int32
}

func (h *hostWideField) Hidden() int32 { return h.hidden }

// TestARM64_HostStructLoop covers STRUCT_GET and STRUCT_SET against a
// *HostStruct, whose fields hold Go memory rather than VM words. Every case
// reads or writes inside a counted loop and hands the loop's own value back, so
// a row that loads the wrong width or extension reports a wrong value rather
// than merely a different speed, and the native entry count separates an access
// that stayed in the loop from one that exited on every iteration.
func TestARM64_HostStructLoop(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	const size = int32(24)
	negI32, negI64 := int32(-99), int64(-1)<<41
	seed := hostLoopFields{
		Flag: true, I8: -8, I16: -300, I32: -70000, Int: -1 << 40, I64: -1 << 40,
		U8: 200, U16: 60000, U32: 0xFFFF_FFFF, U64: 1 << 40, F32: 1.5, F64: -2.5,
		Text: "text", Big: 1 << 60,
	}

	// run marshals its own copy of seed, so a JIT run and a threaded run each
	// own the Go memory they write and the comparison between them stays
	// honest. body runs once per iteration and tail leaves the result.
	run := func(t *testing.T, locals []types.Type, body, tail []instr.Instruction, opts ...interp.Option) (types.Value, hostLoopFields, float64) {
		t.Helper()
		setup := interp.New(program.New(nil))
		defer func() { require.NoError(t, setup.Close()) }()
		src := seed
		host, err := interp.NewRegistry().Marshal(setup, &src)
		require.NoError(t, err)

		b := program.NewBuilder()
		require.Equal(t, 0, b.Const(host))
		b.Locals(append([]types.Type{types.TypeI32}, locals...)...)
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		for _, step := range body {
			b.Emit(step.Opcode(), step.Operands()...)
		}
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		for _, step := range tail {
			b.Emit(step.Opcode(), step.Operands()...)
		}
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		i := interp.New(prog, append(opts, interp.WithProfiler(profile))...)
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		// Metrics land when the interpreter closes, so the count is read
		// after the run has finished rather than during it.
		require.NoError(t, i.Close())

		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		return got, src, entries
	}

	t.Run("a read of every lowered field kind stays native and agrees with threaded", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			at   uint64
			typ  types.Type
			want types.Value
		}{
			{name: "bool", at: 0, typ: types.TypeI1, want: types.I1(true)},
			{name: "int8", at: 1, typ: types.TypeI8, want: types.I8(-8)},
			{name: "int16", at: 2, typ: types.TypeI32, want: types.I32(-300)},
			{name: "int32", at: 3, typ: types.TypeI32, want: types.I32(-70000)},
			{name: "int", at: 4, typ: types.TypeI64, want: types.I64(-1 << 40)},
			{name: "int64", at: 5, typ: types.TypeI64, want: types.I64(-1 << 40)},
			{name: "uint8", at: 6, typ: types.TypeI32, want: types.I32(200)},
			{name: "uint16", at: 7, typ: types.TypeI32, want: types.I32(60000)},
			// A uint32 field reaches the guest as the signed i32 its
			// conversion casts to, so the load sign-extends the same four
			// bytes rather than widening them.
			{name: "uint32", at: 8, typ: types.TypeI32, want: types.I32(-1)},
			{name: "uint64", at: 9, typ: types.TypeI64, want: types.I64(1 << 40)},
			{name: "float32", at: 10, typ: types.TypeF32, want: types.F32(1.5)},
			{name: "float64", at: 11, typ: types.TypeF64, want: types.F64(-2.5)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				locals := []types.Type{tt.typ}
				body := []instr.Instruction{
					instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, tt.at),
					instr.New(instr.STRUCT_GET), instr.New(instr.LOCAL_SET, 1),
				}
				tail := []instr.Instruction{instr.New(instr.LOCAL_GET, 1)}

				want, _, _ := run(t, locals, body, tail, interp.WithTick(1), interp.WithThreshold(-1))
				require.Equal(t, tt.want, want)

				got, _, entries := run(t, locals, body, tail, interp.WithTick(1), interp.WithThreshold(0))
				require.Equal(t, want, got)
				require.Greater(t, entries, float64(0), "expected a native entry")
				require.Less(t, entries, float64(size), "the read exits the native loop")
			})
		}
	})

	t.Run("a read the interpreter still owns agrees with threaded", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			at   uint64
			typ  types.Type
			want types.Value
		}{
			// A string field publishes a heap reference rather than loading
			// a word, so it has no row at all.
			{name: "no row for the field kind", at: 12, typ: types.TypeString, want: types.String("text")},
			// An i64 past the box payload cannot stay raw, so the read
			// leaves the loop where the interpreter spills it to the heap.
			{name: "a value past the box payload", at: 13, typ: types.TypeI64, want: types.I64(1 << 60)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				locals := []types.Type{tt.typ}
				body := []instr.Instruction{
					instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, tt.at),
					instr.New(instr.STRUCT_GET), instr.New(instr.LOCAL_SET, 1),
				}
				tail := []instr.Instruction{instr.New(instr.LOCAL_GET, 1)}

				want, _, _ := run(t, locals, body, tail, interp.WithTick(1), interp.WithThreshold(-1))
				require.Equal(t, tt.want, want)
				got, _, _ := run(t, locals, body, tail, interp.WithTick(1), interp.WithThreshold(0))
				require.Equal(t, want, got)
			})
		}
	})

	t.Run("a write of every exactly imaged field kind reaches the Go value", func(t *testing.T) {
		for _, tt := range []struct {
			name  string
			at    uint64
			store []instr.Instruction
			want  types.Value
			check func(*testing.T, hostLoopFields)
		}{
			{
				name: "bool", at: 0,
				store: []instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_EQZ)},
				want:  types.I1(false),
				check: func(t *testing.T, got hostLoopFields) { require.False(t, got.Flag) },
			},
			{
				// No opcode makes an i8 constant, so the value written is the
				// one a read of the same field produced.
				name: "int8", at: 1,
				store: []instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.STRUCT_GET)},
				want:  types.I8(-8),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, int8(-8), got.I8) },
			},
			{
				name: "int32", at: 3,
				store: []instr.Instruction{instr.New(instr.I32_CONST, uint64(uint32(negI32)))},
				want:  types.I32(negI32),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, negI32, got.I32) },
			},
			{
				name: "int64", at: 5,
				store: []instr.Instruction{instr.New(instr.I64_CONST, uint64(negI64))},
				want:  types.I64(negI64),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, negI64, got.I64) },
			},
			{
				// A uint32 field is as wide as its slot, so the store writes
				// the same four bytes the conversion would reinterpret.
				name: "uint32", at: 8,
				store: []instr.Instruction{instr.New(instr.I32_CONST, uint64(uint32(negI32)))},
				want:  types.I32(negI32),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, uint32(0xFFFF_FF9D), got.U32) },
			},
			{
				name: "uint64", at: 9,
				store: []instr.Instruction{instr.New(instr.I64_CONST, uint64(negI64))},
				want:  types.I64(negI64),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, uint64(0xFFFF_FE00_0000_0000), got.U64) },
			},
			{
				name: "float32", at: 10,
				store: []instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(-3.5)))},
				want:  types.F32(-3.5),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, float32(-3.5), got.F32) },
			},
			{
				name: "float64", at: 11,
				store: []instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(4.25))},
				want:  types.F64(4.25),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, float64(4.25), got.F64) },
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				body := append([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, tt.at)}, tt.store...)
				body = append(body, instr.New(instr.STRUCT_SET))
				tail := []instr.Instruction{
					instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, tt.at), instr.New(instr.STRUCT_GET),
				}

				want, threaded, _ := run(t, nil, body, tail, interp.WithTick(1), interp.WithThreshold(-1))
				require.Equal(t, tt.want, want)
				tt.check(t, threaded)

				got, jit, entries := run(t, nil, body, tail, interp.WithTick(1), interp.WithThreshold(0))
				require.Equal(t, want, got)
				require.Equal(t, threaded, jit, "the Go value diverged from the threaded run")
				require.Greater(t, entries, float64(0), "expected a native entry")
				require.Less(t, entries, float64(size), "the write exits the native loop")
			})
		}
	})

	t.Run("a write a range check governs agrees with threaded", func(t *testing.T) {
		// An int16 field is narrower than the i32 slot the guest writes, so
		// the store can overflow and the interpreter is the one that says so.
		for _, threshold := range []int{-1, 0} {
			setup := interp.New(program.New(nil))
			src := seed
			host, err := interp.NewRegistry().Marshal(setup, &src)
			require.NoError(t, err)
			i := interp.New(program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 70000), instr.New(instr.STRUCT_SET),
			}, program.WithConstants(host)), interp.WithTick(1), interp.WithThreshold(threshold))
			require.ErrorIs(t, i.Run(context.Background()), interp.ErrValueOverflow)
			require.Equal(t, int16(-300), src.I16, "the rejected write left the Go field alone")
			require.NoError(t, i.Close())
			require.NoError(t, setup.Close())
		}
	})

	t.Run("a field of another Go width at the same read exits", func(t *testing.T) {
		// A Go int16 and a Go int32 field both reach the guest as i32, so the
		// same compiled read serves both once the container arrives from the
		// stack. Only the kind guard keeps the second run from loading two
		// bytes of a four-byte field.
		b := program.NewBuilder()
		b.Locals(types.TypeAny, types.TypeI32, types.TypeI32)
		loop, done := b.Label(), b.Label()
		b.Emit(instr.LOCAL_SET, 0).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)

		read := func(threshold int) (types.Value, types.Value, float64) {
			profile := prof.New()
			i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(threshold), interp.WithProfiler(profile))
			pass := func(value any) types.Value {
				host, err := i.Marshal(value)
				require.NoError(t, err)
				require.NoError(t, i.Push(host))
				require.NoError(t, i.Run(context.Background()))
				got, err := i.Pop()
				require.NoError(t, err)
				i.Reset()
				return got
			}
			narrow, wide := pass(&hostNarrowField{V: -300}), pass(&hostWideField{V: -70000})
			require.NoError(t, i.Close())
			var entries float64
			for _, metric := range profile.Metrics() {
				if metric.Name == "vm_jit_native_entries_total" {
					entries += metric.Value
				}
			}
			return narrow, wide, entries
		}

		narrow, wide, _ := read(-1)
		require.Equal(t, types.I32(-300), narrow)
		require.Equal(t, types.I32(-70000), wide)

		gotNarrow, gotWide, entries := read(0)
		require.Equal(t, narrow, gotNarrow)
		require.Equal(t, wide, gotWide)
		require.Greater(t, entries, float64(0), "expected a native entry")
	})

	t.Run("a write to a field a range check narrows agrees with threaded", func(t *testing.T) {
		// A uint8 field is narrower than the i32 slot the guest writes, so a
		// store past 255 has to report an overflow rather than truncate. The
		// loop writes in range long enough to compile before it does not.
		const wide = int32(64)
		b := program.NewBuilder()
		b.Locals(types.TypeAny, types.TypeI32)
		loop, done := b.Label(), b.Label()
		b.Emit(instr.LOCAL_SET, 0).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(wide))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 6)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 200).Emit(instr.I32_ADD)
		b.Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		for _, threshold := range []int{-1, 0} {
			i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(threshold))
			src := seed
			host, err := i.Marshal(&src)
			require.NoError(t, err)
			require.NoError(t, i.Push(host))
			require.ErrorIs(t, i.Run(context.Background()), interp.ErrValueOverflow)
			// 200+55 is the last value a uint8 holds, so the field stops
			// there instead of wrapping to what a raw byte store would leave.
			require.Equal(t, uint8(255), src.U8)
			require.NoError(t, i.Close())
		}
	})

	t.Run("a write a range check governs agrees with threaded", func(t *testing.T) {
		// An int16 field is narrower than the i32 slot the guest writes, so
		// the store can overflow and the interpreter is the one that says so.
		for _, threshold := range []int{-1, 0} {
			setup := interp.New(program.New(nil))
			src := seed
			host, err := interp.NewRegistry().Marshal(setup, &src)
			require.NoError(t, err)
			i := interp.New(program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 70000), instr.New(instr.STRUCT_SET),
			}, program.WithConstants(host)), interp.WithTick(1), interp.WithThreshold(threshold))
			require.ErrorIs(t, i.Run(context.Background()), interp.ErrValueOverflow)
			require.Equal(t, int16(-300), src.I16, "the rejected write left the Go field alone")
			require.NoError(t, i.Close())
			require.NoError(t, setup.Close())
		}
	})

	t.Run("a field of another Go width at the same read exits", func(t *testing.T) {
		// A Go int16 and a Go int32 field both reach the guest as i32, so one
		// STRUCT_GET alternating between them keeps the same VM kind while
		// changing the load width. Only the kind guard separates them.
		build := func() *program.Program {
			setup := interp.New(program.New(nil))
			defer func() { require.NoError(t, setup.Close()) }()
			registry := interp.NewRegistry()
			narrow, err := registry.Marshal(setup, &hostNarrowField{V: -300})
			require.NoError(t, err)
			wide, err := registry.Marshal(setup, &hostWideField{V: -70000})
			require.NoError(t, err)

			b := program.NewBuilder()
			require.Equal(t, 0, b.Const(narrow))
			require.Equal(t, 1, b.Const(wide))
			b.Locals(types.TypeI32, types.TypeAny, types.TypeI32)
			loop, odd, read, done := b.Label(), b.Label(), b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(odd)
			b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 1).Br(read)
			b.Bind(odd)
			b.Emit(instr.CONST_GET, 1).Emit(instr.LOCAL_SET, 1)
			b.Bind(read)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.LOCAL_SET, 2)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
			b.Br(loop)
			b.Bind(done)
			b.Emit(instr.LOCAL_GET, 2)
			prog, err := b.Build()
			require.NoError(t, err)
			return prog
		}

		read := func(threshold int) (types.Value, float64) {
			profile := prof.New()
			i := interp.New(build(), interp.WithTick(1), interp.WithThreshold(threshold), interp.WithProfiler(profile))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.NoError(t, i.Close())
			var entries float64
			for _, metric := range profile.Metrics() {
				if metric.Name == "vm_jit_native_entries_total" {
					entries += metric.Value
				}
			}
			return got, entries
		}
		want, _ := read(-1)
		require.Equal(t, types.I32(-70000), want)
		got, entries := read(0)
		require.Equal(t, want, got)
		require.Greater(t, entries, float64(0), "expected a native entry")
	})

	t.Run("an index past the layout faults the same way", func(t *testing.T) {
		for _, threshold := range []int{-1, 0} {
			setup := interp.New(program.New(nil))
			src := seed
			host, err := interp.NewRegistry().Marshal(setup, &src)
			require.NoError(t, err)
			i := interp.New(program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 99), instr.New(instr.STRUCT_GET),
			}, program.WithConstants(host)), interp.WithTick(1), interp.WithThreshold(threshold))
			require.ErrorIs(t, i.Run(context.Background()), interp.ErrSegmentationFault)
			require.NoError(t, i.Close())
			require.NoError(t, setup.Close())
		}
	})
}

func TestInterpreter_Marshal(t *testing.T) {
	// Marshal forwards to the installed codec, so the conversion contract is
	// owned by TestRegistry_Marshal and only the delegation is checked here.
	i := interp.New(program.New(nil), interp.WithCodec(upperCodec(0)))
	defer i.Close()

	got, err := i.Marshal("go")
	require.NoError(t, err)
	require.Equal(t, types.String("GO"), got)
}

func TestInterpreter_Unmarshal(t *testing.T) {
	i := interp.New(program.New(nil), interp.WithCodec(upperCodec(0)))
	defer i.Close()

	var dst string
	require.NoError(t, i.Unmarshal(types.String("GO"), &dst))
	require.Equal(t, "go", dst)
}

func TestInterpreter_Context(t *testing.T) {
	var got context.Context
	prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
	i := interp.New(prog, interp.WithTick(1), interp.WithHook(func(i *interp.Interpreter) error {
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
	i := interp.New(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
	require.Equal(t, 0, i.Func())
}

func TestInterpreter_IP(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.NOP)})
	i := interp.New(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
	require.Equal(t, 6, i.IP())
}

func TestInterpreter_FP(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.NOP)})
	i := interp.New(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
	require.Equal(t, 1, i.FP())
}

func TestInterpreter_Opcode(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.NOP)})
	i := interp.New(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
	op, err := i.Opcode()
	require.NoError(t, err)
	require.Equal(t, instr.NOP, op)
}

func TestInterpreter_Frame(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.NOP)})
	i := interp.New(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
	fn, ip, bp, err := i.Frame(0)
	require.NoError(t, err)
	require.Equal(t, 0, fn)
	require.Equal(t, 6, ip)
	require.Equal(t, 0, bp)
}

func TestInterpreter_Const(t *testing.T) {
	i := interp.New(program.New(nil, program.WithConstants(types.I32(9))))
	defer i.Close()

	v, err := i.Const(0)
	require.NoError(t, err)
	require.Equal(t, types.BoxI32(9), v)
}

func TestInterpreter_Global(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 4), instr.New(instr.GLOBAL_SET, 0)}, program.WithGlobals(types.TypeI32))
	i := interp.New(prog)
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	v, err := i.Global(0)
	require.NoError(t, err)
	require.Equal(t, types.BoxI32(4), v)
}

func TestInterpreter_SetGlobal(t *testing.T) {
	t.Run("sets scalar", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.GLOBAL_SET, 0)}, program.WithGlobals(types.TypeI32))
		i := interp.New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.NoError(t, i.SetGlobal(0, types.BoxI32(8)))
		v, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(8), v)
	})

	t.Run("rejects incompatible type", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeI32))
		i := interp.New(prog)
		defer i.Close()

		require.ErrorIs(t, i.SetGlobal(0, types.BoxF32(1)), interp.ErrTypeMismatch)
	})

	t.Run("accepts dynamic ref value", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeAny))
		i := interp.New(prog)
		defer i.Close()

		require.NoError(t, i.SetGlobal(0, types.BoxI32(8)))
	})

	t.Run("accepts heap backed i64", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeI64))
		i := interp.New(prog)
		defer i.Close()

		require.NoError(t, i.Push(types.I64(1<<60)))
		val, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.KindRef, val.Kind())
		require.NoError(t, i.SetGlobal(0, val))
	})

	t.Run("rejects incompatible concrete ref type", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.NewArrayType(types.TypeI32)))
		i := interp.New(prog)
		defer i.Close()

		matching, err := i.Alloc(types.TypedArray[int32]{1})
		require.NoError(t, err)
		require.NoError(t, i.SetGlobal(0, types.BoxRef(matching)))

		mismatching, err := i.Alloc(types.TypedArray[float32]{1})
		require.NoError(t, err)
		require.ErrorIs(t, i.SetGlobal(0, types.BoxRef(mismatching)), interp.ErrTypeMismatch)
		require.NoError(t, i.Release(mismatching))
	})

	t.Run("preserves same reference", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeAny))
		i := interp.New(prog)
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
		i := interp.New(prog)
		defer i.Close()

		before, err := i.Global(0)
		require.NoError(t, err)
		require.ErrorIs(t, i.SetGlobal(0, types.BoxRef(9999)), interp.ErrSegmentationFault)
		after, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, before, after)
	})
}

func TestInterpreter_Local(t *testing.T) {
	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 6), instr.New(instr.LOCAL_SET, 0), instr.New(instr.YIELD),
	}, program.WithLocals(types.TypeI32))
	i := interp.New(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
	v, err := i.Local(0)
	require.NoError(t, err)
	require.Equal(t, types.BoxI32(6), v)
}

func TestInterpreter_SetLocal(t *testing.T) {
	t.Run("sets scalar", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.YIELD)}, program.WithLocals(types.TypeI32))
		i := interp.New(prog)
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
		require.NoError(t, i.SetLocal(0, types.BoxI32(3)))
		v, err := i.Local(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(3), v)
	})

	t.Run("preserves same reference", func(t *testing.T) {
		prog := program.New(nil, program.WithLocals(types.TypeAny))
		i := interp.New(prog)
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
		i := interp.New(prog)
		defer i.Close()

		before, err := i.Local(0)
		require.NoError(t, err)
		require.ErrorIs(t, i.SetLocal(0, types.BoxRef(9999)), interp.ErrSegmentationFault)
		after, err := i.Local(0)
		require.NoError(t, err)
		require.Equal(t, before, after)
	})
}

func TestInterpreter_Load(t *testing.T) {
	i := interp.New(program.New(nil))
	defer i.Close()

	addr, err := i.Alloc(types.I32(5))
	require.NoError(t, err)
	v, err := i.Load(addr)
	require.NoError(t, err)
	require.Equal(t, types.I32(5), v)
}

func TestInterpreter_Store(t *testing.T) {
	t.Run("replaces scalar", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)
		require.NoError(t, i.Store(addr, types.BoxI32(9)))
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.I32(9), v)
	})

	t.Run("finalizes replaced value", func(t *testing.T) {
		i := interp.New(program.New(nil))
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
		i := interp.New(program.New(nil))
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
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})

	t.Run("ignores same-address reference", func(t *testing.T) {
		i := interp.New(program.New(nil))
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
		i := interp.New(program.New(nil))
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
		i := interp.New(program.New(nil))
		defer i.Close()

		source := &trackedValue{}
		sourceAddr, err := i.Alloc(source)
		require.NoError(t, err)
		targetAddr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)

		require.ErrorIs(t, i.Store(targetAddr, types.BoxRef(sourceAddr)), interp.ErrTypeMismatch)
		require.Equal(t, 0, source.closed)
		v, err := i.Load(targetAddr)
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})

	t.Run("rejects owned pointer", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		source := &trackedValue{}
		_, err := i.Alloc(source)
		require.NoError(t, err)
		targetAddr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)

		require.ErrorIs(t, i.Store(targetAddr, source), interp.ErrTypeMismatch)
		require.Equal(t, 0, source.closed)
		v, err := i.Load(targetAddr)
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})

	t.Run("ignores same-address ref", func(t *testing.T) {
		i := interp.New(program.New(nil))
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
		i := interp.New(program.New(nil))
		defer i.Close()

		sourceAddr, err := i.Alloc(types.I32(7))
		require.NoError(t, err)
		targetAddr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)

		require.ErrorIs(t, i.Store(targetAddr, types.Ref(sourceAddr)), interp.ErrTypeMismatch)
		v, err := i.Load(targetAddr)
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})

	t.Run("rejects invalid ref", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)
		require.ErrorIs(t, i.Store(addr, types.Ref(9999)), interp.ErrSegmentationFault)
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})

	t.Run("rejects invalid boxed ref", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)
		require.ErrorIs(t, i.Store(addr, types.BoxRef(9999)), interp.ErrSegmentationFault)
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})
}

func TestInterpreter_Alloc(t *testing.T) {
	t.Run("allocates value", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.String("hi"), v)
	})

	t.Run("copies boxed reference ownership", func(t *testing.T) {
		i := interp.New(program.New(nil))
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
		i := interp.New(program.New(nil))
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
		i := interp.New(program.New(nil))
		defer i.Close()

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		_, err = i.Alloc(value)
		require.ErrorIs(t, err, interp.ErrTypeMismatch)
		loaded, err := i.Load(addr)
		require.NoError(t, err)
		require.Same(t, value, loaded)
		require.Equal(t, 0, value.closed)
	})

	t.Run("rejects pointer read back out of the heap", func(t *testing.T) {
		i := interp.New(program.New(nil))
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
		require.ErrorIs(t, err, interp.ErrTypeMismatch)
	})

	t.Run("accepts pointer whose slot was released", func(t *testing.T) {
		i := interp.New(program.New(nil))
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
	i := interp.New(program.New(nil))
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
	i := interp.New(program.New(nil))
	defer i.Close()

	addr, err := i.Alloc(types.String("hi"))
	require.NoError(t, err)
	require.NoError(t, i.Release(addr))
	_, err = i.Load(addr)
	require.ErrorIs(t, err, interp.ErrSegmentationFault)
}

func TestInterpreter_RefCount(t *testing.T) {
	t.Run("counts a fresh allocation", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		count, err := i.RefCount(addr)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})

	t.Run("tracks retain and release", func(t *testing.T) {
		i := interp.New(program.New(nil))
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
		i := interp.New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))

		_, err = i.RefCount(addr)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})
}

func TestInterpreter_HeapCap(t *testing.T) {
	t.Run("grows to cover a new allocation", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		before := i.HeapLen()
		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		require.GreaterOrEqual(t, i.HeapLen(), before)
		require.Less(t, addr, i.HeapLen())
	})

	t.Run("bounds a scan over live addresses", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		first, err := i.Alloc(types.String("one"))
		require.NoError(t, err)
		second, err := i.Alloc(types.String("two"))
		require.NoError(t, err)

		live := map[int]int{}
		for addr := 1; addr < i.HeapLen(); addr++ {
			count, rcErr := i.RefCount(addr)
			if rcErr != nil {
				continue
			}
			live[addr] = count
		}
		require.Equal(t, map[int]int{first: 1, second: 1}, live)
	})

	t.Run("keeps a released slot in range until it is reused", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))

		require.Less(t, addr, i.HeapLen())
		_, err = i.RefCount(addr)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})
}

func TestInterpreter_Push(t *testing.T) {
	t.Run("pushes scalar", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(4)))
		require.Equal(t, 1, i.Len())
	})

	t.Run("rejects owned pointer", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		value := &trackedValue{}
		_, err := i.Alloc(value)
		require.NoError(t, err)
		require.ErrorIs(t, i.Push(value), interp.ErrTypeMismatch)
		require.Equal(t, 0, i.Len())
		require.Equal(t, 0, value.closed)
	})
}

func TestInterpreter_Pop(t *testing.T) {
	t.Run("scalar", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(4)))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(4), value)
	})

	t.Run("reference value releases its heap ownership", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.String("value")))
		boxed, err := i.Peek(0)
		require.NoError(t, err)
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.String("value"), value)
		_, err = i.Load(boxed.Ref())
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})

	t.Run("stack underflow", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		_, err := i.Pop()
		require.ErrorIs(t, err, interp.ErrStackUnderflow)
	})
}

func TestInterpreter_PopBoxed(t *testing.T) {
	t.Run("scalar f64 returns raw box without allocation", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.F64(3.5)))
		v, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.KindF64, v.Kind())
		require.Equal(t, 3.5, v.F64())
		require.Equal(t, 0, i.Len())
	})

	t.Run("ref kind transfers the reference to the caller", func(t *testing.T) {
		i := interp.New(program.New(nil))
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
		i := interp.New(program.New(nil))
		defer i.Close()

		_, err := i.PopBoxed()
		require.ErrorIs(t, err, interp.ErrStackUnderflow)
	})
}

func TestInterpreter_Peek(t *testing.T) {
	t.Run("leaves value on stack", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(4)))
		value, err := i.Peek(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(4), value)
		require.Equal(t, 1, i.Len())
	})

	t.Run("keeps reference owned by stack", func(t *testing.T) {
		i := interp.New(program.New(nil))
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
		i := interp.New(program.New(nil))
		defer i.Close()

		_, err := i.Peek(0)
		require.ErrorIs(t, err, interp.ErrStackUnderflow)
	})
}

func TestInterpreter_Len(t *testing.T) {
	i := interp.New(program.New(nil))
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
		i := interp.New(prog, interp.WithProfiler(p), interp.WithTick(1))
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
		i := interp.New(program.New(nil))
		defer i.Close()

		require.NotPanics(t, func() { i.Flush() })
	})
}

func TestInterpreter_Close(t *testing.T) {
	i := interp.New(program.New(nil))
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
		i := interp.New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(1)))
		i.Reset()
		require.Equal(t, 0, i.Len())
	})

	t.Run("restarts module after unpopped result", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 7)})
		i := interp.New(prog)
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
		i := interp.New(prog)
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
		i := interp.New(prog)
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
		i := interp.New(program.New(nil), interp.WithHeap(4))

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		count, err := i.RefCount(addr)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		i.Reset()
		require.Equal(t, 1, value.closed)

		// Reset must not leave the earlier value reachable: the address it
		// lived at should no longer resolve to a live value. (This used to
		// also scan i.heap past len for nil slots, but that pins Go slice and
		// GC hygiene of the backing array, which no caller can observe.)
		_, err = i.RefCount(addr)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)

		require.NoError(t, i.Close())
		require.Equal(t, 1, value.closed)
	})

	t.Run("preserves arrays detached by pop", func(t *testing.T) {
		typ := types.NewArrayType(types.TypeAny)
		prog := program.New(
			[]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0)},
			program.WithTypes(typ),
		)
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(prog, interp.WithThreshold(-1))
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
		i := interp.New(program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5)}))
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
		i := interp.New(prog)
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
			i := interp.New(tt.prog, interp.WithTick(1), interp.WithThreshold(-1), interp.WithHook(func(*interp.Interpreter) error {
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
	i := interp.New(program.New(nil), interp.WithCodec(upperCodec(0)))
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
		i := interp.New(program.New(nil), interp.WithProfiler(nil))
		defer i.Close()

		// This used to assert require.Nil(t, i.profiler) directly. With no
		// profiler attached there is nothing public to read back, so the
		// only observable claim left is that interp.WithProfiler(nil) does not
		// panic and the interpreter still closes cleanly.
	})

	t.Run("samples execution", func(t *testing.T) {
		p := prof.New()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD),
		})
		i := interp.New(prog, interp.WithProfiler(p), interp.WithTick(1))
		require.NoError(t, i.Run(context.Background()))
		require.NoError(t, i.Close())

		total, ok := p.Metric("vm_samples_total")
		require.True(t, ok)
		require.Equal(t, float64(3), total)
	})

	t.Run("records compilation and native entry", func(t *testing.T) {
		p := prof.New()
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		i := interp.New(prog, interp.WithProfiler(p), interp.WithTick(1), interp.WithThreshold(0))
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
		i := interp.New(program.New(code), interp.WithProfiler(p), interp.WithTick(1), interp.WithThreshold(0))
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
		i := interp.New(prog, interp.WithProfiler(p), interp.WithTick(1), interp.WithThreshold(0))
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
		i := interp.New(prog, interp.WithProfiler(p), interp.WithTick(1), interp.WithThreshold(0))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		attempts := func() float64 {
			i.Flush()
			v, _ := p.Metric("vm_jit_attempts_total")
			return v
		}

		for range 4 {
			require.NoError(t, i.Run(context.Background()))
			i.Reset()
		}
		early := captures()
		require.Greater(t, early, float64(0))
		earlyAttempts := attempts()
		require.Greater(t, earlyAttempts, float64(0))

		for range 16 {
			require.NoError(t, i.Run(context.Background()))
			i.Reset()
		}
		require.Equal(t, early, captures(), "capture attempts must not grow once the function is cold")
		require.Equal(t, earlyAttempts, attempts(), "the function should have given up after repeated unproductive observations")
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
		i := interp.New(prog, interp.WithProfiler(p), interp.WithTick(1), interp.WithThreshold(0))
		defer i.Close()

		for range 4 {
			require.NoError(t, i.Run(context.Background()))
			got, err := i.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, types.BoxI32(iterations), got, "result must stay correct across a native trace-cut fallback")
			i.Reset()
		}

		// The result must hold on every architecture; only the exit reason
		// needs a backend that emits native code, and CI runs amd64, where the
		// ARM64 lowering is not compiled at all.
		if runtime.GOARCH != "arm64" {
			return
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
		i := interp.New(prog, interp.WithFrame(3))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrFrameOverflow)
	})

	t.Run("host call succeeds once frames are exhausted", func(t *testing.T) {
		hostFn := interp.NewHostFunction(&types.FunctionType{Returns: []types.Type{types.TypeI32}},
			func(_ *interp.Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(1)}, nil
			})
		fillFn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.CONST_GET, 1), instr.New(instr.CALL), instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(fillFn, hostFn))
		i := interp.New(prog, interp.WithFrame(2))
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
		i := interp.New(prog, interp.WithFrame(nativeFrameLimit+2), interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(firstMetrics))
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
		i = interp.New(prog, interp.WithFrame(nativeFrameLimit+2), interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(metrics))

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrFrameOverflow)
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
		i := interp.New(prog, interp.WithStack(2))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrStackOverflow)
	})

	t.Run("zero normalizes to one slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
		})
		i := interp.New(prog, interp.WithStack(0))
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
		i := interp.New(prog, interp.WithHeap(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 3, i.Len())
	})

	t.Run("collects cycle at backing capacity", func(t *testing.T) {
		i := interp.New(program.New(nil), interp.WithHeap(2))
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
		i := interp.New(prog, interp.WithHeap(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 1, i.Len())
	})

	t.Run("collects cycles at adaptive goal", func(t *testing.T) {
		const capacity = 2 * heapRunway

		i := interp.New(program.New(nil), interp.WithHeap(capacity), interp.WithHeapLimit(capacity))
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

		i := interp.New(program.New(nil), interp.WithHeap(capacity), interp.WithHeapLimit(capacity))
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

		i := interp.New(program.New(nil), interp.WithHeap(capacity), interp.WithHeapLimit(4*heapRunway))
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
		i := interp.New(prog, interp.WithHeapLimit(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrHeapExhausted)
	})

	t.Run("preserves host-owned reference", func(t *testing.T) {
		i := interp.New(program.New(nil), interp.WithHeap(2), interp.WithHeapLimit(2))
		defer i.Close()

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		_, err = i.Alloc(types.String("blocked"))
		require.ErrorIs(t, err, interp.ErrHeapExhausted)
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
		i := interp.New(prog, interp.WithHeap(4), interp.WithHeapLimit(4))
		defer i.Close()

		_, err := i.Alloc(types.String("blocked"))
		require.ErrorIs(t, err, interp.ErrHeapExhausted)
		got, err := i.Load(leafAddr)
		require.NoError(t, err)
		require.Same(t, leaf, got)
		require.Equal(t, 0, leaf.closed)

		i.Reset()
		_, err = i.Alloc(types.String("blocked again"))
		require.ErrorIs(t, err, interp.ErrHeapExhausted)
		got, err = i.Load(leafAddr)
		require.NoError(t, err)
		require.Same(t, leaf, got)
		require.Equal(t, 0, leaf.closed)
	})

	t.Run("collects unreachable cycle", func(t *testing.T) {
		i := interp.New(program.New(nil), interp.WithHeap(3), interp.WithHeapLimit(3))
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
		i := interp.New(program.New(nil), interp.WithHeap(3), interp.WithHeapLimit(3))
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
		require.ErrorIs(t, err, interp.ErrHeapExhausted)
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
		i := interp.New(program.New(nil), interp.WithHeap(2), interp.WithHeapLimit(2))
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
		i := interp.New(program.New(nil), interp.WithHeap(3), interp.WithHeapLimit(3))
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
		i := interp.New(program.New(nil), interp.WithHeap(4), interp.WithHeapLimit(4))
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
	i := interp.New(prog, interp.WithTick(2), interp.WithHook(func(i *interp.Interpreter) error {
		calls++
		return nil
	}))
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	require.Equal(t, 2, calls)
}

// refCounts snapshots every live heap address's reference count, so two
// interpreters that ran the same program can be compared for ownership parity.
func refCounts(i *interp.Interpreter) map[int]int {
	out := map[int]int{}
	for addr := 1; addr < i.HeapLen(); addr++ {
		count, err := i.RefCount(addr)
		if err != nil {
			continue
		}
		out[addr] = count
	}
	return out
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
func jitMetricSum(i *interp.Interpreter, p *prof.Profiler, name string, match func(labels []prof.Label) bool) float64 {
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
func jitCompiledAt(i *interp.Interpreter, p *prof.Profiler, fn, ip int) bool {
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
func jitSideExitCompiles(i *interp.Interpreter, p *prof.Profiler, fn int) float64 {
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
func jitNativeExits(i *interp.Interpreter, p *prof.Profiler, fn int) float64 {
	want := strconv.Itoa(fn)
	return jitMetricSum(i, p, "vm_jit_native_exits_total", func(labels []prof.Label) bool {
		return jitLabel(labels, "func") == want
	})
}

// jitCompileAttempts sums every compile attempt recorded for fn at an ip
// satisfying ipMatch, regardless of outcome, trigger, or reason: the public
// signal behind the private i.tried map used to gate on a specific anchor
// having been offered to the compiler at all.
func jitCompileAttempts(i *interp.Interpreter, p *prof.Profiler, fn int, ipMatch func(ip string) bool) float64 {
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
	// This used to force the per-address hot-entry counter and the tier-up
	// trigger to MaxUint64 and call the private hit() to prove the counter
	// saturates instead of wrapping. That overflow edge is not reachable from
	// any public entry point, so the saturation invariant is no longer covered
	// here; only the ordinary tier-up path's absence of a fault survives, and
	// the name says so rather than promising the counter check.
	t.Run("tiering a trivial program does not fault", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		i := interp.New(prog, interp.WithThreshold(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
	})

	t.Run("disabled", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 7)})
		i := interp.New(prog, interp.WithThreshold(-1))
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
	t.Run("the ordinary entry path does not fault", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.NOP),
			instr.New(instr.NOP),
		})
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(program.New(code), interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithThreshold(3), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithThreshold(3), interp.WithProfiler(p))
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

		p := prof.New()
		// A threshold of 5 is reachable within 4 runs only if both CALL and
		// RESUME count as entries into the coroutine body: at the documented
		// 2 entries per run (CALL and RESUME each counting once) the count
		// reaches 8 >= 5 by the fourth run, while 1 entry per run would only
		// reach 4.
		i := interp.New(prog, interp.WithThreshold(5), interp.WithProfiler(p))
		defer i.Close()

		for range 4 {
			require.NoError(t, i.Run(context.Background()))
			_, err := i.Pop()
			require.NoError(t, err)
			i.Reset()
		}
		require.Greater(t, jitCompileAttempts(i, p, 1, func(string) bool { return true }), float64(0),
			"CALL and RESUME each enter the coroutine once per run")
	})

	// A function only ever reached from a host callback is entered through the
	// one-instruction trampoline invoke installs, not through the module's own
	// table, so it counts only if that trampoline carries the hook too.
	t.Run("counts entries made through a host callback", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32},
		}).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN)).MustBuild()
		prog := program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0)}, program.WithConstants(fn))

		p := prof.New()
		// A threshold of 4 is reachable only if the trampoline routes every
		// call through the normal entry hook, one entry per call.
		i := interp.New(prog, interp.WithThreshold(4), interp.WithProfiler(p))
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
		require.Greater(t, jitCompileAttempts(i, p, ref.Ref(), func(string) bool { return true }), float64(0),
			"the invoke trampoline never counted its callee")
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
				i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
				i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
				i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
				i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		require.ErrorIs(t, i.Run(context.Background()), interp.ErrIndexOutOfRange)
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
				i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
				i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
				require.ErrorIs(t, i.Run(context.Background()), interp.ErrDivideByZero)
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		require.ErrorIs(t, i.Run(context.Background()), interp.ErrTypeMismatch)
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(1), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(1), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(p))
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
	i := interp.New(prog, interp.WithTick(1), interp.WithFuel(2))
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrFuelExhausted)
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
		var vm *interp.Interpreter
		var closeErr error
		var elapsed time.Duration
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			start := time.Now()
			vm = interp.New(prog)
			elapsed += time.Since(start)
			closeErr = vm.Close()
		}
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
		require.NoError(b, closeErr)
	})

	b.Run("Program", func(b *testing.B) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 42)})
		var vm *interp.Interpreter
		var closeErr error
		var elapsed time.Duration
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			start := time.Now()
			vm = interp.New(prog, interp.WithThreshold(-1))
			elapsed += time.Since(start)
			closeErr = vm.Close()
		}
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
		require.NoError(b, closeErr)
	})

	b.Run("JITEnabled", func(b *testing.B) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 42)})
		var vm *interp.Interpreter
		var closeErr error
		var elapsed time.Duration
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			start := time.Now()
			vm = interp.New(prog, interp.WithThreshold(0))
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
		vm := interp.New(prog, interp.WithTick(1<<20), interp.WithThreshold(1<<30))
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
				opts []interp.Option
			}{
				{name: "Threaded", opts: []interp.Option{interp.WithTick(1), interp.WithThreshold(-1)}},
				{name: "Fused", opts: []interp.Option{interp.WithThreshold(-1)}},
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
					opts []interp.Option
				}{name: "JITWarm", opts: []interp.Option{interp.WithTick(1), interp.WithThreshold(0)}})
			}

			for _, mode := range modes {
				b.Run(mode.name, func(b *testing.B) {
					ctx := context.Background()
					opts := mode.opts
					// §14 requires a warm JIT benchmark to prove native
					// installation before timing. A profiler is the only public
					// way to see it, but attaching one to the timed interpreter
					// would fail dispatch's fast-path guard and add a safepoint
					// on every instruction at WithTick(1). So a throwaway probe
					// proves this program and option set do install, and the
					// timed interpreter below is warmed identically without one.
					if mode.name == "JITWarm" {
						profile := prof.New()
						probe := interp.New(tt.program, append(append([]interp.Option(nil), opts...), interp.WithProfiler(profile))...)
						installed := false
						for range 16 {
							if err := probe.Run(ctx); err != nil && tt.err == nil {
								require.NoError(b, err)
							}
							probe.Reset()
							if jitCompiledAt(probe, profile, 0, 0) {
								installed = true
								break
							}
						}
						require.NoError(b, probe.Close())
						if !installed {
							b.Skip("entry does not compile")
						}
					}
					vm := interp.New(tt.program, opts...)
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
				vm := interp.New(tt.program)
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
		opts []interp.Option
	}{
		{
			name: "Scalar",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 42), instr.New(instr.GLOBAL_SET, 0),
			}, program.WithGlobals(types.TypeI32)),
			opts: []interp.Option{interp.WithThreshold(-1)},
		},
		{
			name: "Heap",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 8), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			}, program.WithTypes(types.NewArrayType(types.TypeAny))),
			opts: []interp.Option{interp.WithThreshold(-1)},
		},
		{
			name: "JITState",
			prog: program.New(jitCode),
			opts: []interp.Option{interp.WithThreshold(0)},
		},
	}

	for _, tt := range tests {
		if tt.name == "JITState" && runtime.GOARCH != "arm64" {
			continue
		}
		b.Run(tt.name, func(b *testing.B) {
			ctx := context.Background()
			opts := tt.opts
			// Only the JITState case needs to prove native installation;
			// attaching the profiler is harmless here because the timed
			// section below measures Reset(), never Run().
			var profile *prof.Profiler
			if tt.name == "JITState" {
				profile = prof.New()
				opts = append(opts, interp.WithProfiler(profile))
			}
			vm := interp.New(tt.prog, opts...)
			defer vm.Close()
			require.NoError(b, vm.Run(ctx))
			if tt.name == "JITState" {
				for range 16 {
					if jitCompiledAt(vm, profile, 0, 0) {
						break
					}
					vm.Reset()
					require.NoError(b, vm.Run(ctx))
				}
				require.True(b, jitCompiledAt(vm, profile, 0, 0))
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
		vm := interp.New(program.New(nil))
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
		vm := interp.New(program.New(nil))
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
	vm := interp.New(program.New(nil))
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
	vm := interp.New(program.New(nil))
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
	vm := interp.New(program.New(nil))
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
	vm := interp.New(program.New(nil))
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
	vm := interp.New(program.New(nil))
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
	vm := interp.New(program.New(nil))
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
	vm := interp.New(prog, interp.WithThreshold(-1)) // threaded + fused, no JIT
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
			vm := interp.New(tt.prog, interp.WithThreshold(-1)) // threaded + fused, no JIT
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

// BenchmarkInterpreter_ArraySetContainerFusion measures ARRAY_SET fused onto
// a LOCAL_GET, GLOBAL_GET, and UPVAL_GET container -- the three sources
// element()'s isContainerSource branch (internal/cmd/geninterp/lower.go)
// specializes. Each subtest writes arr[j] = j through the fused container in
// a nested loop instead of summing, so the timed body is dominated by
// array.set rather than array.get; a final single pass sums the written
// array so the benchmark can still verify correctness.
func BenchmarkInterpreter_ArraySetContainerFusion(b *testing.B) {
	const size, repeats = 64, 4000
	for _, tt := range []struct {
		name string
		prog *program.Program
	}{
		{name: "local", prog: arrayFillLocal(size, repeats)},
		{name: "global", prog: arrayFillGlobal(size, repeats)},
		{name: "upvalue", prog: arrayFillUpvalue(size, repeats)},
	} {
		b.Run(tt.name, func(b *testing.B) {
			vm := interp.New(tt.prog, interp.WithThreshold(-1)) // threaded + fused, no JIT
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

// arrayFillLocal builds a kernel that holds a size-length int32 array in a
// declared LOCAL_GET slot and writes arr[j] = j repeats times in a nested
// loop, so every array.set is a LOCAL_GET whose declared type is a concrete
// *types.ArrayType -- the shape interp/threaded.go's generated ARRAY_SET
// local-container fusion specializes. A final pass sums the written array
// once to produce a checksum the benchmark can verify.
func arrayFillLocal(size, repeats int32) *program.Program {
	b := program.NewBuilder()
	b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32, types.TypeI32) // 0=arr, 1=outer, 2=inner, 3=sum
	outerLoop, outerDone := b.Label(), b.Label()
	innerLoop, innerDone := b.Label(), b.Label()
	sumLoop, sumDone := b.Label(), b.Label()
	b.ConstGet(types.TypedArray[int32](make([]int32, size))).Emit(instr.LOCAL_SET, 0).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1).
		Bind(outerLoop).
		Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(repeats))).Emit(instr.I32_GE_S).
		BrIf(outerDone).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2).
		Bind(innerLoop).
		Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).
		BrIf(innerDone).
		Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 2).Emit(instr.ARRAY_SET).
		Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2).
		Br(innerLoop).
		Bind(innerDone).
		Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).
		Br(outerLoop).
		Bind(outerDone).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2).
		Bind(sumLoop).
		Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).
		BrIf(sumDone).
		Emit(instr.LOCAL_GET, 3).
		Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.ARRAY_GET).
		Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3).
		Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2).
		Br(sumLoop).
		Bind(sumDone).
		Emit(instr.LOCAL_GET, 3)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

// arrayFillGlobal builds the same kernel as arrayFillLocal, except the array
// is held in a declared GLOBAL_GET slot instead of a local, so every
// array.set is a GLOBAL_GET whose declared type is a concrete
// *types.ArrayType -- the shape interp/threaded.go's generated ARRAY_SET
// global-container fusion specializes.
func arrayFillGlobal(size, repeats int32) *program.Program {
	b := program.NewBuilder()
	b.Globals(types.TypeI32Array)
	b.Locals(types.TypeI32, types.TypeI32, types.TypeI32) // 0=outer, 1=inner, 2=sum
	outerLoop, outerDone := b.Label(), b.Label()
	innerLoop, innerDone := b.Label(), b.Label()
	sumLoop, sumDone := b.Label(), b.Label()
	b.ConstGet(types.TypedArray[int32](make([]int32, size))).Emit(instr.GLOBAL_SET, 0).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0).
		Bind(outerLoop).
		Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(repeats))).Emit(instr.I32_GE_S).
		BrIf(outerDone).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1).
		Bind(innerLoop).
		Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).
		BrIf(innerDone).
		Emit(instr.GLOBAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_SET).
		Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).
		Br(innerLoop).
		Bind(innerDone).
		Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).
		Br(outerLoop).
		Bind(outerDone).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2).
		Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1).
		Bind(sumLoop).
		Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).
		BrIf(sumDone).
		Emit(instr.LOCAL_GET, 2).
		Emit(instr.GLOBAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET).
		Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2).
		Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).
		Br(sumLoop).
		Bind(sumDone).
		Emit(instr.LOCAL_GET, 2)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

// arrayFillUpvalue builds the same kernel as arrayFillLocal, except the
// array is captured as a closure upvalue instead of stored in a local, so
// every array.set is a UPVAL_GET whose declared type is a concrete
// *types.ArrayType -- the shape interp/threaded.go's generated ARRAY_SET
// upvalue-container fusion specializes.
func arrayFillUpvalue(size, repeats int32) *program.Program {
	fillBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Captures(types.TypeI32Array).
		Locals(types.TypeI32, types.TypeI32, types.TypeI32) // 0=outer, 1=inner, 2=sum
	outerLoop, outerDone := fillBuilder.Label(), fillBuilder.Label()
	innerLoop, innerDone := fillBuilder.Label(), fillBuilder.Label()
	sumLoop, sumDone := fillBuilder.Label(), fillBuilder.Label()
	fillFn := fillBuilder.
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 0)).
		Bind(outerLoop).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, uint64(uint32(repeats))), instr.New(instr.I32_GE_S)).
		BrIf(outerDone).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 1)).
		Bind(innerLoop).
		Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, uint64(uint32(size))), instr.New(instr.I32_GE_S)).
		BrIf(innerDone).
		Emit(
			instr.New(instr.UPVAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 1), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1),
		).
		Br(innerLoop).
		Bind(innerDone).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 0)).
		Br(outerLoop).
		Bind(outerDone).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 2)).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 1)).
		Bind(sumLoop).
		Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, uint64(uint32(size))), instr.New(instr.I32_GE_S)).
		BrIf(sumDone).
		Emit(
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.UPVAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.ARRAY_GET),
			instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1),
		).
		Br(sumLoop).
		Bind(sumDone).
		Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN)).
		MustBuild()

	b := program.NewBuilder()
	fnIdx := b.Const(fillFn)
	b.ConstGet(types.TypedArray[int32](make([]int32, size)))
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
			vm := interp.New(prog, interp.WithThreshold(tt.threshold))
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
			i := interp.New(program.New(nil))
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
			i := interp.New(program.New(nil))
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
