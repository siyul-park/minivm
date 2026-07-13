package interp

import (
	"context"
	"encoding/binary"
	"math"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type upperMarshaler struct{}

type contextKey struct{}

type contextHost struct{}

type trackedValue struct {
	refs   []types.Ref
	closed int
}

func (v *trackedValue) Kind() types.Kind { return types.KindRef }
func (v *trackedValue) Type() types.Type { return types.TypeRef }
func (v *trackedValue) String() string   { return "tracked" }

func (v *trackedValue) Refs(dst []types.Ref) []types.Ref {
	return append(dst, v.refs...)
}

func (v *trackedValue) Close() error {
	v.closed++
	return nil
}

func (*contextHost) Value(ctx context.Context) int32 {
	if ctx.Value(contextKey{}) == "value" {
		return 7
	}
	return 0
}

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
	name    string
	program *program.Program
	values  []types.Value
	err     error
}{
	{
		name:    "i32.const nop returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.NOP)}),
		values:  []types.Value{types.I32(1)},
	},
	{
		name:    "unreachable reports unreachable executed",
		program: program.New([]instr.Instruction{instr.New(instr.UNREACHABLE)}),
		err:     ErrUnreachableExecuted,
	},
	{
		name:    "i32.const i32.const drop returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.DROP)}),
		values:  []types.Value{types.I32(1)},
	},
	{
		name:    "i32.const dup returns i32 i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 7), instr.New(instr.DUP)}),
		values:  []types.Value{types.I32(7), types.I32(7)},
	},
	{
		name:    "i32.const i32.const swap returns i32 i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.SWAP)}),
		values:  []types.Value{types.I32(1), types.I32(2)},
	},
	{
		name: "i32.const i32.const i32.const select returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 1), instr.New(instr.SELECT),
		}),
		values: []types.Value{types.I32(10)},
	},
	{
		name: "br i32.const i32.const returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.BR, 5),
			instr.New(instr.I32_CONST, 999),
			instr.New(instr.I32_CONST, 1),
		}),
		values: []types.Value{types.I32(1)},
	},
	{
		name: "i32.const br_if i32.const i32.const returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.BR_IF, 5),
			instr.New(instr.I32_CONST, 999),
			instr.New(instr.I32_CONST, 1),
		}),
		values: []types.Value{types.I32(1)},
	},
	{
		name: "i32.const br_table i32.const i32.const returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.BR_TABLE, 1, 5, 0),
			instr.New(instr.I32_CONST, 999),
			instr.New(instr.I32_CONST, 1),
		}),
		values: []types.Value{types.I32(1)},
	},
	{
		name: "const.get call i32.const return returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(42)},
	},
	{
		name: "const.get call i32.const i32.const return returns i32 i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32, types.TypeI32}}).
			Emit(instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 20), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(20), types.I32(10)},
	},
	{
		name: "i32.const const.get return_call local.get i32.const i32.add return returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.RETURN_CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(6)},
	},
	{
		name:    "i32.const yield reports yield",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.YIELD)}),
		err:     ErrYield,
	},
	{
		name: "const.get call through yield i32.const i32.add return returns i32",
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
		name: "const.get call coro.done i32.const yield return returns i1",
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.CORO_DONE),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I1(false)},
	},
	{
		name: "const.get call coro.value i32.const yield return returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.CORO_VALUE),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.YIELD), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(1)},
	},
	{
		name: "i32.const global.set global.get returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.GLOBAL_SET, 0), instr.New(instr.GLOBAL_GET, 0),
		}, program.WithGlobals(types.TypeI32)),
		values: []types.Value{types.I32(3)},
	},
	{
		name:    "i32.const global.tee returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 6), instr.New(instr.GLOBAL_TEE, 0)}, program.WithGlobals(types.TypeI32)),
		values:  []types.Value{types.I32(6)},
	},
	{
		name: "i32.const local.set local.get returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.LOCAL_SET, 0), instr.New(instr.LOCAL_GET, 0),
		}, program.WithLocals(types.TypeI32)),
		values: []types.Value{types.I32(5)},
	},
	{
		name:    "i32.const local.tee returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 9), instr.New(instr.LOCAL_TEE, 0)}, program.WithLocals(types.TypeI32)),
		values:  []types.Value{types.I32(9)},
	},
	{
		name:    "const.get returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0)}, program.WithConstants(types.I32(11))),
		values:  []types.Value{types.I32(11)},
	},
	{
		name: "i32.const const.get closure.new call upval.get return returns i32",
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
		name: "i32.const const.get through i32.const upval.set upval.get return returns i32",
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
		name:    "ref.null returns ref",
		program: program.New([]instr.Instruction{instr.New(instr.REF_NULL)}),
		values:  []types.Value{types.Null},
	},
	{
		name:    "i32.const ref.new returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.REF_NEW)}),
		values:  []types.Value{types.I32(5)},
	},
	{
		name:    "i32.const ref.new ref.get returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 9), instr.New(instr.REF_NEW), instr.New(instr.REF_GET)}),
		values:  []types.Value{types.I32(9)},
	},
	{
		name: "i32.const ref.new dup i32.const ref.set ref.get returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 77), instr.New(instr.REF_SET),
			instr.New(instr.REF_GET),
		}),
		values: []types.Value{types.I32(77)},
	},
	{
		name:    "i32.const ref.test returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.REF_TEST, 0)}, program.WithTypes(types.TypeI32)),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i32.const ref.cast returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.REF_CAST, 0)}, program.WithTypes(types.TypeI32)),
		values:  []types.Value{types.I32(5)},
	},
	{
		name:    "ref.null ref.is_null returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.REF_NULL), instr.New(instr.REF_IS_NULL)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "ref.null ref.null ref.eq returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.REF_NULL), instr.New(instr.REF_NULL), instr.New(instr.REF_EQ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "ref.null i32.const ref.new ref.ne returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.REF_NULL), instr.New(instr.I32_CONST, 5), instr.New(instr.REF_NEW), instr.New(instr.REF_NE)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i32.const returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 42)}),
		values:  []types.Value{types.I32(42)},
	},
	{
		name:    "i32.const i32.const i32.add returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_ADD)}),
		values:  []types.Value{types.I32(5)},
	},
	{
		name:    "i32.const i32.const i32.sub returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_SUB)}),
		values:  []types.Value{types.I32(2)},
	},
	{
		name:    "i32.const i32.const i32.mul returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 4), instr.New(instr.I32_MUL)}),
		values:  []types.Value{types.I32(12)},
	},
	{
		name: "i32.const i32.const i32.div_s returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-7)), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_DIV_S),
		}),
		values: []types.Value{types.I32(-3)},
	},
	{
		name: "i32.const i32.const i32.div_u returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_DIV_U),
		}),
		values: []types.Value{types.I32(int32(uint32(math.MaxUint32) / 2))},
	},
	{
		name: "i32.const i32.const i32.rem_s returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-7)), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_REM_S),
		}),
		values: []types.Value{types.I32(-1)},
	},
	{
		name: "i32.const i32.const i32.rem_u returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_REM_U),
		}),
		values: []types.Value{types.I32(int32(uint32(math.MaxUint32) % 3))},
	},
	{
		name:    "i32.const i32.const i32.shl returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_SHL)}),
		values:  []types.Value{types.I32(8)},
	},
	{
		name: "i32.const i32.const i32.shr_s returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-8)), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SHR_S),
		}),
		values: []types.Value{types.I32(-4)},
	},
	{
		name: "i32.const i32.const i32.shr_u returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SHR_U),
		}),
		values: []types.Value{types.I32(int32(uint32(math.MaxUint32) >> 1))},
	},
	{
		name:    "i32.const i32.const i32.and returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 12), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_AND)}),
		values:  []types.Value{types.I32(8)},
	},
	{
		name:    "i32.const i32.const i32.or returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 12), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_OR)}),
		values:  []types.Value{types.I32(14)},
	},
	{
		name:    "i32.const i32.const i32.xor returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 12), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_XOR)}),
		values:  []types.Value{types.I32(6)},
	},
	{
		name:    "i32.const i32.clz returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CLZ)}),
		values:  []types.Value{types.I32(31)},
	},
	{
		name:    "i32.const i32.ctz returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 8), instr.New(instr.I32_CTZ)}),
		values:  []types.Value{types.I32(3)},
	},
	{
		name:    "i32.const i32.popcnt returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 7), instr.New(instr.I32_POPCNT)}),
		values:  []types.Value{types.I32(3)},
	},
	{
		name:    "i32.const i32.const i32.rotl returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 4), instr.New(instr.I32_ROTL)}),
		values:  []types.Value{types.I32(16)},
	},
	{
		name:    "i32.const i32.const i32.rotr returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 16), instr.New(instr.I32_CONST, 4), instr.New(instr.I32_ROTR)}),
		values:  []types.Value{types.I32(1)},
	},
	{
		name:    "i32.const i32.extend8_s returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 255), instr.New(instr.I32_EXTEND8_S)}),
		values:  []types.Value{types.I32(-1)},
	},
	{
		name:    "i32.const i32.extend16_s returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 65535), instr.New(instr.I32_EXTEND16_S)}),
		values:  []types.Value{types.I32(-1)},
	},
	{
		name:    "i32.const i32.eqz returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.I32_EQZ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i32.const i32.const i32.eq returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 5), instr.New(instr.I32_EQ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i32.const i32.const i32.ne returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 6), instr.New(instr.I32_NE)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i32.const i32.const i32.lt_s returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LT_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i32.const i32.const i32.lt_u returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LT_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		name:    "i32.const i32.const i32.gt_s returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_GT_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i32.const i32.const i32.gt_u returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_GT_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		name:    "i32.const i32.const i32.le_s returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_LE_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i32.const i32.const i32.le_u returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		name:    "i32.const i32.const i32.ge_s returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_GE_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i32.const i32.const i32.ge_u returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_GE_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		name:    "i32.const i32.to_i64_s returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_TO_I64_S)}),
		values:  []types.Value{types.I64(-1)},
	},
	{
		name:    "i32.const i32.to_i64_u returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_TO_I64_U)}),
		values:  []types.Value{types.I64(int64(uint32(math.MaxUint32)))},
	},
	{
		name:    "i32.const i32.to_f32_s returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_TO_F32_S)}),
		values:  []types.Value{types.F32(float32(int32(-1)))},
	},
	{
		name:    "i32.const i32.to_f32_u returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_TO_F32_U)}),
		values:  []types.Value{types.F32(float32(uint32(math.MaxUint32)))},
	},
	{
		name:    "i32.const i32.to_f64_s returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_TO_F64_S)}),
		values:  []types.Value{types.F64(float64(int32(-1)))},
	},
	{
		name:    "i32.const i32.to_f64_u returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, i32operand(-1)), instr.New(instr.I32_TO_F64_U)}),
		values:  []types.Value{types.F64(float64(uint32(math.MaxUint32)))},
	},
	{
		name:    "f32.const i32.reinterpret_f32 returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(1))), instr.New(instr.I32_REINTERPRET_F32)}),
		values:  []types.Value{types.I32(int32(math.Float32bits(1)))},
	},
	{
		name:    "i64.const returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 42)}),
		values:  []types.Value{types.I64(42)},
	},
	{
		name:    "i64.const i64.const i64.add returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 2), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_ADD)}),
		values:  []types.Value{types.I64(5)},
	},
	{
		name:    "i64.const i64.const i64.sub returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 5), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_SUB)}),
		values:  []types.Value{types.I64(2)},
	},
	{
		name:    "i64.const i64.const i64.mul returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 3), instr.New(instr.I64_CONST, 4), instr.New(instr.I64_MUL)}),
		values:  []types.Value{types.I64(12)},
	},
	{
		name: "i64.const i64.const i64.div_s returns i64",
		program: program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(-7)), instr.New(instr.I64_CONST, 2), instr.New(instr.I64_DIV_S),
		}),
		values: []types.Value{types.I64(-3)},
	},
	{
		name: "i64.const i64.const i64.div_u returns i64",
		program: program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, 2), instr.New(instr.I64_DIV_U),
		}),
		values: []types.Value{types.I64(int64(uint64(math.MaxUint64) / 2))},
	},
	{
		name: "i64.const i64.const i64.rem_s returns i64",
		program: program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(-7)), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_REM_S),
		}),
		values: []types.Value{types.I64(-1)},
	},
	{
		name: "i64.const i64.const i64.rem_u returns i64",
		program: program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_REM_U),
		}),
		values: []types.Value{types.I64(int64(uint64(math.MaxUint64) % 3))},
	},
	{
		name:    "i64.const i64.const i64.shl returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 1), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_SHL)}),
		values:  []types.Value{types.I64(8)},
	},
	{
		name: "i64.const i64.const i64.shr_s returns i64",
		program: program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(-8)), instr.New(instr.I64_CONST, 1), instr.New(instr.I64_SHR_S),
		}),
		values: []types.Value{types.I64(-4)},
	},
	{
		name: "i64.const i64.const i64.shr_u returns i64",
		program: program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, 1), instr.New(instr.I64_SHR_U),
		}),
		values: []types.Value{types.I64(int64(uint64(math.MaxUint64) >> 1))},
	},
	{
		name:    "i64.const i64.const i64.xor returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 12), instr.New(instr.I64_CONST, 10), instr.New(instr.I64_XOR)}),
		values:  []types.Value{types.I64(6)},
	},
	{
		name:    "i64.const i64.const i64.and returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 12), instr.New(instr.I64_CONST, 10), instr.New(instr.I64_AND)}),
		values:  []types.Value{types.I64(8)},
	},
	{
		name:    "i64.const i64.const i64.or returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 12), instr.New(instr.I64_CONST, 10), instr.New(instr.I64_OR)}),
		values:  []types.Value{types.I64(14)},
	},
	{
		name:    "i64.const i64.clz returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 1), instr.New(instr.I64_CLZ)}),
		values:  []types.Value{types.I64(63)},
	},
	{
		name:    "i64.const i64.ctz returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 8), instr.New(instr.I64_CTZ)}),
		values:  []types.Value{types.I64(3)},
	},
	{
		name:    "i64.const i64.popcnt returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 7), instr.New(instr.I64_POPCNT)}),
		values:  []types.Value{types.I64(3)},
	},
	{
		name:    "i64.const i64.const i64.rotl returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 1), instr.New(instr.I64_CONST, 4), instr.New(instr.I64_ROTL)}),
		values:  []types.Value{types.I64(16)},
	},
	{
		name:    "i64.const i64.const i64.rotr returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 16), instr.New(instr.I64_CONST, 4), instr.New(instr.I64_ROTR)}),
		values:  []types.Value{types.I64(1)},
	},
	{
		name:    "i64.const i64.extend8_s returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 255), instr.New(instr.I64_EXTEND8_S)}),
		values:  []types.Value{types.I64(-1)},
	},
	{
		name:    "i64.const i64.extend16_s returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 65535), instr.New(instr.I64_EXTEND16_S)}),
		values:  []types.Value{types.I64(-1)},
	},
	{
		name:    "i64.const i64.extend32_s returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, uint64(uint32(math.MaxUint32))), instr.New(instr.I64_EXTEND32_S)}),
		values:  []types.Value{types.I64(-1)},
	},
	{
		name:    "i64.const i64.eqz returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 0), instr.New(instr.I64_EQZ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i64.const i64.const i64.eq returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 5), instr.New(instr.I64_CONST, 5), instr.New(instr.I64_EQ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i64.const i64.const i64.ne returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 5), instr.New(instr.I64_CONST, 6), instr.New(instr.I64_NE)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i64.const i64.const i64.lt_s returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, 0), instr.New(instr.I64_LT_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i64.const i64.const i64.lt_u returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, 0), instr.New(instr.I64_LT_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		name:    "i64.const i64.const i64.gt_s returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 0), instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_GT_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i64.const i64.const i64.gt_u returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 0), instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_GT_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		name:    "i64.const i64.const i64.le_s returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_LE_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i64.const i64.const i64.le_u returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_CONST, 0), instr.New(instr.I64_LE_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		name:    "i64.const i64.const i64.ge_s returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 0), instr.New(instr.I64_CONST, 0), instr.New(instr.I64_GE_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		name:    "i64.const i64.const i64.ge_u returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, 0), instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_GE_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		name:    "i64.const i64.to_i32 returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, uint64(int64(1)<<32+1)), instr.New(instr.I64_TO_I32)}),
		values:  []types.Value{types.I32(1)},
	},
	{
		name:    "i64.const i64.to_f32_s returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_TO_F32_S)}),
		values:  []types.Value{types.F32(float32(int64(-1)))},
	},
	{
		name:    "i64.const i64.to_f32_u returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_TO_F32_U)}),
		values:  []types.Value{types.F32(float32(uint64(math.MaxUint64)))},
	},
	{
		name:    "i64.const i64.to_f64_s returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_TO_F64_S)}),
		values:  []types.Value{types.F64(float64(int64(-1)))},
	},
	{
		name:    "i64.const i64.to_f64_u returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, i64operand(-1)), instr.New(instr.I64_TO_F64_U)}),
		values:  []types.Value{types.F64(float64(uint64(math.MaxUint64)))},
	},
	{
		name:    "f64.const i64.reinterpret_f64 returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(1)), instr.New(instr.I64_REINTERPRET_F64)}),
		values:  []types.Value{types.I64(int64(math.Float64bits(1)))},
	},
	{
		name:    "f32.const returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5)))}),
		values:  []types.Value{types.F32(1.5)},
	},
	{
		name: "f32.const f32.const f32.add returns f32",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2.25))), instr.New(instr.F32_ADD),
		}),
		values: []types.Value{types.F32(3.75)},
	},
	{
		name: "f32.const f32.const f32.sub returns f32",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(5.5))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2.25))), instr.New(instr.F32_SUB),
		}),
		values: []types.Value{types.F32(3.25)},
	},
	{
		name: "f32.const f32.const f32.mul returns f32",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2.5))), instr.New(instr.F32_CONST, uint64(math.Float32bits(4))), instr.New(instr.F32_MUL),
		}),
		values: []types.Value{types.F32(10)},
	},
	{
		name: "f32.const f32.const f32.div returns f32",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(10))), instr.New(instr.F32_CONST, uint64(math.Float32bits(4))), instr.New(instr.F32_DIV),
		}),
		values: []types.Value{types.F32(2.5)},
	},
	{
		name: "f32.const f32.const f32.rem returns f32",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(-7))), instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_REM),
		}),
		values: []types.Value{types.F32(-1)},
	},
	{
		name: "f32.const f32.const f32.mod returns f32",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(-7))), instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_MOD),
		}),
		values: []types.Value{types.F32(2)},
	},
	{
		name: "f32.const f32.const f32.rem reports divide by zero",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1))), instr.New(instr.F32_CONST, 0), instr.New(instr.F32_REM),
		}),
		err: ErrDivideByZero,
	},
	{
		name: "f32.const f32.const f32.mod reports divide by zero",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1))), instr.New(instr.F32_CONST, 0), instr.New(instr.F32_MOD),
		}),
		err: ErrDivideByZero,
	},
	{
		name:    "f32.const f32.abs returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(-3.5))), instr.New(instr.F32_ABS)}),
		values:  []types.Value{types.F32(3.5)},
	},
	{
		name:    "f32.const f32.neg returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(3.5))), instr.New(instr.F32_NEG)}),
		values:  []types.Value{types.F32(-3.5)},
	},
	{
		name:    "f32.const f32.sqrt returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(9))), instr.New(instr.F32_SQRT)}),
		values:  []types.Value{types.F32(3)},
	},
	{
		name:    "f32.const f32.ceil returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(1.2))), instr.New(instr.F32_CEIL)}),
		values:  []types.Value{types.F32(2)},
	},
	{
		name:    "f32.const f32.floor returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(1.8))), instr.New(instr.F32_FLOOR)}),
		values:  []types.Value{types.F32(1)},
	},
	{
		name:    "f32.const f32.trunc returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(-1.8))), instr.New(instr.F32_TRUNC)}),
		values:  []types.Value{types.F32(-1)},
	},
	{
		name:    "f32.const f32.nearest returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(2.5))), instr.New(instr.F32_NEAREST)}),
		values:  []types.Value{types.F32(2)},
	},
	{
		name: "f32.const f32.const f32.min returns f32",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_CONST, uint64(math.Float32bits(5))), instr.New(instr.F32_MIN),
		}),
		values: []types.Value{types.F32(3)},
	},
	{
		name: "f32.const f32.const f32.max returns f32",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_CONST, uint64(math.Float32bits(5))), instr.New(instr.F32_MAX),
		}),
		values: []types.Value{types.F32(5)},
	},
	{
		name: "f32.const f32.const f32.copysign returns f32",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_CONST, uint64(math.Float32bits(-1))), instr.New(instr.F32_COPYSIGN),
		}),
		values: []types.Value{types.F32(-3)},
	},
	{
		name: "f32.const f32.const f32.eq returns i1",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_EQ),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "f32.const f32.const f32.ne returns i1",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_NE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "f32.const f32.const f32.lt returns i1",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_LT),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "f32.const f32.const f32.gt returns i1",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_GT),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "f32.const f32.const f32.le returns i1",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_LE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "f32.const f32.const f32.ge returns i1",
		program: program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2))), instr.New(instr.F32_GE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		name:    "f32.const f32.to_i32_s returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(-3.7))), instr.New(instr.F32_TO_I32_S)}),
		values:  []types.Value{types.I32(-3)},
	},
	{
		name:    "f32.const f32.to_i32_u returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(3.7))), instr.New(instr.F32_TO_I32_U)}),
		values:  []types.Value{types.I32(3)},
	},
	{
		name:    "f32.const f32.to_i64_s returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(-3.7))), instr.New(instr.F32_TO_I64_S)}),
		values:  []types.Value{types.I64(-3)},
	},
	{
		name:    "f32.const f32.to_i64_u returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(3.7))), instr.New(instr.F32_TO_I64_U)}),
		values:  []types.Value{types.I64(3)},
	},
	{
		name:    "f32.const f32.to_f64 returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))), instr.New(instr.F32_TO_F64)}),
		values:  []types.Value{types.F64(float64(float32(1.5)))},
	},
	{
		name:    "i32.const f32.reinterpret_i32 returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, uint64(math.Float32bits(1))), instr.New(instr.F32_REINTERPRET_I32)}),
		values:  []types.Value{types.F32(1)},
	},
	{
		name:    "f64.const returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(2.5))}),
		values:  []types.Value{types.F64(2.5)},
	},
	{
		name: "f64.const f64.const f64.add returns f64",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(1.5)), instr.New(instr.F64_CONST, math.Float64bits(2.25)), instr.New(instr.F64_ADD),
		}),
		values: []types.Value{types.F64(3.75)},
	},
	{
		name: "f64.const f64.const f64.sub returns f64",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(5.5)), instr.New(instr.F64_CONST, math.Float64bits(2.25)), instr.New(instr.F64_SUB),
		}),
		values: []types.Value{types.F64(3.25)},
	},
	{
		name: "f64.const f64.const f64.mul returns f64",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.F64_CONST, math.Float64bits(4)), instr.New(instr.F64_MUL),
		}),
		values: []types.Value{types.F64(10)},
	},
	{
		name: "f64.const f64.const f64.div returns f64",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(10)), instr.New(instr.F64_CONST, math.Float64bits(4)), instr.New(instr.F64_DIV),
		}),
		values: []types.Value{types.F64(2.5)},
	},
	{
		name: "f64.const f64.const f64.rem returns f64",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(-7)), instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_REM),
		}),
		values: []types.Value{types.F64(-1)},
	},
	{
		name: "f64.const f64.const f64.mod returns f64",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(-7)), instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_MOD),
		}),
		values: []types.Value{types.F64(2)},
	},
	{
		name: "f64.const f64.const f64.rem reports divide by zero",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(1)), instr.New(instr.F64_CONST, 0), instr.New(instr.F64_REM),
		}),
		err: ErrDivideByZero,
	},
	{
		name: "f64.const f64.const f64.mod reports divide by zero",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(1)), instr.New(instr.F64_CONST, 0), instr.New(instr.F64_MOD),
		}),
		err: ErrDivideByZero,
	},
	{
		name:    "f64.const f64.abs returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(-3.5)), instr.New(instr.F64_ABS)}),
		values:  []types.Value{types.F64(3.5)},
	},
	{
		name:    "f64.const f64.neg returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(3.5)), instr.New(instr.F64_NEG)}),
		values:  []types.Value{types.F64(-3.5)},
	},
	{
		name:    "f64.const f64.sqrt returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(9)), instr.New(instr.F64_SQRT)}),
		values:  []types.Value{types.F64(3)},
	},
	{
		name:    "f64.const f64.ceil returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(1.2)), instr.New(instr.F64_CEIL)}),
		values:  []types.Value{types.F64(2)},
	},
	{
		name:    "f64.const f64.floor returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(1.8)), instr.New(instr.F64_FLOOR)}),
		values:  []types.Value{types.F64(1)},
	},
	{
		name:    "f64.const f64.trunc returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(-1.8)), instr.New(instr.F64_TRUNC)}),
		values:  []types.Value{types.F64(-1)},
	},
	{
		name:    "f64.const f64.nearest returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.F64_NEAREST)}),
		values:  []types.Value{types.F64(2)},
	},
	{
		name: "f64.const f64.const f64.min returns f64",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_CONST, math.Float64bits(5)), instr.New(instr.F64_MIN),
		}),
		values: []types.Value{types.F64(3)},
	},
	{
		name: "f64.const f64.const f64.max returns f64",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_CONST, math.Float64bits(5)), instr.New(instr.F64_MAX),
		}),
		values: []types.Value{types.F64(5)},
	},
	{
		name: "f64.const f64.const f64.copysign returns f64",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_CONST, math.Float64bits(-1)), instr.New(instr.F64_COPYSIGN),
		}),
		values: []types.Value{types.F64(-3)},
	},
	{
		name: "f64.const f64.const f64.eq returns i1",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_EQ),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "f64.const f64.const f64.ne returns i1",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_NE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "f64.const f64.const f64.lt returns i1",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_LT),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "f64.const f64.const f64.gt returns i1",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_GT),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "f64.const f64.const f64.le returns i1",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_LE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "f64.const f64.const f64.ge returns i1",
		program: program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_CONST, math.Float64bits(2)), instr.New(instr.F64_GE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		name:    "f64.const f64.to_i32_s returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(-3.7)), instr.New(instr.F64_TO_I32_S)}),
		values:  []types.Value{types.I32(-3)},
	},
	{
		name:    "f64.const f64.to_i32_u returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(3.7)), instr.New(instr.F64_TO_I32_U)}),
		values:  []types.Value{types.I32(3)},
	},
	{
		name:    "f64.const f64.to_i64_s returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(-3.7)), instr.New(instr.F64_TO_I64_S)}),
		values:  []types.Value{types.I64(-3)},
	},
	{
		name:    "f64.const f64.to_i64_u returns i64",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(3.7)), instr.New(instr.F64_TO_I64_U)}),
		values:  []types.Value{types.I64(3)},
	},
	{
		name:    "f64.const f64.to_f32 returns f32",
		program: program.New([]instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(1.5)), instr.New(instr.F64_TO_F32)}),
		values:  []types.Value{types.F32(1.5)},
	},
	{
		name:    "i64.const f64.reinterpret_i64 returns f64",
		program: program.New([]instr.Instruction{instr.New(instr.I64_CONST, math.Float64bits(1)), instr.New(instr.F64_REINTERPRET_I64)}),
		values:  []types.Value{types.F64(1)},
	},
	{
		name: "i32.const i32.const i32.const array.new string.new_utf32 returns ref",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 72), instr.New(instr.I32_CONST, 105), instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.STRING_NEW_UTF32),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.String("Hi")},
	},
	{
		name:    "const.get string.len returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.STRING_LEN)}, program.WithConstants(types.String("Hi"))),
		values:  []types.Value{types.I32(2)},
	},
	{
		name: "const.get const.get string.concat returns ref",
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_CONCAT)},
			program.WithConstants(types.String("Hi"), types.String("There"))),
		values: []types.Value{types.String("HiThere")},
	},
	{
		name: "const.get const.get string.eq returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_EQ)},
			program.WithConstants(types.String("Go"), types.String("Go"))),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "const.get const.get string.ne returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_NE)},
			program.WithConstants(types.String("Go"), types.String("No"))),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "const.get const.get string.lt returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_LT)},
			program.WithConstants(types.String("Go"), types.String("No"))),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "const.get const.get string.gt returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_GT)},
			program.WithConstants(types.String("No"), types.String("Go"))),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "const.get const.get string.le returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_LE)},
			program.WithConstants(types.String("Go"), types.String("Go"))),
		values: []types.Value{types.I1(true)},
	},
	{
		name: "const.get const.get string.ge returns i1",
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.STRING_GE)},
			program.WithConstants(types.String("Go"), types.String("Go"))),
		values: []types.Value{types.I1(true)},
	},
	{
		name:    "const.get string.encode_utf32 returns ref",
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.STRING_ENCODE_UTF32)}, program.WithConstants(types.String("Hi"))),
		values:  []types.Value{types.TypedArray[int32]{72, 105}},
	},
	{
		name: "i32.const i32.const i32.const i32.const array.new returns ref",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 30), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW, 0),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.TypedArray[int32]{10, 20, 30}},
	},
	{
		name:    "i32.const array.new_default returns ref",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW_DEFAULT, 0)}, program.WithTypes(types.TypeI32Array)),
		values:  []types.Value{types.TypedArray[int32]{0, 0, 0}},
	},
	{
		name: "i32.const i32.const i32.const array.new array.len returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.ARRAY_LEN),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.I32(2)},
	},
	{
		name: "i32.const i32.const i32.const i32.const array.new i32.const array.get returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 100), instr.New(instr.I32_CONST, 200), instr.New(instr.I32_CONST, 300), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.I32(200)},
	},
	{
		name: "i32.const i32.const through i32.const array.set i32.const array.get returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 99), instr.New(instr.ARRAY_SET),
			instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.I32(99)},
	},
	{
		name: "i32.const array.new_default through i32.const array.fill i32.const array.get returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 7), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_FILL),
			instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.I32(7)},
	},
	{
		name: "i32.const array.new_default through i32.const array.copy i32.const array.get returns i32",
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
		name: "i32.const i32.const through i32.const i32.const i32.const array.append returns ref",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 4), instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_APPEND),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.TypedArray[int32]{1, 2, 3, 4}},
	},
	{
		name: "i32.const i32.const i32.const i32.const array.new i32.const array.delete returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_DELETE),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.I32(2)},
	},
	{
		name: "i32.const i32.const through array.new i32.const i32.const array.slice returns ref",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 30), instr.New(instr.I32_CONST, 40), instr.New(instr.I32_CONST, 4), instr.New(instr.ARRAY_NEW, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_SLICE),
		}, program.WithTypes(types.TypeI32Array)),
		values: []types.Value{types.TypedArray[int32]{20, 30}},
	},
	{
		name: "i32.const f64.const struct.new returns ref",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.STRUCT_NEW, 0),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)))),
		values: []types.Value{types.NewStruct(
			types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)),
			types.BoxI32(7), types.BoxF64(2.5),
		)},
	},
	{
		name: "struct.new_default returns ref",
		program: program.New([]instr.Instruction{instr.New(instr.STRUCT_NEW_DEFAULT, 0)},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)))),
		values: []types.Value{types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)))},
	},
	{
		name: "i32.const f64.const struct.new i32.const struct.get returns f64",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.STRUCT_NEW, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.STRUCT_GET),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)))),
		values: []types.Value{types.F64(2.5)},
	},
	{
		name: "i32.const f64.const through i32.const struct.set i32.const struct.get returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.STRUCT_NEW, 0),
			instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 99), instr.New(instr.STRUCT_SET),
			instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)))),
		values: []types.Value{types.I32(99)},
	},
	{
		name: "i32.const i32.const through i32.const map.new i32.const map.get returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 2), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(10)},
	},
	{
		name: "i32.const map.new_default map.len returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 4), instr.New(instr.MAP_NEW_DEFAULT, 0),
			instr.New(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(0)},
	},
	{
		name: "i32.const i32.const i32.const i32.const i32.const map.new map.len returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 2), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(2)},
	},
	{
		name: "i32.const i32.const i32.const map.new i32.const map.get returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.I32_CONST, 2), instr.New(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(0)},
	},
	{
		name: "i32.const i32.const i32.const map.new i32.const map.lookup returns i1 i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_LOOKUP),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I1(true), types.I32(10)},
	},
	{
		name: "i32.const i32.const through i32.const i32.const map.set map.len returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 20), instr.New(instr.MAP_SET),
			instr.New(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(2)},
	},
	{
		name: "i32.const i32.const through dup i32.const map.delete map.len returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 2), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_DELETE),
			instr.New(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(1)},
	},
	{
		name: "i32.const i32.const i32.const map.new dup map.clear map.len returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.DUP),
			instr.New(instr.MAP_CLEAR),
			instr.New(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(0)},
	},
	{
		name: "i32.const i32.const through i32.const map.new map.keys array.len returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 2), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.MAP_KEYS), instr.New(instr.ARRAY_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(2)},
	},
	{
		name: "i32.const i32.const i32.const map.new map.iter coro.value returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
			instr.New(instr.MAP_ITER), instr.New(instr.CORO_VALUE),
		}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
		values: []types.Value{types.I32(1)},
	},
	{
		name: "i32.const throw i32.const returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 99),
			instr.New(instr.THROW),
			instr.New(instr.I32_CONST, 0),
		}, program.WithHandlers(instr.Handler{Start: 0, End: 6, Catch: 11, Depth: 0})),
		values: []types.Value{types.I32(99)},
	},
	{
		name:    "i32.const i32.const error.new returns ref",
		program: program.New([]instr.Instruction{instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 7), instr.New(instr.ERROR_NEW)}),
		values:  []types.Value{types.NewError(types.ErrorCode(7), "5", types.BoxI32(5))},
	},
	{
		name: "i32.const i32.const error.new error.get returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 7), instr.New(instr.ERROR_NEW), instr.New(instr.ERROR_GET),
		}),
		values: []types.Value{types.I32(5)},
	},
	{
		name: "i32.const i32.const error.new error.code returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 7), instr.New(instr.ERROR_NEW), instr.New(instr.ERROR_CODE),
		}),
		values: []types.Value{types.I32(7)},
	},
	{
		name:    "const.get string.iter coro.value returns i32",
		program: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.STRING_ITER), instr.New(instr.CORO_VALUE)}, program.WithConstants(types.String("Hi"))),
		values:  []types.Value{types.I32(72)},
	},
	{
		name: "const.get i32.const array.get returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.ARRAY_GET),
		}, program.WithConstants(types.TypedArray[int32]{3, 5})),
		values: []types.Value{types.I32(5)},
	},
	{
		name: "const.get i32.const struct.get returns i32",
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.STRUCT_GET),
		}, program.WithConstants(types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32)), types.BoxI32(7)))),
		values: []types.Value{types.I32(7)},
	},
	{
		name: "const.get ref.get returns i64",
		program: program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.REF_GET),
		}, program.WithConstants(types.I64(math.MaxInt64))),
		values: []types.Value{types.I64(math.MaxInt64)},
	},
	{
		name: "i32.const i32.const const.get call returns i32",
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
		name: "i32.const array.new_default i32.const array.get reports index out of range",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 5), instr.New(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array)),
		err: ErrIndexOutOfRange,
	},
	{
		name: "i32.const array.new_default i32.const i32.const array.set reports index out of range",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 9), instr.New(instr.ARRAY_SET),
		}, program.WithTypes(types.TypeI32Array)),
		err: ErrIndexOutOfRange,
	},
	{
		name: "i32.const array.new_default i32.const i32.const i32.const array.fill reports index out of range",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 7), instr.New(instr.I32_CONST, 5), instr.New(instr.ARRAY_FILL),
		}, program.WithTypes(types.TypeI32Array)),
		err: ErrIndexOutOfRange,
	},
	{
		name: "i32.const array.new_default i32.const array.delete reports index out of range",
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 5), instr.New(instr.ARRAY_DELETE),
		}, program.WithTypes(types.TypeI32Array)),
		err: ErrIndexOutOfRange,
	},
	{
		name: "i32.const array.new_default through array.new_default i32.const i32.const array.copy reports index out of range",
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
		name: "i32.const i32.const through array.new i32.const i32.const array.copy reports index out of range",
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

func TestInterpreter_Run(t *testing.T) {
	t.Run("covers every runtime opcode", func(t *testing.T) {
		covered := make(map[instr.Opcode]struct{})
		names := make(map[string]struct{})
		for _, tt := range runTests {
			require.NotEmpty(t, tt.name)
			_, duplicate := names[tt.name]
			require.False(t, duplicate, "duplicate runtime case %q", tt.name)
			names[tt.name] = struct{}{}
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
	})

	for _, tt := range runTests {
		t.Run(tt.name, func(t *testing.T) {
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
	run := func(t *testing.T, prog *program.Program, opts ...func(*option)) outcome {
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
		runHost := func(opts ...func(*option)) (types.Value, int) {
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
		require.Equal(t, 1, i.rc[1]) // selected ref survives on the stack
		require.Equal(t, 0, i.rc[2]) // discarded ref released to zero
	})

	t.Run("GLOBAL_TEE retains the ref stored into the global slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.GLOBAL_TEE, 0), // duplicates ownership: stack + global
			instr.New(instr.DROP),          // drop stack copy; global still owns
		}, program.WithGlobals(types.TypeRef))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		g, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, 1, g.Ref())
		require.Equal(t, 1, i.rc[1]) // global slot keeps the ref alive
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
		require.Equal(t, 1, i.rc[1]) // local slot keeps the ref alive
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

		require.Equal(t, 0, i.rc[1])
		require.Equal(t, 0, i.rc[2])
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

		require.Equal(t, 0, i.rc[1])
		require.Equal(t, 0, i.rc[2])
	})

	t.Run("REF_TEST releases the consumed ref", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.REF_TEST, 0),
		}, program.WithTypes(types.TypeI32))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		require.Equal(t, 0, i.rc[1])
	})

	t.Run("REF_IS_NULL releases the consumed ref", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.REF_IS_NULL),
		})
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		require.Equal(t, 0, i.rc[1])
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
		require.Equal(t, 1, i.sp)
	})

	t.Run("STRUCT_NEW_DEFAULT reports stack overflow before mutating sp", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.STRUCT_NEW_DEFAULT, 0),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32))))
		i := New(prog, WithStack(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrStackOverflow)
		require.Equal(t, 1, i.sp)
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
		}, program.WithGlobals(types.TypeI32, types.TypeF64, types.TypeRef))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 3, i.sp)
		require.Equal(t, types.BoxI32(2), i.stack[0])
		require.Equal(t, types.BoxF64(0), i.stack[1])
		require.Equal(t, types.BoxedNull, i.stack[2])
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
		require.Equal(t, 1, i.sp)
		require.Equal(t, types.BoxI32(7), i.stack[i.sp-1])
	})

	t.Run("GLOBAL_TEE retains the ref stored into a declared ref global", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW), // heap[1]
			instr.New(instr.GLOBAL_TEE, 0),
			instr.New(instr.DROP),
		}, program.WithGlobals(types.TypeRef))
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		g, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, 1, g.Ref())
		require.Equal(t, 1, i.rc[1])
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

		require.Equal(t, 0, i.rc[2]) // every overwritten element is released,
		require.Equal(t, 0, i.rc[3]) // not just the first one
		require.Equal(t, 0, i.rc[4])
		require.Equal(t, 3, i.rc[5]) // fill value owned once per filled slot
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
		hostFn := NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeRef}, Returns: []types.Type{types.TypeI32}},
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
		require.Equal(t, 0, i.rc[2]) // arg not returned: host cleanup released it
	})

	t.Run("host call releases a ref param the callee does not return (generic, exact)", func(t *testing.T) {
		hostFn := NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeRef}, Returns: []types.Type{types.TypeI32}},
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
		require.Equal(t, 0, i.rc[2])
	})

	t.Run("host call releases the consumed callable ref on fused and generic paths", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			opts []func(*option)
		}{
			{name: "fused"},
			{name: "generic", opts: []func(*option){WithTick(1)}},
		} {
			t.Run(tt.name, func(t *testing.T) {
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
				require.Equal(t, 1, i.rc[1])
			})
		}
	})

	t.Run("generic host call can return the consumed callable ref", func(t *testing.T) {
		hostFn := NewHostFunction(&types.FunctionType{Returns: []types.Type{types.TypeRef}},
			func(i *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{i.stack[i.sp-1]}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(hostFn))
		i := New(prog, WithTick(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 2, i.rc[1])
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
		require.Equal(t, 0, i.rc[2]) // promoted i64 arg released: I64 params keep the generic scanning path
	})

	t.Run("UPVAL_GET retains a ref capture (generic path)", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{}).
			WithCaptures(types.TypeRef).Emit(
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
			if len(i.heap) > 2 && i.rc[2] > maxRC {
				maxRC = i.rc[2]
			}
			return nil
		}))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 2, maxRC) // UPVAL_GET's retainBox held the capture live alongside its pushed copy
	})

	t.Run("UPVAL_SET releases a ref capture when overwritten (generic path)", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			WithCaptures(types.TypeRef).Emit(
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
		require.Equal(t, 0, i.rc[2]) // old ref capture released on overwrite
	})

	t.Run("UPVAL_SET releases a promoted i64 capture even though I64 is declared (not the scalar fast path)", func(t *testing.T) {
		oldHuge := int64(1) << 50
		newHuge := int64(1) << 51
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI64}}).
			WithCaptures(types.TypeI64).Emit(
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
		require.Equal(t, 0, i.rc[2]) // old promoted capture released: I64 captures keep the generic ref-aware path
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

	t.Run("fused LOCAL_GET+LOCAL_GET binop computes correctly for i64 (interp-only)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(5)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.I64_CONST, i64operand(3)), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I64_ADD),
		}, program.WithLocals(types.TypeI64, types.TypeI64))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I64(8), v)
	})

	t.Run("fused LOCAL_GET+LOCAL_GET binop computes correctly for f32 (interp-only)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(5))), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.F32_ADD),
		}, program.WithLocals(types.TypeF32, types.TypeF32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.F32(8), v)
	})

	t.Run("fused LOCAL_GET+LOCAL_GET binop computes correctly for f64 (interp-only)", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(5)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.F64_CONST, math.Float64bits(3)), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.F64_ADD),
		}, program.WithLocals(types.TypeF64, types.TypeF64))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.F64(8), v)
	})

	t.Run("promoted I64 stack ownership matches exact execution across fused consumers", func(t *testing.T) {
		type snapshot struct {
			ip      int
			fp      int
			sp      int
			stack   []types.Boxed
			globals []types.Boxed
			live    int
		}
		run := func(t *testing.T, prog *program.Program, opts ...func(*option)) snapshot {
			t.Helper()
			i := New(prog, opts...)
			defer i.Close()
			require.NoError(t, i.Run(context.Background()))
			live := 0
			for _, rc := range i.rc[1:] {
				if rc > 0 {
					live += rc
				}
			}
			return snapshot{
				ip:      i.fr.ip,
				fp:      i.fp,
				sp:      i.sp,
				stack:   append([]types.Boxed(nil), i.stack[:i.sp]...),
				globals: append([]types.Boxed(nil), i.globals...),
				live:    live,
			}
		}

		huge := int64(1) << 50
		cases := []struct {
			name string
			prog *program.Program
		}{
			{
				name: "eqz branch",
				prog: program.New([]instr.Instruction{
					instr.New(instr.I64_CONST, i64operand(huge)),
					instr.New(instr.I64_EQZ),
					instr.New(instr.BR_IF, 0),
				}),
			},
			{
				name: "compare branch",
				prog: program.New([]instr.Instruction{
					instr.New(instr.I64_CONST, i64operand(huge)),
					instr.New(instr.I64_CONST, i64operand(huge)),
					instr.New(instr.I64_EQ),
					instr.New(instr.BR_IF, 0),
				}),
			},
			{
				name: "stack and local binary",
				prog: program.New([]instr.Instruction{
					instr.New(instr.I64_CONST, i64operand(1)),
					instr.New(instr.LOCAL_SET, 0),
					instr.New(instr.I64_CONST, i64operand(huge)),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I64_ADD),
					instr.New(instr.DROP),
				}, program.WithLocals(types.TypeI64)),
			},
		}
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				exact := run(t, tt.prog, WithTick(1))
				fused := run(t, tt.prog, WithThreshold(-1))
				require.Equal(t, exact, fused)
			})
		}
	})

	t.Run("fused ref drops preserve exact ownership", func(t *testing.T) {
		type snapshot struct {
			sp       int
			stack    []types.Boxed
			live     int
			interned int
		}
		run := func(t *testing.T, prog *program.Program, opts ...func(*option)) snapshot {
			t.Helper()
			i := New(prog, opts...)
			defer i.Close()
			require.NoError(t, i.Run(context.Background()))
			live := 0
			for _, rc := range i.rc[1:] {
				if rc > 0 {
					live += rc
				}
			}
			return snapshot{
				sp:       i.sp,
				stack:    append([]types.Boxed(nil), i.stack[:i.sp]...),
				live:     live,
				interned: len(i.interned),
			}
		}

		fn := types.NewFunctionBuilder(nil).Emit(instr.New(instr.RETURN)).MustBuild()
		cases := []struct {
			name string
			prog *program.Program
		}{
			{
				name: "local ref",
				prog: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 7),
					instr.New(instr.REF_NEW),
					instr.New(instr.LOCAL_SET, 0),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.DROP),
				}, program.WithLocals(types.TypeRef)),
			},
			{
				name: "function constant",
				prog: program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.DROP),
				}, program.WithConstants(fn)),
			},
			{
				name: "string constant",
				prog: program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.DROP),
				}, program.WithConstants(types.String("value"))),
			},
		}
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				exact := run(t, tt.prog, WithTick(1))
				fused := run(t, tt.prog, WithThreshold(-1))
				require.Equal(t, exact, fused)
			})
		}
	})

	t.Run("fused numeric traps preserve exact state", func(t *testing.T) {
		type snapshot struct {
			ip    int
			fp    int
			sp    int
			stack []types.Boxed
			live  int
		}
		run := func(t *testing.T, prog *program.Program, opts ...func(*option)) (snapshot, error) {
			t.Helper()
			i := New(prog, opts...)
			defer i.Close()
			err := i.Run(context.Background())
			live := 0
			for _, rc := range i.rc[1:] {
				if rc > 0 {
					live += rc
				}
			}
			return snapshot{
				ip:    i.fr.ip,
				fp:    i.fp,
				sp:    i.sp,
				stack: append([]types.Boxed(nil), i.stack[:i.sp]...),
				live:  live,
			}, err
		}

		huge := int64(1) << 50
		cases := []struct {
			name string
			prog *program.Program
		}{
			{
				name: "i32 constants",
				prog: program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 90),
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.I32_DIV_S),
				}),
			},
			{
				name: "promoted i64 constants",
				prog: program.New([]instr.Instruction{
					instr.New(instr.I64_CONST, i64operand(huge)),
					instr.New(instr.I64_CONST, 0),
					instr.New(instr.I64_DIV_S),
				}),
			},
			{
				name: "promoted i64 local",
				prog: program.New([]instr.Instruction{
					instr.New(instr.I64_CONST, i64operand(huge)),
					instr.New(instr.LOCAL_SET, 0),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I64_CONST, 0),
					instr.New(instr.I64_DIV_S),
				}, program.WithLocals(types.TypeI64)),
			},
		}
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				exact, exactErr := run(t, tt.prog, WithTick(1))
				fused, fusedErr := run(t, tt.prog, WithThreshold(-1))
				require.ErrorIs(t, exactErr, ErrDivideByZero)
				require.ErrorIs(t, fusedErr, ErrDivideByZero)
				require.Equal(t, exact, fused)
			})
		}
	})

	t.Run("promoted I64 local keeps a balanced refcount across fused const-binop and local-local binop", func(t *testing.T) {
		huge := int64(1) << 50
		prog := program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, i64operand(huge)), instr.New(instr.LOCAL_SET, 0), // heap[1] owned by local0
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.I64_ADD), instr.New(instr.DROP),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_ADD), instr.New(instr.DROP),
		}, program.WithLocals(types.TypeI64))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 1, i.rc[1]) // local slot still owns exactly one reference after both fused reads
	})

	t.Run("promoted I64 slot keeps ownership across repeated fused rhs reads", func(t *testing.T) {
		// Regression: the fused rhs loaders previously used unboxI64, whose
		// internal release dropped the slot's own reference — the first fused
		// read freed the heap I64 and the second read was use-after-free.
		huge := int64(1) << 62
		t.Run("local", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)), instr.New(instr.LOCAL_SET, 0), // heap[1] owned by local0
				instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_ADD), instr.New(instr.DROP),
				instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_ADD), instr.New(instr.DROP),
			}, program.WithLocals(types.TypeI64))
			i := New(prog, WithThreshold(-1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			require.Equal(t, 1, i.rc[1]) // slot still owns exactly one reference after both fused reads
		})
		t.Run("global", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)), instr.New(instr.GLOBAL_SET, 0), // heap[1] owned by global0
				instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I64_ADD), instr.New(instr.DROP),
				instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I64_ADD), instr.New(instr.DROP),
			}, program.WithGlobals(types.TypeI64))
			i := New(prog, WithThreshold(-1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			require.Equal(t, 1, i.rc[1])
		})
		t.Run("upval", func(t *testing.T) {
			fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI64}}).
				WithCaptures(types.TypeI64).Emit(
				instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.UPVAL_GET, 0), instr.New(instr.I64_ADD), instr.New(instr.DROP),
				instr.New(instr.I64_CONST, i64operand(1)), instr.New(instr.UPVAL_GET, 0), instr.New(instr.I64_ADD),
				instr.New(instr.RETURN),
			).MustBuild()
			prog := program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)), // heap[1] is fn; heap[2] is the promoted capture
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CLOSURE_NEW),
				instr.New(instr.CALL),
			}, program.WithConstants(fn))
			i := New(prog, WithThreshold(-1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I64(huge+1), v)
		})
		t.Run("global pair fusion lhs+rhs", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(huge)), instr.New(instr.GLOBAL_SET, 0), // heap[1] owned by global0
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I64_ADD), instr.New(instr.DROP),
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I64_ADD),
			}, program.WithGlobals(types.TypeI64))
			i := New(prog, WithThreshold(-1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I64(2*huge), v)
			require.Equal(t, 1, i.rc[1])
		})
	})

	t.Run("fused UPVAL_GET+CONST binop computes correctly for i32/i64/f32/f64 (interp-only)", func(t *testing.T) {
		cases := []struct {
			name    string
			capture types.Type
			body    func(cst uint64) []instr.Instruction
			cst     uint64
			want    types.Value
		}{
			{
				name:    "i32",
				capture: types.TypeI32,
				body: func(cst uint64) []instr.Instruction {
					return []instr.Instruction{instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, cst), instr.New(instr.I32_ADD), instr.New(instr.RETURN)}
				},
				cst:  i32operand(3),
				want: types.I32(8),
			},
			{
				name:    "i64",
				capture: types.TypeI64,
				body: func(cst uint64) []instr.Instruction {
					return []instr.Instruction{instr.New(instr.UPVAL_GET, 0), instr.New(instr.I64_CONST, cst), instr.New(instr.I64_ADD), instr.New(instr.RETURN)}
				},
				cst:  i64operand(3),
				want: types.I64(8),
			},
			{
				name:    "f32",
				capture: types.TypeF32,
				body: func(cst uint64) []instr.Instruction {
					return []instr.Instruction{instr.New(instr.UPVAL_GET, 0), instr.New(instr.F32_CONST, cst), instr.New(instr.F32_ADD), instr.New(instr.RETURN)}
				},
				cst:  uint64(math.Float32bits(3)),
				want: types.F32(8),
			},
			{
				name:    "f64",
				capture: types.TypeF64,
				body: func(cst uint64) []instr.Instruction {
					return []instr.Instruction{instr.New(instr.UPVAL_GET, 0), instr.New(instr.F64_CONST, cst), instr.New(instr.F64_ADD), instr.New(instr.RETURN)}
				},
				cst:  math.Float64bits(3),
				want: types.F64(8),
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{tc.capture}}).
					WithCaptures(tc.capture).Emit(tc.body(tc.cst)...).MustBuild()
				var seed instr.Instruction
				switch tc.capture {
				case types.TypeI32:
					seed = instr.New(instr.I32_CONST, i32operand(5))
				case types.TypeI64:
					seed = instr.New(instr.I64_CONST, i64operand(5))
				case types.TypeF32:
					seed = instr.New(instr.F32_CONST, uint64(math.Float32bits(5)))
				case types.TypeF64:
					seed = instr.New(instr.F64_CONST, math.Float64bits(5))
				}
				prog := program.New([]instr.Instruction{
					seed,
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CLOSURE_NEW),
					instr.New(instr.CALL),
				}, program.WithConstants(fn))
				i := New(prog, WithThreshold(-1))
				defer i.Close()

				require.NoError(t, i.Run(context.Background()))
				v, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, tc.want, v)
			})
		}
	})

	t.Run("fused UPVAL_GET+LOCAL_GET binop computes correctly for i32 (interp-only)", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			WithCaptures(types.TypeI32).
			WithLocals(types.TypeI32).Emit(
			instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.UPVAL_GET, 0), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, i32operand(5)),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(8), v)
	})

	t.Run("fused GLOBAL_GET+source pair binop computes correctly (interp-only)", func(t *testing.T) {
		t.Run("global+const i32", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.I32_ADD),
			}, program.WithGlobals(types.TypeI32))
			i := New(prog, WithThreshold(-1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(8), v)
		})
		t.Run("global+global i32", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, i32operand(5)), instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.I32_CONST, i32operand(3)), instr.New(instr.GLOBAL_SET, 1),
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.I32_ADD),
			}, program.WithGlobals(types.TypeI32, types.TypeI32))
			i := New(prog, WithThreshold(-1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(8), v)
		})
		t.Run("global+upval i64", func(t *testing.T) {
			fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI64}}).
				WithCaptures(types.TypeI64).Emit(
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.UPVAL_GET, 0), instr.New(instr.I64_ADD),
				instr.New(instr.RETURN),
			).MustBuild()
			prog := program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(5)), instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.I64_CONST, i64operand(3)),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CLOSURE_NEW),
				instr.New(instr.CALL),
			}, program.WithConstants(fn), program.WithGlobals(types.TypeI64))
			i := New(prog, WithThreshold(-1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I64(8), v)
		})
	})

	t.Run("fused UPVAL_GET+source pair binop computes correctly (interp-only)", func(t *testing.T) {
		t.Run("upval+const f32", func(t *testing.T) {
			fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeF32}}).
				WithCaptures(types.TypeF32).Emit(
				instr.New(instr.UPVAL_GET, 0), instr.New(instr.F32_CONST, uint64(math.Float32bits(3))), instr.New(instr.F32_ADD),
				instr.New(instr.RETURN),
			).MustBuild()
			prog := program.New([]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(5))),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CLOSURE_NEW),
				instr.New(instr.CALL),
			}, program.WithConstants(fn))
			i := New(prog, WithThreshold(-1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F32(8), v)
		})
		t.Run("upval+upval i32", func(t *testing.T) {
			fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				WithCaptures(types.TypeI32, types.TypeI32).Emit(
				instr.New(instr.UPVAL_GET, 0), instr.New(instr.UPVAL_GET, 1), instr.New(instr.I32_ADD),
				instr.New(instr.RETURN),
			).MustBuild()
			prog := program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, i32operand(5)),
				instr.New(instr.I32_CONST, i32operand(3)),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CLOSURE_NEW),
				instr.New(instr.CALL),
			}, program.WithConstants(fn))
			i := New(prog, WithThreshold(-1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(8), v)
		})
	})

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

	t.Run("preserves same reference", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeRef))
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
		prog := program.New(nil, program.WithGlobals(types.TypeRef))
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
		prog := program.New(nil, program.WithLocals(types.TypeRef))
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
		prog := program.New(nil, program.WithLocals(types.TypeRef))
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
			require.Equal(t, 0, i.frames[0].bp)
			require.Equal(t, 0, i.frames[0].ip)
			require.Same(t, &i.frames[0], i.fr)
		}
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), v)
	})

	t.Run("restores declared-kind zero globals", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeI32, types.TypeRef))
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

		require.Equal(t, i.base, len(i.heap))
		require.NoError(t, i.Push(types.String("temporary")))
		require.Greater(t, len(i.heap), i.base)

		i.Reset()
		require.Equal(t, i.base, len(i.heap))
		require.Equal(t, 0, i.sp)
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

		b := types.NewFunctionBuilder(nil).WithParams(types.TypeI32).WithReturns(types.TypeI32)
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
		i := New(prog, WithFrame(nativeFrameLimit+2), WithTick(1), WithThreshold(0))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(nativeFrameLimit), v)
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))

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
				t.Fatalf("native overflow must not increment %s", metric.Name)
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

		i := New(program.New(nil), WithHeap(capacity), WithMaxHeap(capacity))
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

		i := New(program.New(nil), WithHeap(capacity), WithMaxHeap(capacity))
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

		i := New(program.New(nil), WithHeap(capacity), WithMaxHeap(4*heapRunway))
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

