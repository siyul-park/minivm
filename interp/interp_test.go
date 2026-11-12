package interp_test

import (
	"context"
	"errors"
	"maps"
	"math"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/testenv"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type upperCodec byte

type trackedValue struct {
	refs   []types.Ref
	closed int
}

// panicCloser is a host value whose Close panics.
type panicCloser struct{ trackedValue }

type structGetHostFields struct {
	Count  int32
	hidden int32
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

// heapRunway mirrors the interpreter's unexported heapRunway. Keep in sync.
const heapRunway = 64

// zeroTypes declares one slot of each kind, plus any; zeroReads pushes each
// unwritten local, and zeroValues is what an unwritten local or global reads,
// popped last first.
var (
	zeroTypes  = []types.Type{types.TypeI32(), types.TypeI8(), types.TypeI1(), types.TypeI64(), types.TypeF32(), types.TypeF64(), types.TypeString(), types.TypeAny()}
	zeroValues = []types.Value{types.Null, types.Null, types.F64(0), types.F32(0), types.I64(0), types.I1(false), types.I8(0), types.I32(0)}
	zeroReads  = []instr.Instruction{
		instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.LOCAL_GET, 3),
		instr.MustNew(instr.LOCAL_GET, 4), instr.MustNew(instr.LOCAL_GET, 5), instr.MustNew(instr.LOCAL_GET, 6), instr.MustNew(instr.LOCAL_GET, 7),
	}
)

var runTests = []struct {
	program *program.Program
	values  []types.Value
	err     error
}{
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.NOP)}),
		values:  []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.UNREACHABLE)}),
		err:     interp.ErrUnreachableExecuted,
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.DROP)}),
		values:  []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.DUP)}),
		values:  []types.Value{types.I32(7), types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.SWAP)}),
		values:  []types.Value{types.I32(1), types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.SELECT),
		}),
		values: []types.Value{types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.BR, 5),
			instr.MustNew(instr.I32_CONST, 999),
			instr.MustNew(instr.I32_CONST, 1),
		}),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.BR_IF, 6),
			instr.MustNew(instr.I32_CONST, 999),
			instr.MustNew(instr.DROP),
			instr.MustNew(instr.I32_CONST, 1),
		}),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 0),
			instr.MustNew(instr.BR_TABLE, 1, 6, 0),
			instr.MustNew(instr.I32_CONST, 999),
			instr.MustNew(instr.DROP),
			instr.MustNew(instr.I32_CONST, 1),
		}),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
			Append(instr.MustNew(instr.I32_CONST, 42), instr.MustNew(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32(), types.TypeI32()}}).
			Append(instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(20), types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.RETURN_CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).
			Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(6)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.RETURN_CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).
			Locals(types.TypeI32()).
			Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 100), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(105)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.YIELD)}),
		err:     interp.ErrYield,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CALL),
			instr.MustNew(instr.I32_CONST, 41),
			instr.MustNew(instr.RESUME),
			instr.MustNew(instr.CORO_VALUE),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny()}}).
			Append(instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.YIELD), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CALL),
			instr.MustNew(instr.CORO_DONE),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny()}}).
			Append(instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.YIELD), instr.MustNew(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CALL),
			instr.MustNew(instr.CORO_VALUE),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny()}}).
			Append(instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.YIELD), instr.MustNew(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.GLOBAL_SET, 0), instr.MustNew(instr.GLOBAL_GET, 0),
		}, program.WithGlobals(types.TypeI32())),
		values: []types.Value{types.I32(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 6), instr.MustNew(instr.GLOBAL_TEE, 0)}, program.WithGlobals(types.TypeI32())),
		values:  []types.Value{types.I32(6)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.LOCAL_SET, 0), instr.MustNew(instr.LOCAL_GET, 0),
		}, program.WithLocals(types.TypeI32())),
		values: []types.Value{types.I32(5)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 9), instr.MustNew(instr.LOCAL_TEE, 0)}, program.WithLocals(types.TypeI32())),
		values:  []types.Value{types.I32(9)},
	},
	{
		program: program.New(zeroReads, program.WithLocals(zeroTypes...)),
		values:  zeroValues,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.GLOBAL_GET, 1), instr.MustNew(instr.GLOBAL_GET, 2), instr.MustNew(instr.GLOBAL_GET, 3),
			instr.MustNew(instr.GLOBAL_GET, 4), instr.MustNew(instr.GLOBAL_GET, 5), instr.MustNew(instr.GLOBAL_GET, 6), instr.MustNew(instr.GLOBAL_GET, 7),
		}, program.WithGlobals(zeroTypes...)),
		values: zeroValues,
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(zeroFunction())),
		values:  zeroValues,
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.CALL)}, program.WithConstants(zeroFunction())),
		values:  zeroValues,
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.RETURN_CALL)}, program.WithConstants(zeroFunction())),
		values:  zeroValues,
	},
	{

		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(
			types.NewFunctionBuilder(&types.FunctionType{Returns: zeroTypes}).Locals(types.TypeI32()).Append(
				instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.LOCAL_SET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.RETURN_CALL),
			).MustBuild(),
			zeroFunction(),
		)),
		values: zeroValues,
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0)}, program.WithConstants(types.I32(11))),
		values:  []types.Value{types.I32(11)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 7),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CLOSURE_NEW),
			instr.MustNew(instr.CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
			Captures(types.TypeI32()).Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 0),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CLOSURE_NEW),
			instr.MustNew(instr.CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
			Captures(types.TypeI32()).Append(
			instr.MustNew(instr.I32_CONST, 99), instr.MustNew(instr.UPVAL_SET, 0), instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.RETURN),
		).MustBuild())),
		values: []types.Value{types.I32(99)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.REF_NULL)}),
		values:  []types.Value{types.Null},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.REF_NEW)}),
		values:  []types.Value{types.I32(5)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 9), instr.MustNew(instr.REF_NEW), instr.MustNew(instr.REF_GET)}),
		values:  []types.Value{types.I32(9)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.REF_NEW), instr.MustNew(instr.DUP),
			instr.MustNew(instr.I32_CONST, 77), instr.MustNew(instr.REF_SET),
			instr.MustNew(instr.REF_GET),
		}),
		values: []types.Value{types.I32(77)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.DUP),
			instr.MustNew(instr.I32_CONST, 77), instr.MustNew(instr.REF_SET),
		}, program.WithConstants(types.NewFunctionBuilder(nil).Append(instr.MustNew(instr.RETURN)).MustBuild())),
		err: interp.ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.REF_TEST, 0)}, program.WithTypes(types.TypeI32())),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.REF_CAST, 0)}, program.WithTypes(types.TypeI32())),
		values:  []types.Value{types.I32(5)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.REF_NULL), instr.MustNew(instr.REF_IS_NULL)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.REF_NULL), instr.MustNew(instr.REF_NULL), instr.MustNew(instr.REF_EQ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.REF_NULL), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.REF_NEW), instr.MustNew(instr.REF_NE)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 42)}),
		values:  []types.Value{types.I32(42)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_ADD)}),
		values:  []types.Value{types.I32(5)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_SUB)}),
		values:  []types.Value{types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_CONST, 4), instr.MustNew(instr.I32_MUL)}),
		values:  []types.Value{types.I32(12)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, i32operand(-7)), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_DIV_S),
		}),
		values: []types.Value{types.I32(-3)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_DIV_U),
		}),
		values: []types.Value{types.I32(int32(uint32(math.MaxUint32) / 2))},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, i32operand(-7)), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_REM_S),
		}),
		values: []types.Value{types.I32(-1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_REM_U),
		}),
		values: []types.Value{types.I32(int32(uint32(math.MaxUint32) % 3))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_SHL)}),
		values:  []types.Value{types.I32(8)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, i32operand(-8)), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SHR_S),
		}),
		values: []types.Value{types.I32(-4)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SHR_U),
		}),
		values: []types.Value{types.I32(int32(uint32(math.MaxUint32) >> 1))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 12), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_AND)}),
		values:  []types.Value{types.I32(8)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 12), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_OR)}),
		values:  []types.Value{types.I32(14)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 12), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_XOR)}),
		values:  []types.Value{types.I32(6)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CLZ)}),
		values:  []types.Value{types.I32(31)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 8), instr.MustNew(instr.I32_CTZ)}),
		values:  []types.Value{types.I32(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.I32_POPCNT)}),
		values:  []types.Value{types.I32(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 4), instr.MustNew(instr.I32_ROTL)}),
		values:  []types.Value{types.I32(16)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 16), instr.MustNew(instr.I32_CONST, 4), instr.MustNew(instr.I32_ROTR)}),
		values:  []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 255), instr.MustNew(instr.I32_EXTEND8_S)}),
		values:  []types.Value{types.I32(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 65535), instr.MustNew(instr.I32_EXTEND16_S)}),
		values:  []types.Value{types.I32(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_EQZ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.I32_EQ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.I32_CONST, 6), instr.MustNew(instr.I32_NE)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_LT_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_LT_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_GT_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_GT_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_LE_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_LE_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_GE_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_GE_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_TO_I64_S)}),
		values:  []types.Value{types.I64(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_TO_I64_U)}),
		values:  []types.Value{types.I64(int64(uint32(math.MaxUint32)))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_TO_F32_S)}),
		values:  []types.Value{types.F32(float32(int32(-1)))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_TO_F32_U)}),
		values:  []types.Value{types.F32(float32(uint32(math.MaxUint32)))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_TO_F64_S)}),
		values:  []types.Value{types.F64(float64(int32(-1)))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, i32operand(-1)), instr.MustNew(instr.I32_TO_F64_U)}),
		values:  []types.Value{types.F64(float64(uint32(math.MaxUint32)))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(1))), instr.MustNew(instr.I32_REINTERPRET_F32)}),
		values:  []types.Value{types.I32(int32(math.Float32bits(1)))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 42)}),
		values:  []types.Value{types.I64(42)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 2), instr.MustNew(instr.I64_CONST, 3), instr.MustNew(instr.I64_ADD)}),
		values:  []types.Value{types.I64(5)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 5), instr.MustNew(instr.I64_CONST, 3), instr.MustNew(instr.I64_SUB)}),
		values:  []types.Value{types.I64(2)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 3), instr.MustNew(instr.I64_CONST, 4), instr.MustNew(instr.I64_MUL)}),
		values:  []types.Value{types.I64(12)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I64_CONST, i64operand(-7)), instr.MustNew(instr.I64_CONST, 2), instr.MustNew(instr.I64_DIV_S),
		}),
		values: []types.Value{types.I64(-3)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_CONST, 2), instr.MustNew(instr.I64_DIV_U),
		}),
		values: []types.Value{types.I64(int64(uint64(math.MaxUint64) / 2))},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I64_CONST, i64operand(-7)), instr.MustNew(instr.I64_CONST, 3), instr.MustNew(instr.I64_REM_S),
		}),
		values: []types.Value{types.I64(-1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_CONST, 3), instr.MustNew(instr.I64_REM_U),
		}),
		values: []types.Value{types.I64(int64(uint64(math.MaxUint64) % 3))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 1), instr.MustNew(instr.I64_CONST, 3), instr.MustNew(instr.I64_SHL)}),
		values:  []types.Value{types.I64(8)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I64_CONST, i64operand(-8)), instr.MustNew(instr.I64_CONST, 1), instr.MustNew(instr.I64_SHR_S),
		}),
		values: []types.Value{types.I64(-4)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_CONST, 1), instr.MustNew(instr.I64_SHR_U),
		}),
		values: []types.Value{types.I64(int64(uint64(math.MaxUint64) >> 1))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 12), instr.MustNew(instr.I64_CONST, 10), instr.MustNew(instr.I64_XOR)}),
		values:  []types.Value{types.I64(6)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 12), instr.MustNew(instr.I64_CONST, 10), instr.MustNew(instr.I64_AND)}),
		values:  []types.Value{types.I64(8)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 12), instr.MustNew(instr.I64_CONST, 10), instr.MustNew(instr.I64_OR)}),
		values:  []types.Value{types.I64(14)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 1), instr.MustNew(instr.I64_CLZ)}),
		values:  []types.Value{types.I64(63)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 8), instr.MustNew(instr.I64_CTZ)}),
		values:  []types.Value{types.I64(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 7), instr.MustNew(instr.I64_POPCNT)}),
		values:  []types.Value{types.I64(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 1), instr.MustNew(instr.I64_CONST, 4), instr.MustNew(instr.I64_ROTL)}),
		values:  []types.Value{types.I64(16)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 16), instr.MustNew(instr.I64_CONST, 4), instr.MustNew(instr.I64_ROTR)}),
		values:  []types.Value{types.I64(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 255), instr.MustNew(instr.I64_EXTEND8_S)}),
		values:  []types.Value{types.I64(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 65535), instr.MustNew(instr.I64_EXTEND16_S)}),
		values:  []types.Value{types.I64(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, uint64(uint32(math.MaxUint32))), instr.MustNew(instr.I64_EXTEND32_S)}),
		values:  []types.Value{types.I64(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 0), instr.MustNew(instr.I64_EQZ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 5), instr.MustNew(instr.I64_CONST, 5), instr.MustNew(instr.I64_EQ)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 5), instr.MustNew(instr.I64_CONST, 6), instr.MustNew(instr.I64_NE)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_CONST, 0), instr.MustNew(instr.I64_LT_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_CONST, 0), instr.MustNew(instr.I64_LT_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 0), instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_GT_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 0), instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_GT_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_LE_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_CONST, 0), instr.MustNew(instr.I64_LE_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 0), instr.MustNew(instr.I64_CONST, 0), instr.MustNew(instr.I64_GE_S)}),
		values:  []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, 0), instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_GE_U)}),
		values:  []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, uint64(int64(1)<<32+1)), instr.MustNew(instr.I64_TO_I32)}),
		values:  []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_TO_F32_S)}),
		values:  []types.Value{types.F32(float32(int64(-1)))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_TO_F32_U)}),
		values:  []types.Value{types.F32(float32(uint64(math.MaxUint64)))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_TO_F64_S)}),
		values:  []types.Value{types.F64(float64(int64(-1)))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, i64operand(-1)), instr.MustNew(instr.I64_TO_F64_U)}),
		values:  []types.Value{types.F64(float64(uint64(math.MaxUint64)))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(1)), instr.MustNew(instr.I64_REINTERPRET_F64)}),
		values:  []types.Value{types.I64(int64(math.Float64bits(1)))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(1.5)))}),
		values:  []types.Value{types.F32(1.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(1.5))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(2.25))), instr.MustNew(instr.F32_ADD),
		}),
		values: []types.Value{types.F32(3.75)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(5.5))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(2.25))), instr.MustNew(instr.F32_SUB),
		}),
		values: []types.Value{types.F32(3.25)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(2.5))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(4))), instr.MustNew(instr.F32_MUL),
		}),
		values: []types.Value{types.F32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(10))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(4))), instr.MustNew(instr.F32_DIV),
		}),
		values: []types.Value{types.F32(2.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(-7))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(3))), instr.MustNew(instr.F32_REM),
		}),
		values: []types.Value{types.F32(-1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(-7))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(3))), instr.MustNew(instr.F32_MOD),
		}),
		values: []types.Value{types.F32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(1))), instr.MustNew(instr.F32_CONST, 0), instr.MustNew(instr.F32_REM),
		}),
		err: interp.ErrDivideByZero,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(1))), instr.MustNew(instr.F32_CONST, 0), instr.MustNew(instr.F32_MOD),
		}),
		err: interp.ErrDivideByZero,
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(-3.5))), instr.MustNew(instr.F32_ABS)}),
		values:  []types.Value{types.F32(3.5)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(3.5))), instr.MustNew(instr.F32_NEG)}),
		values:  []types.Value{types.F32(-3.5)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(9))), instr.MustNew(instr.F32_SQRT)}),
		values:  []types.Value{types.F32(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(1.2))), instr.MustNew(instr.F32_CEIL)}),
		values:  []types.Value{types.F32(2)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(1.8))), instr.MustNew(instr.F32_FLOOR)}),
		values:  []types.Value{types.F32(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(-1.8))), instr.MustNew(instr.F32_TRUNC)}),
		values:  []types.Value{types.F32(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(2.5))), instr.MustNew(instr.F32_NEAREST)}),
		values:  []types.Value{types.F32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(3))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(5))), instr.MustNew(instr.F32_MIN),
		}),
		values: []types.Value{types.F32(3)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(3))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(5))), instr.MustNew(instr.F32_MAX),
		}),
		values: []types.Value{types.F32(5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(3))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(-1))), instr.MustNew(instr.F32_COPYSIGN),
		}),
		values: []types.Value{types.F32(-3)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(2))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(2))), instr.MustNew(instr.F32_EQ),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(2))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(3))), instr.MustNew(instr.F32_NE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(2))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(3))), instr.MustNew(instr.F32_LT),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(3))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(2))), instr.MustNew(instr.F32_GT),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(2))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(2))), instr.MustNew(instr.F32_LE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(2))), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(2))), instr.MustNew(instr.F32_GE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(-3.7))), instr.MustNew(instr.F32_TO_I32_S)}),
		values:  []types.Value{types.I32(-3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(3.7))), instr.MustNew(instr.F32_TO_I32_U)}),
		values:  []types.Value{types.I32(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(-3.7))), instr.MustNew(instr.F32_TO_I64_S)}),
		values:  []types.Value{types.I64(-3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(3.7))), instr.MustNew(instr.F32_TO_I64_U)}),
		values:  []types.Value{types.I64(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(1.5))), instr.MustNew(instr.F32_TO_F64)}),
		values:  []types.Value{types.F64(float64(float32(1.5)))},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, uint64(math.Float32bits(1))), instr.MustNew(instr.F32_REINTERPRET_I32)}),
		values:  []types.Value{types.F32(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(2.5))}),
		values:  []types.Value{types.F64(2.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(1.5)), instr.MustNew(instr.F64_CONST, math.Float64bits(2.25)), instr.MustNew(instr.F64_ADD),
		}),
		values: []types.Value{types.F64(3.75)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(5.5)), instr.MustNew(instr.F64_CONST, math.Float64bits(2.25)), instr.MustNew(instr.F64_SUB),
		}),
		values: []types.Value{types.F64(3.25)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(2.5)), instr.MustNew(instr.F64_CONST, math.Float64bits(4)), instr.MustNew(instr.F64_MUL),
		}),
		values: []types.Value{types.F64(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(10)), instr.MustNew(instr.F64_CONST, math.Float64bits(4)), instr.MustNew(instr.F64_DIV),
		}),
		values: []types.Value{types.F64(2.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(-7)), instr.MustNew(instr.F64_CONST, math.Float64bits(3)), instr.MustNew(instr.F64_REM),
		}),
		values: []types.Value{types.F64(-1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(-7)), instr.MustNew(instr.F64_CONST, math.Float64bits(3)), instr.MustNew(instr.F64_MOD),
		}),
		values: []types.Value{types.F64(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(1)), instr.MustNew(instr.F64_CONST, 0), instr.MustNew(instr.F64_REM),
		}),
		err: interp.ErrDivideByZero,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(1)), instr.MustNew(instr.F64_CONST, 0), instr.MustNew(instr.F64_MOD),
		}),
		err: interp.ErrDivideByZero,
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(-3.5)), instr.MustNew(instr.F64_ABS)}),
		values:  []types.Value{types.F64(3.5)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(3.5)), instr.MustNew(instr.F64_NEG)}),
		values:  []types.Value{types.F64(-3.5)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(9)), instr.MustNew(instr.F64_SQRT)}),
		values:  []types.Value{types.F64(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(1.2)), instr.MustNew(instr.F64_CEIL)}),
		values:  []types.Value{types.F64(2)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(1.8)), instr.MustNew(instr.F64_FLOOR)}),
		values:  []types.Value{types.F64(1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(-1.8)), instr.MustNew(instr.F64_TRUNC)}),
		values:  []types.Value{types.F64(-1)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(2.5)), instr.MustNew(instr.F64_NEAREST)}),
		values:  []types.Value{types.F64(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(3)), instr.MustNew(instr.F64_CONST, math.Float64bits(5)), instr.MustNew(instr.F64_MIN),
		}),
		values: []types.Value{types.F64(3)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(3)), instr.MustNew(instr.F64_CONST, math.Float64bits(5)), instr.MustNew(instr.F64_MAX),
		}),
		values: []types.Value{types.F64(5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(3)), instr.MustNew(instr.F64_CONST, math.Float64bits(-1)), instr.MustNew(instr.F64_COPYSIGN),
		}),
		values: []types.Value{types.F64(-3)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(2)), instr.MustNew(instr.F64_CONST, math.Float64bits(2)), instr.MustNew(instr.F64_EQ),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(2)), instr.MustNew(instr.F64_CONST, math.Float64bits(3)), instr.MustNew(instr.F64_NE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(2)), instr.MustNew(instr.F64_CONST, math.Float64bits(3)), instr.MustNew(instr.F64_LT),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(3)), instr.MustNew(instr.F64_CONST, math.Float64bits(2)), instr.MustNew(instr.F64_GT),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(2)), instr.MustNew(instr.F64_CONST, math.Float64bits(2)), instr.MustNew(instr.F64_LE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(2)), instr.MustNew(instr.F64_CONST, math.Float64bits(2)), instr.MustNew(instr.F64_GE),
		}),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(-3.7)), instr.MustNew(instr.F64_TO_I32_S)}),
		values:  []types.Value{types.I32(-3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(3.7)), instr.MustNew(instr.F64_TO_I32_U)}),
		values:  []types.Value{types.I32(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(-3.7)), instr.MustNew(instr.F64_TO_I64_S)}),
		values:  []types.Value{types.I64(-3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(3.7)), instr.MustNew(instr.F64_TO_I64_U)}),
		values:  []types.Value{types.I64(3)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.F64_CONST, math.Float64bits(1.5)), instr.MustNew(instr.F64_TO_F32)}),
		values:  []types.Value{types.F32(1.5)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I64_CONST, math.Float64bits(1)), instr.MustNew(instr.F64_REINTERPRET_I64)}),
		values:  []types.Value{types.F64(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 72), instr.MustNew(instr.I32_CONST, 105), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW, 0),
			instr.MustNew(instr.STRING_NEW_UTF32),
		}, program.WithTypes(types.TypeI32Array())),
		values: []types.Value{types.String("Hi")},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.STRING_LEN)}, program.WithConstants(types.String("Hi"))),
		values:  []types.Value{types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_CONCAT)},
			program.WithConstants(types.String("Hi"), types.String("There"))),
		values: []types.Value{types.String("HiThere")},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_EQ)},
			program.WithConstants(types.String("Go"), types.String("Go"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_NE)},
			program.WithConstants(types.String("Go"), types.String("No"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_CONCAT),
			instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.STRING_EQ),
		}, program.WithConstants(types.String("Hi"), types.String("There"), types.String("HiThere"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_CONCAT),
			instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.STRING_NE),
		}, program.WithConstants(types.String("Hi"), types.String("There"), types.String("HiThere"))),
		values: []types.Value{types.I1(false)},
	},
	{
		program: program.New([]instr.Instruction{

			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_CONCAT),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_CONCAT),
			instr.MustNew(instr.DROP),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.STRING_EQ),
		}, program.WithConstants(types.String("Hi"), types.String("There"), types.String("HiThere")),
			program.WithLocals(types.TypeString())),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_CONCAT)}, program.WithConstants(types.String(""), types.String(""))),
		values:  []types.Value{types.String("")},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_CONCAT),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.STRING_CONCAT),
			instr.MustNew(instr.DROP),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CONST_GET, 3), instr.MustNew(instr.STRING_EQ),
		}, program.WithConstants(
			types.String("abc"), types.String("def"), types.String("0123456789abcdef"), types.String("abcdef")),
			program.WithLocals(types.TypeString())),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_CONCAT), instr.MustNew(instr.DROP),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.STRING_CONCAT),
			instr.MustNew(instr.CONST_GET, 3), instr.MustNew(instr.STRING_EQ),
		}, program.WithConstants(types.String("a"), types.String("b"), types.String("c"), types.String("ac"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.REF_NULL), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.STRING_EQ),
		}, program.WithConstants(types.String("Go"))),
		err: interp.ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_LT)},
			program.WithConstants(types.String("Go"), types.String("No"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_GT)},
			program.WithConstants(types.String("No"), types.String("Go"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_LE)},
			program.WithConstants(types.String("Go"), types.String("Go"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_GE)},
			program.WithConstants(types.String("Go"), types.String("Go"))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.STRING_ENCODE_UTF32)}, program.WithConstants(types.String("Hi"))),
		values:  []types.Value{types.TypedArray[int32]{72, 105}},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 30), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW, 0),
		}, program.WithTypes(types.TypeI32Array())),
		values: []types.Value{types.TypedArray[int32]{10, 20, 30}},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0)}, program.WithTypes(types.TypeI32Array())),
		values:  []types.Value{types.TypedArray[int32]{0, 0, 0}},
	},
	{
		program: program.New(
			[]instr.Instruction{instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0)},
			program.WithTypes(types.NewArrayType(types.TypeAny())),
		),
		values: []types.Value{types.NewArray(types.NewArrayType(types.TypeAny()), types.BoxedNull, types.BoxedNull)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW, 0),
			instr.MustNew(instr.ARRAY_LEN),
		}, program.WithTypes(types.TypeI32Array())),
		values: []types.Value{types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 100), instr.MustNew(instr.I32_CONST, 200), instr.MustNew(instr.I32_CONST, 300), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW, 0),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array())),
		values: []types.Value{types.I32(200)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW, 0),
			instr.MustNew(instr.DUP),
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 99), instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array())),
		values: []types.Value{types.I32(99)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.DUP),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_FILL),
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array())),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.DUP),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.F64_CONST, math.Float64bits(1.5)), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_FILL),
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeF64Array())),
		values: []types.Value{types.F64(1.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.DUP),
			instr.MustNew(instr.I32_CONST, 0),
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.DUP),
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 9), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_FILL),
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_COPY),
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array())),
		values: []types.Value{types.I32(9)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW, 0),
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_CONST, 4), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_APPEND),
		}, program.WithTypes(types.TypeI32Array())),
		values: []types.Value{types.TypedArray[int32]{1, 2, 3, 4}},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW, 0),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_DELETE),
		}, program.WithTypes(types.TypeI32Array())),
		values: []types.Value{types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 30), instr.MustNew(instr.I32_CONST, 40), instr.MustNew(instr.I32_CONST, 4), instr.MustNew(instr.ARRAY_NEW, 0),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_SLICE),
		}, program.WithTypes(types.TypeI32Array())),
		values: []types.Value{types.TypedArray[int32]{20, 30}},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithTypes(types.NewArrayType(types.TypeAny())),
			program.WithLocals(types.TypeI32Array()),
		),
		values: []types.Value{types.Null},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithConstants(types.TypedArray[float32]{1.5}),
			program.WithLocals(types.TypeI32Array()),
		),
		values: []types.Value{types.F32(1.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.F64_CONST, math.Float64bits(2.5)), instr.MustNew(instr.STRUCT_NEW, 0),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64())))),
		values: []types.Value{types.NewStruct(
			types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64())),
			types.BoxI32(7), types.BoxF64(2.5),
		)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.STRUCT_NEW_DEFAULT, 0)},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64())))),
		values: []types.Value{types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64())))},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.STRUCT_NEW_DEFAULT, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.STRUCT_GET),
			instr.MustNew(instr.REF_IS_NULL),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeAny())))),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.F64_CONST, math.Float64bits(2.5)), instr.MustNew(instr.STRUCT_NEW, 0),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.STRUCT_GET),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64())))),
		values: []types.Value{types.F64(2.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.F64_CONST, math.Float64bits(2.5)), instr.MustNew(instr.STRUCT_NEW, 0),
			instr.MustNew(instr.DUP),
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 99), instr.MustNew(instr.STRUCT_SET),
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.STRUCT_GET),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64())))),
		values: []types.Value{types.I32(99)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.F64_CONST, math.Float64bits(2.5)), instr.MustNew(instr.STRUCT_NEW, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.STRUCT_GET),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64()))),
			program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64()))),
		),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.F64_CONST, math.Float64bits(2.5)), instr.MustNew(instr.STRUCT_NEW, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.STRUCT_GET),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64()))),
			program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64()))),
		),
		values: []types.Value{types.F64(2.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.F64_CONST, math.Float64bits(2.5)), instr.MustNew(instr.STRUCT_NEW, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.STRUCT_GET),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64()))),
			program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64()))),
			program.WithConstants(types.I32(1)),
		),
		values: []types.Value{types.F64(2.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.STRUCT_NEW, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.STRUCT_GET),
		},
			program.WithConstants(types.String("hi")),
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeString()))),
			program.WithLocals(types.NewStructType(types.NewStructField(types.TypeString()))),
		),
		values: []types.Value{types.String("hi")},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.STRUCT_GET),
		}, program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32())))),
		err: interp.ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.STRUCT_GET),
		}, program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32())))),
		err: interp.ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.REF_NULL), instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.STRUCT_GET),
		}, program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32())))),
		err: interp.ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 9), instr.MustNew(instr.STRUCT_NEW, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.STRUCT_GET),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32()))),
			program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64()))),
		),
		err: interp.ErrSegmentationFault,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.STRUCT_NEW, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.STRUCT_GET),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32()))),
			program.WithLocals(types.NewStructType(types.NewStructField(types.TypeI32()))),
		),
		err: interp.ErrSegmentationFault,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW, 0),
			instr.MustNew(instr.GLOBAL_SET, 0),
			instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI32Array()),
			program.WithGlobals(types.TypeI32Array()),
		),
		values: []types.Value{types.I32(20)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.F64_CONST, math.Float64bits(2.5)), instr.MustNew(instr.STRUCT_NEW, 0),
			instr.MustNew(instr.GLOBAL_SET, 0),
			instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.STRUCT_GET),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64()))),
			program.WithGlobals(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64()))),
		),
		values: []types.Value{types.I32(7)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.GLOBAL_SET, 0),
			instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithTypes(types.NewArrayType(types.TypeAny())),
			program.WithGlobals(types.TypeI32Array()),
		),
		values: []types.Value{types.Null},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.GLOBAL_SET, 0),
			instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithConstants(types.TypedArray[float32]{1.5}),
			program.WithGlobals(types.TypeF32Array()),
		),
		values: []types.Value{types.F32(1.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW, 0),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CLOSURE_NEW),
			instr.MustNew(instr.CALL),
		},
			program.WithTypes(types.TypeI32Array()),
			program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
				Captures(types.TypeI32Array()).
				Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_GET), instr.MustNew(instr.RETURN)).
				MustBuild()),
		),
		values: []types.Value{types.I32(20)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.F64_CONST, math.Float64bits(2.5)), instr.MustNew(instr.STRUCT_NEW, 0),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CLOSURE_NEW),
			instr.MustNew(instr.CALL),
		},
			program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64()))),
			program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
				Captures(types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeF64()))).
				Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.STRUCT_GET), instr.MustNew(instr.RETURN)).
				MustBuild()),
		),
		values: []types.Value{types.I32(7)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CLOSURE_NEW),
			instr.MustNew(instr.CALL),
		},
			program.WithTypes(types.NewArrayType(types.TypeAny())),
			program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
				Captures(types.TypeI32Array()).
				Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET), instr.MustNew(instr.RETURN)).
				MustBuild()),
		),
		values: []types.Value{types.Null},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CONST_GET, 1),
			instr.MustNew(instr.CLOSURE_NEW),
			instr.MustNew(instr.CALL),
		},
			program.WithConstants(
				types.TypedArray[float32]{1.5},
				types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeF32()}}).
					Captures(types.TypeI32Array()).
					Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET), instr.MustNew(instr.RETURN)).
					MustBuild(),
			),
		),
		values: []types.Value{types.F32(1.5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CLOSURE_NEW),
			instr.MustNew(instr.CALL),
		},
			program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
				Captures(types.TypeI32Array()).
				Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET), instr.MustNew(instr.RETURN)).
				MustBuild()),
		),
		err: interp.ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CLOSURE_NEW),
			instr.MustNew(instr.CALL),
		},
			program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
				Captures(types.NewStructType(types.NewStructField(types.TypeI32()))).
				Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.STRUCT_GET), instr.MustNew(instr.RETURN)).
				MustBuild()),
		),
		err: interp.ErrTypeMismatch,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeI32(), types.TypeI32()))),
		values: []types.Value{types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_CONCAT),
			instr.MustNew(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeString(), types.TypeI32())),
			program.WithConstants(types.String("Hi"), types.String("There"), types.String("HiThere"))),
		values: []types.Value{types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_CONCAT),
			instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeString(), types.TypeI32())),
			program.WithConstants(types.String("Hi"), types.String("There"), types.String("HiThere"))),
		values: []types.Value{types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_CONCAT),
			instr.MustNew(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeAny(), types.TypeI32())),
			program.WithConstants(types.String("Hi"), types.String("There"), types.String("HiThere"))),
		values: []types.Value{types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_EQZ),
			instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_EQZ), instr.MustNew(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeAny(), types.TypeI32()))),
		values: []types.Value{types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 4), instr.MustNew(instr.MAP_NEW_DEFAULT, 0),
			instr.MustNew(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32(), types.TypeI32()))),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32(), types.TypeI32()))),
		values: []types.Value{types.I32(2)},
	},
	{
		// -0 and +0 are one f64 key.
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(math.Copysign(0, -1))), instr.MustNew(instr.I32_CONST, 10),
			instr.MustNew(instr.F64_CONST, 0), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeF64(), types.TypeI32()))),
		values: []types.Value{types.I32(1)},
	},
	{
		// -0 and +0 are one f64 key.
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(math.Copysign(0, -1))), instr.MustNew(instr.I32_CONST, 10),
			instr.MustNew(instr.F64_CONST, 0), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.F64_CONST, math.Float64bits(math.Copysign(0, -1))), instr.MustNew(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeF64(), types.TypeI32()))),
		values: []types.Value{types.I32(20)},
	},
	{
		// NaN never matches, so both entries stay.
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(math.NaN())), instr.MustNew(instr.I32_CONST, 10),
			instr.MustNew(instr.F64_CONST, math.Float64bits(math.NaN())), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeF64(), types.TypeI32()))),
		values: []types.Value{types.I32(2)},
	},
	{
		// NaN never matches, so get misses.
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(math.NaN())), instr.MustNew(instr.I32_CONST, 10),
			instr.MustNew(instr.F64_CONST, math.Float64bits(math.NaN())), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.F64_CONST, math.Float64bits(math.NaN())), instr.MustNew(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeF64(), types.TypeI32()))),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeI32(), types.TypeI32()))),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.I32_CONST, 42),
			instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.MAP_GET),
		}, program.WithTypes(types.NewMapType(types.TypeI32(), types.TypeAny()))),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.MAP_LOOKUP),
		}, program.WithTypes(types.NewMapType(types.TypeI32(), types.TypeI32()))),
		values: []types.Value{types.I1(true), types.I32(10)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.DUP),
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.MAP_SET),
			instr.MustNew(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32(), types.TypeI32()))),
		values: []types.Value{types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.DUP),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.MAP_DELETE),
			instr.MustNew(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32(), types.TypeI32()))),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.DUP),
			instr.MustNew(instr.MAP_CLEAR),
			instr.MustNew(instr.MAP_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32(), types.TypeI32()))),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.MAP_KEYS), instr.MustNew(instr.ARRAY_LEN),
		}, program.WithTypes(types.NewMapType(types.TypeI32(), types.TypeI32()))),
		values: []types.Value{types.I32(2)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_CONCAT),
			instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.DUP), instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.MAP_KEYS),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.MAP_CLEAR),
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		}, program.WithTypes(types.NewMapType(types.TypeString(), types.TypeI32())),
			program.WithConstants(types.String("Hi"), types.String("There")),
			program.WithLocals(types.NewMapType(types.TypeString(), types.TypeI32()))),
		values: []types.Value{types.String("HiThere")},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.MAP_NEW, 0),
			instr.MustNew(instr.MAP_ITER), instr.MustNew(instr.CORO_VALUE),
		}, program.WithTypes(types.NewMapType(types.TypeI32(), types.TypeI32()))),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 99),
			instr.MustNew(instr.THROW),
			instr.MustNew(instr.I32_CONST, 0),
			instr.MustNew(instr.NOP),
		}, program.WithHandlers(instr.Handler{Start: 0, End: 6, Catch: 11, Depth: 0})),
		values: []types.Value{types.I32(99)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.ERROR_NEW)}),
		values:  []types.Value{types.NewError(types.ErrorCode(7), "5", types.BoxI32(5))},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.ERROR_NEW), instr.MustNew(instr.ERROR_GET),
		}),
		values: []types.Value{types.I32(5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.ERROR_NEW), instr.MustNew(instr.ERROR_CODE),
		}),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.STRING_ITER), instr.MustNew(instr.CORO_VALUE)}, program.WithConstants(types.String("Hi"))),
		values:  []types.Value{types.I32(72)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.ARRAY_GET),
		}, program.WithConstants(types.TypedArray[int32]{3, 5})),
		values: []types.Value{types.I32(5)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.I32_CONST, 0),
			instr.MustNew(instr.STRUCT_GET),
		}, program.WithConstants(types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32())), types.BoxI32(7)))),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_CONST, 4),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
		}, program.WithConstants(interp.NewHostFunction(
			&types.FunctionType{Params: []types.Type{types.TypeI32(), types.TypeI32()}, Returns: []types.Type{types.TypeI32()}},
			func(_ *interp.Interpreter, args []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(args[0].I32() + args[1].I32())}, nil
			},
		))),
		values: []types.Value{types.I32(7)},
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.ARRAY_GET),
		}, program.WithTypes(types.TypeI32Array())),
		err: interp.ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.I32_CONST, 9), instr.MustNew(instr.ARRAY_SET),
		}, program.WithTypes(types.TypeI32Array())),
		err: interp.ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.ARRAY_FILL),
		}, program.WithTypes(types.TypeI32Array())),
		err: interp.ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.ARRAY_DELETE),
		}, program.WithTypes(types.TypeI32Array())),
		err: interp.ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.I32_CONST, 0),
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 5),
			instr.MustNew(instr.ARRAY_COPY),
		}, program.WithTypes(types.TypeI32Array())),
		err: interp.ErrIndexOutOfRange,
	},
	{
		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.I32_CONST, 2),
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, uint64(^uint32(0))),
			instr.MustNew(instr.ARRAY_COPY),
		}, program.WithTypes(types.TypeI32Array())),
		err: interp.ErrIndexOutOfRange,
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 42), instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_GET),
		}, program.WithConstants(types.TypedArray[int32]{1, 2, 3})),
		values: []types.Value{types.I32(42)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 42), instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI32Array()),
			program.WithLocals(types.TypeI32Array()),
		),
		values: []types.Value{types.I32(42)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.GLOBAL_SET, 0),
			instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 42), instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI32Array()),
			program.WithGlobals(types.TypeI32Array()),
		),
		values: []types.Value{types.I32(42)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CLOSURE_NEW),
			instr.MustNew(instr.CALL),
		},
			program.WithTypes(types.TypeI32Array()),
			program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
				Captures(types.TypeI32Array()).
				Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 42), instr.MustNew(instr.ARRAY_SET),
					instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_GET), instr.MustNew(instr.RETURN)).
				MustBuild()),
		),
		values: []types.Value{types.I32(42)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI1Array()),
			program.WithLocals(types.TypeI1Array()),
		),
		values: []types.Value{types.I1(true)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI8Array()),
			program.WithLocals(types.TypeI8Array()),
		),
		values: []types.Value{types.I8(7)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 42), instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI32Array()),
			program.WithLocals(types.TypeI32Array()),
		),
		values: []types.Value{types.I32(42)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I64_CONST, i64operand(42)), instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeI64Array()),
			program.WithLocals(types.TypeI64Array()),
		),
		values: []types.Value{types.I64(42)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(1.5))), instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeF32Array()),
			program.WithLocals(types.TypeF32Array()),
		),
		values: []types.Value{types.F32(1.5)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.F64_CONST, math.Float64bits(2.5)), instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithTypes(types.TypeF64Array()),
			program.WithLocals(types.TypeF64Array()),
		),
		values: []types.Value{types.F64(2.5)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 42), instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithTypes(types.NewArrayType(types.TypeAny())),
			program.WithLocals(types.TypeI32Array()),
		),
		values: []types.Value{types.I32(42)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
		},
			program.WithConstants(types.TypedArray[float32]{1.5}),
			program.WithLocals(types.TypeI32Array()),
		),
		values: []types.Value{types.F32(0)},
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.I32_CONST, 9), instr.MustNew(instr.ARRAY_SET),
		},
			program.WithTypes(types.TypeI32Array()),
			program.WithLocals(types.TypeI32Array()),
		),
		err: interp.ErrIndexOutOfRange,
	},
	{

		program: program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 9), instr.MustNew(instr.ARRAY_SET),
		}, program.WithLocals(types.TypeI32Array())),
		err: interp.ErrTypeMismatch,
	},
}

