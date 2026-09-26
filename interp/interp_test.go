package interp_test

import (
	"context"
	"math"
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

type upperCodec byte

type contextKey byte

type trackedValue struct {
	refs   []types.Ref
	closed int
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

// hostNarrowField and hostWideField hold one field of the same VM kind in two
// Go widths, which is what makes a lowered read of one wrong for the other.
type hostNarrowField struct {
	V      int16
	hidden int32
}

type hostWideField struct {
	V      int32
	hidden int32
}

// hostFieldKinds is the Go struct the *HostStruct field-kind tests below read
// through a live view: one field per Go kind this backend's hostRead lowers.
// The unexported field forces the codec to publish that view rather than
// copying the struct into a plain VM one (see hostCounter).
type hostFieldKinds struct {
	Bool    bool
	Int8    int8
	Int16   int16
	Uint16  uint16
	Int32   int32
	Float32 float32
	Float64 float64
	hidden  int32
}

// resumeSnapshot is the interpreter state its hook records at its last
// firing during one Run: the resumed IP, operand-stack depth, and refcount of
// constant 0. The tests use STRUCT_GET as the final opcode, so the last hook
// firing always lands there.
type resumeSnapshot struct {
	ip, sp, refcount int
}

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
		program: program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.RETURN_CALL),
		}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).
			Locals(types.TypeI32).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 100), instr.New(instr.I32_ADD), instr.New(instr.RETURN)).MustBuild())),
		values: []types.Value{types.I32(105)},
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
		// array.new_default stores a generic *types.Array in an i32-declared slot; fused array.get
		// must miss TypedArray[int32] specialization and read the actual representation.
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
		// LOCAL_SET permits a f32 array in an i32-declared slot. Fused array.get must miss the
		// TypedArray[int32] specialization and preserve generic behavior.
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
		// GLOBAL_SET permits a f32 array in an i32-declared global. Fusion must miss
		// TypedArray[int32] and fall back to the generic array reader.
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
		// CLOSURE_NEW permits a f32 array in an i32-declared capture. Fusion must miss
		// TypedArray[int32] and fall back to the generic array reader.
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
		// array.new_default produces generic *types.Array even when the slot declares i32
		// elements. Fused array.set must miss specialization and preserve the generic store.
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
		// LOCAL_SET permits a f32 array in an i32-declared slot. Fusion must miss
		// TypedArray[int32] and preserve the generic store, including raw-bit conversion.
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

	t.Run("reuses the frame across a self tail call", func(t *testing.T) {
		// A tail call replaces the running frame in place, so its callee's
		// locals must start where the caller's did. A teardown that leaves sp
		// past the reused frame grows the stack by one frame per call and
		// exhausts a bounded stack long before the recursion ends.
		fb := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).Locals(types.TypeI32)
		done := fb.Label()
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_EQZ)).BrIf(done)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB))
		fb.Emit(instr.New(instr.CONST_GET, 0), instr.New(instr.RETURN_CALL))
		fb.Bind(done)
		fb.Emit(instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN))
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 2000),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fb.MustBuild()))
		require.NoError(t, program.Verify(prog))

		i := interp.New(prog, interp.WithStack(64))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		value, err := i.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(7), value)
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

	t.Run("ref set and get round-trip", func(t *testing.T) {
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
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0),
				instr.New(instr.REF_NEW),
				instr.New(instr.DUP),
				instr.New(instr.GLOBAL_GET, 1),
				instr.New(instr.REF_SET),
				instr.New(instr.REF_GET),
			}, program.WithGlobals(tt.typ, tt.typ))
			i := interp.New(prog)
			defer i.Close()
			require.NoError(t, i.SetGlobal(0, tt.initial))
			require.NoError(t, i.SetGlobal(1, tt.replacement))

			require.NoError(t, i.Run(context.Background()), tt.name)
			got, err := i.Pop()
			require.NoError(t, err, tt.name)
			require.Equal(t, tt.want, got, tt.name)
		}
	})

	modes := []struct {
		name string
		opts []interp.Option
	}{
		{name: "standalone", opts: []interp.Option{interp.WithTick(1)}},
		{name: "fused", opts: []interp.Option{}},
	}
	t.Run("interpreter modes", func(t *testing.T) {
		for _, tt := range runTests {
			name := runTestName(tt.program)
			for _, mode := range modes {
				i := interp.New(tt.program, mode.opts...)

				err := i.Run(context.Background())
				if tt.err != nil {
					require.ErrorIs(t, err, tt.err, name+"/"+mode.name)
					require.NoError(t, i.Close(), name+"/"+mode.name)
					continue
				}
				require.NoError(t, err, name+"/"+mode.name)
				for _, want := range tt.values {
					got, err := i.Pop()
					require.NoError(t, err, name+"/"+mode.name)
					require.Equal(t, want, got, name+"/"+mode.name)
				}
				require.Equal(t, len(tt.program.Locals), i.Len(), name+"/"+mode.name)
				require.NoError(t, i.Close(), name+"/"+mode.name)
			}
		}
	})

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
	t.Run("interpreter parity corpus", func(t *testing.T) {
		for _, tt := range parityPrograms {
			oracle := run(t, tt.prog, interp.WithTick(1))
			require.Equal(t, oracle, run(t, tt.prog), tt.name)
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

	t.Run("host call releases the consumed callable ref", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			opts []interp.Option
		}{
			{name: "fused"},
			{name: "generic", opts: []interp.Option{interp.WithTick(1)}},
		} {
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

			require.NoError(t, i.Run(context.Background()), tt.name)
			rc, err := i.RefCount(1)
			require.NoError(t, err, tt.name)
			require.Equal(t, 1, rc, tt.name)
		}
	})

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
		{
			name: "module completion adopts a borrowed constant reference",
			prog: program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
			}, program.WithConstants(types.TypedArray[int32]{1, 2, 3})),
		},
		{
			name: "an owned reference dropped before module completion keeps every count",
			prog: program.New([]instr.Instruction{
				instr.New(instr.REF_NULL),
				instr.New(instr.DROP),
				instr.New(instr.CONST_GET, 0),
			}, program.WithConstants(types.TypedArray[int32]{1, 2, 3})),
		},
	}
	t.Run("parity corpus", func(t *testing.T) {
		for _, tt := range parity {
			states := make([]parityState, 0, 2)
			for _, opts := range [][]interp.Option{
				{interp.WithTick(1)},
				{},
			} {
				i := interp.New(tt.prog, opts...)
				err := i.Run(context.Background())
				if tt.err == nil {
					require.NoError(t, err, tt.name)
				} else {
					require.ErrorIs(t, err, tt.err, tt.name)
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
					require.NoError(t, peekErr, tt.name)
					state.stack = append(state.stack, v)
				}
				for idx := range tt.prog.Globals {
					v, globalErr := i.Global(idx)
					require.NoError(t, globalErr, tt.name)
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
				require.NoError(t, i.Close(), tt.name)
			}
			require.Equal(t, states[0], states[1], tt.name)
		}
	})

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
	t.Run("fusion cases", func(t *testing.T) {
		for _, tt := range fusions {
			i := interp.New(tt.prog)
			defer i.Close()

			require.NoError(t, i.Run(context.Background()), tt.name)
			got, err := i.Pop()
			require.NoError(t, err, tt.name)
			require.Equal(t, tt.want, got, tt.name)
		}
	})

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
	t.Run("reference cases", func(t *testing.T) {
		for _, tt := range refs {
			i := interp.New(tt.prog)
			defer i.Close()

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
		}
	})

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
		i := interp.New(prog)
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
		i := interp.New(prog)
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
			i := interp.New(tt.prog, interp.WithTick(1), interp.WithHook(func(*interp.Interpreter) error {
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
	profiler := prof.New()
	i := interp.New(program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1)}), interp.WithProfiler(profiler), interp.WithTick(1))
	defer i.Close()
	require.NoError(t, i.Run(context.Background()))

	i.Flush()
	samples, ok := profiler.Metric("vm_samples_total")
	require.True(t, ok)
	require.Equal(t, float64(1), samples)
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

func TestWithFuel(t *testing.T) {
	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD),
	})
	i := interp.New(prog, interp.WithTick(1), interp.WithFuel(2))
	defer i.Close()

	require.ErrorIs(t, i.Run(context.Background()), interp.ErrFuelExhausted)
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
			vm = interp.New(prog)
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
				{name: "Threaded", opts: []interp.Option{interp.WithTick(1)}},
				{name: "Fused"},
			} {
				b.Run(mode.name, func(b *testing.B) {
					ctx := context.Background()
					vm := interp.New(tt.program, mode.opts...)
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
	tests := []struct {
		name string
		prog *program.Program
	}{
		{
			name: "Scalar",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 42), instr.New(instr.GLOBAL_SET, 0),
			}, program.WithGlobals(types.TypeI32)),
		},
		{
			name: "Heap",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 8), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			}, program.WithTypes(types.NewArrayType(types.TypeAny))),
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			vm := interp.New(tt.prog)
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
	vm := interp.New(prog)
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
			vm := interp.New(tt.prog)
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
			vm := interp.New(tt.prog)
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
	vm := interp.New(prog)
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

func (h *hostLoopFields) Hidden() int32 { return h.hidden }

func (h *hostNarrowField) Hidden() int32 { return h.hidden }

func (h *hostWideField) Hidden() int32 { return h.hidden }

func (h *hostFieldKinds) Hidden() int32 { return h.hidden }

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

// refCountAt reads one address's reference count through the public API. The
// callers below assert on an address they just observed live, so a lookup
// error is a test failure rather than a case to handle.
func refCountAt(t *testing.T, i *interp.Interpreter, addr int) int {
	t.Helper()
	count, err := i.RefCount(addr)
	require.NoError(t, err)
	return count
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