func TestWithMaxHeap(t *testing.T) {
	t.Run("rejects live heap at limit", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.REF_NEW)})
		i := New(prog, WithMaxHeap(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrHeapExhausted)
	})

	t.Run("preserves host-owned reference", func(t *testing.T) {
		i := New(program.New(nil), WithHeap(2), WithMaxHeap(2))
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
		mid := types.NewArray(types.NewArrayType(types.TypeRef), types.BoxRef(leafAddr))
		root := types.NewArray(types.NewArrayType(types.TypeRef), types.BoxRef(midAddr), types.BoxRef(midAddr))
		prog := program.New(nil, program.WithConstants(leaf, mid, root))
		i := New(prog, WithHeap(4), WithMaxHeap(4))
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
		i := New(program.New(nil), WithHeap(3), WithMaxHeap(3))
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
		i := New(program.New(nil), WithHeap(3), WithMaxHeap(3))
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
		i := New(program.New(nil), WithHeap(2), WithMaxHeap(2))
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
		i := New(program.New(nil), WithHeap(3), WithMaxHeap(3))
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
		i := New(program.New(nil), WithHeap(4), WithMaxHeap(4))
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

	t.Run("records entry only from actual entry state", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.NOP),
			instr.New(instr.NOP),
		})
		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()

		i.fr.ip = 1
		require.NoError(t, i.compile(anchor{}))
		i.tracer.mu.Lock()
		_, recorded := i.tracer.trees[anchor{addr: 0, ip: 0}]
		i.tracer.mu.Unlock()
		require.False(t, recorded)

		i.fr.ip = 0
		require.NoError(t, i.observe(i.fr))
		require.NotNil(t, i.tracer.rootAt(anchor{addr: 0, ip: 0}))
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
		require.NotNil(t, i.exits[anchor{addr: 0, ip: 0}])
		require.Equal(t, float64(1), i.samples.Value("vm_jit_emits_total"))
	})

	t.Run("jits select with comparison condition", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10),
			instr.New(instr.I32_CONST, 20),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_LT_S),
			instr.New(instr.SELECT),
		})
		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(10), v)
		require.Equal(t, float64(1), i.samples.Value("vm_jit_emits_total"))
	})

	t.Run("jits oversized top-level code in bounded segments", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		code := make([]instr.Instruction, 0, opLimit+3)
		for range opLimit/2 + 1 {
			code = append(code, instr.New(instr.I32_CONST, 1), instr.New(instr.DROP))
		}
		code = append(code, instr.New(instr.I32_CONST, 7))
		i := New(program.New(code), WithTick(1), WithThreshold(0))
		defer i.Close()

		for range exitThreshold + 3 {
			i.Reset()
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(7), v)
		}
		emits := i.samples.Value("vm_jit_emits_total")
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
		eval, err := types.NewFunctionBuilder(nil).WithParams(types.TypeI32).WithReturns(types.TypeI32).
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

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		addr := i.constants[0].Ref()

		// Warm the callee entry: run until its native entry installs.
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(41)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(42), v)
			if i.exits[anchor{addr: addr, ip: 0}] != nil {
				break
			}
		}
		require.NotNil(t, i.exits[anchor{addr: addr, ip: 0}], "callee entry never warmed")

		// Once warm, the entry dispatches natively and the threaded safepoint no
		// longer samples it: the sample count must not grow across further runs.
		warm := i.samples.Samples(addr)
		for range 32 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(41)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(42), v)
		}
		require.Equal(t, warm, i.samples.Samples(addr))
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
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
		require.NotNil(t, i.exits[anchor{addr: 0, ip: 0}])
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_attempts_total"), float64(1))
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
		require.NotNil(t, i.exits[anchor{addr: 0, ip: 0}])
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_attempts_total"), float64(1))
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
		require.NotNil(t, i.exits[anchor{addr: 0, ip: 0}])
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_attempts_total"), float64(1))
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
				require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
				require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
				require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
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
		if id < 0 {
			require.Greater(t, i.samples.Value("vm_jit_emits_total"), float64(0))
			return
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

	t.Run("jits learned br_if continuation over a live ref value", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		row := []float64{10, 20}
		b := types.NewFunctionBuilder(nil).
			WithParams(types.TypeI32, types.TypeF64Array).
			WithReturns(types.TypeF64)
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

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		root := anchor{addr: i.constants[1].Ref(), ip: 0}

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

		id := -1
		for range exitThreshold * 4 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(-1)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(20), v)

			tree := i.tracer.rootAt(root)
			require.NotNil(t, tree)
			for bid, branch := range tree.branches {
				if branch == nil || tree.hits[bid] < exitThreshold {
					continue
				}
				for _, op := range branch.ops {
					if op.op != instr.I32_CONST || op.fn < 0 || op.fn >= len(i.instrs) {
						continue
					}
					code := i.instrs[op.fn]
					if op.ip+5 <= len(code) && int32(instr.ParseI32(code, op.ip+1)) == 1 {
						id = bid
					}
				}
			}
			if id >= 0 {
				break
			}
		}
		require.GreaterOrEqual(t, id, 0, "no branch reading array index 1 was learned")
		hits := i.tracer.rootAt(root).hits[id]
		require.Equal(t, int64(exitThreshold), hits)

		for range 3 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(-1)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(20), v)
		}
		require.Equal(t, hits, i.tracer.rootAt(root).hits[id])
	})

	t.Run("deopts array get on negative index", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		row := []float64{7}
		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeI32, types.TypeF64Array).
			WithReturns(types.TypeF64)
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

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(0)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(7), v)
		}
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))

		i.Reset()
		require.NoError(t, i.Push(types.I32(-1)))
		require.ErrorIs(t, i.Run(context.Background()), ErrIndexOutOfRange)
	})

	t.Run("jits constant nonzero divisors", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		for _, tt := range []struct {
			name  string
			typ   types.Type
			cnst  instr.Instruction
			div   instr.Opcode
			value types.Value
			want  types.Value
		}{
			{
				name:  "i32",
				typ:   types.TypeI32,
				cnst:  instr.New(instr.I32_CONST, 3),
				div:   instr.I32_DIV_S,
				value: types.I32(90),
				want:  types.I32(30),
			},
			{
				name:  "i64",
				typ:   types.TypeI64,
				cnst:  instr.New(instr.I64_CONST, 3),
				div:   instr.I64_DIV_S,
				value: types.I64(90),
				want:  types.I64(30),
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				eval := types.NewFunctionBuilder(nil).
					WithParams(tt.typ).
					WithReturns(tt.typ)
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

				i := New(prog, WithTick(1), WithThreshold(0))
				defer i.Close()
				for range 8 {
					i.Reset()
					require.NoError(t, i.Push(tt.value))
					require.NoError(t, i.Run(context.Background()))
					got, err := i.Pop()
					require.NoError(t, err)
					require.Equal(t, tt.want, got)
				}
				require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
			})
		}
	})

	t.Run("deopts variable zero divisors", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		for _, tt := range []struct {
			name  string
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
				name:  "i32",
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
				name:  "i64",
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
			t.Run(tt.name, func(t *testing.T) {
				eval := types.NewFunctionBuilder(nil).
					WithParams(tt.typ, tt.typ).
					WithReturns(tt.typ)
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

				i := New(prog, WithTick(1), WithThreshold(0))
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
				require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))

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
	})

	t.Run("deopts array len on shape mismatch", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeRef).
			WithReturns(types.TypeI32)
		fn, err := eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.ARRAY_LEN)).
			Emit(instr.New(instr.RETURN)).
			Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		root := anchor{addr: i.constants[0].Ref(), ip: 0}
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.TypedArray[int32]{1, 2}))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(2), got)
		}
		require.NotNil(t, i.exits[root])

		i.Reset()
		require.NoError(t, i.Push(types.NewArray(types.NewArrayType(types.TypeI32), types.BoxI32(1), types.BoxI32(2), types.BoxI32(3))))
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(3), got)

		var hits int64
		tree := i.tracer.rootAt(root)
		require.NotNil(t, tree)
		for _, hit := range tree.hits {
			hits += hit
		}
		require.Greater(t, hits, int64(0))
	})

	t.Run("deopts struct get on type mismatch", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeRef).
			WithReturns(types.TypeI32)
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

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		root := anchor{addr: i.constants[0].Ref(), ip: 0}
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
		require.NotNil(t, i.exits[root])

		i.Reset()
		require.NoError(t, i.Push(types.NewStruct(second, types.BoxI32(9))))
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(9), got)

		var hits int64
		tree := i.tracer.rootAt(root)
		require.NotNil(t, tree)
		for _, hit := range tree.hits {
			hits += hit
		}
		require.Greater(t, hits, int64(0))
	})

	t.Run("deopts string len on type mismatch", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeRef).
			WithReturns(types.TypeI32)
		fn, err := eval.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.STRING_LEN)).
			Emit(instr.New(instr.RETURN)).
			Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		root := anchor{addr: i.constants[0].Ref(), ip: 0}
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.String("hello")))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(5), got)
		}
		require.NotNil(t, i.exits[root])

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
			WithParams(arrTyp, types.TypeString).
			WithReturns(types.TypeI32)
		// Store the same host-pushed local into index 0 twice in a row: the
		// second ARRAY_SET observes old==val (both reads of LOCAL_GET 1 name
		// the same ref), exercising releaseBoxUnlessEqual's aliased-store path
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

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		for range 5000 {
			i.Reset()
			arr := types.NewArray(arrTyp, types.BoxedNull, types.BoxedNull)
			require.NoError(t, i.Push(arr))
			require.NoError(t, i.Push(types.String("stored")))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(len("stored")), got)
		}
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("jits struct set for a ref-field struct argument", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		typ := types.NewStructType(types.NewStructField(types.TypeString))
		eval := types.NewFunctionBuilder(nil).
			WithParams(typ, types.TypeString).
			WithReturns(types.TypeI32)
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

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		for range 5000 {
			i.Reset()
			s := types.NewStruct(typ, types.BoxedNull)
			require.NoError(t, i.Push(s))
			require.NoError(t, i.Push(types.String("stored")))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(len("stored")), got)
		}
		require.GreaterOrEqual(t, i.samples.Value("vm_jit_emits_total"), float64(1))
	})

	t.Run("continues i64 array get through arithmetic", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeI64Array).
			WithReturns(types.TypeI64)
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

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		root := anchor{addr: i.constants[0].Ref(), ip: 0}
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.TypedArray[int64]{41}))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I64(42), got)
		}
		require.NotNil(t, i.exits[root])

		var hits int64
		tree := i.tracer.rootAt(root)
		require.NotNil(t, tree)
		for _, hit := range tree.hits {
			hits += hit
		}
		require.Equal(t, int64(0), hits)
	})

	t.Run("deopts after i64 array get with stack shape intact", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeI64Array).
			WithReturns(types.TypeI64)
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

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		root := anchor{addr: i.constants[0].Ref(), ip: 0}
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.TypedArray[int64]{1<<48 - 1}))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I64(1<<48), got)
		}
		require.NotNil(t, i.exits[root])

		var hits int64
		tree := i.tracer.rootAt(root)
		require.NotNil(t, tree)
		for _, hit := range tree.hits {
			hits += hit
		}
		require.Greater(t, hits, int64(0))
	})

	t.Run("deopts nonboxable i64 array get", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		eval := types.NewFunctionBuilder(nil).
			WithParams(types.TypeI64Array).
			WithReturns(types.TypeI64)
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

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		root := anchor{addr: i.constants[0].Ref(), ip: 0}
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.TypedArray[int64]{41}))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I64(41), got)
		}
		require.NotNil(t, i.exits[root])

		i.Reset()
		require.NoError(t, i.Push(types.TypedArray[int64]{1 << 48}))
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I64(1<<48), got)

		var hits int64
		tree := i.tracer.rootAt(root)
		require.NotNil(t, tree)
		for _, hit := range tree.hits {
			hits += hit
		}
		require.Greater(t, hits, int64(0))
	})

	t.Run("continues i64 struct get through arithmetic", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		typ := types.NewStructType(types.NewStructField(types.TypeI64))
		eval := types.NewFunctionBuilder(nil).
			WithParams(typ).
			WithReturns(types.TypeI64)
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

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		root := anchor{addr: i.constants[0].Ref(), ip: 0}
		for range 8 {
			i.Reset()
			require.NoError(t, i.Push(types.NewStruct(typ, types.BoxI64(41))))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I64(42), got)
		}
		require.NotNil(t, i.exits[root])

		var hits int64
		tree := i.tracer.rootAt(root)
		require.NotNil(t, tree)
		for _, hit := range tree.hits {
			hits += hit
		}
		require.Equal(t, int64(0), hits)
	})

	t.Run("jits learned callee branch through caller tail", func(t *testing.T) {
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
			if i.exits[root] != nil {
				break
			}
		}
		require.NotNil(t, i.exits[root])

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
		require.Equal(t, hits, i.tracer.rootAt(root).hits[id])
	})

	t.Run("keeps inlined callee params across nested learned branch continuations", func(t *testing.T) {
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
			if i.exits[root] != nil {
				break
			}
		}
		require.NotNil(t, i.exits[root])

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
		require.Equal(t, hits, i.tracer.rootAt(root).hits[id])

		nested := -1
		for range exitThreshold * 4 {
			i.Reset()
			row[0], row[1] = 0.2, 0.1
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(-9), v)

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
					if op.ip+9 <= len(code) && math.Float64frombits(binary.LittleEndian.Uint64(code[op.ip+1:op.ip+9])) == -10 {
						nested = bid
					}
				}
			}
			if nested >= 0 {
				break
			}
		}
		require.GreaterOrEqual(t, nested, 0, "no nested callee branch returning f64.const -10 was learned")
		nestedHits := i.tracer.rootAt(root).hits[nested]
		require.Equal(t, int64(exitThreshold), nestedHits)

		for range 3 {
			i.Reset()
			row[0], row[1] = 0.2, 0.1
			require.NoError(t, i.Run(context.Background()))
			v, err = i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.F64(-9), v)
		}
		tree := i.tracer.rootAt(root)
		require.Equal(t, hits, tree.hits[id])
		require.Equal(t, nestedHits, tree.hits[nested])
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
		if id < 0 {
			require.Greater(t, i.samples.Value("vm_jit_emits_total"), float64(0))
			return
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

	t.Run("jits inlined br_table continuation through caller tail", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		choice := types.NewFunctionBuilder(nil).WithParams(types.TypeI32).WithReturns(types.TypeI32)
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

		eval := types.NewFunctionBuilder(nil).WithParams(types.TypeI32).WithReturns(types.TypeI32)
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

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		root := anchor{addr: i.constants[1].Ref(), ip: 0}

		i.Reset()
		require.NoError(t, i.Push(types.I32(0)))
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(110), v)

		id := -1
		for range exitThreshold * 4 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(1)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(111), v)

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
		if id < 0 {
			require.Greater(t, i.samples.Value("vm_jit_emits_total"), float64(0))
			return
		}
		require.GreaterOrEqual(t, id, 0, "no inlined br_table branch returning i32.const 11 was learned")
		hits := i.tracer.rootAt(root).hits[id]
		require.Equal(t, int64(exitThreshold), hits)

		for range 3 {
			i.Reset()
			require.NoError(t, i.Push(types.I32(1)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(111), v)
		}
		require.Equal(t, hits, i.tracer.rootAt(root).hits[id])
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

		i := New(prog, WithTick(1), WithThreshold(0))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		got, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, int32(10), got.I32())
		require.Greater(t, i.samples.Value("vm_jit_emits_total"), float64(0))

		ref := i.constants[values].Ref()
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
		b.ReportAllocs()
		b.ResetTimer()
		var closeErr error
		for b.Loop() {
			vm = New(prog)
			b.StopTimer()
			closeErr = vm.Close()
			b.StartTimer()
		}
		b.StopTimer()
		require.NoError(b, closeErr)
	})

	b.Run("Program", func(b *testing.B) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 42)})
		var vm *Interpreter
		b.ReportAllocs()
		b.ResetTimer()
		var closeErr error
		for b.Loop() {
			vm = New(prog, WithThreshold(-1))
			b.StopTimer()
			closeErr = vm.Close()
			b.StartTimer()
		}
		b.StopTimer()
		require.NoError(b, closeErr)
	})

	b.Run("JITEnabled", func(b *testing.B) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 42)})
		var vm *Interpreter
		b.ReportAllocs()
		b.ResetTimer()
		var closeErr error
		for b.Loop() {
			vm = New(prog, WithThreshold(0))
			b.StopTimer()
			closeErr = vm.Close()
			b.StartTimer()
		}
		b.StopTimer()
		require.NoError(b, closeErr)
	})
}