func (v *trackedValue) Kind() types.Kind { return types.KindRef }

func (v *trackedValue) Type() types.Type { return types.TypeAny() }

func (v *trackedValue) String() string { return "tracked" }

func (v *trackedValue) Refs(dst []types.Ref) []types.Ref {
	return append(dst, v.refs...)
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

func (v *trackedValue) Close() error {
	v.closed++
	return nil
}

func (*panicCloser) Close() error {
	panic("close")
}

func (h *structGetHostFields) Bump(n int32) int32 {
	h.Count += n
	h.hidden++
	return h.Count
}

func (v *marshalBenchMethods) Bump(n int32) int32 {
	v.Count += n
	v.hidden++
	return v.Count
}

// zeroFunction returns its locals, one of each zeroTypes, unwritten.
func zeroFunction() *types.Function {
	return types.NewFunctionBuilder(&types.FunctionType{Returns: zeroTypes}).
		Locals(zeroTypes...).
		Append(append(slices.Clone(zeroReads), instr.MustNew(instr.RETURN))...).
		MustBuild()
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
	for _, line := range lines[1:] {
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

func i32operand(v int32) uint64 {
	return uint64(uint32(v))
}

func i64operand(v int64) uint64 {
	return uint64(v)
}

// structSumTree builds a small binary-tree kernel shaped like
// benchmarks/memory_test.go's structTreeWalk, except sumFn's tree parameter
// is declared as the concrete node struct type instead of types.TypeAny, so
// every struct.get on it is a LOCAL_GET whose declared type is a concrete
// *types.StructType -- the shape interp/threaded.go's generated STRUCT_GET
// local-container fusion specializes.
func structSumTree(depth, repeats int32) *program.Program {
	nodeType := types.NewStructType(
		types.NewStructField(types.TypeI32(), types.FieldWithName("value")),
		types.NewStructField(types.TypeAny(), types.FieldWithName("left")),
		types.NewStructField(types.TypeAny(), types.FieldWithName("right")),
	)

	buildBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny()}}).
		Params(types.TypeI32()).
		Locals(types.TypeAny())
	buildDone := buildBuilder.Label()
	buildFn := buildBuilder.
		Append(
			instr.MustNew(instr.STRUCT_NEW_DEFAULT, 0), instr.MustNew(instr.LOCAL_SET, 1),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 0),
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.STRUCT_SET),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_LE_S),
		).
		BrIf(buildDone).
		Append(
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SUB),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
			instr.MustNew(instr.STRUCT_SET),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 2),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SUB),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
			instr.MustNew(instr.STRUCT_SET),
		).
		Bind(buildDone).
		Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.RETURN)).
		MustBuild()

	sumBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
		Params(nodeType)
	nullCase := sumBuilder.Label()
	sumFn := sumBuilder.
		Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.REF_IS_NULL)).
		BrIf(nullCase).
		Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.STRUCT_GET),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.STRUCT_GET),
			instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL),
			instr.MustNew(instr.I32_ADD),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.STRUCT_GET),
			instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL),
			instr.MustNew(instr.I32_ADD),
			instr.MustNew(instr.RETURN),
		).
		Bind(nullCase).
		Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.RETURN)).
		MustBuild()

	b := program.NewBuilder()
	buildIdx := b.Const(buildFn)
	sumIdx := b.Const(sumFn)
	b.Type(nodeType)
	b.Locals(types.TypeAny(), types.TypeI32(), types.TypeI32())
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
	b.Globals(types.TypeI32Array())
	b.Locals(types.TypeI32(), types.TypeI32(), types.TypeI32())
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

	sumBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
		Captures(types.TypeI32Array()).
		Locals(types.TypeI32(), types.TypeI32(), types.TypeI32())
	outerLoop, outerDone := sumBuilder.Label(), sumBuilder.Label()
	innerLoop, innerDone := sumBuilder.Label(), sumBuilder.Label()
	sumFn := sumBuilder.
		Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 2)).
		Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 0)).
		Bind(outerLoop).
		Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, uint64(uint32(repeats))), instr.MustNew(instr.I32_GE_S)).
		BrIf(outerDone).
		Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 1)).
		Bind(innerLoop).
		Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, uint64(uint32(size))), instr.MustNew(instr.I32_GE_S)).
		BrIf(innerDone).
		Append(
			instr.MustNew(instr.LOCAL_GET, 2),
			instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.ARRAY_GET),
			instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1),
		).
		Br(innerLoop).
		Bind(innerDone).
		Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 0)).
		Br(outerLoop).
		Bind(outerDone).
		Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.RETURN)).
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

// arrayFillLocal builds a kernel that holds a size-length int32 array in a
// declared LOCAL_GET slot and writes arr[j] = j repeats times in a nested
// loop, so every array.set is a LOCAL_GET whose declared type is a concrete
// *types.ArrayType -- the shape interp/threaded.go's generated ARRAY_SET
// local-container fusion specializes. A final pass sums the written array
// once to produce a checksum the benchmark can verify.
func arrayFillLocal(size, repeats int32) *program.Program {
	b := program.NewBuilder()
	b.Locals(types.TypeI32Array(), types.TypeI32(), types.TypeI32(), types.TypeI32())
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
	b.Globals(types.TypeI32Array())
	b.Locals(types.TypeI32(), types.TypeI32(), types.TypeI32())
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
	fillBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
		Captures(types.TypeI32Array()).
		Locals(types.TypeI32(), types.TypeI32(), types.TypeI32())
	outerLoop, outerDone := fillBuilder.Label(), fillBuilder.Label()
	innerLoop, innerDone := fillBuilder.Label(), fillBuilder.Label()
	sumLoop, sumDone := fillBuilder.Label(), fillBuilder.Label()
	fillFn := fillBuilder.
		Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 0)).
		Bind(outerLoop).
		Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, uint64(uint32(repeats))), instr.MustNew(instr.I32_GE_S)).
		BrIf(outerDone).
		Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 1)).
		Bind(innerLoop).
		Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, uint64(uint32(size))), instr.MustNew(instr.I32_GE_S)).
		BrIf(innerDone).
		Append(
			instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1),
		).
		Br(innerLoop).
		Bind(innerDone).
		Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 0)).
		Br(outerLoop).
		Bind(outerDone).
		Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 2)).
		Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 1)).
		Bind(sumLoop).
		Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, uint64(uint32(size))), instr.MustNew(instr.I32_GE_S)).
		BrIf(sumDone).
		Append(
			instr.MustNew(instr.LOCAL_GET, 2),
			instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.ARRAY_GET),
			instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1),
		).
		Br(sumLoop).
		Bind(sumDone).
		Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.RETURN)).
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

// structGetHostLoop reads field 0 of a host-backed struct repeats times,
// accumulating the reads so nothing is optimized away.
func structGetHostLoop(repeats int32) *program.Program {
	b := program.NewBuilder()
	b.Locals(types.TypeAny(), types.TypeI32(), types.TypeI32())
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
				instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_ADD),
			}),
			want: types.I32(3),
			ops:  3,
		},
		{
			name: "local arithmetic store",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.LOCAL_SET, 0),
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 0),
				instr.MustNew(instr.LOCAL_GET, 0),
			}, program.WithLocals(types.TypeI32())),
			want: types.I32(8),
			ops:  7,
		},
		{
			name: "typed array load",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_GET),
			}, program.WithConstants(types.TypedArray[int32]{3, 5})),
			want: types.I32(5),
			ops:  3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			i := interp.MustNew(tt.prog, interp.WithTick(1), interp.WithHook(func(*interp.Interpreter) error {
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
	i := interp.MustNew(program.New(nil), interp.WithCodec(upperCodec(0)))
	defer i.Close()

	v, err := i.Marshal("go")
	require.NoError(t, err)
	require.Equal(t, types.String("GO"), v)

	var dst string
	require.NoError(t, i.Unmarshal(v, &dst))
	require.Equal(t, "go", dst)
}

func TestWithProfiler(t *testing.T) {
	profiler := prof.New()
	i := interp.MustNew(program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1)}), interp.WithProfiler(profiler), interp.WithTick(1))
	defer i.Close()
	require.NoError(t, i.Run(context.Background()))

	i.Flush()
	samples, ok := profiler.Metric("vm_samples_total")
	require.True(t, ok)
	require.Equal(t, float64(1), samples)
}

func TestWithFrame(t *testing.T) {
	t.Run("function call overflows once frames are exhausted", func(t *testing.T) {
		selfFn := types.NewFunctionBuilder(&types.FunctionType{}).Append(
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL), instr.MustNew(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
		}, program.WithConstants(selfFn))
		i := interp.MustNew(prog, interp.WithFrame(3))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrFrameOverflow)
	})

	t.Run("host call succeeds once frames are exhausted", func(t *testing.T) {
		hostFn := interp.NewHostFunction(&types.FunctionType{Returns: []types.Type{types.TypeI32()}},
			func(_ *interp.Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(1)}, nil
			})
		fillFn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).Append(
			instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL), instr.MustNew(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
		}, program.WithConstants(fillFn, hostFn))
		i := interp.MustNew(prog, interp.WithFrame(2))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1), v)
	})
}

func TestWithStack(t *testing.T) {
	t.Run("reports overflow", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 3),
		})
		i := interp.MustNew(prog, interp.WithStack(2))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrStackOverflow)
	})

	t.Run("zero normalizes to one slot", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1),
		})
		i := interp.MustNew(prog, interp.WithStack(0))
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
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.REF_NEW),
		})
		i := interp.MustNew(prog, interp.WithHeap(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 3, i.Len())
	})

	t.Run("collects cycle at backing capacity", func(t *testing.T) {
		i := interp.MustNew(program.New(nil), interp.WithHeap(2))
		defer i.Close()

		value, addr := selfCycle(t, i)

		reused, err := i.Alloc(types.I32(1))
		require.NoError(t, err)
		require.Equal(t, addr, reused)
		require.Equal(t, 1, value.closed)
	})

	t.Run("negative capacity normalizes", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.REF_NEW),
		})
		i := interp.MustNew(prog, interp.WithHeap(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 1, i.Len())
	})

	t.Run("collects cycles at adaptive goal", func(t *testing.T) {
		const capacity = 2 * heapRunway

		i := interp.MustNew(program.New(nil), interp.WithHeap(capacity), interp.WithHeapLimit(capacity))
		defer i.Close()

		_, err := i.Alloc(types.I32(1))
		require.NoError(t, err)
		for range capacity - 2 {
			selfCycle(t, i)
		}

		_, err = i.Alloc(types.I32(2))
		require.NoError(t, err)

		cycle, _ := selfCycle(t, i)

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

		i := interp.MustNew(program.New(nil), interp.WithHeap(capacity), interp.WithHeapLimit(capacity))
		defer i.Close()

		for n := range heapRunway + 1 {
			_, err := i.Alloc(types.I32(n))
			require.NoError(t, err)
		}
		for range capacity - heapRunway - 2 {
			selfCycle(t, i)
		}

		_, err := i.Alloc(types.I32(heapRunway + 1))
		require.NoError(t, err)

		cycle, _ := selfCycle(t, i)

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

		i := interp.MustNew(program.New(nil), interp.WithHeap(capacity), interp.WithHeapLimit(4*heapRunway))
		defer i.Close()

		for n := range capacity {
			_, err := i.Alloc(types.I32(n))
			require.NoError(t, err)
		}
		i.Reset()

		cycle, _ := selfCycle(t, i)

		for n := range heapRunway - 1 {
			_, err := i.Alloc(types.I32(n))
			require.NoError(t, err)
		}
		require.Equal(t, 0, cycle.closed)

		_, err := i.Alloc(types.I32(heapRunway - 1))
		require.NoError(t, err)
		require.Equal(t, 1, cycle.closed)
	})
}

func TestWithHeapLimit(t *testing.T) {
	t.Run("rejects live heap at limit", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.REF_NEW)})
		i := interp.MustNew(prog, interp.WithHeapLimit(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrHeapExhausted)
	})

	t.Run("preserves host-owned reference", func(t *testing.T) {
		i := interp.MustNew(program.New(nil), interp.WithHeap(2), interp.WithHeapLimit(2))
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
		mid := types.NewArray(types.NewArrayType(types.TypeAny()), types.BoxRef(leafAddr))
		root := types.NewArray(types.NewArrayType(types.TypeAny()), types.BoxRef(midAddr), types.BoxRef(midAddr))
		prog := program.New(nil, program.WithConstants(leaf, mid, root))
		i := interp.MustNew(prog, interp.WithHeap(4), interp.WithHeapLimit(4))
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
		i := interp.MustNew(program.New(nil), interp.WithHeap(3), interp.WithHeapLimit(3))
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
		i := interp.MustNew(program.New(nil), interp.WithHeap(3), interp.WithHeapLimit(3))
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
		i := interp.MustNew(program.New(nil), interp.WithHeap(2), interp.WithHeapLimit(2))
		defer i.Close()

		value, _ := selfCycle(t, i)

		reused, err := i.Alloc(types.String("reused"))
		require.NoError(t, err)
		require.Equal(t, 1, value.closed)
		require.NoError(t, i.Release(reused))
	})

	t.Run("collects duplicate cycle edges", func(t *testing.T) {
		i := interp.MustNew(program.New(nil), interp.WithHeap(3), interp.WithHeapLimit(3))
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
		i := interp.MustNew(program.New(nil), interp.WithHeap(4), interp.WithHeapLimit(4))
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

	t.Run("fails a native STRUCT_NEW_DEFAULT where threaded fails", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := links(t, 2000, -1, true)

		limit := fits(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithHeapLimit(limit))
		defer vm.Close()
		warm(t, vm, profiler)
		vm.Flush()
		deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		require.NoError(t, vm.Run(context.Background()))
		vm.Flush()
		after, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		require.Equal(t, deopts, after)
		vm.Reset()

		shortProfiler := prof.New()
		short := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(shortProfiler), interp.WithHeapLimit(limit-1))
		defer short.Close()
		warm(t, short, shortProfiler)
		require.ErrorIs(t, short.Run(context.Background()), interp.ErrHeapExhausted)

		threaded := interp.MustNew(prog, interp.WithThreshold(-1), interp.WithHeapLimit(limit-1))
		defer threaded.Close()
		require.ErrorIs(t, threaded.Run(context.Background()), interp.ErrHeapExhausted)
		require.Equal(t, liveOf(threaded), liveOf(short))
	})

	t.Run("fails a native ARRAY_NEW_DEFAULT where threaded fails", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := chains(t, 2000, 0, 1, 1)

		limit := fits(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithHeapLimit(limit))
		defer vm.Close()
		warm(t, vm, profiler)
		vm.Flush()
		entered := nativeEntries(profiler)
		require.NoError(t, vm.Run(context.Background()))
		vm.Flush()
		require.Greater(t, nativeEntries(profiler), entered)
		vm.Reset()

		shortProfiler := prof.New()
		short := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(shortProfiler), interp.WithHeapLimit(limit-1))
		defer short.Close()
		warm(t, short, shortProfiler)
		require.ErrorIs(t, short.Run(context.Background()), interp.ErrHeapExhausted)

		threaded := interp.MustNew(prog, interp.WithThreshold(-1), interp.WithHeapLimit(limit-1))
		defer threaded.Close()
		require.ErrorIs(t, threaded.Run(context.Background()), interp.ErrHeapExhausted)
		require.Equal(t, liveOf(threaded), liveOf(short))
	})
}

func TestWithTick(t *testing.T) {
	calls := 0
	prog := program.New([]instr.Instruction{
		instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_CONST, 4),
	})
	i := interp.MustNew(prog, interp.WithTick(2), interp.WithHook(func(i *interp.Interpreter) error {
		calls++
		return nil
	}))
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	require.Equal(t, 2, calls)
}

func TestWithFuel(t *testing.T) {
	prog := program.New([]instr.Instruction{
		instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_ADD),
	})
	i := interp.MustNew(prog, interp.WithTick(1), interp.WithFuel(2))
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrFuelExhausted)
}

func TestWithThreshold(t *testing.T) {
	t.Run("raises a divide by zero whose quotient native code drops", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		typ := &types.FunctionType{Params: []types.Type{types.TypeI32(), types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}
		b := types.NewFunctionBuilder(typ)
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_EQ).BrIf(done)
		b.Emit(instr.I32_CONST, 7).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
		prog := program.NewBuilder().
			Globals(types.TypeI32()).
			Emit(instr.I32_CONST, 64).Emit(instr.GLOBAL_GET, 0).ConstGet(b.MustBuild()).Emit(instr.CALL).
			MustBuild()

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		run := func(divisor int32) error {
			vm.Reset()
			require.NoError(t, vm.SetGlobal(0, types.BoxI32(divisor)))
			return vm.Run(context.Background())
		}
		poll(t, func() bool {
			require.NoError(t, run(1))
			vm.Flush()
			return nativeEntries(profiler) > 0
		})

		require.ErrorIs(t, run(0), interp.ErrDivideByZero)
	})

	t.Run("runs a call of a leaf callee as threaded does", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		for _, indirect := range []bool{false, true} {
			prog := leafSumProgram(t, indirect)
			run := func(vm *interp.Interpreter, divisor int32) (int32, error) {
				vm.Reset()
				require.NoError(t, vm.SetGlobal(0, types.BoxI32(divisor)))
				if indirect {
					require.NoError(t, vm.SetGlobal(1, types.BoxRef(1)))
				}
				if err := vm.Run(context.Background()); err != nil {
					return 0, err
				}
				v, err := vm.Pop()
				require.NoError(t, err)
				return int32(v.(types.I32)), nil
			}
			threaded := interp.MustNew(prog, interp.WithThreshold(-1))
			defer threaded.Close()
			profiler := prof.New()
			vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
			defer vm.Close()

			// Both the sum and its caller graduate to Optimized after 1024 entries.
			poll(t, func() bool {
				for range 1100 {
					_, err := run(vm, 1)
					require.NoError(t, err)
				}
				vm.Flush()
				compiled, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
				return compiled >= 2
			})

			// A zero divisor traps inside the leaf; native code must raise the
			// same error from the callee, then keep running afterwards.
			for range 40 {
				for _, divisor := range []int32{1, 3, -7, 5, 0, 2, 0, 4, 9} {
					want, wantErr := run(threaded, divisor)
					got, gotErr := run(vm, divisor)
					require.Equal(t, wantErr, gotErr, "indirect=%v divisor=%d", indirect, divisor)
					require.Equal(t, want, got, "indirect=%v divisor=%d", indirect, divisor)
				}
				time.Sleep(time.Millisecond)
			}
			vm.Flush()
			require.Positive(t, nativeEntries(profiler))
		}
	})

	t.Run("reports a fused trap at the faulting instruction in every tier", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		tests := []struct {
			name  string
			body  func(b *instr.Builder, target int)
			good  int32
			bad   int32
			null  int
			limit int
			// boxed defers wide i64 boxes in native code, so a heap limit trips at
			// another instruction, in the same frames.
			boxed bool
		}{
			{
				name: "array.get on a null container",
				body: func(b *instr.Builder, _ int) {
					b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.DROP)
				},
				bad:  1,
				null: 2,
			},
			{
				name: "array.get past the end of a typed array",
				body: func(b *instr.Builder, _ int) {
					b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET).Emit(instr.DROP)
				},
				bad: 1000,
			},
			{
				name: "array.set past the end of a typed array",
				body: func(b *instr.Builder, _ int) {
					b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
				},
				bad: 1000,
			},
			{
				name: "struct.get on a null container",
				body: func(b *instr.Builder, _ int) {
					b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.DROP)
				},
				bad:  1,
				null: 4,
			},
			{
				name: "i32.div_s by a zero local after a pending operand",
				body: func(b *instr.Builder, _ int) {
					b.Emit(instr.I32_CONST, 7).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.DROP)
				},
				good: 1,
			},
			{
				name: "i32.div_s by zero",
				body: func(b *instr.Builder, _ int) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.DROP)
				},
				good: 1,
			},
			{
				name: "closure.new on an exhausted heap",
				body: func(b *instr.Builder, target int) {
					skip := b.Label()
					b.Emit(instr.LOCAL_GET, 1).BrIf(skip)
					b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, uint64(target)).Emit(instr.CLOSURE_NEW).Emit(instr.ARRAY_SET)
					b.Bind(skip)
				},
				good:  1,
				limit: 40,
			},
			{
				name: "i64.add boxing a wide result into a local on an exhausted heap",
				body: func(b *instr.Builder, _ int) {
					skip := b.Label()
					b.Emit(instr.LOCAL_GET, 1).BrIf(skip)
					b.Emit(instr.I64_CONST, uint64(1)<<60).Emit(instr.LOCAL_SET, 5)
					b.Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 5).Emit(instr.I64_ADD).Emit(instr.LOCAL_SET, 6)
					b.Bind(skip)
				},
				good:  1,
				limit: 7,
				boxed: true,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				pb := program.NewBuilder().Globals(types.TypeI32())
				pb.Type(types.NewArrayType(types.TypeI32()))
				pb.Type(types.NewArrayType(types.TypeAny()))
				pb.Type(types.NewStructType(types.NewStructField(types.TypeI32())))
				target := pb.Const(incFunction())

				b := instr.NewBuilder()
				loop, done, keep := b.Label(), b.Label(), b.Label()
				b.Emit(instr.I32_CONST, 128).Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 2)
				b.Emit(instr.I32_CONST, 128).Emit(instr.ARRAY_NEW_DEFAULT, 1).Emit(instr.LOCAL_SET, 3)
				b.Emit(instr.STRUCT_NEW_DEFAULT, 2).Emit(instr.LOCAL_SET, 4)
				if tt.null != 0 {
					b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_EQZ).BrIf(keep)
					b.Emit(instr.REF_NULL).Emit(instr.LOCAL_SET, uint64(tt.null))
					b.Bind(keep)
				}
				b.Bind(loop)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_EQZ).BrIf(done)
				tt.body(b, target)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB).Emit(instr.LOCAL_SET, 0).Br(loop)
				b.Bind(done)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
				code, err := b.Assemble()
				require.NoError(t, err)
				fn := &types.Function{
					Typ: &types.FunctionType{Params: []types.Type{types.TypeI32(), types.TypeI32()}, Returns: []types.Type{types.TypeI32()}},
					Locals: []types.Type{
						types.NewArrayType(types.TypeI32()),
						types.NewArrayType(types.TypeAny()),
						types.NewStructType(types.NewStructField(types.TypeI32())),
						types.TypeI64(),
						types.TypeI64(),
					},
					Code: instr.Marshal(code),
				}
				prog := pb.Emit(instr.I32_CONST, 64).Emit(instr.GLOBAL_GET, 0).ConstGet(fn).Emit(instr.CALL).MustBuild()

				run := func(vm *interp.Interpreter, arg int32) error {
					vm.Reset()
					require.NoError(t, vm.SetGlobal(0, types.BoxI32(arg)))
					return vm.Run(context.Background())
				}
				opts := []interp.Option{}
				if tt.limit > 0 {
					opts = append(opts, interp.WithHeapLimit(tt.limit))
				}

				exact := interp.MustNew(prog, append(opts, interp.WithTick(1))...)
				defer exact.Close()
				want := run(exact, tt.bad)
				require.Error(t, want)

				threaded := interp.MustNew(prog, append(opts, interp.WithThreshold(-1))...)
				defer threaded.Close()
				require.EqualError(t, run(threaded, tt.bad), want.Error())
				profiler := prof.New()
				native := interp.MustNew(prog, append(opts, interp.WithThreshold(1), interp.WithProfiler(profiler))...)
				defer native.Close()
				poll(t, func() bool {
					require.NoError(t, run(native, tt.good))
					native.Flush()
					return nativeEntries(profiler) > 0
				})
				got := run(native, tt.bad)
				require.Error(t, got)
				if tt.boxed {
					ip := regexp.MustCompile(`ip=\d+`)
					require.Equal(t, ip.ReplaceAllString(want.Error(), "ip"), ip.ReplaceAllString(got.Error(), "ip"))
					return
				}
				require.EqualError(t, got, want.Error())
			})
		}
	})

	t.Run("reports threaded's heap exhaustion from a callout with threaded's counts", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		b := instr.NewBuilder()
		loop, inner, next := b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.DUP)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(next)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(inner)
		b.Bind(next)
		b.Br(loop)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeAny(), types.TypeI32()), program.WithTypes(types.NewArrayType(types.TypeAny())))

		threaded := interp.MustNew(prog, interp.WithThreshold(-1), interp.WithHeapLimit(300))
		defer threaded.Close()
		wantErr := threaded.Run(context.Background())
		require.ErrorIs(t, wantErr, interp.ErrHeapExhausted)

		wantCounts := make([]int, threaded.HeapLen()-1)
		for j := range wantCounts {
			wantCounts[j], _ = threaded.RefCount(j + 1)
		}

		var runErr error
		var bridges float64
		var counts []int
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithHeapLimit(300))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			counts = make([]int, vm.HeapLen()-1)
			for j := range counts {
				counts[j], _ = vm.RefCount(j + 1)
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return bridges > 0
		})
		require.Equal(t, wantErr, runErr)
		require.Equal(t, wantCounts, counts)
	})

	t.Run("serves array.slice as a callout with threaded's results and counts", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		// slicer sums the lengths of n slices of an eight-element array of
		// elem; refs fills two of its elements, so a ref slice copies them.
		slicer := func(elem types.Type, refs bool, n uint64, from, to func(b *instr.Builder)) *program.Program {
			b := instr.NewBuilder()
			loop, done := b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 8).Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 0)
			if refs {
				for _, at := range []uint64{0, 3} {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, at).Emit(instr.CONST_GET, 0).Emit(instr.ARRAY_SET)
				}
			}
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, n).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 0)
			from(b)
			to(b)
			b.Emit(instr.ARRAY_SLICE).Emit(instr.ARRAY_LEN)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(loop)
			b.Bind(done).Emit(instr.LOCAL_GET, 2)
			code, err := b.Assemble()
			require.NoError(t, err)
			return program.New(code,
				program.WithLocals(types.TypeAny(), types.TypeI32(), types.TypeI32()),
				program.WithTypes(types.NewArrayType(elem)),
				program.WithConstants(types.String("ref")))
		}
		konst := func(v int32) func(b *instr.Builder) {
			return func(b *instr.Builder) { b.Emit(instr.I32_CONST, uint64(uint32(v))) }
		}
		counter := func(b *instr.Builder) { b.Emit(instr.LOCAL_GET, 1) }

		for _, tt := range []struct {
			name     string
			elem     types.Type
			refs     bool
			from, to func(b *instr.Builder)
		}{
			{"typed range", types.TypeI32(), false, konst(1), konst(5)},
			{"typed empty", types.TypeI32(), false, konst(3), konst(3)},
			{"typed full", types.TypeI32(), false, konst(0), konst(8)},
			{"ref range", types.TypeAny(), true, konst(1), konst(5)},
			{"ref full", types.TypeAny(), true, konst(0), konst(8)},
			{"ref empty", types.TypeAny(), true, konst(2), konst(2)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				prog := slicer(tt.elem, tt.refs, 500, tt.from, tt.to)
				threaded := interp.MustNew(prog, interp.WithThreshold(-1))
				defer threaded.Close()
				require.NoError(t, threaded.Run(context.Background()))
				want, err := threaded.Pop()
				require.NoError(t, err)
				wantCounts := make([]int, threaded.HeapLen()-1)
				for j := range wantCounts {
					wantCounts[j], _ = threaded.RefCount(j + 1)
				}

				// Callouts count as bridge exits, so native entries stand in for
				// the exit: the loop ran native and matched threaded.
				profiler := prof.New()
				vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
				defer vm.Close()
				var got types.Value
				var counts []int
				poll(t, func() bool {
					require.NoError(t, vm.Run(context.Background()))
					got, err = vm.Pop()
					require.NoError(t, err)
					counts = make([]int, vm.HeapLen()-1)
					for j := range counts {
						counts[j], _ = vm.RefCount(j + 1)
					}
					vm.Reset()
					vm.Flush()
					return nativeEntries(profiler) > 0
				})
				require.Equal(t, want, got)
				require.Equal(t, wantCounts, counts)
			})
		}

		for _, tt := range []struct {
			name     string
			elem     types.Type
			refs     bool
			from, to func(b *instr.Builder)
		}{
			{"end past the length", types.TypeI32(), false, konst(0), counter},
			{"ref end past the length", types.TypeAny(), true, konst(0), counter},
			{"negative start", types.TypeI32(), false, konst(-1), konst(4)},
			{"start past end", types.TypeAny(), true, konst(5), konst(2)},
		} {
			t.Run("traps "+tt.name+" as threaded", func(t *testing.T) {
				prog := slicer(tt.elem, tt.refs, 12, tt.from, tt.to)
				ip := regexp.MustCompile(`ip=\d+`)

				threaded := interp.MustNew(prog, interp.WithThreshold(-1))
				defer threaded.Close()
				want := threaded.Run(context.Background())
				require.ErrorIs(t, want, interp.ErrIndexOutOfRange)
				wantCounts := make([]int, threaded.HeapLen()-1)
				for j := range wantCounts {
					wantCounts[j], _ = threaded.RefCount(j + 1)
				}

				profiler := prof.New()
				vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
				defer vm.Close()
				var got error
				var counts []int
				poll(t, func() bool {
					got = vm.Run(context.Background())
					counts = make([]int, vm.HeapLen()-1)
					for j := range counts {
						counts[j], _ = vm.RefCount(j + 1)
					}
					vm.Reset()
					vm.Flush()
					return nativeEntries(profiler) > 0
				})
				require.Error(t, got)
				require.Equal(t, ip.ReplaceAllString(want.Error(), "ip"), ip.ReplaceAllString(got.Error(), "ip"))
				require.Equal(t, wantCounts, counts)
			})
		}
	})

	t.Run("enters a hot recursive function's native code", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := fibCallsProgram(t, 20)
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var entries float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("runs a hot function natively by default", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := fibCallsProgram(t, 20)
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var entries float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries = nativeEntries(profiler)
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("enters native code within one long profiled Run by default", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := fibCallsProgram(t, 2048)
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.Pop()
		require.NoError(t, err)
		vm.Flush()

		require.Equal(t, want, got)
		require.Greater(t, nativeEntries(profiler), float64(0))
	})

	t.Run("enters native code within one long Run without safepoints by default", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		for _, prog := range []*program.Program{
			fibCallsProgram(t, 2048),
			iterativeFibProgram(t, 4<<20),
		} {
			want := runProgram(t, prog)

			profiler := prof.New()
			vm := interp.MustNew(prog, interp.WithProfiler(profiler), interp.WithTick(1<<30))
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			vm.Flush()
			require.NoError(t, vm.Close())

			require.Equal(t, want, got)
			require.Greater(t, nativeEntries(profiler), float64(0))
		}
	})

	t.Run("enters native code under a hook or fuel", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 20).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		for _, prog := range []*program.Program{
			fibCallsProgram(t, 64),
			iterativeFibProgram(t, 1<<16),
			program.New(code, program.WithConstants(fibFunction())),
		} {
			want := runProgram(t, prog)
			for _, opt := range []interp.Option{
				interp.WithHook(func(*interp.Interpreter) error { return nil }),
				interp.WithFuel(math.MaxUint64),
			} {
				profiler := prof.New()
				vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), opt)
				var runErr, popErr error
				var result types.Value
				run := func() bool {
					if runErr = vm.Run(context.Background()); runErr == nil {
						result, popErr = vm.Pop()
					}
					vm.Reset()
					vm.Flush()
					return runErr == nil && popErr == nil && result == want
				}
				poll(t, func() bool {
					return !run() || nativeEntries(profiler) > 0
				})

				for range 64 {
					if !run() {
						break
					}
				}
				require.NoError(t, vm.Close())
				require.NoError(t, runErr)
				require.NoError(t, popErr)
				require.Equal(t, want, result)
			}
		}
	})

	t.Run("exhausts fuel inside a native loop", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		prog := iterativeFibProgram(t, 1<<22)
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithFuel(1<<20))
		defer vm.Close()
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			vm.Flush()
			return runErr != interp.ErrFuelExhausted || nativeEntries(profiler) > 0
		})
		require.Equal(t, interp.ErrFuelExhausted, runErr)
	})

	t.Run("shows a hook threaded state inside a native loop and keeps its writes", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n = 1 << 20
		prog := iterativeFibProgram(t, n)
		// The loop header follows the three local initializations; the
		// locals hold through its exit test, the next four instructions.
		var bounds [11]int
		for k := 1; k < len(bounds); k++ {
			bounds[k] = bounds[k-1] + instr.Instruction(prog.Code[bounds[k-1]:]).Width()
		}
		header, body := bounds[6], bounds[10]
		fibs := make([]int32, n+2)
		fibs[1] = 1
		for k := 2; k < len(fibs); k++ {
			fibs[k] = fibs[k-1] + fibs[k-2]
		}

		seen, stop, at, torn := 0, -1, 0, false
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithHook(func(vm *interp.Interpreter) error {
			ip := vm.IP()
			if ip < header || ip > body || stop >= 0 {
				return nil
			}
			var locals [3]int32
			for slot := range locals {
				v, err := vm.Local(slot)
				if err != nil {
					return err
				}
				locals[slot] = v.I32()
			}
			i := locals[0]
			torn = torn || i < 0 || i >= n || locals[1] != fibs[i] || locals[2] != fibs[i+1]
			if seen++; seen == 64 {
				stop, at = int(i), ip
				return vm.SetLocal(0, types.BoxI32(n))
			}
			return nil
		}))
		defer vm.Close()
		var runErr, popErr error
		var result types.Value
		run := func() {
			seen, stop = 0, -1
			if runErr = vm.Run(context.Background()); runErr == nil {
				result, popErr = vm.Pop()
			}
			vm.Reset()
			vm.Flush()
		}
		poll(t, func() bool {
			run()
			return runErr != nil || popErr != nil || nativeEntries(profiler) > 0
		})
		entries := nativeEntries(profiler)
		run()

		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Greater(t, nativeEntries(profiler), entries)
		require.False(t, torn)
		require.Positive(t, stop)
		want := fibs[stop+1]
		if at == header {
			want = fibs[stop]
		}
		require.Equal(t, types.I32(want), result)
	})

	t.Run("shows a hook the native callers of a call it runs", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		g := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}})
		refuse(g)
		g.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.RETURN))
		f := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).
			Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL), instr.MustNew(instr.RETURN)).
			MustBuild()
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1<<12).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32(), types.TypeI32()), program.WithConstants(f, g.MustBuild()))

		resume := 0
		for instr.Instruction(prog.Code[resume:]).Opcode() != instr.CALL {
			resume += instr.Instruction(prog.Code[resume:]).Width()
		}
		resume++

		ips := map[int]int{}
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithTick(2), interp.WithProfiler(profiler), interp.WithHook(func(vm *interp.Interpreter) error {
			callee, err := vm.Const(1)
			if err != nil || vm.Func() != callee.Ref() {
				return err
			}
			_, ip, err := vm.Frame(2)
			ips[ip]++
			return err
		}))
		defer vm.Close()
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			vm.Flush()
			return runErr != nil || nativeEntries(profiler) > 0
		})
		require.NoError(t, runErr)
		clear(ips)
		require.NoError(t, vm.Run(context.Background()))

		require.Equal(t, []int{resume}, slices.Collect(maps.Keys(ips)))
	})

	t.Run("stops a Run with a hook's error inside native code", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := iterativeFibProgram(t, 1<<20)
		stop := errors.New("stop")
		calls := 0
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithHook(func(*interp.Interpreter) error {
			if calls++; calls == 256 {
				return stop
			}
			return nil
		}))
		defer vm.Close()
		var runErr error
		poll(t, func() bool {
			calls = 0
			runErr = vm.Run(context.Background())
			vm.Reset()
			vm.Flush()
			return runErr != stop || nativeEntries(profiler) > 0
		})
		require.Equal(t, stop, runErr)
	})

	t.Run("runs WithTick(1) exactly, without native code", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := iterativeFibProgram(t, 1<<12)
		count := func(opts ...interp.Option) int {
			calls := 0
			vm := interp.MustNew(prog, append(opts, interp.WithTick(1), interp.WithHook(func(*interp.Interpreter) error {
				calls++
				return nil
			}))...)
			defer vm.Close()
			for range 8 {
				require.NoError(t, vm.Run(context.Background()))
				vm.Reset()
			}
			return calls
		}
		want := count(interp.WithThreshold(-1))

		profiler := prof.New()
		got := count(interp.WithThreshold(1), interp.WithProfiler(profiler))

		require.Equal(t, want, got)
		require.Zero(t, nativeEntries(profiler))
	})

	t.Run("recompiles a invalidated function only after more calls than its first compile", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		inc := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}})
		refuse(inc)
		inc.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN))
		dec := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}})
		refuse(dec)
		dec.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SUB), instr.MustNew(instr.RETURN))
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 7).Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithGlobals(types.TypeAny()),
			program.WithConstants(applyFunction(), inc.MustBuild(), dec.MustBuild()))

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		// calls runs the program with callee constant c until apply has
		// compiled ok compiles in total, and returns how many calls it took.
		const step = 8
		var runErr error
		calls := func(c, ok int) int {
			callee, err := vm.Const(c)
			require.NoError(t, err)
			n := 0
			poll(t, func() bool {
				for range step {

					if runErr = vm.SetGlobal(0, callee); runErr != nil {
						return true
					}
					if runErr = vm.Run(context.Background()); runErr != nil {
						return true
					}
					vm.Reset()
				}
				n += step
				vm.Flush()
				v, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
				return v >= float64(ok)
			})
			require.NoError(t, runErr)
			return n
		}
		first := calls(1, 1)
		second := calls(2, 2)

		require.Greater(t, second, 2*first)
	})

	t.Run("runs code shorter than a Go entry threaded by default", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		for _, prog := range []*program.Program{
			program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.NOP)}),
			program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(incFunction())),
		} {
			profiler := prof.New()
			vm := interp.MustNew(prog, interp.WithProfiler(profiler))

			for range 8 {
				for range 512 {
					require.NoError(t, vm.Run(context.Background()))
					vm.Reset()
				}
				time.Sleep(5 * time.Millisecond)
			}
			vm.Flush()
			require.NoError(t, vm.Close())

			require.Zero(t, nativeEntries(profiler))
		}
	})

	t.Run("runs a self tail call as a native loop", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := tailRefProgram(t, 1000)
		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		held, err := threaded.Const(1)
		require.NoError(t, err)
		wantCount, err := threaded.RefCount(held.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		var runErr, popErr, countErr error
		var result types.Value
		var count int
		var entries float64
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			count, countErr = vm.RefCount(held.Ref())
			vm.Reset()
			vm.Flush()
			entries = nativeEntries(profiler)
			return entries >= 8
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, countErr)
		require.Equal(t, want, result)
		require.Equal(t, wantCount, count)

		before := entries
		for range 16 {
			require.NoError(t, vm.Run(context.Background()))
			result, err = vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, result)
			count, err = vm.RefCount(held.Ref())
			require.NoError(t, err)
			require.Equal(t, wantCount, count)
			vm.Reset()
		}
		vm.Flush()
		require.Greater(t, nativeEntries(profiler), before)
		deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		require.Zero(t, deopts)
	})

	t.Run("runs mutual tail calls as one native unit", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n = 1000
		for _, tt := range []struct {
			name    string
			slow    int
			dynamic bool
		}{
			{"returning normally", -1, false},
			{"deoptimizing inside the second member", n - 501, false},
			{"entered through an owned reference", -1, true},
			{"entered through an owned reference and deoptimizing inside the second member", n - 501, true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				prog := pingPongProgram(t, n, tt.slow, tt.dynamic)
				refs := []int{0, 1, 2}

				threaded := interp.MustNew(prog, interp.WithThreshold(-1))
				wantErr := threaded.Run(context.Background())
				var want types.Value
				if tt.slow < 0 {
					var err error
					want, err = threaded.Pop()
					require.NoError(t, err)
				} else {
					require.Error(t, wantErr)
				}
				counts := make([]int, len(refs))
				for i, c := range refs {
					boxed, err := threaded.Const(c)
					require.NoError(t, err)
					counts[i], err = threaded.RefCount(boxed.Ref())
					require.NoError(t, err)
				}
				require.NoError(t, threaded.Close())

				profiler := prof.New()
				vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
				defer vm.Close()
				run := func() {
					err := vm.Run(context.Background())
					if tt.slow < 0 {
						require.NoError(t, err)
						result, err := vm.Pop()
						require.NoError(t, err)
						require.Equal(t, want, result)
					} else {
						require.True(t, errorsEqual(err, wantErr), "%v != %v", err, wantErr)
					}
					for i, c := range refs {
						boxed, err := vm.Const(c)
						require.NoError(t, err)
						count, err := vm.RefCount(boxed.Ref())
						require.NoError(t, err)
						require.Equal(t, counts[i], count, "constant %d", c)
					}
					vm.Reset()
				}
				var entries float64
				poll(t, func() bool {
					run()
					vm.Flush()
					entries = nativeEntries(profiler)
					compiled, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
					return entries >= 8 && compiled >= 2
				})

				before := entries
				deopted, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
				for range 16 {
					run()
				}
				vm.Flush()
				require.Greater(t, nativeEntries(profiler), before)
				deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
				if tt.slow < 0 {
					require.Equal(t, deopted, deopts)
				} else {
					require.Greater(t, deopts, deopted)
				}
			})
		}
	})

	t.Run("recompiles with a guard refuted inside a tail-call member bridged", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n = 1000
		prog := memberGuardProgram(t, n, n-501)

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		require.ErrorIs(t, threaded.Run(context.Background()), interp.ErrTypeMismatch)
		require.NoError(t, threaded.Close())

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			require.ErrorIs(t, vm.Run(context.Background()), interp.ErrTypeMismatch)
			vm.Reset()
			vm.Flush()
			bridges, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return bridges > 0
		})
	})

	t.Run("completes a deep self tail call without growing frames", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := tailRefProgram(t, 1_000_000)
		want := runProgram(t, prog)

		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithFrame(4))
		defer vm.Close()
		for range 8 {
			require.NoError(t, vm.Run(context.Background()))
			result, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, result)
			vm.Reset()
			vm.Flush()
		}
	})

	t.Run("cancels a long self tail call", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := tailRefProgram(t, math.MaxInt32)

		var runErr error
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			runErr = vm.Run(ctx)
			if !errors.Is(runErr, context.DeadlineExceeded) {
				return false
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			return exits > 0
		})
		require.ErrorIs(t, runErr, context.DeadlineExceeded)
	})

	t.Run("keeps a bridged loop-free module native", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		record := node()
		b := instr.NewBuilder()
		b.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.DROP)
		for range 16 {
			b.Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		}
		b.Emit(instr.I32_CONST, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		// The callee is too large to inline, so its calls stay calls.
		inc := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}})
		for range 40 {
			inc.Emit(instr.NOP)
		}
		inc.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.RETURN)
		prog := program.New(code, program.WithTypes(record), program.WithConstants(inc.MustBuild()))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()

		var result types.Value
		var runErr, popErr error
		var entries float64
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries >= 64
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
		require.GreaterOrEqual(t, entries, float64(64))

		recent := make([]float64, 0, 32)
		for range 32 {
			require.NoError(t, vm.Run(context.Background()))
			result, err = vm.Pop()
			require.NoError(t, err)
			vm.Flush()
			count, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			recent = append(recent, count)
			vm.Reset()
		}
		require.Equal(t, want, result)
		for i := 1; i < len(recent); i++ {
			require.Greater(t, recent[i], recent[i-1])
		}
		deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		require.Zero(t, deopts)
	})

	t.Run("invalidates a loop that only bridges and releases", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 100).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.STRING_CONCAT).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(types.String("a")))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()

		var result types.Value
		var runErr, popErr error
		var bridges float64
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return bridges > 1
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
		require.Greater(t, bridges, float64(0))

		for range 32 {
			require.NoError(t, vm.Run(context.Background()))
			result, err = vm.Pop()
			require.NoError(t, err)
			vm.Reset()
		}
		vm.Flush()
		before, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		for range 32 {
			require.NoError(t, vm.Run(context.Background()))
			result, err = vm.Pop()
			require.NoError(t, err)
			vm.Reset()
		}
		vm.Flush()
		after, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		require.Equal(t, want, result)
		require.Equal(t, before, after)
	})

	t.Run("refills a loop's budget at a safepoint", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		sum := sumFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, uint64(200000)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(sum))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("releases a dying ref parameter on exit", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		fnBuilder := instr.NewBuilder()
		fnBuilder.Emit(instr.I32_CONST, 42).Emit(instr.RETURN)
		fnCode, err := fnBuilder.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeString()}, Returns: []types.Type{types.TypeI32()}},
			Code: instr.Marshal(fnCode),
		}
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 500).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.STRING_CONCAT)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.STRING_CONCAT)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()),
			program.WithConstants(fn, types.String("native-"), types.String("release")))

		var runErr, popErr, refErr error
		var survivor types.Boxed
		var count int
		var released float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			survivor, popErr = vm.PopBoxed()
			if popErr != nil {
				return true
			}
			count, refErr = vm.RefCount(survivor.Ref())
			if refErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			released, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "release"})
			return released > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, refErr)
		require.Equal(t, 1, count)
	})

	t.Run("treats NaN as unordered in float comparisons", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const warm = 1000
		nan := uint64(math.Float32bits(float32(math.NaN())))
		b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}})
		for _, op := range []instr.Opcode{instr.F32_EQ, instr.F32_NE, instr.F32_LE} {
			b.Append(instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 0))
			b.Append(instr.MustNew(instr.F32_CONST, nan), instr.MustNew(instr.F32_CONST, nan), instr.MustNew(op), instr.MustNew(instr.SELECT))
			if op != instr.F32_EQ {
				b.Append(instr.MustNew(instr.I32_ADD))
			}
		}
		fn := b.Append(instr.MustNew(instr.RETURN)).MustBuild()

		module := instr.NewBuilder()
		loop, done := module.Label(), module.Label()
		module.Bind(loop)
		module.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, warm).Emit(instr.I32_GE_S).BrIf(done)
		module.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		module.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		module.Br(loop)
		module.Bind(done).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := module.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(fn))
		want := runProgram(t, prog)
		require.Equal(t, types.I32(1), want)

		var runErr, popErr error
		var result types.Value
		var entries float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries = nativeEntries(profiler)
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("catches a native division by zero in a guest handler", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		fn := divFunction(t)
		b := instr.NewBuilder()
		loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Bind(start).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Try(start, end, catch, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(fn), program.WithHandlers(b.Handlers()...))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("reports an uncaught division by zero's stack trace", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		fn := divFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(fn))
		wantErr := runProgramErr(t, prog, interp.WithThreshold(-1))
		require.Error(t, wantErr)

		var gotErr error
		vm := interp.MustNew(prog, interp.WithThreshold(1))
		defer vm.Close()
		poll(t, func() bool {
			gotErr = vm.Run(context.Background())
			vm.Reset()
			return gotErr != nil
		})
		require.Error(t, gotErr)
		require.True(t, errorsEqual(gotErr, wantErr))
	})

	t.Run("bridges STRING_CONCAT", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		fn := concatFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()),
			program.WithConstants(fn, types.String("bridge-"), types.String("concat")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
	})

	t.Run("bridges STRUCT_NEW", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		record := node()
		b := instr.NewBuilder()
		loop, done, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.CONST_GET, 0)
		b.Emit(instr.STRUCT_NEW, 0)
		b.Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(inner)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.STRUCT_GET)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32(), record, types.TypeI32()), program.WithTypes(record),
			program.WithConstants(types.String("held")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var bridges, deopts float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return bridges > 1
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)

		require.Equal(t, float64(0), deopts)
	})

	t.Run("bridges ARRAY_NEW_DEFAULT", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const length = 4
		elem := types.NewArrayType(types.TypeI32())
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, length).Emit(instr.ARRAY_NEW_DEFAULT, 0)
		b.Emit(instr.ARRAY_LEN)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32(), types.TypeI32()), program.WithTypes(elem))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var bridges, deopts float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return bridges > 1
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
		require.Equal(t, float64(0), deopts)
	})

	t.Run("declines a trapping ARRAY_NEW_DEFAULT", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		elem := types.NewArrayType(types.TypeI32())
		b := instr.NewBuilder()
		loop, done, bad, length, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)+1).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_EQ).BrIf(bad)
		b.Emit(instr.I32_CONST, 1).Br(length)
		b.Bind(bad).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB)
		b.Bind(length)
		b.Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.ARRAY_LEN).Emit(instr.DROP)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(inner)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32(), types.TypeI32()), program.WithTypes(elem))
		wantErr := runProgramErr(t, prog, interp.WithThreshold(-1))
		require.Error(t, wantErr)

		// A declined bridge's own exit is still kind=bridge (the metric
		// names the exit, not its outcome), so only error/stack-trace parity
		// with threaded execution proves native code correctly handed the
		// trapping instruction back instead of silently accepting it.
		var gotErr error
		var bridges float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			gotErr = vm.Run(context.Background())
			if gotErr == nil {
				return false
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return bridges > 1
		})
		require.Error(t, gotErr)
		require.True(t, errorsEqual(gotErr, wantErr))
	})

	t.Run("bridges string ops", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := texts(t, 20_000)
		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		require.NoError(t, threaded.Run(context.Background()))
		wantValue, wantCount, err := popString(threaded)
		require.NoError(t, err)
		ab, err := threaded.Const(1)
		require.NoError(t, err)
		wantConst, err := threaded.RefCount(ab.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		var runErr, popErr, constErr error
		var value string
		var count, constCount int
		var bridges, entries float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			constCount, constErr = vm.RefCount(ab.Ref())
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return bridges > entries
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, constErr)

		require.Greater(t, bridges, entries)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Equal(t, wantConst, constCount)
	})

	t.Run("declines a string op that exhausts the heap", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const limit = 20_000
		prog := texts(t, 2*limit)
		threaded := interp.MustNew(prog, interp.WithHeapLimit(limit), interp.WithThreshold(-1))
		wantErr := threaded.Run(context.Background())
		require.ErrorIs(t, wantErr, interp.ErrHeapExhausted)
		ab, err := threaded.Const(1)
		require.NoError(t, err)
		wantConst, err := threaded.RefCount(ab.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		var gotErr, constErr error
		var constCount int
		var bridges, entries float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithHeapLimit(limit), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			gotErr = vm.Run(context.Background())
			constCount, constErr = vm.RefCount(ab.Ref())
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return bridges > entries
		})
		require.Greater(t, bridges, entries)
		require.True(t, errorsEqual(gotErr, wantErr), "got %v, want %v", gotErr, wantErr)
		require.NoError(t, constErr)
		require.Equal(t, wantConst, constCount)
	})

	t.Run("bridges unlowered operators in a loop", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		dict, list := types.NewMapType(types.TypeI32(), types.TypeI32()), types.NewArrayType(types.TypeI32())
		wide, named := types.NewMapType(types.TypeI32(), types.TypeI64()), types.NewMapType(types.TypeString(), types.TypeI32())
		b := instr.NewBuilder()
		loop, done, first, firstDone, second, secondDone, third, thirdDone := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.MAP_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.I32_CONST, 0).Emit(instr.MAP_NEW_DEFAULT, 3).Emit(instr.LOCAL_SET, 5)
		b.Emit(instr.I32_CONST, 0).Emit(instr.MAP_NEW_DEFAULT, 4).Emit(instr.LOCAL_SET, 6)
		b.Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_NEW_DEFAULT, 1).Emit(instr.LOCAL_SET, 3)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 300).Emit(instr.I32_GE_S).BrIf(done)

		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(first)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 16).Emit(instr.I32_GE_S).BrIf(firstDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(first)
		b.Bind(firstDone)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_GET)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_APPEND).Emit(instr.LOCAL_SET, 3)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 1).Emit(instr.STRING_EQ)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_LOOKUP).Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SLICE).Emit(instr.ARRAY_LEN)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(second)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 16).Emit(instr.I32_GE_S).BrIf(secondDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(second)
		b.Bind(secondDone)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_FILL)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_COPY)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_DELETE)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 7).Emit(instr.ERROR_NEW).Emit(instr.ERROR_CODE)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.CONST_GET, 0).Emit(instr.REF_TEST, 2)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 3).Emit(instr.REF_EQ)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_DELETE)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(third)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 16).Emit(instr.I32_GE_S).BrIf(thirdDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(third)
		b.Bind(thirdDone)

		b.Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 1<<60).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_GET)
		b.Emit(instr.I64_CONST, 58).Emit(instr.I64_SHR_U).Emit(instr.I64_TO_I32)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_DELETE)

		b.Emit(instr.LOCAL_GET, 6).Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 6).Emit(instr.CONST_GET, 0).Emit(instr.MAP_DELETE)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_APPEND)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code,
			program.WithLocals(types.TypeI32(), types.TypeI32(), dict, list, types.TypeI32(), wide, named),
			program.WithTypes(dict, list, types.TypeString(), wide, named),
			program.WithConstants(types.String("x"), types.String("y")))

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		require.NoError(t, threaded.Run(context.Background()))
		boxed, err := threaded.PopBoxed()
		require.NoError(t, err)
		want, err := threaded.Load(boxed.Ref())
		require.NoError(t, err)
		wantCount, err := threaded.RefCount(boxed.Ref())
		require.NoError(t, err)
		x, err := threaded.Const(0)
		require.NoError(t, err)
		wantConst, err := threaded.RefCount(x.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name, key, value string) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, prof.Label{Key: key, Value: value})
			return v
		}

		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_entries_total", "tier", "optimized") > 0
		})
		require.NoError(t, runErr)

		entries := metric("vm_jit_entries_total", "tier", "optimized")
		bridges := metric("vm_jit_exits_total", "kind", "bridge")
		for range 8 {
			require.NoError(t, vm.Run(context.Background()))
			boxed, err := vm.PopBoxed()
			require.NoError(t, err)
			got, err := vm.Load(boxed.Ref())
			require.NoError(t, err)
			count, err := vm.RefCount(boxed.Ref())
			require.NoError(t, err)
			constCount, err := vm.RefCount(x.Ref())
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantCount, count)
			require.Equal(t, wantConst, constCount)
			vm.Reset()
		}
		require.Greater(t, metric("vm_jit_entries_total", "tier", "optimized"), entries)
		require.Greater(t, metric("vm_jit_exits_total", "kind", "bridge"), bridges)
		require.Zero(t, metric("vm_jit_exits_total", "kind", "deopt"))
	})

	t.Run("selects between two refs", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 300).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND)
		b.Emit(instr.SELECT).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(types.String("x"), types.String("y")))

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		x, err := threaded.Const(0)
		require.NoError(t, err)
		y, err := threaded.Const(1)
		require.NoError(t, err)
		wantX, err := threaded.RefCount(x.Ref())
		require.NoError(t, err)
		wantY, err := threaded.RefCount(y.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name, key, value string) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, prof.Label{Key: key, Value: value})
			return v
		}

		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_entries_total", "tier", "optimized") > 0
		})
		require.NoError(t, runErr)

		for range 8 {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			xCount, err := vm.RefCount(x.Ref())
			require.NoError(t, err)
			yCount, err := vm.RefCount(y.Ref())
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantX, xCount)
			require.Equal(t, wantY, yCount)
			vm.Reset()
		}
		require.Greater(t, metric("vm_jit_entries_total", "tier", "optimized"), 0.0)
	})

	t.Run("deoptimizes an unlowered operator that traps", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const warm = 4000
		b := instr.NewBuilder()
		loop, done, inner, innerDone, same := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2*warm).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(inner)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, warm).Emit(instr.I32_NE).BrIf(same)
		b.Emit(instr.CONST_GET, 1).Emit(instr.LOCAL_SET, 2)
		b.Bind(same)

		b.Emit(instr.LOCAL_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.STRING_CONCAT).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code,
			program.WithLocals(types.TypeI32(), types.TypeI32(), types.TypeAny()),
			program.WithConstants(types.String("x"), types.TypedArray[int32]{1}))

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		wantErr := threaded.Run(context.Background())
		require.ErrorIs(t, wantErr, interp.ErrTypeMismatch)
		x, err := threaded.Const(0)
		require.NoError(t, err)
		array, err := threaded.Const(1)
		require.NoError(t, err)
		wantX, err := threaded.RefCount(x.Ref())
		require.NoError(t, err)
		wantArray, err := threaded.RefCount(array.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		var gotErr, xErr, arrayErr error
		var gotX, gotArray int
		var bridges float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			gotErr = vm.Run(context.Background())
			gotX, xErr = vm.RefCount(x.Ref())
			gotArray, arrayErr = vm.RefCount(array.Ref())
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return bridges > 1
		})

		require.Greater(t, bridges, float64(1))
		require.True(t, errorsEqual(gotErr, wantErr), "got %v, want %v", gotErr, wantErr)
		require.NoError(t, xErr)
		require.NoError(t, arrayErr)
		require.Equal(t, wantX, gotX)
		require.Equal(t, wantArray, gotArray)
	})

	t.Run("traps a non-string operand of a lowered string relation as threaded does", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const warm = 4000
		for _, op := range []instr.Opcode{instr.STRING_LT, instr.STRING_GT, instr.STRING_LE, instr.STRING_GE} {
			b := instr.NewBuilder()
			loop, done, same := b.Label(), b.Label(), b.Label()
			b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 1)
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2*warm).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, warm).Emit(instr.I32_NE).BrIf(same)
			b.Emit(instr.CONST_GET, 1).Emit(instr.LOCAL_SET, 1)
			b.Bind(same)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, 0).Emit(op).Emit(instr.DROP)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
			b.Bind(done).Emit(instr.LOCAL_GET, 0)
			code, err := b.Assemble()
			require.NoError(t, err)
			prog := program.New(code,
				program.WithLocals(types.TypeI32(), types.TypeAny()),
				program.WithConstants(types.String("x"), types.TypedArray[int32]{1}))

			threaded := interp.MustNew(prog, interp.WithThreshold(-1))
			wantErr := threaded.Run(context.Background())
			require.ErrorIs(t, wantErr, interp.ErrTypeMismatch)
			require.NoError(t, threaded.Close())

			var gotErr error
			var entries float64
			profiler := prof.New()
			vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
			poll(t, func() bool {
				gotErr = vm.Run(context.Background())
				vm.Reset()
				vm.Flush()
				entries = nativeEntries(profiler)
				return entries > 0
			})
			bridges, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			require.NoError(t, vm.Close())

			require.True(t, errorsEqual(gotErr, wantErr), "got %v, want %v", gotErr, wantErr)
			require.Zero(t, bridges)
		}
	})

	t.Run("keeps a callee native while its native calls amortize bridges", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		// The loop adds f(i) for i < n. f(0) bridges three string ops with no
		// native work between them; every other f(i) loops four times first,
		// amortizing its bridges.
		const n = 2000
		fb := instr.NewBuilder()
		work, loop, done := fb.Label(), fb.Label(), fb.Label()
		fb.Emit(instr.LOCAL_GET, 0).BrIf(work)
		fb.Emit(instr.CONST_GET, 1).Emit(instr.STRING_ENCODE_UTF32).Emit(instr.STRING_NEW_UTF32).Emit(instr.STRING_LEN).Emit(instr.RETURN)
		fb.Bind(work).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		fb.Bind(loop)
		fb.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(done)
		fb.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		fb.Br(loop)
		fb.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.STRING_ENCODE_UTF32).Emit(instr.STRING_NEW_UTF32).Emit(instr.STRING_LEN).Emit(instr.RETURN)
		fcode, err := fb.Assemble()
		require.NoError(t, err)
		f := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}},
			Locals: []types.Type{types.TypeI32()},
			Code:   instr.Marshal(fcode),
		}

		b := instr.NewBuilder()
		header, exit, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(header)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(exit)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(inner)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(header)
		b.Bind(exit).Emit(instr.LOCAL_GET, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32(), types.TypeI32(), types.TypeI32()), program.WithConstants(f, types.String("ab")))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()

		// f(0) runs threaded-entered at most once per Run; enough Runs pass
		// for its bridges to reach the invalidation limit several times over.
		const runs = 16
		var got types.Value
		var runErr, popErr error
		var round int
		var entries, prior float64
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			round++
			total, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries, prior = total-prior, total
			return round >= runs && entries < n/2
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.GreaterOrEqual(t, round, runs)

		require.Less(t, entries, float64(n/2))
	})

	t.Run("compares strings and refs natively as threaded does", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		const n = 1000
		// fresh builds a string from constant c through string.new_utf32, so two
		// builds of equal content are different refs.
		fresh := func(b *instr.Builder, c, local int) {
			b.Emit(instr.CONST_GET, uint64(c)).Emit(instr.STRING_ENCODE_UTF32).Emit(instr.STRING_NEW_UTF32).Emit(instr.LOCAL_SET, uint64(local))
		}
		long := func(tail string) string { return "0123456789abcdefghij" + tail }
		type tc struct {
			name string
			op   instr.Opcode
			x, y string
		}
		var cases []tc
		names := map[instr.Opcode]string{
			instr.STRING_EQ: "string.eq", instr.STRING_NE: "string.ne", instr.STRING_LT: "string.lt",
			instr.STRING_GT: "string.gt", instr.STRING_LE: "string.le", instr.STRING_GE: "string.ge",
		}
		for _, op := range []instr.Opcode{instr.STRING_EQ, instr.STRING_NE, instr.STRING_LT, instr.STRING_GT, instr.STRING_LE, instr.STRING_GE} {
			for _, c := range [][3]string{
				{"empty and empty", "", ""},
				{"empty and non-empty", "", "a"},
				{"equal contents behind different refs", "hello", "hello"},
				{"different lengths", "hello", "hell"},
				{"first byte differs", "xello", "hello"},
				{"middle byte differs", "hexlo", "hello"},
				{"last byte differs", "hellx", "hello"},
				{"one word exactly", "abcdefgh", "abcdefgh"},
				{"one word differing at the end", "abcdefgh", "abcdefgx"},
				{"two words and a tail equal", long("kl"), long("kl")},
				{"second word differs", long("kl")[:8] + "X" + long("kl")[9:], long("kl")},
				{"tail differs after equal words", long("kl"), long("kX")},
				{"non-ascii contents", "héllo wörld, ünïcode", "héllo wörld, ünïcode"},
				{"left a proper prefix", "hell", "hello"},
				{"right a proper prefix", "hello", "hell"},
				{"left a word-long prefix", long("")[:16], long("kl")},
				{"right a word-long prefix", long("kl"), long("")[:16]},
				{"difference inside the first word", "abcxefgh", "abcdefgh"},
				{"difference inside the first word, reversed", "abcdefgh", "abcxefgh"},
				{"difference after the first word", long("kl")[:12] + "x" + long("kl")[13:], long("kl")},
				{"difference after the first word, reversed", long("kl"), long("kl")[:12] + "x" + long("kl")[13:]},
				{"tail byte greater", long("kz"), long("kl")},
				{"tail byte lesser", long("kl"), long("kz")},
				{"bytes at or above 0x80 order unsigned", "é", "z"},
				{"bytes at or above 0x80 order unsigned, reversed", "z", "é"},
				{"high bytes in a word", "abcdefgé", "abcdefgz"},
				{"empty and non-empty, reversed", "a", ""},
			} {
				cases = append(cases, tc{names[op] + " " + c[0], op, c[1], c[2]})
			}
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				b := instr.NewBuilder()
				loop, done, hit, skip := b.Label(), b.Label(), b.Label(), b.Label()
				fresh(b, 0, 0)
				fresh(b, 1, 1)
				b.Bind(loop)
				b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, n).Emit(instr.I32_GE_S).BrIf(done)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(c.op).BrIf(hit).Br(skip)
				b.Bind(hit)
				b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
				b.Bind(skip)
				b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2).Br(loop)
				b.Bind(done).Emit(instr.LOCAL_GET, 3)
				code, err := b.Assemble()
				require.NoError(t, err)
				prog := program.New(code,
					program.WithLocals(types.TypeString(), types.TypeString(), types.TypeI32(), types.TypeI32()),
					program.WithConstants(types.String(c.x), types.String(c.y)))
				requireNative(t, prog)
			})
		}

		{
			refOps := map[instr.Opcode]string{instr.REF_EQ: "ref.eq", instr.REF_NE: "ref.ne"}
			for _, op := range []instr.Opcode{instr.REF_EQ, instr.REF_NE} {
				for _, c := range []struct {
					name string
					x, y func(b *instr.Builder)
				}{
					{"same ref", func(b *instr.Builder) { b.Emit(instr.CONST_GET, 0) }, func(b *instr.Builder) { b.Emit(instr.CONST_GET, 0) }},
					{"different refs of equal contents", func(b *instr.Builder) { b.Emit(instr.CONST_GET, 0) }, func(b *instr.Builder) { b.Emit(instr.CONST_GET, 1) }},
					{"null and null", func(b *instr.Builder) { b.Emit(instr.REF_NULL) }, func(b *instr.Builder) { b.Emit(instr.REF_NULL) }},
					{"null and non-null", func(b *instr.Builder) { b.Emit(instr.REF_NULL) }, func(b *instr.Builder) { b.Emit(instr.CONST_GET, 0) }},
					{"non-null and null", func(b *instr.Builder) { b.Emit(instr.CONST_GET, 0) }, func(b *instr.Builder) { b.Emit(instr.REF_NULL) }},
				} {
					t.Run(refOps[op]+" "+c.name, func(t *testing.T) {
						b := instr.NewBuilder()
						loop, done, hit, skip := b.Label(), b.Label(), b.Label(), b.Label()
						c.x(b)
						b.Emit(instr.LOCAL_SET, 0)
						c.y(b)
						b.Emit(instr.LOCAL_SET, 1)
						b.Bind(loop)
						b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, n).Emit(instr.I32_GE_S).BrIf(done)
						b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(op).BrIf(hit).Br(skip)
						b.Bind(hit)
						b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
						b.Bind(skip)
						b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2).Br(loop)
						b.Bind(done).Emit(instr.LOCAL_GET, 3)
						code, err := b.Assemble()
						require.NoError(t, err)
						prog := program.New(code,
							program.WithLocals(types.TypeAny(), types.TypeAny(), types.TypeI32(), types.TypeI32()),
							program.WithConstants(types.String("same"), types.String("same")))
						requireNative(t, prog)
					})
				}
			}
		}

		{
			const limit = 10000
			ops32 := []instr.Opcode{
				instr.I32_ADD, instr.I32_SUB, instr.I32_EQ, instr.I32_NE,
				instr.I32_LT_S, instr.I32_GT_S, instr.I32_LE_S, instr.I32_GE_S,
				instr.I32_LT_U, instr.I32_GT_U, instr.I32_LE_U, instr.I32_GE_U,
			}
			ops64 := []instr.Opcode{
				instr.I64_ADD, instr.I64_SUB, instr.I64_EQ, instr.I64_NE,
				instr.I64_LT_S, instr.I64_GT_S, instr.I64_LE_S, instr.I64_GE_S,
				instr.I64_LT_U, instr.I64_GT_U, instr.I64_LE_U, instr.I64_GE_U,
			}
			edges := []int64{0, 1, -1, 4095, 4096, -4095, -4096, math.MinInt32, math.MaxInt32}
			wide := append([]int64{1 << 32, 1<<32 - 1, math.MinInt64, math.MaxInt64}, edges...)

			// Locals: 0 index, 1 fold, 2 i32 operand, 3 i32 operand across the sign
			// bit, 4 and 5 their i64 extensions. Wide i64 results stay on the
			// stack and fold to i32 halves, so no box outlives the run.
			for _, order := range []string{"constant second", "constant first"} {
				t.Run("folds immediate-range constants as threaded does, "+order, func(t *testing.T) {
					b := instr.NewBuilder()
					loop, done := b.Label(), b.Label()
					b.Bind(loop)
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, limit).Emit(instr.I32_GE_S).BrIf(done)
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(limit/2))).Emit(instr.I32_SUB).Emit(instr.LOCAL_SET, 2)
					b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 0x80000000).Emit(instr.I32_XOR).Emit(instr.LOCAL_SET, 3)
					b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_TO_I64_S).Emit(instr.LOCAL_SET, 4)
					b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_TO_I64_S).Emit(instr.LOCAL_SET, 5)
					operand := func(c int64, x int, wide bool, op instr.Opcode) {
						push := func() {
							if wide {
								b.Emit(instr.I64_CONST, uint64(c))
							} else {
								b.Emit(instr.I32_CONST, uint64(uint32(c)))
							}
						}
						if order == "constant first" {
							push()
						}
						b.Emit(instr.LOCAL_GET, uint64(x))
						if order == "constant second" {
							push()
						}
						b.Emit(op)
					}
					fold := func() {
						b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 31).Emit(instr.I32_MUL).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
					}
					for _, op := range ops32 {
						for _, c := range edges {
							for _, x := range []int{2, 3} {
								operand(c, x, false, op)
								fold()
							}
						}
					}
					for _, op := range ops64 {
						for _, c := range wide {
							for _, x := range []int{4, 5} {
								operand(c, x, true, op)
								if op == instr.I64_ADD || op == instr.I64_SUB {
									b.Emit(instr.DUP).Emit(instr.I64_TO_I32)
									fold()
									b.Emit(instr.I64_CONST, 32).Emit(instr.I64_SHR_S).Emit(instr.I64_TO_I32)
								}
								fold()
							}
						}
					}
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
					b.Bind(done).Emit(instr.LOCAL_GET, 1)
					code, err := b.Assemble()
					require.NoError(t, err)
					prog := program.New(code, program.WithLocals(
						types.TypeI32(), types.TypeI32(), types.TypeI32(), types.TypeI32(), types.TypeI64(), types.TypeI64()))
					requireNative(t, prog)
				})
			}
		}

		{
			for _, c := range []struct {
				name string
				bad  func(vm *interp.Interpreter) types.Boxed
			}{
				{"array", func(vm *interp.Interpreter) types.Boxed {
					addr, err := vm.Alloc(types.TypedArray[int32]{1})
					require.NoError(t, err)
					return types.BoxRef(addr)
				}},
				{"null", func(*interp.Interpreter) types.Boxed { return types.BoxedNull }},
			} {
				t.Run("string.eq traps a "+c.name+" operand as threaded does", func(t *testing.T) {
					b := instr.NewBuilder()
					loop, done := b.Label(), b.Label()
					b.Bind(loop)
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, n).Emit(instr.I32_GE_S).BrIf(done)
					b.Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.STRING_EQ).Emit(instr.DROP)
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
					b.Bind(done)
					code, err := b.Assemble()
					require.NoError(t, err)
					prog := program.New(code,
						program.WithGlobals(types.TypeAny()),
						program.WithLocals(types.TypeI32()),
						program.WithConstants(types.String("abc")))

					run := func(vm *interp.Interpreter, global func(*interp.Interpreter) types.Boxed) error {
						vm.Reset()
						require.NoError(t, vm.SetGlobal(0, global(vm)))
						return vm.Run(context.Background())
					}
					threaded := interp.MustNew(prog, interp.WithThreshold(-1))
					defer threaded.Close()
					wantErr := run(threaded, c.bad)
					require.ErrorIs(t, wantErr, interp.ErrTypeMismatch)

					profiler := prof.New()
					vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
					defer vm.Close()
					poll(t, func() bool {
						require.NoError(t, run(vm, func(vm *interp.Interpreter) types.Boxed {
							addr, err := vm.Alloc(types.String("abc"))
							require.NoError(t, err)
							return types.BoxRef(addr)
						}))
						vm.Flush()
						return nativeEntries(profiler) > 0
					})
					gotErr := run(vm, c.bad)
					require.True(t, errorsEqual(gotErr, wantErr), "got %v, want %v", gotErr, wantErr)
				})
			}
		}
	})

	t.Run("releases a ref slot's old value on store", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n = 200_000
		prog := store(t, n)

		threaded := interp.MustNew(store(t, n), interp.WithThreshold(-1))
		require.NoError(t, threaded.Run(context.Background()))
		wantConst, err := threaded.Const(0)
		require.NoError(t, err)
		want, err := threaded.RefCount(wantConst.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		vm := interp.MustNew(prog, interp.WithThreshold(1))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		gotConst, err := vm.Const(0)
		require.NoError(t, err)
		got, err := vm.RefCount(gotConst.Ref())
		require.NoError(t, err)

		require.Equal(t, want, got)
	})

	t.Run("bridges a dynamically entered call", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		fn := concatFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32(), types.TypeAny()),
			program.WithConstants(fn, types.String("dyn-"), types.String("call")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
	})

	t.Run("overflows a small frame limit inside a served call", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		fib := fibFunction()
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, uint64(30)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(fib))
		wantErr := runProgramErr(t, prog, interp.WithFrame(8), interp.WithThreshold(-1))
		require.Error(t, wantErr)

		var gotErr error
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithFrame(8), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			gotErr = vm.Run(context.Background())
			if gotErr == nil {
				return false
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		})
		require.Error(t, gotErr)
		// The innermost frame's IP is ignored: a deoptimized frame runs exact
		// code, so a trap at a fused CONST_GET;CALL call site (fib's own
		// recursive call) resumes at CALL's own IP rather than the fused
		// unit's, unlike the threaded baseline which never separates them.
		var got, want *interp.RuntimeError
		require.ErrorAs(t, gotErr, &got)
		require.ErrorAs(t, wantErr, &want)
		require.ErrorIs(t, got.Err, interp.ErrFrameOverflow)
		require.ErrorIs(t, want.Err, interp.ErrFrameOverflow)
		require.Len(t, got.Frames, len(want.Frames))
		for i := range got.Frames {
			require.Equal(t, want.Frames[i].Func, got.Frames[i].Func)
			if i > 0 {
				require.Equal(t, want.Frames[i].IP, got.Frames[i].IP)
			}
		}
	})

	t.Run("deopts self-recursive fib at a frame limit", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		fib := fibFunction()
		b := instr.NewBuilder()
		loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Bind(start).Emit(instr.I32_CONST, uint64(30)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Try(start, end, catch, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(fib), program.WithHandlers(b.Handlers()...))
		wantVM := interp.MustNew(prog, interp.WithFrame(8), interp.WithThreshold(-1))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		wantConst, err := wantVM.Const(0)
		require.NoError(t, err)
		wantRC, err := wantVM.RefCount(wantConst.Ref())
		require.NoError(t, err)
		require.Equal(t, 1, wantRC)

		var runErr, rcErr error
		var gotRC int
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithFrame(8), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			c, _ := vm.Const(0)
			gotRC, rcErr = vm.RefCount(c.Ref())
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, rcErr)

		require.Equal(t, wantRC, gotRC)
	})

	t.Run("deopts a callee with register-passed ref and i32 parameters", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		fb := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString(), types.TypeI32()}, Returns: []types.Type{types.TypeI32()}})
		small := fb.Label()
		fb.Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_LT_S)).BrIf(small)
		fb.Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SUB),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN),
		)
		fb.Bind(small).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.RETURN))
		rec := fb.MustBuild()

		mb := instr.NewBuilder()
		loop, done, start, end, catch := mb.Label(), mb.Label(), mb.Label(), mb.Label(), mb.Label()
		mb.Bind(loop)
		mb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1000).Emit(instr.I32_GE_S).BrIf(done)
		mb.Emit(instr.CONST_GET, 1).Emit(instr.I32_CONST, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		mb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		mb.Br(loop)
		mb.Bind(done)
		mb.Bind(start).Emit(instr.CONST_GET, 1).Emit(instr.I32_CONST, 30).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		mb.Bind(end)
		mb.Bind(catch).Emit(instr.ERROR_CODE)
		mb.Try(start, end, catch, 1)
		code, err := mb.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(rec, types.String("s")), program.WithHandlers(mb.Handlers()...))

		wantVM := interp.MustNew(prog, interp.WithFrame(8), interp.WithThreshold(-1))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)
		wantConst, err := wantVM.Const(1)
		require.NoError(t, err)
		wantRC, err := wantVM.RefCount(wantConst.Ref())
		require.NoError(t, err)

		var runErr, popErr, rcErr error
		var got types.Value
		var gotRC int
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithFrame(8), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			c, _ := vm.Const(1)
			gotRC, rcErr = vm.RefCount(c.Ref())
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, want, got)
		require.Equal(t, wantRC, gotRC)
	})

	t.Run("resumes a caller after a call to a function without native code", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		s := types.String("s")
		deep := []interp.Option{interp.WithFrame(1024), interp.WithStack(1 << 14)}
		sumrecBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString(), types.TypeI32()}, Returns: []types.Type{types.TypeI32()}})
		zero := sumrecBuilder.Label()
		sumrecBuilder.Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_EQZ)).BrIf(zero)
		sumrecBuilder.Append(
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SUB),
			instr.MustNew(instr.CONST_GET, uint64(1)), instr.MustNew(instr.CALL), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN),
		)
		sumrecBuilder.Bind(zero).Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.RETURN))
		sumrec := sumrecBuilder.MustBuild()
		for _, c := range []struct {
			name string
			prog *program.Program
			opts []interp.Option
		}{
			{"a callee that never compiles", loopProgram(t, 8, 64, 64, false, guardFunction(1000, false), loopFunction(0, false), s), nil},
			{"a callee that never compiles, passed an owned argument", loopProgram(t, 8, 64, 64, false, guardFunction(1000, false), loopFunction(0, true), s), nil},
			{"a callee that never compiles calling native code that calls it again", loopProgram(t, 8, 64, 64, false, guardFunction(1000, false), loopFunction(4, false), s, relayFunction(0, false), relayFunction(3, true)), nil},
			{"recursion past the native activation limit", loopProgram(t, 8, 10, 600, false, guardFunction(1000, false), sumrec, s), deep},
		} {
			want := interp.MustNew(c.prog, append([]interp.Option{interp.WithThreshold(-1)}, c.opts...)...)
			require.NoError(t, want.Run(context.Background()), c.name)
			wantSum, wantCount, err := popLoop(want)
			require.NoError(t, err, c.name)
			require.NoError(t, want.Close())

			profiler := prof.New()
			vm := interp.MustNew(c.prog, append([]interp.Option{interp.WithThreshold(1), interp.WithProfiler(profiler)}, c.opts...)...)
			exits := func(kind string) float64 {
				vm.Flush()
				v, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: kind})
				return v
			}
			run := func() {
				require.NoError(t, vm.Run(context.Background()), c.name)
				sum, count, err := popLoop(vm)
				require.NoError(t, err, c.name)
				require.Equal(t, wantSum, sum, c.name)
				require.Equal(t, wantCount, count, c.name)
				vm.Reset()
			}
			poll(t, func() bool {
				err := vm.Run(context.Background())
				vm.Reset()
				return err != nil || exits("call") > 0
			})
			for range 16 {
				run()
			}
			before := exits("call")
			for range 16 {
				run()
			}
			require.Greater(t, exits("call"), before, c.name)
			require.Zero(t, exits("deopt"), c.name)
			require.NoError(t, vm.Close())
		}
	})

	t.Run("resumes a dynamic call through call exits", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := mixedProgram(t, 64, false)
		want := interp.MustNew(prog, interp.WithThreshold(-1))
		require.NoError(t, want.Run(context.Background()))
		wantSum, wantCount, err := popLoop(want)
		require.NoError(t, err)
		require.NoError(t, want.Close())

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(8), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) + metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		run := func() {
			require.NoError(t, vm.Run(context.Background()))
			sum, count, err := popLoop(vm)
			require.NoError(t, err)
			require.Equal(t, wantSum, sum)
			require.Equal(t, wantCount, count)
			vm.Reset()
		}
		call, deopt := prof.Label{Key: "kind", Value: "call"}, prof.Label{Key: "kind", Value: "deopt"}
		poll(t, func() bool {
			err := vm.Run(context.Background())
			vm.Reset()
			return err != nil || metric("vm_jit_exits_total", call) > 0
		})
		beforeEntries, beforeCalls := entries(), metric("vm_jit_exits_total", call)
		for range 16 {
			run()
		}
		require.Greater(t, entries(), beforeEntries)
		require.Greater(t, metric("vm_jit_exits_total", call), beforeCalls)
		require.Zero(t, metric("vm_jit_exits_total", deopt))

		require.Zero(t, metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "unsupported"}))
	})

	t.Run("invalidates a caller whose served calls outweigh its work", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const runs = 16

		held := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}})
		refuse(held)
		held.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.RETURN))
		served := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}})
		served.Append(instr.MustNew(instr.I32_CONST, 0))
		for range 16 {
			served.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL), instr.MustNew(instr.I32_ADD))
		}
		served.Append(instr.MustNew(instr.RETURN))

		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(served.MustBuild(), held.MustBuild()))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		calls := func() float64 {
			vm.Flush()
			v, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return v
		}
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || calls() > 0
		})
		require.NoError(t, runErr)

		// Compiles finish asynchronously: a round with no served call may
		// still have one in flight that publishes code making calls again.
		// Settled is two rounds in a row with no served call and no compile.
		compiles := func() float64 {
			var n float64
			for _, m := range profiler.Metrics() {
				if m.Name == "vm_jit_compiles_total" {
					n += m.Value
				}
			}
			return n
		}
		quiet := 0
		poll(t, func() bool {
			before, compiled := calls(), compiles()
			for range runs {
				if runErr = vm.Run(context.Background()); runErr != nil {
					return true
				}
				vm.Reset()
			}
			if calls() != before || compiles() != compiled {
				quiet = 0
				return false
			}
			quiet++
			return quiet == 2
		})
		require.NoError(t, runErr)

		before := calls()
		for range runs {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
		}
		require.Equal(t, before, calls())
	})

	t.Run("deopts a dynamic call whose callee takes other parameters", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := mixedProgram(t, 64, true)
		want := interp.MustNew(prog, interp.WithThreshold(-1))
		require.NoError(t, want.Run(context.Background()))
		wantSum, wantCount, err := popLoop(want)
		require.NoError(t, err)
		require.NoError(t, want.Close())

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(8), interp.WithProfiler(profiler))
		defer vm.Close()
		entries := func() float64 {
			vm.Flush()
			v, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return v
		}
		for range 64 {
			require.NoError(t, vm.Run(context.Background()))
			sum, count, err := popLoop(vm)
			require.NoError(t, err)
			require.Equal(t, wantSum, sum)
			require.Equal(t, wantCount, count)
			vm.Reset()
			time.Sleep(time.Millisecond)
		}
		require.Positive(t, entries())
	})

	t.Run("deopts a dynamic call only once it runs", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		for _, c := range []struct {
			name  string
			final int
		}{
			{"it stays cold", -1},
			{"it runs", 3},
		} {
			b := instr.NewBuilder()
			loop, done := b.Label(), b.Label()
			b.Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_SET, 2)
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(64)).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 100).Emit(instr.I32_CONST, uint64(math.MaxUint32)).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
			b.Br(loop)
			b.Bind(done)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 100).Emit(instr.I32_CONST, uint64(c.final)).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2)
			code, err := b.Assemble()
			require.NoError(t, err)
			coldBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString(), types.TypeI32(), types.TypeI32(), types.TypeAny()}, Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32(), types.TypeI32())
			coldLoop, coldDone, hit, next := coldBuilder.Label(), coldBuilder.Label(), coldBuilder.Label(), coldBuilder.Label()
			coldBuilder.Bind(coldLoop).Append(instr.MustNew(instr.LOCAL_GET, 5), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_GE_S)).BrIf(coldDone)
			coldBuilder.Append(instr.MustNew(instr.LOCAL_GET, 5), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_EQ)).BrIf(hit)
			coldBuilder.Append(instr.MustNew(instr.LOCAL_GET, 5)).Br(next)
			coldBuilder.Bind(hit).Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 5), instr.MustNew(instr.LOCAL_GET, 3), instr.MustNew(instr.CALL))
			coldBuilder.Bind(next).Append(
				instr.MustNew(instr.LOCAL_GET, 4), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 4),
				instr.MustNew(instr.LOCAL_GET, 5), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 5),
			).Br(coldLoop)
			coldBuilder.Bind(coldDone).Append(instr.MustNew(instr.LOCAL_GET, 4), instr.MustNew(instr.RETURN))
			cold := coldBuilder.MustBuild()
			prog := program.New(code, program.WithLocals(types.TypeI32(), types.TypeI32(), types.TypeString()),
				program.WithConstants(cold, bumpFunction(1), types.String("s")))
			want := interp.MustNew(prog, interp.WithThreshold(-1))
			require.NoError(t, want.Run(context.Background()), c.name)
			wantSum, wantCount, err := popLoop(want)
			require.NoError(t, err, c.name)
			require.NoError(t, want.Close())

			profiler := prof.New()
			vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
			metric := func(name string, labels ...prof.Label) float64 {
				vm.Flush()
				v, _ := profiler.Metric(name, labels...)
				return v
			}
			run := func() {
				require.NoError(t, vm.Run(context.Background()), c.name)
				sum, count, err := popLoop(vm)
				require.NoError(t, err, c.name)
				require.Equal(t, wantSum, sum, c.name)
				require.Equal(t, wantCount, count, c.name)
				vm.Reset()
			}
			entered := prof.Label{Key: "tier", Value: "baseline"}
			poll(t, func() bool {
				err := vm.Run(context.Background())
				vm.Reset()
				return err != nil || metric("vm_jit_entries_total", entered) > 0
			})
			for range 16 {
				run()
			}
			deopts := metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			if c.final < 0 {
				require.Zero(t, deopts, c.name)
			} else {
				require.Positive(t, deopts, c.name)
			}
			require.NoError(t, vm.Close())
		}
	})

	t.Run("reports an uncaught trap in a resumed callee", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := loopProgram(t, 8, 50, 100, false, guardFunction(60, false), loopFunction(0, false), types.String("s"))
		wantErr := runProgramErr(t, prog, interp.WithThreshold(-1))
		require.ErrorIs(t, wantErr, interp.ErrDivideByZero)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		var gotErr error
		var exits float64
		poll(t, func() bool {
			gotErr = vm.Run(context.Background())
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return !errorsEqual(gotErr, wantErr) || exits > 0
		})
		require.True(t, errorsEqual(gotErr, wantErr), "%v != %v", gotErr, wantErr)
		require.Positive(t, exits)
	})

	t.Run("serves a trapping, throwing, or coroutine callee by call exit", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		s := types.String("s")

		co := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString(), types.TypeI32()}, Returns: []types.Type{types.TypeAny()}}).
			Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.YIELD), instr.MustNew(instr.RETURN)).MustBuild()
		drive := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString(), types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32())
		loop, done := drive.Label(), drive.Label()
		drive.Bind(loop).Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_GE_S)).BrIf(done)
		drive.Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL), instr.MustNew(instr.DROP),
			instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2),
		).Br(loop)
		drive.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.RETURN))
		for _, c := range []struct {
			name string
			prog *program.Program
		}{
			{"coroutine", loopProgram(t, 64, 8, 8, false, co, drive.MustBuild(), s)},
			{"trap", loopProgram(t, 8, 50, 100, true, guardFunction(60, false), loopFunction(0, false), s)},
			{"trap with an owned argument", loopProgram(t, 8, 50, 100, true, guardFunction(60, false), loopFunction(0, true), s)},
			{"throw", loopProgram(t, 8, 50, 100, true, guardFunction(60, true), loopFunction(0, false), s)},
		} {
			want := interp.MustNew(c.prog, interp.WithThreshold(-1))
			require.NoError(t, want.Run(context.Background()), c.name)
			wantSum, wantCount, err := popLoop(want)
			require.NoError(t, err, c.name)
			require.NoError(t, want.Close())

			profiler := prof.New()
			vm := interp.MustNew(c.prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
			exits := func() float64 {
				vm.Flush()
				v, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
				return v
			}
			poll(t, func() bool {
				err := vm.Run(context.Background())
				vm.Reset()
				return err != nil || exits() > 0
			})
			require.NoError(t, vm.Run(context.Background()), c.name)
			sum, count, err := popLoop(vm)
			require.NoError(t, err, c.name)
			require.Equal(t, wantSum, sum, c.name)
			require.Equal(t, wantCount, count, c.name)
			require.Positive(t, exits(), c.name)
			require.NoError(t, vm.Close())
		}
	})

	t.Run("deopts two native activations deep", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		ib := instr.NewBuilder()
		ib.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_EQ).Emit(instr.DROP)
		ib.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_CONCAT).Emit(instr.RETURN)
		icode, err := ib.Assemble()
		require.NoError(t, err)
		inner := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeString(), types.TypeString()}, Returns: []types.Type{types.TypeString()}},
			Code: instr.Marshal(icode),
		}
		outerBuilder := instr.NewBuilder()
		outerBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
		outerCode, err := outerBuilder.Assemble()
		require.NoError(t, err)
		outer := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeString(), types.TypeString()}, Returns: []types.Type{types.TypeString()}},
			Code: instr.Marshal(outerCode),
		}
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()),
			program.WithConstants(inner, outer, types.String("deep-"), types.String("concat")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr, refErr error
		var value string
		var count, innerCount int
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			innerCount, refErr = vm.RefCount(1)
			if refErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, refErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Equal(t, 1, innerCount)
	})

	t.Run("keeps a borrowed callee's RefCount across a served call", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		held := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString()}, Returns: []types.Type{types.TypeString()}})
		refuse(held)
		inner := held.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.RETURN)).MustBuild()
		outer := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString()}, Returns: []types.Type{types.TypeString()}}).
			Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL), instr.MustNew(instr.RETURN)).MustBuild()

		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(inner, outer, types.String("chain")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr, refErr error
		var value string
		var count, innerCount int
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			innerCount, refErr = vm.RefCount(1)
			if refErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, refErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Equal(t, 1, innerCount)
	})

	t.Run("passes a borrowed parameter through native recursion", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		fib := indirectFibFunction()
		b := instr.NewBuilder()
		loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Bind(start).Emit(instr.I32_CONST, uint64(30)).Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Try(start, end, catch, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(fib), program.WithHandlers(b.Handlers()...))
		wantVM := interp.MustNew(prog, interp.WithFrame(8), interp.WithThreshold(-1))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		wantConst, err := wantVM.Const(0)
		require.NoError(t, err)
		wantRC, err := wantVM.RefCount(wantConst.Ref())
		require.NoError(t, err)

		var runErr, rcErr error
		var gotRC int
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithFrame(8), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			c, _ := vm.Const(0)
			gotRC, rcErr = vm.RefCount(c.Ref())
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, rcErr)

		require.Equal(t, wantRC, gotRC)
	})

	t.Run("transfers an owned argument lent to a borrowed parameter", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := lentProgram(t, 3000)

		wantVM := interp.MustNew(lentProgram(t, 3000), interp.WithThreshold(-1))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)
		wantInc, err := wantVM.Const(2)
		require.NoError(t, err)
		wantIncRC, err := wantVM.RefCount(wantInc.Ref())
		require.NoError(t, err)
		wantDec, err := wantVM.Const(3)
		require.NoError(t, err)
		wantDecRC, err := wantVM.RefCount(wantDec.Ref())
		require.NoError(t, err)

		var runErr, popErr, rcErr error
		var got types.Value
		var gotIncRC, gotDecRC int
		var deopts float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			gotInc, err := vm.Const(2)
			if err != nil {
				popErr = err
				return true
			}
			gotIncRC, rcErr = vm.RefCount(gotInc.Ref())
			if rcErr != nil {
				return true
			}
			gotDec, err := vm.Const(3)
			if err != nil {
				popErr = err
				return true
			}
			gotDecRC, rcErr = vm.RefCount(gotDec.Ref())
			if rcErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return deopts >= 1
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, want, got)

		require.Equal(t, wantIncRC, gotIncRC)
		require.Equal(t, wantDecRC, gotDecRC)
		require.GreaterOrEqual(t, deopts, float64(1))
		require.Less(t, deopts, float64(3000))
	})

	t.Run("deopts a deep recursion at its frame limit", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		record := types.NewStructType(types.NewStructField(types.TypeI32(), types.FieldWithName("n")))
		fb := instr.NewBuilder()
		loop, done, small := fb.Label(), fb.Label(), fb.Label()
		fb.Bind(loop)
		fb.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 20000).Emit(instr.I32_GE_S).BrIf(done)
		fb.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		fb.Br(loop)
		fb.Bind(done)
		fb.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.DROP)
		fb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_LT_S).BrIf(small)
		fb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
		fb.Bind(small).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
		fnCode, err := fb.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}},
			Locals: []types.Type{types.TypeI32()},
			Code:   instr.Marshal(fnCode),
		}

		mb := instr.NewBuilder()
		wloop, wdone, start, end, catch := mb.Label(), mb.Label(), mb.Label(), mb.Label(), mb.Label()
		mb.Bind(wloop)
		mb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 5).Emit(instr.I32_GE_S).BrIf(wdone)
		mb.Emit(instr.I32_CONST, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		mb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		mb.Br(wloop)
		mb.Bind(wdone)
		mb.Bind(start).Emit(instr.I32_CONST, 15).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		mb.Bind(end)
		mb.Bind(catch).Emit(instr.ERROR_CODE)
		mb.Try(start, end, catch, 1)
		code, err := mb.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithTypes(record), program.WithConstants(fn), program.WithHandlers(mb.Handlers()...))

		wantVM := interp.MustNew(prog, interp.WithFrame(8), interp.WithThreshold(-1))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)

		var runErr, popErr error
		var got types.Value
		var safepoints, releases, calls float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithFrame(8), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			safepoints, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			releases, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "release"})
			calls, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return safepoints > 0 && releases > 0 && calls > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("returns a wide i64 from native code", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		fnBuilder := instr.NewBuilder()
		fnBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.RETURN)
		fnCode, err := fnBuilder.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeI64()}, Returns: []types.Type{types.TypeI64()}},
			Code: instr.Marshal(fnCode),
		}
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(fn))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		result, err := vm.Pop()
		require.NoError(t, err)
		vm.Flush()

		deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		require.Zero(t, deopts)
		require.Equal(t, want, result)
	})

	t.Run("returns a wide i64 from native recursion", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		fnBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI64()}, Returns: []types.Type{types.TypeI64()}})
		small := fnBuilder.Label()
		fnBuilder.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I64_CONST, 2), instr.MustNew(instr.I64_LT_S)).BrIf(small)
		fnBuilder.Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I64_CONST, 1), instr.MustNew(instr.I64_SUB), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I64_CONST, 2), instr.MustNew(instr.I64_SUB), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
			instr.MustNew(instr.I64_ADD), instr.MustNew(instr.RETURN),
		)
		fnBuilder.Bind(small).Append(
			instr.MustNew(instr.LOCAL_GET, 0),
			instr.MustNew(instr.I64_CONST, 1), instr.MustNew(instr.I64_CONST, 50), instr.MustNew(instr.I64_SHL), instr.MustNew(instr.I64_ADD),
			instr.MustNew(instr.RETURN),
		)
		fn := fnBuilder.MustBuild()
		b := instr.NewBuilder()
		b.Emit(instr.I64_CONST, uint64(20)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fn))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries = baseline + optimized
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("passes a wide i64 argument into a native function", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		fn := wideArgFunction()
		b := instr.NewBuilder()
		for range 2000 {
			b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		}
		b.Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fn))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries = baseline + optimized
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("passes a wide i64 register argument from a native caller", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		inner := wideArgFunction()
		outerBuilder := instr.NewBuilder()
		loop, done := outerBuilder.Label(), outerBuilder.Label()
		outerBuilder.Bind(loop)
		outerBuilder.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(done)
		outerBuilder.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		outerBuilder.Br(loop)
		outerBuilder.Bind(done)
		outerBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.I64_ADD)
		outerBuilder.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
		outerCode, err := outerBuilder.Assemble()
		if err != nil {
			panic(err)
		}
		outer := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI64()}, Returns: []types.Type{types.TypeI64()}},
			Locals: []types.Type{types.TypeI32()},
			Code:   instr.Marshal(outerCode),
		}
		b := instr.NewBuilder()
		for index := range 2 * 2000 {
			b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, uint64(index/2000)).Emit(instr.CALL).Emit(instr.DROP)
		}
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(inner, outer))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries = baseline + optimized
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("returns a wide i64 from an OSR unit", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		fnBuilder := instr.NewBuilder()
		loop, done := fnBuilder.Label(), fnBuilder.Label()
		fnBuilder.Bind(loop)
		fnBuilder.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
		fnBuilder.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		fnBuilder.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		fnBuilder.Br(loop)
		fnBuilder.Bind(done)
		fnBuilder.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_TO_I64_S)
		fnBuilder.Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.I64_ADD)
		fnBuilder.Emit(instr.RETURN)
		fnCode, err := fnBuilder.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI64()}},
			Locals: []types.Type{types.TypeI32(), types.TypeI32()},
			Code:   instr.Marshal(fnCode),
		}
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fn))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, compiles float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("compiles a loop seeded with a wide i64", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		seed := int64(-3750763034362895579)
		for _, c := range []struct {
			name string
			prog *program.Program
		}{
			{"const", fnvProgram(t, 100_000, instr.I64_CONST, uint64(seed))},
			{"pool cell", fnvProgram(t, 100_000, instr.CONST_GET, 1, types.I64(seed))},
		} {
			want := runProgram(t, c.prog)

			var got types.Value
			var runErr, popErr error
			var deopts, compiles float64
			profiler := prof.New()
			vm := interp.MustNew(c.prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
			defer vm.Close()
			poll(t, func() bool {
				runErr = vm.Run(context.Background())
				if runErr != nil {
					return true
				}
				got, popErr = vm.Pop()
				if popErr != nil {
					return true
				}
				vm.Reset()
				vm.Flush()
				deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
				compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
				return compiles > 0
			})
			require.NoError(t, runErr, c.name)
			require.NoError(t, popErr, c.name)
			require.Zero(t, deopts, c.name)
			require.Equal(t, want, got, c.name)
		}
	})

	t.Run("keeps a wide i64 constant's RefCount across a shape-guard deopt", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		seed := int64(-3750763034362895579)
		fnBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32())}, Returns: []types.Type{types.TypeI64()}})
		fnBuilder.Append(instr.MustNew(instr.I64_CONST, uint64(seed)))
		fnBuilder.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.DROP))
		fnBuilder.Append(instr.MustNew(instr.RETURN))
		fn := fnBuilder.MustBuild()
		elem := types.NewArrayType(types.TypeI32())
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 4).Emit(instr.ARRAY_NEW_DEFAULT, 0)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithGlobals(elem),
			program.WithConstants(fn), program.WithTypes(elem))

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		hostVal, err := threaded.Marshal([]int32{1, 2, 3})
		require.NoError(t, err)
		hostAddr, err := threaded.Alloc(hostVal)
		require.NoError(t, err)
		require.NoError(t, threaded.SetGlobal(0, types.BoxRef(hostAddr)))
		require.NoError(t, threaded.Run(context.Background()))
		wantBoxed, err := threaded.PopBoxed()
		require.NoError(t, err)
		wantRC, err := threaded.RefCount(wantBoxed.Ref())
		require.NoError(t, err)
		require.Equal(t, 1, wantRC)

		var runErr, popErr, rcErr, marshalErr, allocErr, globalErr error
		var gotBoxed types.Boxed
		var gotRC int
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			var hostVal types.Value
			hostVal, marshalErr = vm.Marshal([]int32{1, 2, 3})
			if marshalErr != nil {
				return true
			}
			var hostAddr int
			hostAddr, allocErr = vm.Alloc(hostVal)
			if allocErr != nil {
				return true
			}
			globalErr = vm.SetGlobal(0, types.BoxRef(hostAddr))
			if globalErr != nil {
				return true
			}
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			gotBoxed, popErr = vm.PopBoxed()
			if popErr != nil {
				return true
			}
			gotRC, rcErr = vm.RefCount(gotBoxed.Ref())
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		})
		require.NoError(t, marshalErr)
		require.NoError(t, allocErr)
		require.NoError(t, globalErr)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, wantRC, gotRC)
	})

	t.Run("promotes a narrow i64 accumulator in an OSR loop", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		fn := wideArgFunction()
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I64_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I64_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_XOR).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI64(), types.TypeI32()), program.WithConstants(fn))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, compiles float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("promotes an i64 accumulator in an OSR FNV loop", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I64_CONST, uint64(0x1234567)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(100_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_TO_I64_U).Emit(instr.I64_XOR)
		b.Emit(instr.I64_CONST, 1099511628211).Emit(instr.I64_MUL)
		b.Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI64(), types.TypeI32()))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, compiles float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)

		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("boxes a wide store in a loop each iteration", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(50_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_TO_I64_S).Emit(instr.I64_ADD)
		b.Emit(instr.GLOBAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.GLOBAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithGlobals(types.TypeI64()))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries = baseline + optimized
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("tiers a hot function up to Optimized", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := fibFlatCallsProgram(t, 2000)
		want := runProgram(t, prog)

		var got types.Value
		var compiles float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("keeps a caller entering past a callee's caught traps", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const rounds, batches = 32, 64
		b := instr.NewBuilder()
		loop, done, start, end, catch, next := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(rounds)).Emit(instr.I32_GE_S).BrIf(done)
		b.Bind(start).Emit(instr.I32_CONST, uint64(100)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Bind(end).Br(next)
		b.Bind(catch).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Bind(next)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1)
		b.Try(start, end, catch, 2)
		code, err := b.Assemble()
		require.NoError(t, err)
		rareBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32(), types.TypeI32())
		rareLoop, rareDone := rareBuilder.Label(), rareBuilder.Label()
		rareBuilder.Bind(rareLoop).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_GE_S)).BrIf(rareDone)
		rareBuilder.Append(
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SUB), instr.MustNew(instr.I32_NE),
			instr.MustNew(instr.CONST_GET, uint64(1)), instr.MustNew(instr.CALL),
			instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1),
		).Br(rareLoop)
		rareBuilder.Bind(rareDone).Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.RETURN))
		rare := rareBuilder.MustBuild()
		prog := program.New(code, program.WithLocals(types.TypeI32(), types.TypeI32()),
			program.WithConstants(rare, divFunction(t)), program.WithHandlers(b.Handlers()...))

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(rounds), want)
		counts := func(vm *interp.Interpreter) []int {
			var out []int
			for index := range 2 {
				c, err := vm.Const(index)
				require.NoError(t, err)
				count, err := vm.RefCount(c.Ref())
				require.NoError(t, err)
				out = append(out, count)
			}
			return out
		}
		wantCounts := counts(threaded)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) +
				metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}) > 0
		})
		require.NoError(t, runErr)

		before := entries()
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantCounts, counts(vm))
			vm.Reset()
		}
		require.GreaterOrEqual(t, entries()-before, float64(batches*rounds))
	})

	t.Run("keeps a protected hot loop native past caught traps", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const rounds, batches = 32, 64
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(rounds)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, uint64(100)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, uint64(100)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		protectedBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).
			Locals(types.TypeI32(), types.TypeI32(), types.TypeString())
		protectedLoop, protectedDone, start, end, catch, next := protectedBuilder.Label(), protectedBuilder.Label(), protectedBuilder.Label(), protectedBuilder.Label(), protectedBuilder.Label(), protectedBuilder.Label()
		protectedBuilder.Append(instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.LOCAL_SET, 3))
		protectedBuilder.Bind(protectedLoop).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_GE_S)).BrIf(protectedDone)
		protectedBuilder.Bind(start).Append(
			instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 6400),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.I32_CONST, 63), instr.MustNew(instr.I32_AND),
			instr.MustNew(instr.I32_DIV_S), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2))
		protectedBuilder.Bind(end).Br(next)
		protectedBuilder.Bind(catch).Append(instr.MustNew(instr.DROP), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 1000), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2))
		protectedBuilder.Bind(next).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1)).Br(protectedLoop)
		protectedBuilder.Bind(protectedDone).Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.RETURN))
		protectedBuilder.Try(start, end, catch, 4)
		protected := protectedBuilder.MustBuild()
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(protected, types.String("owned")))

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		count := func(vm *interp.Interpreter) int {
			c, err := vm.Const(1)
			require.NoError(t, err)
			n, err := vm.RefCount(c.Ref())
			require.NoError(t, err)
			return n
		}
		wantCount := count(threaded)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) +
				metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}) > 0
		})
		require.NoError(t, runErr)

		before := entries()
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantCount, count(vm))
			vm.Reset()
		}
		require.GreaterOrEqual(t, entries()-before, float64(batches*rounds))
	})

	t.Run("catches a native callee's throw in its native caller", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const calls, batches = 32, 64
		thrower := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}})
		raise := thrower.Label()
		thrower.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 15), instr.MustNew(instr.I32_AND), instr.MustNew(instr.I32_CONST, 15), instr.MustNew(instr.I32_EQ)).BrIf(raise)
		thrower.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.RETURN))
		thrower.Bind(raise).Append(instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.THROW))

		guard := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}})
		start, end, catch := guard.Label(), guard.Label(), guard.Label()
		guard.Bind(start).Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL))
		guard.Bind(end).Append(instr.MustNew(instr.RETURN))
		guard.Bind(catch).Append(instr.MustNew(instr.STRING_LEN), instr.MustNew(instr.RETURN))
		guard.Try(start, end, catch, 1)

		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 0)
		for x := range calls {
			b.Emit(instr.I32_CONST, uint64(x)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.I32_ADD)
		}
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(guard.MustBuild(), thrower.MustBuild(), types.String("boom!")))

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		counts := func(vm *interp.Interpreter) []int {
			var out []int
			for index := range 3 {
				c, err := vm.Const(index)
				require.NoError(t, err)
				count, err := vm.RefCount(c.Ref())
				require.NoError(t, err)
				out = append(out, count)
			}
			return out
		}
		wantCounts := counts(threaded)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) +
				metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"}) > 0
		})
		require.NoError(t, runErr)

		// Tier-up compiles finish asynchronously, so a batch can run some
		// calls threaded while a new tier publishes. Every Run must match
		// threaded execution; native code must carry batches*calls entries
		// eventually, however many Runs that takes.
		before := entries()
		poll(t, func() bool {
			for range batches {
				require.NoError(t, vm.Run(context.Background()))
				got, err := vm.Pop()
				require.NoError(t, err)
				require.Equal(t, want, got)
				require.Equal(t, wantCounts, counts(vm))
				vm.Reset()
			}
			return entries()-before >= float64(batches*calls)
		})

		require.Zero(t, metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "unsupported"}))
	})

	t.Run("enters a protected module loop and reports a trap outside it", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n = 200_000
		want := 0
		for i := range n {
			if d := (i + 1) & 63; d == 0 {
				want += 1000
			} else {
				want += 6400 / d
			}
		}
		b := instr.NewBuilder()
		loop, done, start, end, catch, next := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
		b.Bind(start)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 6400)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.I32_CONST, 63).Emit(instr.I32_AND)
		b.Emit(instr.I32_DIV_S).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Bind(end).Br(next)
		b.Bind(catch).Emit(instr.DROP).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1000).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Bind(next).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(want)).Emit(instr.I32_SUB).Emit(instr.I32_DIV_S)
		b.Try(start, end, catch, 2)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32(), types.TypeI32()), program.WithHandlers(b.Handlers()...))
		wantErr := runProgramErr(t, prog, interp.WithThreshold(-1))
		require.ErrorIs(t, wantErr, interp.ErrDivideByZero)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		var runErr error
		var entries float64
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.Greater(t, entries, float64(0))
		require.EqualError(t, runErr, wantErr.Error())
	})

	t.Run("escapes guest handlers on a cancelled context", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		sum := sumFunction(t)
		b := instr.NewBuilder()
		loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Bind(start).Emit(instr.I32_CONST, uint64(2_000_000_000)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Br(start)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Try(start, end, catch, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(sum), program.WithHandlers(b.Handlers()...))

		var runErr error
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				time.Sleep(time.Second)
				cancel()
			}()
			runErr = vm.Run(ctx)
			if !errors.Is(runErr, context.Canceled) {
				return false
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			return exits > 0
		})
		require.Error(t, runErr)
		require.ErrorIs(t, runErr, context.Canceled)
	})

	t.Run("runs a function allocated after construction interpreted", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 7).Emit(instr.RETURN)
		code, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32()}}, Code: instr.Marshal(code)}
		b = instr.NewBuilder()
		b.Emit(instr.GLOBAL_GET, 0).Emit(instr.CALL)
		code, err = b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithGlobals(types.TypeAny()))

		vm := interp.MustNew(prog, interp.WithThreshold(1))
		defer vm.Close()
		addr, err := vm.Alloc(fn)
		require.NoError(t, err)
		require.NoError(t, vm.SetGlobal(0, types.BoxRef(addr)))
		require.NoError(t, vm.Run(context.Background()))
		result, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), result)
	})

	t.Run("keeps a callee alive across a boxed wide i64 argument", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN)
		code, err := b.Assemble()
		require.NoError(t, err)
		callee := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI64()}, Returns: []types.Type{types.TypeI32()}}, Code: instr.Marshal(code)}
		b = instr.NewBuilder()
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
		code, err = b.Assemble()
		require.NoError(t, err)
		caller := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI64()}, Returns: []types.Type{types.TypeI32()}}, Code: instr.Marshal(code)}
		b = instr.NewBuilder()
		warm, warmed := b.Label(), b.Label()
		b.Bind(warm)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 20000).Emit(instr.I32_GE_S).BrIf(warmed)
		b.Emit(instr.I64_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(warm)
		b.Bind(warmed).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 20000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I64_CONST, 0).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.I32_ADD)
		code, err = b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(callee, caller))
		want := runProgram(t, prog)

		var result types.Value
		var rc int
		var deopts, entries float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			if err = vm.Run(context.Background()); err != nil {
				return true
			}
			result, _ = vm.Pop()
			c, _ := vm.Const(0)
			rc, _ = vm.RefCount(c.Ref())
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.NoError(t, err)
		require.Zero(t, deopts)
		require.Equal(t, want, result)
		require.Equal(t, 1, rc)
	})

	t.Run("sums a typed i32 array in a calling function", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		identBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}})
		identBuilder.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.RETURN))
		ident := identBuilder.MustBuild()
		sum := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.NewArrayType(types.TypeI32())}, Returns: []types.Type{types.TypeI32()},
		})
		header, done := sum.Label(), sum.Label()
		sum.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 1))
		sum.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 2))
		sum.Bind(header)
		sum.Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.I32_GE_S)).BrIf(done)
		sum.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL), instr.MustNew(instr.DROP))
		sum.Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.ARRAY_GET), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2))
		sum.Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1))
		sum.Br(header)
		sum.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.RETURN))
		sumFn := sum.MustBuild()
		sumFn.Locals = []types.Type{types.TypeI32(), types.TypeI32()}

		arr := types.NewArray(types.NewArrayType(types.TypeI32()), types.BoxI32(10), types.BoxI32(20), types.BoxI32(30), types.BoxI32(40))
		b := instr.NewBuilder()
		loop, loopDone := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(loopDone)
		b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(loopDone).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(sumFn, ident, arr))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var entries float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("reads and writes a ref array element", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		wantValue, wantCount := runProgramString(t, refArrayProgram(t, 20000))

		var runErr, popErr error
		var value string
		var count int
		var entries float64
		profiler := prof.New()
		vm := interp.MustNew(refArrayProgram(t, 20000), interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
	})

	t.Run("walks a struct tree", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		want := runProgram(t, structTreeProgram(t, 20000))

		var runErr, popErr error
		var result types.Value
		var compiles float64
		profiler := prof.New()
		vm := interp.MustNew(structTreeProgram(t, 20000), interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()

			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0 && nativeEntries(profiler) > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("catches an out-of-bounds array.get in a guest handler", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		at := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.NewArrayType(types.TypeI32()), types.TypeI32()}, Returns: []types.Type{types.TypeI32()},
		})
		at.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.ARRAY_GET), instr.MustNew(instr.RETURN))
		atFn := at.MustBuild()
		arr := types.NewArray(types.NewArrayType(types.TypeI32()), types.BoxI32(1), types.BoxI32(2), types.BoxI32(3))

		b := instr.NewBuilder()
		warmLoop, warmDone, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(warmLoop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(warmDone)
		b.Emit(instr.CONST_GET, 1).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(warmLoop)
		b.Bind(warmDone)
		b.Bind(start).Emit(instr.CONST_GET, 1).Emit(instr.I32_CONST, 99).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Try(start, end, catch, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(atFn, arr), program.WithHandlers(b.Handlers()...))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("deopts a host array that fails its shape guard", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		ln := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32())}, Returns: []types.Type{types.TypeI32()}})
		ln.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.RETURN))
		lnFn := ln.MustBuild()

		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithGlobals(types.NewArrayType(types.TypeI32())), program.WithConstants(lnFn))

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		hostVal, err := threaded.Marshal([]int32{1, 2, 3, 4, 5})
		require.NoError(t, err)
		hostAddr, err := threaded.Alloc(hostVal)
		require.NoError(t, err)
		require.NoError(t, threaded.SetGlobal(0, types.BoxRef(hostAddr)))
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(5), want)

		var runErr, popErr, marshalErr, allocErr, globalErr error
		var result types.Value
		var exits float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			hostVal, marshalErr := vm.Marshal([]int32{1, 2, 3, 4, 5})
			if marshalErr != nil {
				return true
			}
			hostAddr, allocErr := vm.Alloc(hostVal)
			if allocErr != nil {
				return true
			}
			globalErr = vm.SetGlobal(0, types.BoxRef(hostAddr))
			if globalErr != nil {
				return true
			}
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		})
		require.NoError(t, marshalErr)
		require.NoError(t, allocErr)
		require.NoError(t, globalErr)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("recompiles a null array's refuted container guard with its site bridged", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const rounds, batches = 64, 8
		at := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.NewArrayType(types.TypeI32()), types.TypeI32()}, Returns: []types.Type{types.TypeI32()},
		}).Locals(types.TypeI32())
		count, counted := at.Label(), at.Label()
		at.Bind(count).Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 8), instr.MustNew(instr.I32_GE_S)).BrIf(counted)
		at.Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2)).Br(count)
		at.Bind(counted).Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.ARRAY_GET), instr.MustNew(instr.RETURN))
		arr := types.NewArray(types.NewArrayType(types.TypeI32()), types.BoxI32(1), types.BoxI32(2), types.BoxI32(3))

		b := instr.NewBuilder()
		loop, done, live, start, end, catch, next, null, picked := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 1).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(rounds)).Emit(instr.I32_GE_S).BrIf(done)

		b.Emit(instr.I32_CONST, 1).BrIf(live)
		b.Emit(instr.REF_NULL).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.DROP)
		b.Bind(live).Bind(start)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 15).Emit(instr.I32_AND).Emit(instr.I32_CONST, 15).Emit(instr.I32_EQ).BrIf(null)
		b.Emit(instr.LOCAL_GET, 2).Br(picked)
		b.Bind(null).Emit(instr.LOCAL_GET, 3)
		b.Bind(picked).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Bind(end).Br(next)
		b.Bind(catch).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Bind(next)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1)
		b.Try(start, end, catch, 4)
		code, err := b.Assemble()
		require.NoError(t, err)
		array := types.NewArrayType(types.TypeI32())
		prog := program.New(code, program.WithLocals(types.TypeI32(), types.TypeI32(), array, array),
			program.WithConstants(at.MustBuild(), arr), program.WithHandlers(b.Handlers()...))
		want := runProgram(t, prog)
		require.Equal(t, types.I32(rounds/16), want)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) +
				metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}) > 0
		})
		require.NoError(t, runErr)
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			vm.Reset()
			time.Sleep(time.Millisecond)
		}

		before := entries()
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
		}
		require.GreaterOrEqual(t, entries()-before, float64(batches*rounds))
	})

	t.Run("keeps a loop with a never-true null array read native", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const runs = 32

		b := instr.NewBuilder()
		loop, skip, done := b.Label(), b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 20_000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 0).Emit(instr.I32_GE_S).BrIf(skip)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Bind(skip)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 2)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.NewArrayType(types.TypeI32()), types.TypeI32(), types.TypeI32()))
		want := runProgram(t, prog)

		var errs []error
		var results []types.Value
		var entries, deopts float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			errs, results = nil, nil
			for range runs {
				if err := vm.Run(context.Background()); err != nil {
					errs = append(errs, err)
					return true
				}
				result, err := vm.Pop()
				if err != nil {
					errs = append(errs, err)
					return true
				}
				results = append(results, result)
				vm.Reset()
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return entries >= runs/2
		})
		require.Empty(t, errs)
		require.Len(t, results, runs)
		for _, result := range results {
			require.Equal(t, want, result)
		}
		require.Less(t, deopts, float64(2))
	})

	t.Run("guards an array replaced inside an Optimized loop afresh", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 200_000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 3).Emit(instr.I32_AND).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 3).Emit(instr.I32_AND).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 3).Emit(instr.I32_AND).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 2)
		code, err := b.Assemble()
		require.NoError(t, err)
		elem := types.NewArrayType(types.TypeI32())
		prog := program.New(code, program.WithLocals(elem, types.TypeI32(), types.TypeI32()), program.WithTypes(elem))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var entries float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("reports no vm_jit metrics for a short first Run by default", func(t *testing.T) {
		profiler := prof.New()
		vm := interp.MustNew(fibCallsProgram(t, 3), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		vm.Flush()

		for _, m := range profiler.Metrics() {
			require.NotContains(t, m.Name, "vm_jit_")
		}
	})

	t.Run("enters a module loop by OSR", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := iterativeFibProgram(t, 200_000)
		want := runProgram(t, prog)

		var got types.Value
		var entries float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("compiles a module loop straight to Optimized", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n = 20000
		want := runProgram(t, concat(t, n))
		prog := concat(t, n)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		// Compiles finish asynchronously and count when a later entry drains
		// them, so rerun until one has.
		poll(t, func() bool {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
			vm.Flush()
			compiles, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		})
	})

	t.Run("restores promoted locals when a callee deopts under a loop", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n = 200_000
		want := runProgram(t, deopt(t, n, n-10))
		prog := deopt(t, n, n-10)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		// Compiles finish asynchronously, so rerun until optimized code has
		// both counted and bridged.
		poll(t, func() bool {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
			vm.Flush()
			compiles, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			bridges, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return compiles > 0 && bridges > 0
		})
	})

	t.Run("restores spilled operands of every outer activation when a callee deopts five activations deep", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const warm = 2000
		want := runProgram(t, descent(t, warm))
		prog := descent(t, warm)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		// Compiles finish asynchronously, so rerun until optimized code has
		// both compiled and left through an exit below the entry.
		poll(t, func() bool {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
			vm.Flush()
			compiles, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return compiles > 0 && deopts > 0
		})
	})

	t.Run("tiers up a callee reached only from native code", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n = 200_000
		want := runProgram(t, fib(t, n))
		prog := fib(t, n)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		// Compiles finish asynchronously and count when a later entry drains
		// them, so rerun until both the caller and the callee have.
		poll(t, func() bool {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
			vm.Flush()
			compiles, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles >= 2
		})
	})

	t.Run("tiers up a native-only callee across short Runs", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 100).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(incFunction()))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()

		var got types.Value
		var runErr, popErr error
		var compiles float64
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles >= 2
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.GreaterOrEqual(t, compiles, float64(2))
	})

	t.Run("enters a function's loop header mid-call by OSR", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		// warmCalls*warmEach back edges warm sum's own loop-header OSR site
		// across calls too few (warmCalls) to ever reach its entry-0 CALL
		// threshold on their own, so entry-0 never compiles: the final call
		// starts threaded and can only enter native code through OSR, mid-call,
		// once the site resolves.
		const threshold, warmCalls, warmEach, n = 1000, 20, 300, 200_000
		sum := sumFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warmCalls)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, uint64(warmEach)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(sum))
		want := runProgram(t, prog)

		var got types.Value
		var entries float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(threshold), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("catches a deopt inside an OSR loop in a guest handler", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		fnBuilder := instr.NewBuilder()
		loop, done := fnBuilder.Label(), fnBuilder.Label()
		fnBuilder.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		fnBuilder.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		fnBuilder.Bind(loop)
		fnBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(2_000_000)).Emit(instr.I32_GE_S).BrIf(done)
		fnBuilder.Emit(instr.LOCAL_GET, 1)
		fnBuilder.Emit(instr.I32_CONST, 100)
		fnBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1_500_000)).Emit(instr.I32_SUB)
		fnBuilder.Emit(instr.I32_DIV_S)
		fnBuilder.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		fnBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		fnBuilder.Br(loop)
		fnBuilder.Bind(done).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
		fnCode, err := fnBuilder.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32()}},
			Locals: []types.Type{types.TypeI32(), types.TypeI32()},
			Code:   instr.Marshal(fnCode),
		}
		b := instr.NewBuilder()
		start, end, catch, exit := b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(start).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.I32_CONST, 0).Br(exit)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Bind(exit)
		b.Try(start, end, catch, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fn), program.WithHandlers(b.Handlers()...))
		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		wantCode, err := threaded.PopBoxed()
		require.NoError(t, err)
		wantConst, err := threaded.Const(0)
		require.NoError(t, err)
		wantValue, err := threaded.RefCount(wantConst.Ref())
		require.NoError(t, err)

		var gotCode types.Boxed
		var gotValue int
		var exits float64
		var runErr, popErr, constErr, refErr error
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			gotCode, popErr = vm.PopBoxed()
			if popErr != nil {
				return true
			}
			str, err := vm.Const(0)
			constErr = err
			if constErr != nil {
				return true
			}
			gotValue, refErr = vm.RefCount(str.Ref())
			if refErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, constErr)
		require.NoError(t, refErr)
		require.Equal(t, wantCode, gotCode)
		require.Equal(t, wantValue, gotValue)
	})

	t.Run("unwraps an OSR loop header that cannot compile", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		b := instr.NewBuilder()
		header, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(header)
		b.Emit(instr.LOCAL_GET, 1).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).Emit(instr.LOCAL_SET, 1)
		b.Br(header)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32(), types.TypeI32()))
		want := runProgram(t, prog)

		var got types.Value
		var unsupported float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			unsupported, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "unsupported"})
			return unsupported > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)

		require.Equal(t, float64(1), unsupported)

		require.NoError(t, vm.Run(context.Background()))
		got2, popErr2 := vm.Pop()
		require.NoError(t, popErr2)
		require.Equal(t, want, got2)
		vm.Flush()
		again, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "unsupported"})
		require.Equal(t, float64(1), again)
	})

	t.Run("invalidates an OSR site whose loop body always bridges", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n = 600
		b := instr.NewBuilder()
		header, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(header)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 0).Emit(instr.MAP_NEW_DEFAULT, 0).Emit(instr.MAP_KEYS).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(header)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithTypes(types.NewMapType(types.TypeI32(), types.TypeI32())))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		ctx := context.Background()

		// The header's own back edges cross its cadence within the first
		// Run and it is invalidated (refute deopts) within a handful of Runs. The
		// ip-0 site's submit waits for the header to resolve (resolved),
		// then needs its own cadence (interval) of Runs for a submit
		// window and again for its first lookup to drain and publish the
		// compile: at n=600 back edges per Run that lands around Run 512.
		var got types.Value
		var runErr, popErr error
		var compiles float64
		poll(t, func() bool {
			runErr = vm.Run(ctx)
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles >= 2
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)

		require.Equal(t, float64(2), compiles)

		for range 12 {
			require.NoError(t, vm.Run(ctx))
			_, err := vm.Pop()
			require.NoError(t, err)
			vm.Reset()
		}
		vm.Flush()
		exits, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})

		require.NoError(t, vm.Run(ctx))
		got2, err2 := vm.Pop()
		require.NoError(t, err2)
		require.Equal(t, want, got2)
		vm.Reset()
		vm.Flush()
		compilesAgain, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
		exitsAgain, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		require.Equal(t, float64(2), compilesAgain)
		require.Equal(t, exits, exitsAgain)
	})

	t.Run("reuses a resolved OSR site after Reset", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := iterativeFibProgram(t, 200_000)
		want := runProgram(t, prog)

		profiler := prof.New()
		pool, err := interp.NewPool(prog, 2, interp.WithThreshold(1), interp.WithProfiler(profiler))
		require.NoError(t, err)
		defer pool.Close()
		first, err := pool.Get(context.Background())
		require.NoError(t, err)
		second, err := pool.Get(context.Background())
		require.NoError(t, err)

		var gotFirst, gotSecond types.Value
		var entries float64
		var runErr, firstErr, secondErr error
		poll(t, func() bool {
			runErr = first.Run(context.Background())
			if runErr != nil {
				return true
			}
			gotFirst, firstErr = first.Pop()
			if firstErr != nil {
				return true
			}
			runErr = second.Run(context.Background())
			if runErr != nil {
				return true
			}
			gotSecond, secondErr = second.Pop()
			if secondErr != nil {
				return true
			}
			first.Reset()
			second.Reset()
			first.Flush()
			second.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, firstErr)
		require.NoError(t, secondErr)
		require.Equal(t, want, gotFirst)
		require.Equal(t, want, gotSecond)
	})

	t.Run("runs an indirect self call through a parameter", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		prog := indirectFibCallsProgram(t, 20, 50)

		wantVM := interp.MustNew(indirectFibCallsProgram(t, 20, 50), interp.WithThreshold(-1))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)
		wantConst, err := wantVM.Const(0)
		require.NoError(t, err)
		wantRC, err := wantVM.RefCount(wantConst.Ref())
		require.NoError(t, err)

		var runErr, popErr, rcErr error
		var got types.Value
		var gotRC int
		var compiles, entries, deopts float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(100), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			gotConst, err := vm.Const(0)
			if err != nil {
				popErr = err
				return true
			}
			gotRC, rcErr = vm.RefCount(gotConst.Ref())
			if rcErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return compiles > 0 && entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, want, got)
		require.Equal(t, wantRC, gotRC)
		require.Zero(t, deopts)
	})

	t.Run("recompiles a unit invalidated by a later-recorded site", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := indirectFibCallsProgram(t, 20, 50)
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		run := func() {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
		}
		compiled := prof.Label{Key: "tier", Value: "baseline"}
		ok := prof.Label{Key: "outcome", Value: "ok"}
		poll(t, func() bool {
			err := vm.Run(context.Background())
			vm.Reset()
			return err != nil || metric("vm_jit_compiles_total", compiled, ok) >= 2
		})

		for range 16 {
			run()
			time.Sleep(time.Millisecond)
		}
		deopts := metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		for range 16 {
			run()
		}
		require.Equal(t, deopts, metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}))
	})

	t.Run("compiles a unit before its dynamic sites ran", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := indirectFibCallsProgram(t, 20, 50)
		want := runProgram(t, prog)

		var runErr, popErr error
		var got types.Value
		var unsupported, ok float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			unsupported, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "unsupported"})
			ok, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			return ok >= 1
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.Zero(t, unsupported)
	})

	t.Run("invalidates a speculated callee refuted by a second function", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := applyProgram(t, 3000)

		wantVM := interp.MustNew(applyProgram(t, 3000), interp.WithThreshold(-1))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)
		wantInc, err := wantVM.Const(1)
		require.NoError(t, err)
		wantIncRC, err := wantVM.RefCount(wantInc.Ref())
		require.NoError(t, err)
		wantDec, err := wantVM.Const(2)
		require.NoError(t, err)
		wantDecRC, err := wantVM.RefCount(wantDec.Ref())
		require.NoError(t, err)

		var runErr, popErr, rcErr error
		var got types.Value
		var gotIncRC, gotDecRC int
		var deopts float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			gotInc, err := vm.Const(1)
			if err != nil {
				popErr = err
				return true
			}
			gotIncRC, rcErr = vm.RefCount(gotInc.Ref())
			if rcErr != nil {
				return true
			}
			gotDec, err := vm.Const(2)
			if err != nil {
				popErr = err
				return true
			}
			gotDecRC, rcErr = vm.RefCount(gotDec.Ref())
			if rcErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return deopts >= 1
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, want, got)
		require.Equal(t, wantIncRC, gotIncRC)
		require.Equal(t, wantDecRC, gotDecRC)
		require.GreaterOrEqual(t, deopts, float64(1))

		require.Less(t, deopts, float64(3000))
	})

	t.Run("recompiles a polymorphic speculated site generic", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n, runs = 200, 16
		b := instr.NewBuilder()
		loop, done, inner, counted, high, chosen := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 3).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(counted)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Br(inner)
		b.Bind(counted)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.GLOBAL_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_AND).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(high)
		b.Emit(instr.CONST_GET, 1).Br(chosen)
		b.Bind(high).Emit(instr.CONST_GET, 2)
		b.Bind(chosen).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32(), types.TypeI32(), types.TypeString(), types.TypeI32()), program.WithGlobals(types.TypeI32()),
			program.WithConstants(relayDynamicFunction(), bumpFunction(1), bumpFunction(3), types.String("s")))

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.SetGlobal(0, types.BoxI32(1)))
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		deopts := func() float64 { return metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}) }
		calls := func() float64 { return metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"}) }

		run := func() {
			require.NoError(t, vm.SetGlobal(0, types.BoxI32(1)))
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
		}

		compiled := prof.Label{Key: "tier", Value: "baseline"}
		ok := prof.Label{Key: "outcome", Value: "ok"}
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_compiles_total", compiled, ok) >= 2
		})
		require.NoError(t, runErr)
		for range runs {
			require.NoError(t, vm.Run(context.Background()))
			vm.Reset()
		}

		turned := deopts()
		for range runs {
			run()
			time.Sleep(time.Millisecond)
		}

		require.Less(t, deopts()-turned, float64(8))

		settled, served := deopts(), calls()
		for range runs {
			run()
		}
		require.Equal(t, settled, deopts())
		require.Greater(t, calls(), served)
	})

	t.Run("traps a closure callee's site until native code records it", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 0).Emit(instr.CLOSURE_NEW).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(50)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)

		wrap := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32(), types.TypeAny()}, Returns: []types.Type{types.TypeI32()}})
		wrap.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.CALL), instr.MustNew(instr.RETURN))
		prog := program.New(code, program.WithLocals(types.TypeI32(), types.TypeAny()),
			program.WithConstants(incFunction(), wrap.MustBuild()))
		want := runProgram(t, prog)

		vm := interp.MustNew(prog, interp.WithThreshold(1))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("calls closures natively", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := counterProgram(t)

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		wantCounter, err := threaded.Pop()
		require.NoError(t, err)
		wantValue, wantCount, err := popString(threaded)
		require.NoError(t, err)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, label prof.Label) float64 {
			v, _ := profiler.Metric(name, label)
			return v
		}
		entry := prof.Label{Key: "tier", Value: "optimized"}
		deopt, call := prof.Label{Key: "kind", Value: "deopt"}, prof.Label{Key: "kind", Value: "call"}

		var counter types.Value
		var value string
		var count int
		var runErr, popErr error
		var compiled, entries, deopts, called float64
		poll(t, func() bool {
			vm.Flush()
			entered, deopted, exited := metric("vm_jit_entries_total", entry), metric("vm_jit_exits_total", deopt), metric("vm_jit_exits_total", call)
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if counter, popErr = vm.Pop(); popErr != nil {
				return true
			}
			if value, count, popErr = popString(vm); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, deopts, called = metric("vm_jit_entries_total", entry)-entered, metric("vm_jit_exits_total", deopt)-deopted, metric("vm_jit_exits_total", call)-exited

			compiled, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiled > 0 && entries > 0 && deopts == 0 && called == 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantCounter, counter)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Positive(t, compiled)
		require.Positive(t, entries)
		require.Zero(t, deopts)
		require.Zero(t, called)
	})

	t.Run("keeps a caller entering while its closure callee's Baseline is pending", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		prog := counterProgram(t)
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(2*8), interp.WithProfiler(profiler))
		defer vm.Close()

		metric := func(name string, label prof.Label) float64 {
			v, _ := profiler.Metric(name, label)
			return v
		}
		entry, call := prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "kind", Value: "call"}

		var got types.Value
		var runErr, popErr error
		var entries, called float64
		poll(t, func() bool {
			vm.Flush()
			entered, exited := metric("vm_jit_entries_total", entry), metric("vm_jit_exits_total", call)
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if got, popErr = vm.Pop(); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, called = metric("vm_jit_entries_total", entry)-entered, metric("vm_jit_exits_total", call)-exited
			return entries > 0 && called == 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.Positive(t, entries)
		require.Zero(t, called)
	})

	t.Run("compiles a closure body reached only from native code", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		counter := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
			Captures(types.TypeI32()).
			Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.DUP), instr.MustNew(instr.UPVAL_SET, 0), instr.MustNew(instr.RETURN)).
			MustBuild()
		gb := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeAny(), types.TypeI32())
		loop, done := gb.Label(), gb.Label()
		gb.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.LOCAL_SET, 1))
		gb.Bind(loop).Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 64), instr.MustNew(instr.I32_GE_S)).BrIf(done)
		gb.Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.CALL), instr.MustNew(instr.DROP))
		gb.Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2)).Br(loop)
		gb.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.CALL), instr.MustNew(instr.RETURN))
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 7).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(counter, gb.MustBuild()))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		baseline, optimized := prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "tier", Value: "optimized"}
		call := prof.Label{Key: "kind", Value: "call"}
		entered := func() float64 {
			return metric("vm_jit_entries_total", baseline) + metric("vm_jit_entries_total", optimized)
		}

		var got types.Value
		var runErr, popErr error
		var compiled, entries, called float64
		poll(t, func() bool {
			vm.Flush()
			before, exited := entered(), metric("vm_jit_exits_total", call)
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if got, popErr = vm.Pop(); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiled = metric("vm_jit_compiles_total", baseline, prof.Label{Key: "outcome", Value: "ok"})
			entries, called = entered()-before, metric("vm_jit_exits_total", call)-exited
			return compiled >= 2 && entries > 0 && called == 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.GreaterOrEqual(t, compiled, float64(2))
		require.Positive(t, entries)
		require.Zero(t, called)
	})

	t.Run("rebuilds a closure frame with its upvals after a declined bridge", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const calls = 64
		fb := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32(), types.TypeI1()}, Returns: []types.Type{types.TypeI32()}}).Captures(types.TypeI32())
		loop, done, slow := fb.Label(), fb.Label(), fb.Label()
		fb.Bind(loop).Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_EQZ)).BrIf(done)
		fb.Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.UPVAL_SET, 0))
		fb.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SUB), instr.MustNew(instr.LOCAL_SET, 0)).Br(loop)
		fb.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 1)).BrIf(slow)
		fb.Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.RETURN))
		fb.Bind(slow).Append(instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW, 0), instr.MustNew(instr.ARRAY_LEN))
		fb.Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN))
		gb := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeAny(), types.TypeI32(), types.TypeI32())
		loop, done = gb.Label(), gb.Label()
		gb.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.LOCAL_SET, 0))
		gb.Bind(loop).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, calls), instr.MustNew(instr.I32_GE_S)).BrIf(done)
		gb.Append(instr.MustNew(instr.I32_CONST, 100), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, calls-1), instr.MustNew(instr.I32_EQ))
		gb.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CALL), instr.MustNew(instr.LOCAL_SET, 2))
		gb.Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1)).Br(loop)
		gb.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.RETURN))
		b := instr.NewBuilder()
		b.Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fb.MustBuild(), gb.MustBuild()), program.WithTypes(types.NewArrayType(types.TypeI32())))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		baseline := prof.Label{Key: "tier", Value: "baseline"}
		call := prof.Label{Key: "kind", Value: "call"}
		bridge := prof.Label{Key: "kind", Value: "bridge"}

		var got types.Value
		var runErr, popErr error
		var compiled, bridged, called float64
		poll(t, func() bool {
			vm.Flush()
			declined, exited := metric("vm_jit_exits_total", bridge), metric("vm_jit_exits_total", call)
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if got, popErr = vm.Pop(); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()

			compiled = metric("vm_jit_compiles_total", baseline, prof.Label{Key: "outcome", Value: "ok"})
			bridged, called = metric("vm_jit_exits_total", bridge)-declined, metric("vm_jit_exits_total", call)-exited
			return compiled >= 2 && bridged > 0 && called == 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.GreaterOrEqual(t, compiled, float64(2))
		require.Positive(t, bridged)
		require.Zero(t, called)
	})

	t.Run("bridges CLOSURE_NEW", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n = 200_000
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeString()}}).
			Captures(types.TypeString()).
			Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.RETURN)).
			MustBuild()
		b := instr.NewBuilder()
		loop, done, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, n).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 1).Emit(instr.CLOSURE_NEW).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(inner)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32(), types.TypeAny(), types.TypeI32()), program.WithConstants(types.String("held"), fn))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var bridges, deopts float64
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if value, count, popErr = popString(vm); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})

			return bridges > 1000
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Greater(t, bridges, float64(1000))
		require.Zero(t, deopts)
	})

	t.Run("faults on a directly called function's missing upval", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const calls = 200
		fb := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32(), types.TypeI1()}, Returns: []types.Type{types.TypeI32()}}).Captures(types.TypeI32())
		loop, done, read := fb.Label(), fb.Label(), fb.Label()
		fb.Bind(loop).Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_EQZ)).BrIf(done)
		fb.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SUB), instr.MustNew(instr.LOCAL_SET, 0)).Br(loop)
		fb.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 1)).BrIf(read)
		fb.Append(instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.RETURN))
		fb.Bind(read).Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.RETURN))
		b := instr.NewBuilder()
		loop, done = b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, calls).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1000).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, calls-1).Emit(instr.I32_EQ)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(fb.MustBuild()))
		want := runProgramErr(t, prog, interp.WithThreshold(-1))
		require.Error(t, want)

		got := runProgramErr(t, prog, interp.WithThreshold(1))
		require.True(t, errorsEqual(got, want), "got %v, want %v", got, want)
	})

	t.Run("zeroes each unwritten local of a native function", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 20_000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		for range zeroTypes {
			b.Emit(instr.DROP)
		}
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithConstants(zeroFunction()))

		var got []types.Value
		var entries float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got = got[:0]
			for range zeroValues {
				var v types.Value
				if v, popErr = vm.Pop(); popErr != nil {
					return true
				}
				got = append(got, v)
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, zeroValues, got)
	})

	t.Run("zeroes each unwritten module local and global under OSR", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 200_000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		for j := range zeroTypes {
			b.Emit(instr.LOCAL_GET, uint64(j+1))
		}
		for j := range zeroTypes {
			b.Emit(instr.GLOBAL_GET, uint64(j))
		}
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(append([]types.Type{types.TypeI32()}, zeroTypes...)...), program.WithGlobals(zeroTypes...))
		want := slices.Concat(zeroValues, zeroValues)

		var got []types.Value
		var entries float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got = got[:0]
			for range want {
				var v types.Value
				if v, popErr = vm.Pop(); popErr != nil {
					return true
				}
				got = append(got, v)
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("computes every primitive operator bit for bit", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		w32 := func(v int32) uint64 { return uint64(uint32(v)) }
		w64 := func(v int64) uint64 { return uint64(v) }
		f32 := func(v float32) uint64 { return uint64(math.Float32bits(v)) }
		f64 := math.Float64bits
		nan32, neg32 := float32(math.NaN()), float32(math.Copysign(0, -1))
		i32, i64, t32, t64 := types.TypeI32(), types.TypeI64(), types.TypeF32(), types.TypeF64()

		for _, c := range []struct {
			code   instr.Opcode
			params []types.Type
			args   [][]uint64
		}{
			{instr.I32_CLZ, []types.Type{i32}, [][]uint64{{w32(0)}, {w32(-1)}, {w32(math.MinInt32)}}},
			{instr.I32_CTZ, []types.Type{i32}, [][]uint64{{w32(0)}, {w32(-1)}, {w32(math.MinInt32)}}},
			{instr.I32_POPCNT, []types.Type{i32}, [][]uint64{{w32(0)}, {w32(-1)}, {w32(0x55555555)}}},
			{instr.I32_ROTL, []types.Type{i32, i32}, [][]uint64{{w32(0x12345678), w32(4)}, {w32(0x12345678), w32(-1)}, {w32(0x12345678), w32(37)}}},
			{instr.I32_ROTR, []types.Type{i32, i32}, [][]uint64{{w32(0x12345678), w32(4)}, {w32(0x12345678), w32(-1)}, {w32(0x12345678), w32(37)}}},
			{instr.I64_CLZ, []types.Type{i64}, [][]uint64{{w64(0)}, {w64(-1)}, {w64(1 << 47)}}},
			{instr.I64_CTZ, []types.Type{i64}, [][]uint64{{w64(0)}, {w64(-1)}, {w64(1 << 47)}}},
			{instr.I64_POPCNT, []types.Type{i64}, [][]uint64{{w64(0)}, {w64(-1)}, {w64(0x555555555555)}}},
			{instr.I64_ROTL, []types.Type{i64, i64}, [][]uint64{{w64(0x123456789ABC), w64(4)}, {w64(0x123456789ABC), w64(-1)}, {w64(0x123456789ABC), w64(69)}}},
			{instr.I64_ROTR, []types.Type{i64, i64}, [][]uint64{{w64(0x123456789ABC), w64(4)}, {w64(0x123456789ABC), w64(-1)}, {w64(0x123456789ABC), w64(69)}}},
			{instr.F32_COPYSIGN, []types.Type{t32, t32}, [][]uint64{{f32(1.5), f32(-1)}, {f32(nan32), f32(-1)}, {f32(0), f32(neg32)}}},
			{instr.F64_COPYSIGN, []types.Type{t64, t64}, [][]uint64{{f64(1.5), f64(-1)}, {f64(math.NaN()), f64(-1)}, {f64(0), f64(math.Copysign(0, -1))}}},
			{instr.F32_REM, []types.Type{t32, t32}, [][]uint64{{f32(5.5), f32(2)}, {f32(-5.5), f32(2)}}},
			{instr.F32_MOD, []types.Type{t32, t32}, [][]uint64{{f32(5.5), f32(2)}, {f32(-5.5), f32(2)}}},
			{instr.F64_REM, []types.Type{t64, t64}, [][]uint64{{f64(5.5), f64(2)}, {f64(-5.5), f64(2)}}},
			{instr.F64_MOD, []types.Type{t64, t64}, [][]uint64{{f64(5.5), f64(2)}, {f64(-5.5), f64(2)}}},
		} {
			fb := types.NewFunctionBuilder(&types.FunctionType{Params: c.params, Returns: c.params[:1]})
			for i := range c.params {
				fb.Append(instr.MustNew(instr.LOCAL_GET, uint64(i)))
			}
			fn := fb.Append(instr.MustNew(c.code), instr.MustNew(instr.RETURN)).MustBuild()

			for _, args := range c.args {

				b := instr.NewBuilder()
				loop, done := b.Label(), b.Label()
				call := func() {
					for i, arg := range args {
						b.Emit(map[types.Kind]instr.Opcode{types.KindI32: instr.I32_CONST, types.KindI64: instr.I64_CONST, types.KindF32: instr.F32_CONST, types.KindF64: instr.F64_CONST}[c.params[i].Kind()], arg)
					}
					b.Emit(instr.CONST_GET, 0).Emit(instr.CALL)
				}
				b.Bind(loop).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1000).Emit(instr.I32_GE_S).BrIf(done)
				call()
				b.Emit(instr.DROP).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
				b.Bind(done)
				call()
				code, err := b.Assemble()
				require.NoError(t, err)
				prog := program.New(code, program.WithLocals(i32), program.WithConstants(fn))

				threaded := interp.MustNew(prog, interp.WithThreshold(-1))
				require.NoError(t, threaded.Run(context.Background()))
				want, err := threaded.PopBoxed()
				require.NoError(t, err)
				require.NoError(t, threaded.Close())

				var got types.Boxed
				var entries, deopts float64
				var runErr, popErr error
				profiler := prof.New()
				vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
				defer vm.Close()
				poll(t, func() bool {
					if runErr = vm.Run(context.Background()); runErr != nil {
						return true
					}
					if got, popErr = vm.PopBoxed(); popErr != nil {
						return true
					}
					vm.Reset()
					vm.Flush()
					entries = nativeEntries(profiler)
					deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
					return entries > 0
				})
				require.NoError(t, runErr, "%s %x", c.code, args)
				require.NoError(t, popErr, "%s %x", c.code, args)
				require.Equal(t, want, got, "%s %x", c.code, args)
				require.Positive(t, entries, "%s %x", c.code, args)
				require.Zero(t, deopts, "%s %x", c.code, args)
			}
		}
	})

	t.Run("serves STRUCT_NEW_DEFAULT from the tcache and leaves threaded's heap", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n = 2000
		prog := links(t, n, -1, false)

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		wantLen, wantCounts := heapOf(threaded)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		warm(t, vm, profiler)

		vm.Flush()
		before, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		entered := nativeEntries(profiler)
		require.NoError(t, vm.Run(context.Background()))
		vm.Flush()
		after, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})

		got, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Greater(t, nativeEntries(profiler), entered)
		require.Less(t, after-before, float64(n)/32) // almost every allocation stays native
		gotLen, gotCounts := heapOf(vm)
		require.Equal(t, wantLen, gotLen)
		require.Equal(t, wantCounts, gotCounts)
	})

	t.Run("returns the tcache when a host value's Close panics in native code", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		record := node()
		b := instr.NewBuilder()
		loop, done, keep := b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1000).Emit(instr.I32_NE).BrIf(keep)
		b.Emit(instr.REF_NULL).Emit(instr.GLOBAL_SET, 0)
		b.Bind(keep)
		b.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32(), record, record),
			program.WithGlobals(types.TypeAny()), program.WithTypes(record))

		// hold stores a value whose Close panics in global 0, which iteration
		// 1000 releases.
		hold := func(vm *interp.Interpreter) {
			addr, err := vm.Alloc(&panicCloser{})
			require.NoError(t, err)
			require.NoError(t, vm.SetGlobal(0, types.BoxRef(addr)))
		}

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		hold(threaded)
		require.Error(t, threaded.Run(context.Background()))

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		warm(t, vm, profiler)
		vm.Flush()
		entered := nativeEntries(profiler)
		hold(vm)
		require.Error(t, vm.Run(context.Background()))
		vm.Flush()
		require.Greater(t, nativeEntries(profiler), entered)
		require.Equal(t, liveOf(threaded), liveOf(vm))
	})

	t.Run("builds no batch for a native call that allocates once", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		record := node()
		f := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
			Append(instr.MustNew(instr.STRUCT_NEW_DEFAULT, 0), instr.MustNew(instr.DROP), instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.RETURN)).
			MustBuild()
		b := instr.NewBuilder()
		loop, done, body := b.Label(), b.Label(), b.Label()
		// The module never compiles, so f is entered from threaded code, at depth 0, every call.
		b.Emit(instr.I32_CONST, 1).BrIf(body)
		b.Emit(instr.REF_NULL).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.DROP)
		b.Bind(body).Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 200).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithTypes(record), program.WithConstants(f))

		// A heap this small collects whenever a batch outgrows it.
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithHeap(8))
		defer vm.Close()
		warm(t, vm, profiler)

		// Its ledger retires f's code once the exits outweigh the work, so the
		// whole warm-up is the window native code ran in.
		cycles, _ := profiler.Metric("vm_gc_cycles_total")
		require.Positive(t, nativeEntries(profiler))
		require.Zero(t, cycles)
	})

	t.Run("returns the tcache to a callee that allocates at the heap limit", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		record := node()
		second := types.NewStructType(types.NewStructField(types.TypeI32()))
		g := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}})
		refuse(g)
		g.Append(instr.MustNew(instr.STRUCT_NEW_DEFAULT, 1), instr.MustNew(instr.DROP), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.RETURN))

		// Each pass keeps a node, counts to 64 so its work pays for the call,
		// and calls g, which never compiles: native code reaches it through
		// ExitCall while its tcache holds the nodes it built ahead.
		b := instr.NewBuilder()
		loop, done, count, counted := b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(count).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 64).Emit(instr.I32_GE_S).BrIf(counted)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3).Br(count)
		b.Bind(counted).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32(), record, record, types.TypeI32()),
			program.WithTypes(record, second), program.WithConstants(g.MustBuild()))

		limit := fits(t, prog)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithHeapLimit(limit))
		defer vm.Close()
		warm(t, vm, profiler)

		vm.Flush()
		entered := nativeEntries(profiler)
		require.NoError(t, vm.Run(context.Background()))
		vm.Flush()
		require.Greater(t, nativeEntries(profiler), entered)
		got, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(2000), got)
	})

	t.Run("returns the tcache when a trap leaves native code mid-loop", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := links(t, 2000, 1500, false)

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		wantErr := threaded.Run(context.Background())
		require.Error(t, wantErr)
		wantLen, wantCounts := heapOf(threaded)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		warm(t, vm, profiler)

		vm.Flush()
		entered := nativeEntries(profiler)
		require.EqualError(t, vm.Run(context.Background()), wantErr.Error())
		vm.Flush()
		require.Greater(t, nativeEntries(profiler), entered)
		gotLen, gotCounts := heapOf(vm)
		require.Equal(t, wantLen, gotLen)
		require.Equal(t, wantCounts, gotCounts)
	})
	t.Run("serves ARRAY_NEW_DEFAULT from the tcache and leaves threaded's heap", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n = 2000
		prog := chains(t, n, 0, 1, 1)

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		wantLen, wantCounts := heapOf(threaded)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		warm(t, vm, profiler)

		vm.Flush()
		before, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		entered := nativeEntries(profiler)
		require.NoError(t, vm.Run(context.Background()))
		vm.Flush()
		after, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})

		got, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Greater(t, nativeEntries(profiler), entered)
		require.Less(t, after-before, float64(n)/32) // almost every allocation stays native
		gotLen, gotCounts := heapOf(vm)
		require.Equal(t, wantLen, gotLen)
		require.Equal(t, wantCounts, gotCounts)
	})

	t.Run("refills ARRAY_NEW_DEFAULT when the requested length changes mid-loop", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := chains(t, 2000, 1000, 1, 3)

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(3), want)
		wantLive := liveOf(threaded)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		warm(t, vm, profiler)

		vm.Flush()
		before, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		entered := nativeEntries(profiler)
		require.NoError(t, vm.Run(context.Background()))
		vm.Flush()
		after, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})

		got, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Greater(t, nativeEntries(profiler), entered)
		require.Less(t, after-before, float64(64))
		// Bins of other lengths may leave free slots below a live one, so the heap
		// can be longer than threaded's; what it holds live is the same.
		require.Equal(t, wantLive, liveOf(vm))
	})

	t.Run("serves ARRAY_NEW_DEFAULT of lengths cycling through the size classes natively", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		const n = 2000
		array := types.NewArrayType(types.TypeAny())
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, n).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 5).Emit(instr.I32_REM_S).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithTypes(array))

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		wantLive := liveOf(threaded)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		warm(t, vm, profiler)

		vm.Flush()
		before, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		entered := nativeEntries(profiler)
		require.NoError(t, vm.Run(context.Background()))
		vm.Flush()
		after, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})

		got, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Greater(t, nativeEntries(profiler), entered)
		require.Less(t, after-before, float64(n)/8) // each length keeps its own bin
		// Bins of other lengths may leave free slots below a live one, so the heap
		// can be longer than threaded's; what it holds live is the same.
		require.Equal(t, wantLive, liveOf(vm))
	})

	t.Run("faults a native ARRAY_NEW_DEFAULT of a negative length as threaded does", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := chains(t, 2000, 1500, 1, -1)

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		wantErr := threaded.Run(context.Background())
		require.ErrorIs(t, wantErr, interp.ErrSegmentationFault)
		wantLen, wantCounts := heapOf(threaded)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		warm(t, vm, profiler)

		vm.Flush()
		entered := nativeEntries(profiler)
		require.EqualError(t, vm.Run(context.Background()), wantErr.Error())
		vm.Flush()
		require.Greater(t, nativeEntries(profiler), entered)
		gotLen, gotCounts := heapOf(vm)
		require.Equal(t, wantLen, gotLen)
		require.Equal(t, wantCounts, gotCounts)
	})

	t.Run("stops caching ARRAY_NEW_DEFAULT of a type whose length alternates", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		// Lengths alternate between low and low+1, both at or past the last size
		// class, which holds one length.
		for _, low := range []uint64{7, 8} {
			array := types.NewArrayType(types.TypeAny())
			b := instr.NewBuilder()
			loop, done := b.Label(), b.Label()
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2000).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).Emit(instr.I32_CONST, low).Emit(instr.I32_ADD)
			b.Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.DROP)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
			b.Br(loop)
			b.Bind(done).Emit(instr.LOCAL_GET, 0)
			code, err := b.Assemble()
			require.NoError(t, err)
			prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithTypes(array))

			threaded := interp.MustNew(prog, interp.WithThreshold(-1), interp.WithHeap(64))
			defer threaded.Close()
			require.NoError(t, threaded.Run(context.Background()))
			want, err := threaded.Pop()
			require.NoError(t, err)

			profiler := prof.New()
			vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithHeap(64))
			defer vm.Close()
			// The alternating lengths make every allocation a callout, so the ledger
			// retires the loop's code within the warm-up: it is the window native
			// code ran in.
			warm(t, vm, profiler)
			vm.Flush()
			cycles, _ := profiler.Metric("vm_gc_cycles_total")
			require.Positive(t, nativeEntries(profiler))
			require.Zero(t, cycles)

			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, liveOf(threaded), liveOf(vm))
		}
	})

	t.Run("reuses the arrays a native entry cached and did not take", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := chains(t, 2000, 0, 1, 1)
		run := func(vm *interp.Interpreter) float64 {
			return testing.AllocsPerRun(20, func() {
				require.NoError(t, vm.Run(context.Background()))
				vm.Reset()
			})
		}

		// Threaded code reuses every array at Reset; what it still allocates is
		// the collector's closure and the free list regrowing, and the race
		// detector adds to both.
		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		run(threaded)
		want := run(threaded)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		warm(t, vm, profiler)
		run(vm)

		vm.Flush()
		entered := nativeEntries(profiler)
		// AllocsPerRun counts every goroutine, so a background compile only
		// adds: the least of three runs is the entry's own cost.
		got := min(run(vm), run(vm), run(vm))
		vm.Flush()
		require.Greater(t, nativeEntries(profiler), entered)
		// Native code and its profiler allocate about 35 more than threaded code
		// does; a cached array rebuilt in Go on every entry adds over 140.
		require.LessOrEqual(t, got, want+96)
	})

	t.Run("builds nothing ahead for a typed array the entry allocates twice", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		b := instr.NewBuilder()
		loop, done, skip := b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 4000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2).Emit(instr.I32_GE_S).BrIf(skip)
		b.Emit(instr.I32_CONST, 128).Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.DROP)
		b.Bind(skip).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithTypes(types.NewArrayType(types.TypeI32())))
		run := func(vm *interp.Interpreter) float64 {
			return testing.AllocsPerRun(20, func() {
				require.NoError(t, vm.Run(context.Background()))
				vm.Reset()
			})
		}

		threaded := interp.MustNew(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		run(threaded)
		want := run(threaded)

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		warm(t, vm, profiler)
		for range 4 {
			run(vm)
		}

		vm.Flush()
		entered := nativeEntries(profiler)
		got := run(vm)
		vm.Flush()
		require.Greater(t, nativeEntries(profiler), entered)
		// A typed array is never reused, so a batch built ahead and returned
		// unused costs a Go allocation each; the entry learned it needs only the
		// two it served.
		require.LessOrEqual(t, got, want+8)
	})

	t.Run("fills no more than the heap holds before a collection", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 500).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32()), program.WithTypes(types.NewArrayType(types.TypeAny())))

		threadedProfiler := prof.New()
		threaded := interp.MustNew(prog, interp.WithThreshold(-1), interp.WithProfiler(threadedProfiler))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		threaded.Flush()
		want, _ := threadedProfiler.Metric("vm_gc_cycles_total")

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		warm(t, vm, profiler)
		for range 4 {
			require.NoError(t, vm.Run(context.Background()))
			vm.Reset()
		}

		vm.Flush()
		entered := nativeEntries(profiler)
		before, _ := profiler.Metric("vm_gc_cycles_total")
		const runs = 4
		for range runs {
			require.NoError(t, vm.Run(context.Background()))
			vm.Reset()
		}
		vm.Flush()
		after, _ := profiler.Metric("vm_gc_cycles_total")
		require.Greater(t, nativeEntries(profiler), entered)
		require.LessOrEqual(t, after-before, runs*want)
	})

	t.Run("fills a bin with as many arrays as the last entry used", func(t *testing.T) {
		testenv.MustHaveJIT(t)
		prog := chains(t, 128, 0, 1, 1)
		limit := fits(t, prog)

		// At the smallest heap threaded code runs prog in, a fill that builds
		// more than the loop takes reaches the limit and collects once more than
		// threaded code does; doubling from 16 overshoots, a fill of what the
		// last entry used does not.
		threadedProfiler := prof.New()
		threaded := interp.MustNew(prog, interp.WithThreshold(-1), interp.WithProfiler(threadedProfiler), interp.WithHeapLimit(limit))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		threaded.Flush()
		want, _ := threadedProfiler.Metric("vm_gc_cycles_total")

		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithHeapLimit(limit))
		defer vm.Close()
		warm(t, vm, profiler)
		for range 4 {
			require.NoError(t, vm.Run(context.Background()))
			vm.Reset()
		}

		vm.Flush()
		entered := nativeEntries(profiler)
		before, _ := profiler.Metric("vm_gc_cycles_total")
		const runs = 4
		for range runs {
			require.NoError(t, vm.Run(context.Background()))
			vm.Reset()
		}
		vm.Flush()
		after, _ := profiler.Metric("vm_gc_cycles_total")
		require.Greater(t, nativeEntries(profiler), entered)
		require.Equal(t, runs*want, after-before)
	})

	t.Run("runs map operations natively for every integer key kind", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		for _, tt := range []struct {
			name  string
			key   types.Type
			shift uint64
			space uint64
			wide  bool
		}{
			{"i1", types.TypeI1(), 0, 2, false},
			{"i8 with negative keys", types.TypeI8(), 30, 61, false},
			{"i32", types.TypeI32(), 0, 61, false},
			{"i64", types.TypeI64(), 0, 61, false},
			{"i64 beyond the inline range", types.TypeI64(), 0, 61, true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				typ := types.NewMapType(tt.key, types.TypeI32())
				prog := mapProgram(t, typ, 128, 3000, nil, nil, mapBody(mapKey{tt.key, tt.shift, tt.wide}, tt.space, identity, identity))
				exits := compareNative(t, prog)
				require.Zero(t, exits["bridge"], "the map operations stay native")
				require.Zero(t, exits["deopt"])
			})
		}
	})

	t.Run("runs map operations natively for every element kind", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		for _, tt := range []struct {
			name    string
			elem    types.Type
			in, out func(b *instr.Builder)
		}{
			{"f32", types.TypeF32(), func(b *instr.Builder) { b.Emit(instr.I32_TO_F32_S) }, func(b *instr.Builder) { b.Emit(instr.F32_TO_I32_S) }},
			{"f64", types.TypeF64(), func(b *instr.Builder) { b.Emit(instr.I32_TO_F64_S) }, func(b *instr.Builder) { b.Emit(instr.F64_TO_I32_S) }},
		} {
			t.Run(tt.name, func(t *testing.T) {
				typ := types.NewMapType(types.TypeI32(), tt.elem)
				prog := mapProgram(t, typ, 128, 3000, nil, nil, mapBody(mapKey{types.TypeI32(), 0, false}, 61, tt.in, tt.out))
				exits := compareNative(t, prog)
				require.Zero(t, exits["bridge"], "the map operations stay native")
			})
		}
	})

	t.Run("keeps i64 elements, boxed beyond the inline range, equal to threaded code", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		typ := types.NewMapType(types.TypeI32(), types.TypeI64())
		in := func(b *instr.Builder) { b.Emit(instr.I32_TO_I64_S).Emit(instr.I64_CONST, 1<<50+5).Emit(instr.I64_MUL) }
		out := func(b *instr.Builder) { b.Emit(instr.I64_CONST, 1<<50+5).Emit(instr.I64_DIV_S).Emit(instr.I64_TO_I32) }
		prog := mapProgram(t, typ, 128, 3000, nil, nil, mapBody(mapKey{types.TypeI32(), 0, false}, 61, in, out))
		compareNative(t, prog)
	})

	t.Run("counts references through ref-valued map operations as threaded code does", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		// Each iteration stores one of two constant strings under a key, so a
		// stored key replaces an element, deletes drop one, and every get and
		// lookup keeps the element it reads in local 3.
		for _, tt := range []struct {
			name string
			elem types.Type
		}{
			{"string", types.TypeString()},
			{"any", types.TypeAny()},
		} {
			t.Run(tt.name, func(t *testing.T) {
				typ := types.NewMapType(types.TypeI32(), tt.elem)
				key := mapKey{types.TypeI32(), 0, false}
				prog := mapProgram(t, typ, 128, 3000, []types.Value{types.String("a"), types.String("bb")}, []types.Type{tt.elem}, func(b *instr.Builder) {
					skip, hit, next := b.Label(), b.Label(), b.Label()
					b.Emit(instr.LOCAL_GET, 0)
					key.emit(b, 37, 61)
					b.Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 2).Emit(instr.I32_REM_S).Emit(instr.SELECT)
					b.Emit(instr.MAP_SET)

					b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 3).Emit(instr.I32_REM_S).BrIf(skip)
					b.Emit(instr.LOCAL_GET, 0)
					key.emit(b, 5, 61)
					b.Emit(instr.MAP_DELETE)
					b.Bind(skip)

					b.Emit(instr.LOCAL_GET, 0)
					key.emit(b, 11, 61)
					b.Emit(instr.MAP_GET).Emit(instr.LOCAL_SET, 3)

					b.Emit(instr.LOCAL_GET, 0)
					key.emit(b, 13, 61)
					b.Emit(instr.MAP_LOOKUP).BrIf(hit)
					b.Emit(instr.DROP).Br(next)
					b.Bind(hit)
					b.Emit(instr.LOCAL_SET, 3).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
					b.Bind(next)
				})
				exits := compareNative(t, prog)
				require.Zero(t, exits["bridge"], "the map operations stay native")
			})
		}
	})

	t.Run("falls back to the interpreter behind a guard when the map is not the declared representation", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		for _, tt := range []struct {
			name   string
			key    types.Type
			actual types.Value
		}{
			{"a generic map", types.TypeI32(), types.NewMap(types.NewMapType(types.TypeI32(), types.TypeI32()))},
			{"another key kind", types.TypeI32(), types.NewTypedMap[int8](types.NewMapType(types.TypeI8(), types.TypeI32()), 0)},
			{"a wider key kind", types.TypeI8(), types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				typ := types.NewMapType(tt.key, types.TypeI32())
				prog := mapProgramFrom(t, typ, func(b *instr.Builder) {
					b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 0)
				}, 3000, []types.Value{tt.actual}, nil, mapBody(mapKey{tt.key, 0, false}, 61, identity, identity))
				exits := compareNative(t, prog)
				require.Positive(t, exits["deopt"], "the guard refutes")
			})
		}
	})

	t.Run("keeps ownership of a map read through a local declared with another element kind", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		// One TypedMap[int32] of map[i32]string sits in two locals, the second
		// declared map[i32]i32 (the verifier admits both through CONST_GET). The
		// key kind alone would admit the second local's map.set, which would then
		// overwrite a string reference without releasing it.
		strMap := types.NewMapType(types.TypeI32(), types.TypeString())
		intMap := types.NewMapType(types.TypeI32(), types.TypeI32())
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 2000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 7).Emit(instr.I32_REM_S)
		b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 2).Emit(instr.I32_REM_S).Emit(instr.SELECT).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 7).Emit(instr.I32_REM_S).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.MAP_LEN)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code,
			program.WithLocals(strMap, types.TypeI32(), types.TypeI32(), intMap),
			program.WithTypes(strMap, intMap),
			program.WithConstants(types.NewTypedMap[int32](strMap, 0), types.String("a"), types.String("bb")))
		exits := compareNative(t, prog)
		require.Positive(t, exits["deopt"], "the guard refuses the map declared with another element kind")
	})

	t.Run("walks a map with map.iter while native code sets and deletes in it", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		typ := types.NewMapType(types.TypeI32(), types.TypeI32())
		mutate := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{typ, types.TypeI32()}, Returns: []types.Type{types.TypeI32()}})
		mutate.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.MAP_SET)
		mutate.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1000).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.I32_CONST, 1).Emit(instr.MAP_SET)
		mutate.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_DELETE)
		mutate.Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_LEN).Emit(instr.RETURN)

		// The module fills a small map, walks it, and calls mutate on each key
		// below 1000 it produces: the key just produced is deleted and a key above
		// 1000 inserted, which the walk may or may not produce. The sum of the
		// original keys does not depend on that.
		b := instr.NewBuilder()
		fill, fillDone, walk, walked, skip := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.MAP_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(fill)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 60).Emit(instr.I32_GE_S).BrIf(fillDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 3).Emit(instr.I32_MUL).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_GET, 3).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3).Br(fill)
		b.Bind(fillDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_ITER).Emit(instr.LOCAL_SET, 1)
		b.Bind(walk)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.CORO_DONE).BrIf(walked)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.CORO_VALUE).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 1000).Emit(instr.I32_GE_S).BrIf(skip)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 4).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Bind(skip)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 0).Emit(instr.RESUME).Emit(instr.DROP).Br(walk)
		b.Bind(walked)
		b.Emit(instr.LOCAL_GET, 2)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code,
			program.WithLocals(typ, types.NewIteratorType(types.TypeI32()), types.TypeI32(), types.TypeI32(), types.TypeI32()),
			program.WithTypes(typ),
			program.WithConstants(mutate.MustBuild()))

		want := int32(0)
		for n := range 60 {
			want += int32(n*3 + 1)
		}
		require.Equal(t, types.I32(want), runProgram(t, prog))

		// Which inserted keys the walk produces depends on slot order, so only
		// the sum is compared, not the reference counts that follow that order.
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		var got types.Value
		poll(t, func() bool {
			require.NoError(t, vm.Run(context.Background()))
			var err error
			got, err = vm.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(want), got)
			vm.Reset()
			vm.Flush()
			return nativeEntries(profiler) > 0
		})
	})

	t.Run("grows a map past the load factor mid-loop through the interpreter and keeps running natively", func(t *testing.T) {
		testenv.MustHaveJIT(t)

		typ := types.NewMapType(types.TypeI32(), types.TypeI32())
		prog := mapProgram(t, typ, 0, 600, nil, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 7).Emit(instr.I32_MUL).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_SET)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 7).Emit(instr.I32_MUL).Emit(instr.MAP_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		})
		compareNative(t, prog)

		// The first run that enters native code may enter after the map stopped
		// growing, so the bridges are counted over runs that start native.
		profiler := prof.New()
		vm := interp.MustNew(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		var bridges float64
		poll(t, func() bool {
			before, entries := exitsOf(profiler)["bridge"], nativeEntries(profiler)
			require.NoError(t, vm.Run(context.Background()))
			_, err := vm.Pop()
			require.NoError(t, err)
			vm.Reset()
			vm.Flush()
			bridges = exitsOf(profiler)["bridge"] - before
			return entries > 0 && bridges > 0
		})
		require.Less(t, bridges, float64(20), "only the inserts that rehash leave native code")
	})

}