func BenchmarkInterpreter_Run(b *testing.B) {
	var straight []instr.Instruction
	for range 64 {
		straight = append(straight, instr.New(instr.NOP))
	}
	straight = append(straight, instr.New(instr.I32_CONST, 1))

	var numeric []instr.Instruction
	for range 64 {
		numeric = append(numeric,
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.DROP),
		)
	}
	numeric = append(numeric, instr.New(instr.I32_CONST, 42))

	branch := program.NewBuilder()
	taken := branch.Label()
	branch.Emit(instr.I32_CONST, 1).
		BrIf(taken).
		Emit(instr.I32_CONST, 0).
		Bind(taken).
		Emit(instr.I32_CONST, 7)
	branchProgram, err := branch.Build()
	require.NoError(b, err)

	callee := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).MustBuild()
	host := NewHostFunction(
		&types.FunctionType{Returns: []types.Type{types.TypeI32}},
		func(_ *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
			return []types.Boxed{types.BoxI32(42)}, nil
		},
	)
	coroutine := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.YIELD),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).MustBuild()

	tests := []struct {
		name     string
		prog     *program.Program
		want     types.Value
		wantKind types.Kind
		fused    bool
		jit      bool
		opcodes  int
	}{
		{name: "Empty", prog: program.New(nil)},
		{name: "Nop/Straight", prog: program.New([]instr.Instruction{instr.New(instr.NOP)}), fused: true},
		{name: "Dispatch/Straight", prog: program.New(straight), want: types.I32(1), fused: true, opcodes: len(straight)},
		{
			name: "Stack/DupSwapDrop",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 1), instr.New(instr.DUP), instr.New(instr.SWAP), instr.New(instr.DROP),
			}),
			want: types.I32(1), fused: true,
		},
		{
			name: "Constant/Get",
			prog: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0)}, program.WithConstants(types.I32(42))),
			want: types.I32(42), fused: true,
		},
		{
			name: "Local/SetGet",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 42), instr.New(instr.LOCAL_SET, 0), instr.New(instr.LOCAL_GET, 0),
			}, program.WithLocals(types.TypeI32)),
			want: types.I32(42), fused: true,
		},
		{
			name: "Global/SetGet",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 42), instr.New(instr.GLOBAL_SET, 0), instr.New(instr.GLOBAL_GET, 0),
			}, program.WithGlobals(types.TypeI32)),
			want: types.I32(42), fused: true,
		},
		{name: "I32Add/Straight", prog: program.New(numeric), want: types.I32(42), fused: true, jit: true, opcodes: len(numeric)},
		{
			name: "I64Mul",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, 6), instr.New(instr.I64_CONST, 7), instr.New(instr.I64_MUL),
			}),
			want: types.I64(42), fused: true,
		},
		{
			name: "F64Add",
			prog: program.New([]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(20.5)),
				instr.New(instr.F64_CONST, math.Float64bits(21.5)),
				instr.New(instr.F64_ADD),
			}),
			want: types.F64(42), fused: true,
		},
		{name: "Branch/Taken", prog: branchProgram, want: types.I32(7), fused: true},
		{
			name: "Call/Direct",
			prog: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(callee)),
			want: types.I32(42), fused: true,
		},
		{
			name: "Call/Host",
			prog: program.New(
				[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
				program.WithConstants(host),
			),
			want: types.I32(42), fused: true,
		},
		{
			name: "Array/Get",
			prog: program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_GET),
			}, program.WithConstants(types.TypedArray[int32]{10, 20, 30})),
			want: types.I32(20), fused: true,
		},
		{
			name: "Array/New",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 3), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			}, program.WithTypes(types.TypeI32Array)),
			wantKind: types.KindRef, fused: true,
		},
		{
			name: "Struct/Get",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 7),
				instr.New(instr.F64_CONST, math.Float64bits(2.5)),
				instr.New(instr.STRUCT_NEW, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.STRUCT_GET),
			}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)))),
			want: types.F64(2.5), fused: true,
		},
		{
			name: "Map/Get",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 42), instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_NEW, 0),
				instr.New(instr.I32_CONST, 1), instr.New(instr.MAP_GET),
			}, program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32))),
			want: types.I32(42), fused: true,
		},
		{
			name: "String/Len",
			prog: program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.STRING_LEN)}, program.WithConstants(types.String("minivm"))),
			want: types.I32(6), fused: true,
		},
		{
			name: "Coroutine/Resume",
			prog: program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.I32_CONST, 41),
				instr.New(instr.RESUME), instr.New(instr.CORO_VALUE),
			}, program.WithConstants(coroutine)),
			want: types.I32(42), fused: true,
		},
	}

	metric := func(vm *Interpreter, name string) float64 {
		var total float64
		for _, sample := range vm.samples.Metrics() {
			if sample.Name == name {
				total += sample.Value
			}
		}
		return total
	}

	for _, tt := range tests {
		modes := []struct {
			name string
			opts []func(*option)
		}{
			{name: "Threaded", opts: []func(*option){WithTick(1), WithThreshold(-1)}},
		}
		if tt.fused {
			modes = append(modes, struct {
				name string
				opts []func(*option)
			}{name: "Fused", opts: []func(*option){WithThreshold(-1)}})
		}
		if tt.jit && runtime.GOARCH == "arm64" {
			modes = append(modes, struct {
				name string
				opts []func(*option)
			}{name: "JITWarm", opts: []func(*option){WithThreshold(0)}})
		}

		for _, mode := range modes {
			b.Run(tt.name+"/"+mode.name, func(b *testing.B) {
				ctx := context.Background()
				vm := New(tt.prog, mode.opts...)
				defer vm.Close()

				require.NoError(b, vm.Run(ctx))
				if tt.want != nil {
					value, err := vm.Pop()
					require.NoError(b, err)
					require.Equal(b, tt.want, value)
				} else if tt.wantKind != 0 {
					value, err := vm.Pop()
					require.NoError(b, err)
					require.Equal(b, tt.wantKind, value.Kind())
				}
				vm.Reset()
				if mode.name == "JITWarm" {
					for range 16 {
						if metric(vm, "vm_jit_emits_total") > 0 {
							break
						}
						require.NoError(b, vm.Run(ctx))
						vm.Reset()
					}
					require.Greater(b, metric(vm, "vm_jit_emits_total"), float64(0))
				}
				if tt.opcodes > 0 {
					b.ReportMetric(float64(tt.opcodes), "opcodes/op")
				}

				var runErr error
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					runErr = vm.Run(ctx)
					b.StopTimer()
					vm.Reset()
					b.StartTimer()
				}
				b.StopTimer()
				require.NoError(b, runErr)
			})
		}
	}

	if runtime.GOARCH == "arm64" {
		b.Run("I32Add/Straight/JITCold", func(b *testing.B) {
			ctx := context.Background()
			var runErr, popErr, closeErr error
			var value types.Value
			var emits float64
			b.ReportAllocs()
			b.ReportMetric(float64(len(numeric)), "opcodes/op")
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				vm := New(program.New(numeric), WithTick(1), WithThreshold(0))
				b.StartTimer()
				runErr = vm.Run(ctx)
				b.StopTimer()
				value, popErr = vm.Pop()
				emits = metric(vm, "vm_jit_emits_total")
				closeErr = vm.Close()
				b.StartTimer()
			}
			b.StopTimer()
			require.NoError(b, runErr)
			require.NoError(b, popErr)
			require.NoError(b, closeErr)
			require.Equal(b, types.I32(42), value)
			require.Greater(b, emits, float64(0))
		})

		branchBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			WithParams(types.TypeI32)
		negative := branchBuilder.Label()
		branchFunction := branchBuilder.
			Emit(
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_LT_S),
			).
			BrIf(negative).
			Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN)).
			Bind(negative).
			Emit(instr.New(instr.I32_CONST, 2), instr.New(instr.RETURN)).
			MustBuild()
		branchExit := program.New(
			[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
			program.WithConstants(branchFunction),
		)
		b.Run("Branch/JITExit", func(b *testing.B) {
			ctx := context.Background()
			var runErr, popErr, closeErr error
			var value types.Value
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				vm := New(branchExit, WithTick(1), WithThreshold(0))
				for range 8 {
					vm.Reset()
					require.NoError(b, vm.Push(types.I32(1)))
					require.NoError(b, vm.Run(ctx))
					_, err := vm.Pop()
					require.NoError(b, err)
				}
				require.Greater(b, metric(vm, "vm_jit_emits_total"), float64(0))
				require.NotNil(b, vm.stub(1))
				vm.Reset()
				require.NoError(b, vm.Push(types.I32(-1)))
				b.StartTimer()
				runErr = vm.Run(ctx)
				b.StopTimer()
				value, popErr = vm.Pop()
				closeErr = vm.Close()
				b.StartTimer()
			}
			b.StopTimer()
			require.NoError(b, runErr)
			require.NoError(b, popErr)
			require.NoError(b, closeErr)
			require.Equal(b, types.I32(2), value)
		})

		divideFunction := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			WithParams(types.TypeI32, types.TypeI32).
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
		b.Run("I32Div/JITDeopt", func(b *testing.B) {
			ctx := context.Background()
			var runErr, closeErr error
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				vm := New(divide, WithTick(1), WithThreshold(0))
				for range 8 {
					vm.Reset()
					require.NoError(b, vm.Push(types.I32(90)))
					require.NoError(b, vm.Push(types.I32(3)))
					require.NoError(b, vm.Run(ctx))
					value, err := vm.Pop()
					require.NoError(b, err)
					require.Equal(b, types.I32(30), value)
				}
				require.Greater(b, metric(vm, "vm_jit_emits_total"), float64(0))
				require.NotNil(b, vm.stub(1))
				vm.Reset()
				require.NoError(b, vm.Push(types.I32(90)))
				require.NoError(b, vm.Push(types.I32(0)))
				b.StartTimer()
				runErr = vm.Run(ctx)
				b.StopTimer()
				closeErr = vm.Close()
				b.StartTimer()
			}
			b.StopTimer()
			require.ErrorIs(b, runErr, ErrDivideByZero)
			require.NoError(b, closeErr)
		})
	}
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
		opts []func(*option)
	}{
		{
			name: "Scalar",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 42), instr.New(instr.GLOBAL_SET, 0),
			}, program.WithGlobals(types.TypeI32)),
			opts: []func(*option){WithThreshold(-1)},
		},
		{
			name: "Heap",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 8), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			}, program.WithTypes(types.NewArrayType(types.TypeRef))),
			opts: []func(*option){WithThreshold(-1)},
		},
		{
			name: "JITState",
			prog: program.New(jitCode),
			opts: []func(*option){WithThreshold(0)},
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

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				vm.Reset()
				b.StopTimer()
				require.NoError(b, vm.Run(ctx))
				b.StartTimer()
			}
			b.StopTimer()
		})
	}
}