func TestMustNew(t *testing.T) {
	t.Run("builds a verified program", func(t *testing.T) {
		i := interp.MustNew(program.New([]instr.Instruction{instr.MustNew(instr.NOP)}))
		defer i.Close()
		require.NotNil(t, i)
	})

	t.Run("panics on a malformed program", func(t *testing.T) {
		require.Panics(t, func() {
			interp.MustNew(program.New([]instr.Instruction{instr.MustNew(instr.I32_ADD)}))
		})
	})
}

func TestNew(t *testing.T) {
	t.Run("reports constants past the heap limit", func(t *testing.T) {
		pb := program.NewBuilder()
		pb.Const(types.String("constant"))
		i, err := interp.New(pb.MustBuild(), interp.WithHeapLimit(1))
		require.ErrorIs(t, err, interp.ErrHeapExhausted)
		require.Nil(t, i)
	})

	t.Run("runs a program", func(t *testing.T) {
		i := interp.MustNew(program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 5)}))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})

	t.Run("loads a cyclic constant literal", func(t *testing.T) {
		// Loading shares one copy per object, so a literal holding itself keeps the cycle.
		lit := &types.ArrayLiteral{Typ: types.NewArrayType(types.TypeAny())}
		lit.Elems = []types.Value{lit}
		i := interp.MustNew(program.New(nil, program.WithConstants(lit)))
		defer i.Close()

		c, err := i.Const(0)
		require.NoError(t, err)
		require.NotNil(t, c)
	})

	t.Run("interns duplicate string constants", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.STRING_EQ),
		}, program.WithConstants(types.String("same"), types.String("same")))
		i := interp.MustNew(prog)
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

	t.Run("rejects a malformed program", func(t *testing.T) {
		i, err := interp.New(program.New([]instr.Instruction{instr.MustNew(instr.I32_ADD)}))
		require.Nil(t, i)
		var ve *program.VerifyError
		require.ErrorAs(t, err, &ve)
		require.ErrorIs(t, err, program.ErrStackUnderflow)
	})

	t.Run("rejects a nil program", func(t *testing.T) {
		i, err := interp.New(nil)
		require.Nil(t, i)
		require.ErrorIs(t, err, program.ErrNil)
	})

	t.Run("ignores later mutation of the program code", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 5)})
		i := interp.MustNew(prog)
		defer i.Close()
		clear(prog.Code)

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})

	t.Run("ignores later mutation of a function constant", func(t *testing.T) {
		fn := &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32()}},
			Code: instr.Marshal([]instr.Instruction{instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.RETURN)}),
		}
		prog := program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(fn))
		i := interp.MustNew(prog)
		defer i.Close()
		clear(fn.Code)

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), v)
	})
}

func TestInterpreter_Run(t *testing.T) {
	t.Run("runs struct and map constants", func(t *testing.T) {
		structType := types.NewStructType(types.NewStructField(types.TypeI32()), types.NewStructField(types.TypeString()))
		first := &types.StructLiteral{Typ: structType, Fields: []types.Value{types.I32(10), types.String("ten")}}
		second := &types.StructLiteral{Typ: structType, Fields: []types.Value{types.I32(20), types.String("twenty")}}
		mapType := types.NewMapType(types.TypeI32(), structType)
		value := &types.MapLiteral{
			Typ:    mapType,
			Keys:   []types.Value{types.I32(1), types.I32(2)},
			Values: []types.Value{first, second},
		}

		b := program.NewBuilder()
		b.ConstGet(value).Emit(instr.I32_CONST, 2).Emit(instr.MAP_GET).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, program.Verify(prog))

		vm := interp.MustNew(prog, interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(20), got)
	})

	t.Run("runs nested array constants", func(t *testing.T) {
		typ := types.NewArrayType(types.TypeI32())
		value := &types.ArrayLiteral{
			Typ: types.NewArrayType(typ),
			Elems: []types.Value{
				&types.ArrayLiteral{Typ: typ, Elems: []types.Value{types.I32(1), types.I32(2)}},
				&types.ArrayLiteral{Typ: typ, Elems: []types.Value{types.I32(3), types.I32(4)}},
			},
		}

		b := program.NewBuilder()
		b.ConstGet(value).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_GET).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, program.Verify(prog))

		vm := interp.MustNew(prog, interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(3), got)
	})

	t.Run("runs a map constant with a repeated key and equal values", func(t *testing.T) {
		b := program.NewBuilder()
		b.ConstGet(&types.MapLiteral{
			Typ:    types.NewMapType(types.TypeI32(), types.TypeString()),
			Keys:   []types.Value{types.I32(1), types.I32(1)},
			Values: []types.Value{types.String("same"), types.String("same")},
		}).Emit(instr.I32_CONST, 1).Emit(instr.MAP_GET)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, program.Verify(prog))

		vm := interp.MustNew(prog, interp.WithThreshold(-1))
		defer vm.Close()
		for range 2 {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, types.String("same"), got)
			vm.Reset()
		}
	})

	t.Run("calls a function nested in an array constant", func(t *testing.T) {
		fnType := &types.FunctionType{Returns: []types.Type{types.TypeI32()}}
		fn := types.NewFunctionBuilder(fnType).
			Emit(instr.I32_CONST, 7).Emit(instr.RETURN).
			MustBuild()

		b := program.NewBuilder()
		b.ConstGet(&types.ArrayLiteral{
			Typ:   types.NewArrayType(fnType),
			Elems: []types.Value{fn},
		}).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, program.Verify(prog))

		vm := interp.MustNew(prog, interp.WithThreshold(-1))
		defer vm.Close()
		for range 2 {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, types.BoxI32(7), got)
			vm.Reset()
		}
	})

	t.Run("ignores mutation of the input after New", func(t *testing.T) {
		fnType := &types.FunctionType{Returns: []types.Type{types.TypeI32()}}
		nested := types.NewFunctionBuilder(fnType).
			Append(instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.RETURN)).
			MustBuild()
		raw := types.NewArray(types.NewArrayType(types.TypeI32()), types.BoxI32(1))
		lit := &types.ArrayLiteral{Typ: types.NewArrayType(fnType), Elems: []types.Value{nested}}

		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET), instr.MustNew(instr.CALL),
			instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET),
			instr.MustNew(instr.I32_ADD),
		}, program.WithConstants(lit, types.Value(raw)))
		vm := interp.MustNew(prog, interp.WithThreshold(-1))
		defer vm.Close()

		nested.Code[1] = byte(instr.NOP)
		raw.Elems[0] = types.BoxI32(100)
		lit.Elems[0] = types.I32(0)

		for range 2 {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, types.BoxI32(8), got)
			vm.Reset()
		}
	})

	t.Run("rejects an invalid function nested in a constant", func(t *testing.T) {
		bad := types.NewFunction(nil, nil, []instr.Instruction{instr.MustNew(instr.DROP), instr.MustNew(instr.RETURN)})
		prog := program.New([]instr.Instruction{instr.MustNew(instr.NOP)}, program.WithConstants(
			&types.ArrayLiteral{Typ: types.NewArrayType(types.TypeAny()), Elems: []types.Value{bad}}))

		_, err := interp.New(prog)
		var ve *program.VerifyError
		require.ErrorAs(t, err, &ve)
		require.Equal(t, 1, ve.Slot)
	})

	t.Run("shares one mutable constant between its positions", func(t *testing.T) {
		typ := types.NewArrayType(types.TypeI32())
		shared := types.NewArray(typ, types.BoxI32(1))
		typed := types.TypedArray[int32]{1}

		tests := []struct {
			name      string
			constants []types.Value
			write     []instr.Instruction
			read      []instr.Instruction
		}{
			{
				name:      "two constants",
				constants: []types.Value{shared, shared},
				write:     []instr.Instruction{instr.MustNew(instr.CONST_GET, 0)},
				read:      []instr.Instruction{instr.MustNew(instr.CONST_GET, 1)},
			},
			{
				name: "nested twice",
				constants: []types.Value{&types.ArrayLiteral{
					Typ:   types.NewArrayType(typ),
					Elems: []types.Value{shared, shared},
				}},
				write: []instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET)},
				read:  []instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_GET)},
			},
			{
				name:      "typed array",
				constants: []types.Value{typed, typed},
				write:     []instr.Instruction{instr.MustNew(instr.CONST_GET, 0)},
				read:      []instr.Instruction{instr.MustNew(instr.CONST_GET, 1)},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				code := slices.Concat(tt.write,
					[]instr.Instruction{instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 9), instr.MustNew(instr.ARRAY_SET)},
					tt.read,
					[]instr.Instruction{instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET)})
				prog := program.New(code, program.WithConstants(tt.constants...))
				require.NoError(t, program.Verify(prog))

				vm := interp.MustNew(prog, interp.WithThreshold(-1))
				defer vm.Close()
				for range 2 {
					require.NoError(t, vm.Run(context.Background()))
					got, err := vm.PopBoxed()
					require.NoError(t, err)
					require.Equal(t, types.BoxI32(9), got)
					vm.Reset()
				}
				require.Equal(t, types.BoxI32(1), shared.Elems[0])
				require.Equal(t, int32(1), typed[0])
			})
		}
	})

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
		// is not itself wrong but is worth surfacing: one of the two is likely
		// redundant.
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

	t.Run("map iterator resumed after clear retains no released key", func(t *testing.T) {
		typ := types.NewMapType(types.TypeAny(), types.TypeI32())
		code := []instr.Instruction{}
		for n := uint64(1); n <= 3; n++ {
			code = append(code, instr.MustNew(instr.I32_CONST, n), instr.MustNew(instr.REF_NEW), instr.MustNew(instr.I32_CONST, n))
		}
		code = append(code,
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.MAP_NEW, 0), instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.MAP_ITER), instr.MustNew(instr.LOCAL_SET, 1),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.MAP_CLEAR),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.RESUME),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.CORO_DONE),
		)
		vm := interp.MustNew(program.New(code, program.WithTypes(typ), program.WithLocals(typ, types.NewIteratorType(types.TypeAny()))))
		defer vm.Close()
		require.NoError(t, vm.Run(t.Context()))
		done, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I1(true), done)

		live := 0
		for addr := 1; addr < vm.HeapLen(); addr++ {
			if count, err := vm.RefCount(addr); err == nil && count > 0 {
				live++
			}
		}
		require.Equal(t, 2, live, "only the map and its iterator stay alive")
	})

	t.Run("runs every program standalone and fused", func(t *testing.T) {
		for _, mode := range []struct {
			name string
			opts []interp.Option
		}{
			{name: "standalone", opts: []interp.Option{interp.WithTick(1)}},
			{name: "fused", opts: []interp.Option{}},
		} {
			for _, tt := range runTests {
				name := runTestName(tt.program) + "/" + mode.name
				i := interp.MustNew(tt.program, mode.opts...)

				err := i.Run(context.Background())
				if tt.err != nil {
					require.ErrorIs(t, err, tt.err, name)
					require.NoError(t, i.Close(), name)
					continue
				}
				require.NoError(t, err, name)
				for _, want := range tt.values {
					got, err := i.Pop()
					require.NoError(t, err, name)
					require.Equal(t, want, got, name)
				}
				require.Equal(t, len(tt.program.Locals), i.Len(), name)
				require.NoError(t, i.Close(), name)
			}
		}
	})

	t.Run("releases frame slots on return", func(t *testing.T) {

		callee := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeAny()},
			Returns: []types.Type{types.TypeI32()},
		}).Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.RETURN)).MustBuild()

		b := program.NewBuilder()
		fn := b.Const(callee)
		loop := b.Label()
		done := b.Label()
		b.Locals(types.TypeI32())
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

		i := interp.MustNew(prog, interp.WithHeapLimit(heapRunway))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
	})

	t.Run("reuses the frame across a self tail call", func(t *testing.T) {

		fb := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32()},
			Returns: []types.Type{types.TypeI32()},
		}).Locals(types.TypeI32())
		done := fb.Label()
		fb.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_EQZ)).BrIf(done)
		fb.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SUB))
		fb.Append(instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.RETURN_CALL))
		fb.Bind(done)
		fb.Append(instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.RETURN))
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 2000),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CALL),
		}, program.WithConstants(fb.MustBuild()))
		require.NoError(t, program.Verify(prog))

		i := interp.MustNew(prog, interp.WithStack(64))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		value, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(7), value)
	})

	t.Run("releases every string.concat intermediate", func(t *testing.T) {

		b := program.NewBuilder()
		loop := b.Label()
		done := b.Label()
		b.Locals(types.TypeString(), types.TypeI32())
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

		i := interp.MustNew(prog, interp.WithHeapLimit(heapRunway))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		got, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(4*heapRunway), got)
	})

	t.Run("round-trips a ref set and get", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			typ         types.Type
			initial     types.Boxed
			replacement types.Boxed
			want        types.Value
		}{
			{name: "i1", typ: types.TypeI1(), initial: types.BoxI1(false), replacement: types.BoxI1(true), want: types.I1(true)},
			{name: "i8", typ: types.TypeI8(), initial: types.BoxI8(1), replacement: types.BoxI8(2), want: types.I8(2)},
			{name: "i32", typ: types.TypeI32(), initial: types.BoxI32(1), replacement: types.BoxI32(2), want: types.I32(2)},
			{name: "i64", typ: types.TypeI64(), initial: types.BoxI64(1), replacement: types.BoxI64(2), want: types.I64(2)},
			{name: "f32", typ: types.TypeF32(), initial: types.BoxF32(1), replacement: types.BoxF32(2), want: types.F32(2)},
			{name: "f64", typ: types.TypeF64(), initial: types.BoxF64(1), replacement: types.BoxF64(2), want: types.F64(2)},
		} {
			prog := program.New([]instr.Instruction{
				instr.MustNew(instr.GLOBAL_GET, 0),
				instr.MustNew(instr.REF_NEW),
				instr.MustNew(instr.DUP),
				instr.MustNew(instr.GLOBAL_GET, 1),
				instr.MustNew(instr.REF_SET),
				instr.MustNew(instr.REF_GET),
			}, program.WithGlobals(tt.typ, tt.typ))
			i := interp.MustNew(prog)
			require.NoError(t, i.SetGlobal(0, tt.initial), tt.name)
			require.NoError(t, i.SetGlobal(1, tt.replacement), tt.name)

			require.NoError(t, i.Run(context.Background()), tt.name)
			got, err := i.Pop()
			require.NoError(t, err, tt.name)
			require.Equal(t, tt.want, got, tt.name)
			require.NoError(t, i.Close(), tt.name)
		}
	})

	t.Run("matches exact and fused state", func(t *testing.T) {
		type state struct {
			code    types.ErrorCode
			ip      int
			fp      int
			sp      int
			stack   []types.Boxed
			globals []types.Boxed
			rc      map[int]int
		}

		huge := int64(1) << 50
		fn := types.NewFunctionBuilder(nil).Append(instr.MustNew(instr.RETURN)).MustBuild()
		coroutine := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny()}}).Append(
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.YIELD),
			instr.MustNew(instr.RETURN),
		).MustBuild()
		for _, tt := range []struct {
			name string
			prog *program.Program
			err  error
		}{
			{
				name: "integer arithmetic",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 20),
					instr.MustNew(instr.I32_CONST, 22),
					instr.MustNew(instr.I32_ADD),
				}),
			},
			{
				name: "local arithmetic store",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 5),
					instr.MustNew(instr.LOCAL_SET, 0),
					instr.MustNew(instr.LOCAL_GET, 0),
					instr.MustNew(instr.I32_CONST, 3),
					instr.MustNew(instr.I32_ADD),
					instr.MustNew(instr.LOCAL_SET, 0),
					instr.MustNew(instr.LOCAL_GET, 0),
				}, program.WithLocals(types.TypeI32())),
			},
			{
				name: "global mutation",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 7),
					instr.MustNew(instr.GLOBAL_SET, 0),
					instr.MustNew(instr.GLOBAL_GET, 0),
				}, program.WithGlobals(types.TypeI32())),
			},
			{
				name: "array access",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.CONST_GET, 0),
					instr.MustNew(instr.I32_CONST, 1),
					instr.MustNew(instr.ARRAY_GET),
				}, program.WithConstants(types.TypedArray[int32]{10, 20, 30})),
			},
			{
				name: "coroutine state",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.CONST_GET, 0),
					instr.MustNew(instr.CALL),
					instr.MustNew(instr.CORO_DONE),
				}, program.WithConstants(coroutine)),
			},
			{
				name: "promoted i64 eqz branch",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I64_CONST, i64operand(huge)),
					instr.MustNew(instr.I64_EQZ),
					instr.MustNew(instr.BR_IF, 0),
				}),
			},
			{
				name: "promoted i64 comparison branch",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I64_CONST, i64operand(huge)),
					instr.MustNew(instr.I64_CONST, i64operand(huge)),
					instr.MustNew(instr.I64_EQ),
					instr.MustNew(instr.BR_IF, 0),
				}),
			},
			{
				name: "promoted i64 local binary",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I64_CONST, i64operand(1)),
					instr.MustNew(instr.LOCAL_SET, 0),
					instr.MustNew(instr.I64_CONST, i64operand(huge)),
					instr.MustNew(instr.LOCAL_GET, 0),
					instr.MustNew(instr.I64_ADD),
					instr.MustNew(instr.DROP),
				}, program.WithLocals(types.TypeI64())),
			},
			{
				name: "local ref drop",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 7),
					instr.MustNew(instr.REF_NEW),
					instr.MustNew(instr.LOCAL_SET, 0),
					instr.MustNew(instr.LOCAL_GET, 0),
					instr.MustNew(instr.DROP),
				}, program.WithLocals(types.TypeAny())),
			},
			{
				name: "function constant drop",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.CONST_GET, 0),
					instr.MustNew(instr.DROP),
				}, program.WithConstants(fn)),
			},
			{
				name: "string constant drop",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.CONST_GET, 0),
					instr.MustNew(instr.DROP),
				}, program.WithConstants(types.String("value"))),
			},
			{
				name: "i32 divide by zero",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 90),
					instr.MustNew(instr.I32_CONST, 0),
					instr.MustNew(instr.I32_DIV_S),
				}),
				err: interp.ErrDivideByZero,
			},
			{
				name: "promoted i64 divide by zero",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I64_CONST, i64operand(huge)),
					instr.MustNew(instr.I64_CONST, 0),
					instr.MustNew(instr.I64_DIV_S),
				}),
				err: interp.ErrDivideByZero,
			},
			{
				name: "promoted i64 local divide by zero",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I64_CONST, i64operand(huge)),
					instr.MustNew(instr.LOCAL_SET, 0),
					instr.MustNew(instr.LOCAL_GET, 0),
					instr.MustNew(instr.I64_CONST, 0),
					instr.MustNew(instr.I64_DIV_S),
				}, program.WithLocals(types.TypeI64())),
				err: interp.ErrDivideByZero,
			},
			{
				name: "module completion adopts a borrowed constant reference",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.CONST_GET, 0),
				}, program.WithConstants(types.TypedArray[int32]{1, 2, 3})),
			},
			{
				name: "owned reference dropped before module completion",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.REF_NULL),
					instr.MustNew(instr.DROP),
					instr.MustNew(instr.CONST_GET, 0),
				}, program.WithConstants(types.TypedArray[int32]{1, 2, 3})),
			},
		} {
			states := make([]state, 0, 2)
			for _, opts := range [][]interp.Option{
				{interp.WithTick(1)},
				{},
			} {
				i := interp.MustNew(tt.prog, opts...)
				err := i.Run(context.Background())
				if tt.err == nil {
					require.NoError(t, err, tt.name)
				} else {
					require.ErrorIs(t, err, tt.err, tt.name)
				}

				got := state{
					code: interp.ErrorCode(err),
					ip:   i.IP(),
					fp:   i.FP(),
					sp:   i.Len(),
					rc:   make(map[int]int),
				}
				for idx := 0; idx < got.sp; idx++ {
					v, peekErr := i.Peek(got.sp - 1 - idx)
					require.NoError(t, peekErr, tt.name)
					got.stack = append(got.stack, v)
				}
				for idx := range tt.prog.Globals {
					v, globalErr := i.Global(idx)
					require.NoError(t, globalErr, tt.name)
					got.globals = append(got.globals, v)
				}
				for addr := 1; addr < i.HeapLen(); addr++ {
					count, rcErr := i.RefCount(addr)
					if rcErr != nil || count == 0 {
						continue
					}
					got.rc[addr] = count
				}
				states = append(states, got)
				require.NoError(t, i.Close(), tt.name)
			}
			require.Equal(t, states[0], states[1], tt.name)
		}
	})

	t.Run("resumes an entry-frame yield on the next call", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.YIELD),
			instr.MustNew(instr.I32_CONST, 2),
			instr.MustNew(instr.I32_ADD),
		})
		i := interp.MustNew(prog)
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(3), v)
	})

	t.Run("SELECT keeps the selected ref and releases the discarded one", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.SELECT),
		})
		i := interp.MustNew(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		top, err := i.Peek(0)
		require.NoError(t, err)
		require.Equal(t, 1, top.Ref())
		rc1, err := i.RefCount(1)
		require.NoError(t, err)
		require.Equal(t, 1, rc1)
		_, err = i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})

	t.Run("GLOBAL_TEE retains the ref stored into the global", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.GLOBAL_TEE, 0),
			instr.MustNew(instr.DROP),
		}, program.WithGlobals(types.TypeAny()))
		i := interp.MustNew(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		g, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, 1, g.Ref())
		rc, err := i.RefCount(1)
		require.NoError(t, err)
		require.Equal(t, 1, rc)
	})

	t.Run("LOCAL_TEE retains the ref stored into the local", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.LOCAL_TEE, 0),
			instr.MustNew(instr.DROP),
		}, program.WithLocals(types.TypeI32Array()))
		i := interp.MustNew(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		l, err := i.Local(0)
		require.NoError(t, err)
		require.Equal(t, 1, l.Ref())
		rc, err := i.RefCount(1)
		require.NoError(t, err)
		require.Equal(t, 1, rc)
	})

	t.Run("ref operations release the refs they consume", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			prog     *program.Program
			released []int
		}{
			{
				name: "REF_EQ",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.REF_NEW),
					instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.REF_NEW),
					instr.MustNew(instr.REF_EQ),
				}),
				released: []int{1, 2},
			},
			{
				name: "REF_NE",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.REF_NEW),
					instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.REF_NEW),
					instr.MustNew(instr.REF_NE),
				}),
				released: []int{1, 2},
			},
			{
				name: "REF_TEST",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.REF_NEW),
					instr.MustNew(instr.REF_TEST, 0),
				}, program.WithTypes(types.TypeI32())),
				released: []int{1},
			},
			{
				name: "REF_IS_NULL",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.REF_NEW),
					instr.MustNew(instr.REF_IS_NULL),
				}),
				released: []int{1},
			},
		} {
			i := interp.MustNew(tt.prog)

			require.NoError(t, i.Run(context.Background()), tt.name)
			for _, addr := range tt.released {
				_, err := i.RefCount(addr)
				require.ErrorIs(t, err, interp.ErrSegmentationFault, tt.name)
			}
			require.NoError(t, i.Close(), tt.name)
		}
	})

	t.Run("STRUCT_NEW_DEFAULT reports stack overflow before mutating sp", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.STRUCT_NEW_DEFAULT, 0),
		}, program.WithTypes(types.NewStructType(types.NewStructField(types.TypeI32()))))
		i := interp.MustNew(prog, interp.WithStack(1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrStackOverflow)
		require.Equal(t, 1, i.Len())
	})

	t.Run("faults on an invalid slot or size", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			prog *program.Program
			opts []interp.Option
		}{
			{
				name: "ARRAY_NEW_DEFAULT negative size",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, i32operand(-1)),
					instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
				}, program.WithTypes(types.TypeI32Array())),
			},
		} {
			i := interp.MustNew(tt.prog, tt.opts...)

			require.ErrorIs(t, i.Run(context.Background()), interp.ErrSegmentationFault, tt.name)
			require.NoError(t, i.Close(), tt.name)
		}
	})

	t.Run("reads kind-correct zeros from unseeded globals", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.GLOBAL_GET, 0),
			instr.MustNew(instr.I32_CONST, i32operand(2)),
			instr.MustNew(instr.I32_ADD),
			instr.MustNew(instr.GLOBAL_GET, 1),
			instr.MustNew(instr.GLOBAL_GET, 2),
		}, program.WithGlobals(types.TypeI32(), types.TypeF64(), types.TypeAny()))
		i := interp.MustNew(prog)
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

	t.Run("GLOBAL_GET declares and reads an I32 global when fused", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5),
			instr.MustNew(instr.GLOBAL_SET, 0),
			instr.MustNew(instr.GLOBAL_GET, 0),
			instr.MustNew(instr.I32_CONST, i32operand(2)),
			instr.MustNew(instr.I32_ADD),
		}, program.WithGlobals(types.TypeI32()))
		i := interp.MustNew(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 1, i.Len())
		v, err := i.Peek(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(7), v)
	})

	t.Run("ARRAY_FILL releases every overwritten ref element", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 1),
			instr.MustNew(instr.DUP), instr.MustNew(instr.I32_CONST, 0),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.DUP), instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.DUP), instr.MustNew(instr.I32_CONST, 2),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.ARRAY_SET),
			instr.MustNew(instr.DUP), instr.MustNew(instr.I32_CONST, 0),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_FILL),
		}, program.WithTypes(types.TypeI32Array(), types.NewArrayType(types.TypeI32Array())))
		i := interp.MustNew(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))

		_, err := i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
		_, err = i.RefCount(3)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
		_, err = i.RefCount(4)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
		rc5, err := i.RefCount(5)
		require.NoError(t, err)
		require.Equal(t, 3, rc5)
	})

	t.Run("calls a host function with a scalar signature on the generic path", func(t *testing.T) {
		hostFn := interp.NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeI32(), types.TypeI32()}, Returns: []types.Type{types.TypeI32()}},
			func(_ *interp.Interpreter, args []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(args[0].I32() * args[1].I32())}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 6), instr.MustNew(instr.I32_CONST, 7),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
		}, program.WithConstants(hostFn))
		i := interp.MustNew(prog, interp.WithTick(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), v)
	})

	t.Run("host call releases a ref param the callee does not return", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			opts []interp.Option
		}{
			{name: "fused"},
			{name: "generic", opts: []interp.Option{interp.WithTick(1)}},
		} {
			hostFn := interp.NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeAny()}, Returns: []types.Type{types.TypeI32()}},
				func(_ *interp.Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
					return []types.Boxed{types.BoxI32(1)}, nil
				})
			prog := program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 9), instr.MustNew(instr.REF_NEW),
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
			}, program.WithConstants(hostFn))
			i := interp.MustNew(prog, tt.opts...)

			require.NoError(t, i.Run(context.Background()), tt.name)
			_, err := i.RefCount(2)
			require.ErrorIs(t, err, interp.ErrSegmentationFault, tt.name)
			require.NoError(t, i.Close(), tt.name)
		}
	})

	t.Run("host call releases the consumed callable ref", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			opts []interp.Option
		}{
			{name: "fused"},
			{name: "generic", opts: []interp.Option{interp.WithTick(1)}},
		} {
			hostFn := interp.NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}},
				func(_ *interp.Interpreter, args []types.Boxed) ([]types.Boxed, error) {
					return []types.Boxed{args[0]}, nil
				})
			prog := program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 9),
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
			}, program.WithConstants(hostFn))
			i := interp.MustNew(prog, tt.opts...)

			require.NoError(t, i.Run(context.Background()), tt.name)
			rc, err := i.RefCount(1)
			require.NoError(t, err, tt.name)
			require.Equal(t, 1, rc, tt.name)
			require.NoError(t, i.Close(), tt.name)
		}
	})

	t.Run("generic host call can return the consumed callable ref", func(t *testing.T) {
		hostFn := interp.NewHostFunction(&types.FunctionType{Returns: []types.Type{types.TypeAny()}},
			func(i *interp.Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				v, peekErr := i.Peek(0)
				if peekErr != nil {
					return nil, peekErr
				}
				return []types.Boxed{v}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
		}, program.WithConstants(hostFn))
		i := interp.MustNew(prog, interp.WithTick(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		rc, err := i.RefCount(1)
		require.NoError(t, err)
		require.Equal(t, 2, rc)
	})

	t.Run("host call releases a promoted i64 param", func(t *testing.T) {
		huge := int64(1) << 50
		hostFn := interp.NewHostFunction(&types.FunctionType{Params: []types.Type{types.TypeI64()}, Returns: []types.Type{types.TypeI32()}},
			func(_ *interp.Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(1)}, nil
			})
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I64_CONST, i64operand(huge)),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
		}, program.WithConstants(hostFn))
		i := interp.MustNew(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})

	t.Run("UPVAL_GET retains a ref capture", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{}).
			Captures(types.TypeAny()).Append(
			instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.DROP),
			instr.MustNew(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CLOSURE_NEW),
			instr.MustNew(instr.CALL),
		}, program.WithConstants(fn))

		maxRC := 0
		i := interp.MustNew(prog, interp.WithTick(1), interp.WithHook(func(i *interp.Interpreter) error {
			if count, hookErr := i.RefCount(2); hookErr == nil && count > maxRC {
				maxRC = count
			}
			return nil
		}))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 2, maxRC)
	})

	t.Run("UPVAL_SET releases the overwritten ref capture", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
			Captures(types.TypeAny()).Append(
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.UPVAL_SET, 0),
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CLOSURE_NEW),
			instr.MustNew(instr.CALL),
		}, program.WithConstants(fn))
		i := interp.MustNew(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})

	t.Run("UPVAL_SET releases the overwritten promoted i64 capture", func(t *testing.T) {
		oldHuge := int64(1) << 50
		newHuge := int64(1) << 51
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI64()}}).
			Captures(types.TypeI64()).Append(
			instr.MustNew(instr.I64_CONST, i64operand(newHuge)),
			instr.MustNew(instr.UPVAL_SET, 0),
			instr.MustNew(instr.UPVAL_GET, 0),
			instr.MustNew(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I64_CONST, i64operand(oldHuge)),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CLOSURE_NEW),
			instr.MustNew(instr.CALL),
		}, program.WithConstants(fn))
		i := interp.MustNew(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.RefCount(2)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})

	t.Run("fused loaders compute the same result", func(t *testing.T) {
		upvalI32Const := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
			Captures(types.TypeI32()).Append(
			instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, i32operand(3)), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN),
		).MustBuild()
		upvalI64Const := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI64()}}).
			Captures(types.TypeI64()).Append(
			instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I64_CONST, i64operand(3)), instr.MustNew(instr.I64_ADD), instr.MustNew(instr.RETURN),
		).MustBuild()
		upvalF32Const := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeF32()}}).
			Captures(types.TypeF32()).Append(
			instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(3))), instr.MustNew(instr.F32_ADD), instr.MustNew(instr.RETURN),
		).MustBuild()
		upvalF64Const := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeF64()}}).
			Captures(types.TypeF64()).Append(
			instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.F64_CONST, math.Float64bits(3)), instr.MustNew(instr.F64_ADD), instr.MustNew(instr.RETURN),
		).MustBuild()
		upvalLocal := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
			Captures(types.TypeI32()).Locals(types.TypeI32()).Append(
			instr.MustNew(instr.I32_CONST, i32operand(3)), instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN),
		).MustBuild()
		globalUpval := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI64()}}).
			Captures(types.TypeI64()).Append(
			instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I64_ADD), instr.MustNew(instr.RETURN),
		).MustBuild()
		upvalPair := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
			Captures(types.TypeI32(), types.TypeI32()).Append(
			instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.UPVAL_GET, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN),
		).MustBuild()

		for _, tt := range []struct {
			name string
			prog *program.Program
			opts []interp.Option
			want types.Value
		}{
			{
				name: "local and local i64",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I64_CONST, i64operand(5)), instr.MustNew(instr.LOCAL_SET, 0),
					instr.MustNew(instr.I64_CONST, i64operand(3)), instr.MustNew(instr.LOCAL_SET, 1),
					instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I64_ADD),
				}, program.WithLocals(types.TypeI64(), types.TypeI64())),
				want: types.I64(8),
			},
			{
				name: "local and local f32",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(5))), instr.MustNew(instr.LOCAL_SET, 0),
					instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(3))), instr.MustNew(instr.LOCAL_SET, 1),
					instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.F32_ADD),
				}, program.WithLocals(types.TypeF32(), types.TypeF32())),
				want: types.F32(8),
			},
			{
				name: "local and local f64",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.F64_CONST, math.Float64bits(5)), instr.MustNew(instr.LOCAL_SET, 0),
					instr.MustNew(instr.F64_CONST, math.Float64bits(3)), instr.MustNew(instr.LOCAL_SET, 1),
					instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.F64_ADD),
				}, program.WithLocals(types.TypeF64(), types.TypeF64())),
				want: types.F64(8),
			},
			{
				name: "upval and i32 constant",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, i32operand(5)), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.CALL),
				}, program.WithConstants(upvalI32Const)),
				want: types.I32(8),
			},
			{
				name: "upval and i64 constant",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I64_CONST, i64operand(5)), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.CALL),
				}, program.WithConstants(upvalI64Const)),
				want: types.I64(8),
			},
			{
				name: "upval and f32 constant",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(5))), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.CALL),
				}, program.WithConstants(upvalF32Const)),
				want: types.F32(8),
			},
			{
				name: "upval and f64 constant",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.F64_CONST, math.Float64bits(5)), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.CALL),
				}, program.WithConstants(upvalF64Const)),
				want: types.F64(8),
			},
			{
				name: "upval and local",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, i32operand(5)), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.CALL),
				}, program.WithConstants(upvalLocal)),
				want: types.I32(8),
			},
			{
				name: "global and i32 constant",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, i32operand(5)), instr.MustNew(instr.GLOBAL_SET, 0),
					instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.I32_CONST, i32operand(3)), instr.MustNew(instr.I32_ADD),
				}, program.WithGlobals(types.TypeI32())),
				want: types.I32(8),
			},
			{
				name: "two globals",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, i32operand(5)), instr.MustNew(instr.GLOBAL_SET, 0),
					instr.MustNew(instr.I32_CONST, i32operand(3)), instr.MustNew(instr.GLOBAL_SET, 1),
					instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.GLOBAL_GET, 1), instr.MustNew(instr.I32_ADD),
				}, program.WithGlobals(types.TypeI32(), types.TypeI32())),
				want: types.I32(8),
			},
			{

				name: "two globals unfused",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, i32operand(5)), instr.MustNew(instr.GLOBAL_SET, 0),
					instr.MustNew(instr.I32_CONST, i32operand(3)), instr.MustNew(instr.GLOBAL_SET, 1),
					instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.GLOBAL_GET, 1), instr.MustNew(instr.I32_ADD),
				}, program.WithGlobals(types.TypeI32(), types.TypeI32())),
				opts: []interp.Option{interp.WithTick(1)},
				want: types.I32(8),
			},
			{
				name: "global and upval",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I64_CONST, i64operand(5)), instr.MustNew(instr.GLOBAL_SET, 0),
					instr.MustNew(instr.I64_CONST, i64operand(3)), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.CALL),
				}, program.WithConstants(globalUpval), program.WithGlobals(types.TypeI64())),
				want: types.I64(8),
			},
			{
				name: "two upvals",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, i32operand(5)), instr.MustNew(instr.I32_CONST, i32operand(3)),
					instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.CALL),
				}, program.WithConstants(upvalPair)),
				want: types.I32(8),
			},
		} {
			i := interp.MustNew(tt.prog, tt.opts...)

			require.NoError(t, i.Run(context.Background()), tt.name)
			got, err := i.Pop()
			require.NoError(t, err, tt.name)
			require.Equal(t, tt.want, got, tt.name)
			require.NoError(t, i.Close(), tt.name)
		}
	})

	t.Run("fused loaders borrow promoted i64 refs", func(t *testing.T) {

		huge := int64(1) << 62
		upval := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI64()}}).
			Captures(types.TypeI64()).Append(
			instr.MustNew(instr.I64_CONST, i64operand(1)), instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I64_ADD), instr.MustNew(instr.DROP),
			instr.MustNew(instr.I64_CONST, i64operand(1)), instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I64_ADD),
			instr.MustNew(instr.RETURN),
		).MustBuild()

		for _, tt := range []struct {
			name string
			prog *program.Program
			want types.Value
			refs int
		}{
			{
				name: "repeated local reads keep the local reference",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I64_CONST, i64operand(huge)), instr.MustNew(instr.LOCAL_SET, 0),
					instr.MustNew(instr.I64_CONST, i64operand(1)), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I64_ADD), instr.MustNew(instr.DROP),
					instr.MustNew(instr.I64_CONST, i64operand(1)), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I64_ADD),
				}, program.WithLocals(types.TypeI64())),
				want: types.I64(huge + 1),
				refs: 1,
			},
			{
				name: "mixed local reads keep the local reference",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I64_CONST, i64operand(huge)), instr.MustNew(instr.LOCAL_SET, 0),
					instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I64_CONST, i64operand(1)), instr.MustNew(instr.I64_ADD), instr.MustNew(instr.DROP),
					instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I64_ADD),
				}, program.WithLocals(types.TypeI64())),
				want: types.I64(2 * huge),
				refs: 1,
			},
			{
				name: "repeated global reads keep the global reference",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I64_CONST, i64operand(huge)), instr.MustNew(instr.GLOBAL_SET, 0),
					instr.MustNew(instr.I64_CONST, i64operand(1)), instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.I64_ADD), instr.MustNew(instr.DROP),
					instr.MustNew(instr.I64_CONST, i64operand(1)), instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.I64_ADD),
				}, program.WithGlobals(types.TypeI64())),
				want: types.I64(huge + 1),
				refs: 1,
			},
			{
				name: "repeated upval reads preserve the captured value",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I64_CONST, i64operand(huge)),
					instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.CALL),
				}, program.WithConstants(upval)),
				want: types.I64(huge + 1),
				refs: 1,
			},
			{
				name: "paired global reads preserve the global reference",
				prog: program.New([]instr.Instruction{
					instr.MustNew(instr.I64_CONST, i64operand(huge)), instr.MustNew(instr.GLOBAL_SET, 0),
					instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.I64_ADD), instr.MustNew(instr.DROP),
					instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.I64_ADD),
				}, program.WithGlobals(types.TypeI64())),
				want: types.I64(2 * huge),
				refs: 1,
			},
		} {
			i := interp.MustNew(tt.prog)

			require.NoError(t, i.Run(context.Background()), tt.name)
			got, err := i.Pop()
			require.NoError(t, err, tt.name)
			require.Equal(t, tt.want, got, tt.name)
			live := 0
			for addr := 1; addr < i.HeapLen(); addr++ {
				count, rcErr := i.RefCount(addr)
				if rcErr != nil {
					continue
				}
				live += count
			}
			require.Equal(t, tt.refs, live, tt.name)
			require.NoError(t, i.Close(), tt.name)
		}
	})

	t.Run("keeps coroutine captures live without duplicate collector edges", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeAny()},
			Returns: []types.Type{types.TypeI32()}}).Captures(types.TypeAny()).Append(
			instr.MustNew(instr.LOCAL_GET, 0),
			instr.MustNew(instr.UPVAL_GET, 0),
			instr.MustNew(instr.I32_CONST, 30),
			instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.YIELD),
			instr.MustNew(instr.DROP),
			instr.MustNew(instr.REF_GET),
			instr.MustNew(instr.SWAP),
			instr.MustNew(instr.REF_GET),
			instr.MustNew(instr.I32_ADD),
			instr.MustNew(instr.RETURN)).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 10),
			instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CLOSURE_NEW),
			instr.MustNew(instr.DUP),
			instr.MustNew(instr.I32_CONST, 20),
			instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.SWAP),
			instr.MustNew(instr.CALL),
			instr.MustNew(instr.DUP),
			instr.MustNew(instr.CORO_VALUE),
			instr.MustNew(instr.REF_GET),
			instr.MustNew(instr.RESUME),
			instr.MustNew(instr.CORO_VALUE)}, program.WithConstants(fn))
		collected := false
		var callableStoreErr error
		var coroutineStoreErr error
		var functionStoreErr error
		vm := interp.MustNew(prog,
			interp.WithHeap(8),
			interp.WithHeapLimit(8),
			interp.WithTick(1),
			interp.WithHook(func(vm *interp.Interpreter) error {
				if collected || vm.IP() != 19 {
					return nil
				}
				collected = true
				callable, err := vm.Peek(1)
				if err != nil {
					return err
				}
				callableStoreErr = vm.Store(callable.Ref(), types.I32(99))
				coroutine, err := vm.Peek(0)
				if err != nil {
					return err
				}
				coroutineStoreErr = vm.Store(coroutine.Ref(), types.I32(99))
				value, err := vm.Load(callable.Ref())
				if err != nil {
					return err
				}
				closure, ok := value.(*types.Closure)
				if !ok {
					return interp.ErrTypeMismatch
				}
				functionStoreErr = vm.Store(int(closure.Fn), types.I32(99))
				selfCycle(t, vm)
				pressure, err := vm.Alloc(types.I32(1))
				if err != nil {
					return err
				}
				return vm.Release(pressure)
			}))
		defer vm.Close()

		require.NoError(t, vm.Run(context.Background()))
		require.True(t, collected)
		require.ErrorIs(t, callableStoreErr, interp.ErrTypeMismatch)
		require.ErrorIs(t, coroutineStoreErr, interp.ErrTypeMismatch)
		require.ErrorIs(t, functionStoreErr, interp.ErrTypeMismatch)
		value, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(30), value)
	})

	t.Run("preserves coroutine identity through tail calls", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			tail types.Value
		}{
			{
				name: "function",
				tail: types.NewFunctionBuilder(nil).
					Returns(types.TypeI32()).
					Append(instr.MustNew(instr.I32_CONST, 42), instr.MustNew(instr.RETURN)).
					MustBuild(),
			},
			{
				name: "host function",
				tail: interp.NewHostFunction(
					&types.FunctionType{Returns: []types.Type{types.TypeI32()}},
					func(*interp.Interpreter, []types.Boxed) ([]types.Boxed, error) {
						return []types.Boxed{types.BoxI32(42)}, nil
					}),
			},
		} {
			fn := types.NewFunctionBuilder(nil).
				Returns(types.TypeAny()).
				Append(
					instr.MustNew(instr.I32_CONST, 1),
					instr.MustNew(instr.YIELD),
					instr.MustNew(instr.DROP),
					instr.MustNew(instr.CONST_GET, 1),
					instr.MustNew(instr.RETURN_CALL)).
				MustBuild()
			prog := program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 0),
				instr.MustNew(instr.CALL),
				instr.MustNew(instr.REF_NULL),
				instr.MustNew(instr.RESUME),
				instr.MustNew(instr.CORO_VALUE)}, program.WithConstants(fn, tt.tail))
			vm := interp.MustNew(prog)

			require.NoError(t, vm.Run(context.Background()), tt.name)
			value, err := vm.Pop()
			require.NoError(t, err, tt.name)
			require.Equal(t, types.I32(42), value, tt.name)
			require.NoError(t, vm.Close(), tt.name)
		}
	})

	t.Run("releases discarded coroutine completion values", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).
			Returns(types.TypeAny(), types.TypeAny()).
			Append(
				instr.MustNew(instr.I32_CONST, 0),
				instr.MustNew(instr.YIELD),
				instr.MustNew(instr.DROP),
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.REF_NEW),
				instr.MustNew(instr.I32_CONST, 2),
				instr.MustNew(instr.REF_NEW),
				instr.MustNew(instr.RETURN)).
			MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CALL),
			instr.MustNew(instr.REF_NULL),
			instr.MustNew(instr.RESUME),
			instr.MustNew(instr.DUP),
			instr.MustNew(instr.CORO_VALUE),
			instr.MustNew(instr.REF_GET),
			instr.MustNew(instr.SWAP),
			instr.MustNew(instr.DROP),
			instr.MustNew(instr.DROP),
			instr.MustNew(instr.I32_CONST, 3),
			instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.I32_CONST, 4),
			instr.MustNew(instr.REF_NEW),
			instr.MustNew(instr.I32_CONST, 5),
			instr.MustNew(instr.REF_NEW)}, program.WithConstants(fn))
		vm := interp.MustNew(prog,
			interp.WithHeap(5),
			interp.WithHeapLimit(5))
		defer vm.Close()

		require.NoError(t, vm.Run(context.Background()))
		require.Equal(t, 3, vm.Len())
	})
}