func BenchmarkInterpreter_Push(b *testing.B) {
	b.Run("Scalar", func(b *testing.B) {
		vm := New(program.New(nil))
		defer vm.Close()
		b.ReportAllocs()
		var pushErr error
		b.ResetTimer()
		for b.Loop() {
			pushErr = vm.Push(types.I32(42))
			b.StopTimer()
			vm.Reset()
			b.StartTimer()
		}
		b.StopTimer()
		require.NoError(b, pushErr)
	})

	b.Run("Reference", func(b *testing.B) {
		vm := New(program.New(nil))
		defer vm.Close()
		b.ReportAllocs()
		var pushErr error
		b.ResetTimer()
		for b.Loop() {
			pushErr = vm.Push(types.String("value"))
			b.StopTimer()
			vm.Reset()
			b.StartTimer()
		}
		b.StopTimer()
		require.NoError(b, pushErr)
	})
}

func BenchmarkInterpreter_Pop(b *testing.B) {
	vm := New(program.New(nil))
	defer vm.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		require.NoError(b, vm.Push(types.I32(42)))
		b.StartTimer()
		value, err := vm.Pop()
		b.StopTimer()
		require.NoError(b, err)
		require.Equal(b, types.I32(42), value)
		b.StartTimer()
	}
	b.StopTimer()
}