func TestInterpreter_Context(t *testing.T) {
	var got context.Context
	prog := program.New([]instr.Instruction{instr.MustNew(instr.NOP)})
	i := interp.MustNew(prog, interp.WithTick(1), interp.WithHook(func(i *interp.Interpreter) error {
		got = i.Context()
		return nil
	}))
	defer i.Close()

	ctx := context.Background()
	require.NoError(t, i.Run(ctx))
	require.Equal(t, ctx, got)
}

func TestInterpreter_FP(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.YIELD), instr.MustNew(instr.NOP)})
	i := interp.MustNew(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
	require.Equal(t, 1, i.FP())
}

func TestInterpreter_Opcode(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.YIELD), instr.MustNew(instr.NOP)})
	i := interp.MustNew(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
	op, err := i.Opcode()
	require.NoError(t, err)
	require.Equal(t, instr.NOP, op)
}

func TestInterpreter_Func(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.YIELD), instr.MustNew(instr.NOP)})
	i := interp.MustNew(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
	require.Equal(t, 0, i.Func())
}

func TestInterpreter_IP(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.YIELD), instr.MustNew(instr.NOP)})
	i := interp.MustNew(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
	require.Equal(t, 6, i.IP())
}

func TestInterpreter_Frame(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.YIELD), instr.MustNew(instr.NOP)})
	i := interp.MustNew(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
	fn, ip, err := i.Frame(0)
	require.NoError(t, err)
	require.Equal(t, 0, fn)
	require.Equal(t, 6, ip)
}