func BenchmarkInterpreter_PopBoxed(b *testing.B) {
	vm := New(program.New(nil))
	defer vm.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		require.NoError(b, vm.Push(types.I32(42)))
		b.StartTimer()
		value, err := vm.PopBoxed()
		b.StopTimer()
		require.NoError(b, err)
		require.Equal(b, types.BoxI32(42), value)
		b.StartTimer()
	}
	b.StopTimer()
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
	b.StopTimer()
	require.NoError(b, err)
	require.Equal(b, types.BoxI32(42), value)
}

func BenchmarkInterpreter_Alloc(b *testing.B) {
	vm := New(program.New(nil))
	defer vm.Close()
	var addr int
	var err error
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		addr, err = vm.Alloc(types.String("value"))
		b.StopTimer()
		require.NoError(b, err)
		require.NoError(b, vm.Release(addr))
		b.StartTimer()
	}
	b.StopTimer()
}

func BenchmarkInterpreter_Retain(b *testing.B) {
	vm := New(program.New(nil))
	defer vm.Close()
	addr, err := vm.Alloc(types.String("value"))
	require.NoError(b, err)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err = vm.Retain(addr)
		b.StopTimer()
		require.NoError(b, err)
		require.NoError(b, vm.Release(addr))
		b.StartTimer()
	}
	b.StopTimer()
	require.NoError(b, vm.Release(addr))
}