func TestInterpreter_Const(t *testing.T) {
	i := interp.MustNew(program.New(nil, program.WithConstants(types.I32(9))))
	defer i.Close()

	v, err := i.Const(0)
	require.NoError(t, err)
	require.Equal(t, types.BoxI32(9), v)
}

func TestInterpreter_Global(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 4), instr.MustNew(instr.GLOBAL_SET, 0)}, program.WithGlobals(types.TypeI32()))
	i := interp.MustNew(prog)
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	v, err := i.Global(0)
	require.NoError(t, err)
	require.Equal(t, types.BoxI32(4), v)
}

func TestInterpreter_SetGlobal(t *testing.T) {
	t.Run("sets scalar", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.GLOBAL_SET, 0)}, program.WithGlobals(types.TypeI32()))
		i := interp.MustNew(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		require.NoError(t, i.SetGlobal(0, types.BoxI32(8)))
		v, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(8), v)
	})

	t.Run("rejects incompatible type", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeI32()))
		i := interp.MustNew(prog)
		defer i.Close()

		require.ErrorIs(t, i.SetGlobal(0, types.BoxF32(1)), interp.ErrTypeMismatch)
	})

	t.Run("accepts dynamic ref value", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeAny()))
		i := interp.MustNew(prog)
		defer i.Close()

		require.NoError(t, i.SetGlobal(0, types.BoxI32(8)))
	})

	t.Run("accepts heap backed i64", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeI64()))
		i := interp.MustNew(prog)
		defer i.Close()

		require.NoError(t, i.Push(types.I64(1<<60)))
		val, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.KindRef, val.Kind())
		require.NoError(t, i.SetGlobal(0, val))
	})

	t.Run("rejects incompatible concrete ref type", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.NewArrayType(types.TypeI32())))
		i := interp.MustNew(prog)
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
		prog := program.New(nil, program.WithGlobals(types.TypeAny()))
		i := interp.MustNew(prog)
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
		prog := program.New(nil, program.WithGlobals(types.TypeAny()))
		i := interp.MustNew(prog)
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
		instr.MustNew(instr.I32_CONST, 6), instr.MustNew(instr.LOCAL_SET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.YIELD),
	}, program.WithLocals(types.TypeI32()))
	i := interp.MustNew(prog)
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
	v, err := i.Local(0)
	require.NoError(t, err)
	require.Equal(t, types.BoxI32(6), v)
}

func TestInterpreter_SetLocal(t *testing.T) {
	t.Run("sets scalar", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.YIELD)}, program.WithLocals(types.TypeI32()))
		i := interp.MustNew(prog)
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), interp.ErrYield)
		require.NoError(t, i.SetLocal(0, types.BoxI32(3)))
		v, err := i.Local(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(3), v)
	})

	t.Run("preserves same reference", func(t *testing.T) {
		prog := program.New(nil, program.WithLocals(types.TypeAny()))
		i := interp.MustNew(prog)
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
		prog := program.New(nil, program.WithLocals(types.TypeAny()))
		i := interp.MustNew(prog)
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
	i := interp.MustNew(program.New(nil))
	defer i.Close()

	addr, err := i.Alloc(types.I32(5))
	require.NoError(t, err)
	v, err := i.Load(addr)
	require.NoError(t, err)
	require.Equal(t, types.I32(5), v)
}

func TestInterpreter_Store(t *testing.T) {
	t.Run("replaces scalar", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)
		require.NoError(t, i.Store(addr, types.BoxI32(9)))
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.I32(9), v)
	})

	t.Run("finalizes replaced value", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
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
		i := interp.MustNew(program.New(nil))
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

	t.Run("ignores same-address ref", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		for _, ref := range []types.Value{types.BoxRef(addr), types.Ref(addr)} {
			require.NoError(t, i.Store(addr, ref))
			require.Equal(t, 0, value.closed)
			v, err := i.Load(addr)
			require.NoError(t, err)
			require.Same(t, value, v)
		}
	})

	t.Run("ignores identical value", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		loaded, err := i.Load(addr)
		require.NoError(t, err)
		require.NoError(t, i.Store(addr, loaded))
		require.Equal(t, 0, value.closed)
	})

	t.Run("rejects different-address ref", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		source := &trackedValue{}
		sourceAddr, err := i.Alloc(source)
		require.NoError(t, err)
		targetAddr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)

		for _, ref := range []types.Value{types.BoxRef(sourceAddr), types.Ref(sourceAddr)} {
			require.ErrorIs(t, i.Store(targetAddr, ref), interp.ErrTypeMismatch)
			require.Equal(t, 0, source.closed)
			v, err := i.Load(targetAddr)
			require.NoError(t, err)
			require.Equal(t, types.I32(5), v)
		}
	})

	t.Run("rejects owned pointer", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
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

	t.Run("rejects invalid ref", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.I32(5))
		require.NoError(t, err)
		for _, ref := range []types.Value{types.Ref(9999), types.BoxRef(9999)} {
			require.ErrorIs(t, i.Store(addr, ref), interp.ErrSegmentationFault)
			v, err := i.Load(addr)
			require.NoError(t, err)
			require.Equal(t, types.I32(5), v)
		}
	})
}

func TestInterpreter_Alloc(t *testing.T) {
	t.Run("allocates value", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.String("hi"), v)
	})

	t.Run("copies boxed reference ownership", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
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
		i := interp.MustNew(program.New(nil))
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
		i := interp.MustNew(program.New(nil))
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
		i := interp.MustNew(program.New(nil))
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
		i := interp.MustNew(program.New(nil))
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
	i := interp.MustNew(program.New(nil))
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
	i := interp.MustNew(program.New(nil))
	defer i.Close()

	addr, err := i.Alloc(types.String("hi"))
	require.NoError(t, err)
	require.NoError(t, i.Release(addr))
	_, err = i.Load(addr)
	require.ErrorIs(t, err, interp.ErrSegmentationFault)
}

func TestInterpreter_RefCount(t *testing.T) {
	t.Run("counts a fresh allocation", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		count, err := i.RefCount(addr)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})

	t.Run("tracks retain and release", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
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
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))

		_, err = i.RefCount(addr)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})
}

func TestInterpreter_HeapLen(t *testing.T) {
	t.Run("grows to cover a new allocation", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		before := i.HeapLen()
		addr, err := i.Alloc(types.String("hi"))
		require.NoError(t, err)
		require.GreaterOrEqual(t, i.HeapLen(), before)
		require.Less(t, addr, i.HeapLen())
	})

	t.Run("bounds a scan over live addresses", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
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
		i := interp.MustNew(program.New(nil))
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
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(4)))
		require.Equal(t, 1, i.Len())
	})

	t.Run("rejects owned pointer", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
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
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(4)))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(4), value)
	})

	t.Run("reference value releases its heap ownership", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
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
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		_, err := i.Pop()
		require.ErrorIs(t, err, interp.ErrStackUnderflow)
	})
}

func TestInterpreter_PopBoxed(t *testing.T) {
	t.Run("scalar f64 returns raw box without allocation", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.F64(3.5)))
		v, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.KindF64, v.Kind())
		require.Equal(t, 3.5, v.F64())
		require.Equal(t, 0, i.Len())
	})

	t.Run("ref kind transfers the reference to the caller", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
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
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		_, err := i.PopBoxed()
		require.ErrorIs(t, err, interp.ErrStackUnderflow)
	})
}

func TestInterpreter_Peek(t *testing.T) {
	t.Run("leaves value on stack", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(4)))
		value, err := i.Peek(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(4), value)
		require.Equal(t, 1, i.Len())
	})

	t.Run("keeps reference owned by stack", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
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
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		_, err := i.Peek(0)
		require.ErrorIs(t, err, interp.ErrStackUnderflow)
	})
}

func TestInterpreter_Len(t *testing.T) {
	i := interp.MustNew(program.New(nil))
	defer i.Close()

	require.Equal(t, 0, i.Len())
	require.NoError(t, i.Push(types.I32(1)))
	require.Equal(t, 1, i.Len())
}

func TestInterpreter_Flush(t *testing.T) {
	t.Run("publishes pending samples to the profiler", func(t *testing.T) {
		profiler := prof.New()
		i := interp.MustNew(program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1)}), interp.WithProfiler(profiler), interp.WithTick(1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))

		i.Flush()
		samples, ok := profiler.Metric("vm_samples_total")
		require.True(t, ok)
		require.Equal(t, float64(1), samples)
	})

	t.Run("does nothing without a profiler", func(t *testing.T) {
		i := interp.MustNew(program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1)}), interp.WithTick(1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))

		require.NotPanics(t, i.Flush)
	})
}

func TestInterpreter_Reset(t *testing.T) {
	t.Run("clears pushed values", func(t *testing.T) {
		i := interp.MustNew(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(1)))
		i.Reset()
		require.Equal(t, 0, i.Len())
	})

	t.Run("restarts module after unpopped result", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 7)})
		i := interp.MustNew(prog)
		defer i.Close()

		for range 64 {
			require.NoError(t, i.Run(context.Background()))
			require.Equal(t, 1, i.Len())
			i.Reset()
			require.Equal(t, 1, i.FP())
			fn, ip, err := i.Frame(0)
			require.NoError(t, err)
			require.Equal(t, 0, fn)
			require.Equal(t, 0, ip)
		}
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), v)
	})

	t.Run("restores declared-kind zero globals", func(t *testing.T) {
		prog := program.New(nil, program.WithGlobals(types.TypeI32(), types.TypeAny()))
		i := interp.MustNew(prog)
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
		i := interp.MustNew(prog)
		defer i.Close()

		require.NoError(t, i.Push(types.String("temporary")))
		boxed, err := i.Peek(0)
		require.NoError(t, err)
		addr := boxed.Ref()

		i.Reset()
		require.Equal(t, 0, i.Len())

		reused, err := i.Alloc(types.String("temporary"))
		require.NoError(t, err)
		require.Equal(t, addr, reused)
	})

	t.Run("restores mutable constants to their initial contents", func(t *testing.T) {
		prog := refArrayProgram(t, 1)
		i := interp.MustNew(prog, interp.WithThreshold(-1))
		defer i.Close()

		constant, err := i.Const(3)
		require.NoError(t, err)
		array := func() *types.Array {
			value, err := i.Load(constant.Ref())
			require.NoError(t, err)
			return value.(*types.Array)
		}

		require.NoError(t, i.Run(context.Background()))
		first, _, err := popString(i)
		require.NoError(t, err)
		require.NotEqual(t, types.BoxedNull, array().Elems[0])

		i.Reset()
		after, err := i.Const(3)
		require.NoError(t, err)
		require.Equal(t, constant, after)
		require.Equal(t, []types.Boxed{types.BoxedNull, types.BoxedNull}, array().Elems)

		require.NoError(t, i.Run(context.Background()))
		second, _, err := popString(i)
		require.NoError(t, err)
		require.Equal(t, first, second)
	})

	t.Run("gives each interpreter its own mutable constants", func(t *testing.T) {
		prog := refArrayProgram(t, 1)
		original := prog.Constants[3].(*types.Array)
		a := interp.MustNew(prog, interp.WithThreshold(-1))
		defer a.Close()
		b := interp.MustNew(prog, interp.WithThreshold(-1))
		defer b.Close()

		require.NoError(t, a.Run(context.Background()))

		constant, err := b.Const(3)
		require.NoError(t, err)
		value, err := b.Load(constant.Ref())
		require.NoError(t, err)
		require.NotSame(t, original, value)
		require.Equal(t, []types.Boxed{types.BoxedNull, types.BoxedNull}, value.(*types.Array).Elems)
		require.Equal(t, []types.Boxed{types.BoxedNull, types.BoxedNull}, original.Elems)
	})

	t.Run("finalizes and clears dynamic values", func(t *testing.T) {
		i := interp.MustNew(program.New(nil), interp.WithHeap(4))

		value := &trackedValue{}
		addr, err := i.Alloc(value)
		require.NoError(t, err)
		count, err := i.RefCount(addr)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		i.Reset()
		require.Equal(t, 1, value.closed)

		_, err = i.RefCount(addr)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)

		require.NoError(t, i.Close())
		require.Equal(t, 1, value.closed)
	})

	t.Run("preserves arrays detached by pop", func(t *testing.T) {
		typ := types.NewArrayType(types.TypeAny())
		prog := program.New(
			[]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0)},
			program.WithTypes(typ),
		)
		i := interp.MustNew(prog)
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
		typ := types.NewArrayType(types.TypeAny())
		prog := program.New(
			[]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0)},
			program.WithTypes(typ),
		)
		i := interp.MustNew(prog)
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

func TestInterpreter_Marshal(t *testing.T) {

	i := interp.MustNew(program.New(nil), interp.WithCodec(upperCodec(0)))
	defer i.Close()

	got, err := i.Marshal("go")
	require.NoError(t, err)
	require.Equal(t, types.String("GO"), got)
}

func TestInterpreter_Unmarshal(t *testing.T) {
	i := interp.MustNew(program.New(nil), interp.WithCodec(upperCodec(0)))
	defer i.Close()

	var dst string
	require.NoError(t, i.Unmarshal(types.String("GO"), &dst))
	require.Equal(t, "go", dst)
}

func TestInterpreter_Close(t *testing.T) {
	i := interp.MustNew(program.New(nil))
	value := &trackedValue{}
	_, err := i.Alloc(value)
	require.NoError(t, err)

	require.NoError(t, i.Close())
	require.Equal(t, 1, value.closed)
	require.NoError(t, i.Close())
	require.Equal(t, 1, value.closed)
}

// selfCycle allocates a value that references only itself, held by the heap
// alone: it stays reachable from no root, so only cycle collection frees it.
func selfCycle(t *testing.T, i *interp.Interpreter) (*trackedValue, int) {
	t.Helper()
	value := &trackedValue{}
	addr, err := i.Alloc(value)
	require.NoError(t, err)
	value.refs = []types.Ref{types.Ref(addr)}
	_, err = i.Retain(addr)
	require.NoError(t, err)
	require.NoError(t, i.Release(addr))
	return value, addr
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
			vm = interp.MustNew(prog)
			elapsed += time.Since(start)
			closeErr = vm.Close()
		}
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
		require.NoError(b, closeErr)
	})

	b.Run("Program", func(b *testing.B) {
		prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 42)})
		var vm *interp.Interpreter
		var closeErr error
		var elapsed time.Duration
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			start := time.Now()
			vm = interp.MustNew(prog)
			elapsed += time.Since(start)
			closeErr = vm.Close()
		}
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
		require.NoError(b, closeErr)
	})
}

func BenchmarkInterpreter_Run(b *testing.B) {
	for _, tt := range runTests {
		b.Run(runTestName(tt.program), func(b *testing.B) {
			for _, mode := range []struct {
				name string
				opts []interp.Option
			}{
				{name: "Threaded", opts: []interp.Option{interp.WithTick(1), interp.WithThreshold(-1)}},
				{name: "Fused"},
			} {
				b.Run(mode.name, func(b *testing.B) {
					ctx := context.Background()
					vm := interp.MustNew(tt.program, mode.opts...)
					b.Cleanup(func() { require.NoError(b, vm.Close()) })

					for range 1 {
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

	b.Run("PerOp", func(b *testing.B) {
		for _, tt := range runTests {
			if tt.err != nil {
				continue
			}
			b.Run(runTestName(tt.program), func(b *testing.B) {
				ctx := context.Background()
				vm := interp.MustNew(tt.program)
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
	tests := []struct {
		name string
		prog *program.Program
	}{
		{
			name: "Scalar",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 42), instr.MustNew(instr.GLOBAL_SET, 0),
			}, program.WithGlobals(types.TypeI32())),
		},
		{
			name: "Heap",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 8), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
			}, program.WithTypes(types.NewArrayType(types.TypeAny()))),
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			vm := interp.MustNew(tt.prog)
			defer vm.Close()
			require.NoError(b, vm.Run(context.Background()))

			var elapsed time.Duration
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				start := time.Now()
				vm.Reset()
				elapsed += time.Since(start)
			}
			b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
		})
	}
}

func BenchmarkInterpreter_Push(b *testing.B) {
	b.Run("Scalar", func(b *testing.B) {
		vm := interp.MustNew(program.New(nil))
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
		vm := interp.MustNew(program.New(nil))
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
	vm := interp.MustNew(program.New(nil))
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
	vm := interp.MustNew(program.New(nil))
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
	vm := interp.MustNew(program.New(nil))
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
	vm := interp.MustNew(program.New(nil))
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
	vm := interp.MustNew(program.New(nil))
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
	vm := interp.MustNew(program.New(nil))
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
	vm := interp.MustNew(prog)
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

// BenchmarkInterpreter_ArrayGetContainerFusion covers GLOBAL_GET and UPVAL_GET
// container fusion; both paths share the same sum-loop shape.
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
			vm := interp.MustNew(tt.prog)
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

// BenchmarkInterpreter_ArraySetContainerFusion measures fused LOCAL_GET, GLOBAL_GET,
// and UPVAL_GET stores; a final sum verifies the writes outside the timed body.
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
			vm := interp.MustNew(tt.prog)
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

// BenchmarkInterpreter_StructGetHost isolates STRUCT_GET on a host struct with an
// unexported field; field 0 is i32, so the loop measures dispatch apart from boxing.
func BenchmarkInterpreter_StructGetHost(b *testing.B) {
	const repeats = 10000
	prog := structGetHostLoop(repeats)
	vm := interp.MustNew(prog)
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
			i := interp.MustNew(program.New(nil))
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
			i := interp.MustNew(program.New(nil))
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