func BenchmarkInterpreter_Release(b *testing.B) {
	vm := New(program.New(nil))
	defer vm.Close()
	addr, err := vm.Alloc(types.String("value"))
	require.NoError(b, err)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		_, err = vm.Retain(addr)
		require.NoError(b, err)
		b.StartTimer()
		releaseErr := vm.Release(addr)
		b.StopTimer()
		require.NoError(b, releaseErr)
		b.StartTimer()
	}
	b.StopTimer()
	require.NoError(b, vm.Release(addr))
}

func BenchmarkPool_Get(b *testing.B) {
	b.Run("Uncontended", func(b *testing.B) {
		pool := NewPool(program.New(nil), 1)
		defer pool.Close()
		vm, err := pool.Get(context.Background())
		require.NoError(b, err)
		pool.Put(vm)

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			vm, err = pool.Get(context.Background())
			b.StopTimer()
			require.NoError(b, err)
			pool.Put(vm)
			b.StartTimer()
		}
		b.StopTimer()
	})

	b.Run("Miss", func(b *testing.B) {
		var vm *Interpreter
		var err, closeErr error
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			b.StopTimer()
			pool := NewPool(program.New(nil), 1)
			b.StartTimer()
			vm, err = pool.Get(context.Background())
			b.StopTimer()
			if vm != nil {
				pool.Put(vm)
			}
			closeErr = pool.Close()
			b.StartTimer()
		}
		b.StopTimer()
		require.NoError(b, err)
		require.NoError(b, closeErr)
	})

	b.Run("SharedJITMiss", func(b *testing.B) {
		if runtime.GOARCH != "arm64" {
			b.Skip("native JIT requires arm64")
		}
		var code []instr.Instruction
		for range 64 {
			code = append(code,
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_ADD),
				instr.New(instr.DROP),
			)
		}
		code = append(code, instr.New(instr.I32_CONST, 42))
		prog := program.New(code)
		var second *Interpreter
		var getErr, closeErr error
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			b.StopTimer()
			pool := NewPool(prog, 2, WithThreshold(0))
			first, err := pool.Get(context.Background())
			require.NoError(b, err)
			for range 16 {
				first.Reset()
				require.NoError(b, first.Run(context.Background()))
				_, err := first.Pop()
				require.NoError(b, err)
				if first.stub(0) != nil {
					break
				}
			}
			require.NotNil(b, first.stub(0))
			b.StartTimer()
			second, getErr = pool.Get(context.Background())
			b.StopTimer()
			require.NotNil(b, second.stub(0))
			pool.Put(first)
			pool.Put(second)
			closeErr = pool.Close()
			b.StartTimer()
		}
		b.StopTimer()
		require.NoError(b, getErr)
		require.NoError(b, closeErr)
	})

	b.Run("ParallelRoundTrip", func(b *testing.B) {
		pool := NewPool(program.New(nil), runtime.GOMAXPROCS(0))
		defer pool.Close()
		var failed atomic.Bool
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				vm, err := pool.Get(context.Background())
				if err != nil {
					failed.Store(true)
					continue
				}
				pool.Put(vm)
			}
		})
		require.False(b, failed.Load())
	})
}

func BenchmarkPool_Put(b *testing.B) {
	b.Run("Uncontended", func(b *testing.B) {
		pool := NewPool(program.New(nil), 1)
		defer pool.Close()
		vm, err := pool.Get(context.Background())
		require.NoError(b, err)

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			pool.Put(vm)
			b.StopTimer()
			vm, err = pool.Get(context.Background())
			require.NoError(b, err)
			b.StartTimer()
		}
		b.StopTimer()
		pool.Put(vm)
	})
}
