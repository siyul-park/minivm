package interp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
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

type callableFunc func(uintptr) error

func (fn callableFunc) Call(ctx uintptr) error {
	return fn(ctx)
}

type testIterator struct {
	values []types.Value
	value  types.Value
	done   bool
}

var _ types.Iterator = (*testIterator)(nil)

func (it *testIterator) Kind() types.Kind { return types.KindRef }

func (it *testIterator) Type() types.Type { return types.TypeRef }

func (it *testIterator) String() string { return "test.iterator" }

func (it *testIterator) Next() bool {
	if len(it.values) == 0 {
		it.value = types.BoxedNull
		it.done = true
		return false
	}
	it.value = it.values[0]
	it.values = it.values[1:]
	it.done = false
	return true
}

func (it *testIterator) Current() types.Value { return it.value }

func (it *testIterator) Done() bool { return it.done }

// vmPoint opts into its own VM representation (a "x,y" string) even though it
// has exported fields and methods that would otherwise route to a HostObject.
type vmPoint struct {
	X int32
	Y int32
}

func (p vmPoint) MarshalVM(_ *Interpreter) (types.Value, error) {
	return types.String(fmt.Sprintf("%d,%d", p.X, p.Y)), nil
}

func (p *vmPoint) UnmarshalVM(_ *Interpreter, v types.Value) error {
	s, ok := v.(types.String)
	if !ok {
		return fmt.Errorf("%w: source=%T", ErrTypeMismatch, v)
	}
	if _, err := fmt.Sscanf(string(s), "%d,%d", &p.X, &p.Y); err != nil {
		return err
	}
	return nil
}

// vmOnlyMarshal implements ValueMarshaler but not ValueUnmarshaler.
type vmOnlyMarshal struct{}

func (vmOnlyMarshal) MarshalVM(_ *Interpreter) (types.Value, error) {
	return types.I32(7), nil
}

// extPoint stands in for an external type that cannot implement ValueMarshaler.
type extPoint struct {
	X int32
	Y int32
}

func extPointConverter() Converter {
	return Converter{
		VMType: types.TypeString,
		Marshal: func(_ *Interpreter, v any) (types.Value, error) {
			p := v.(extPoint)
			return types.String(fmt.Sprintf("%d:%d", p.X, p.Y)), nil
		},
		Unmarshal: func(_ *Interpreter, val types.Value, dst any) error {
			s, ok := val.(types.String)
			if !ok {
				return fmt.Errorf("%w: source=%T", ErrTypeMismatch, val)
			}
			_, err := fmt.Sscanf(string(s), "%d:%d", &dst.(*extPoint).X, &dst.(*extPoint).Y)
			return err
		},
	}
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
				).MustBuild(),
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
				).MustBuild(),
			),
		),
		values: []types.Value{types.I32(1)},
	},
	{
		// tail recursion: f(n) = n==0 ? 0 : return_call f(n-1).
		// n=200 far exceeds the 128 frame budget, so this only completes
		// if RETURN_CALL reuses the frame instead of pushing.
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 200),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{
					Params:  []types.Type{types.TypeI32},
					Returns: []types.Type{types.TypeI32},
				}).Emit(
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I32_EQZ),
					instr.New(instr.BR_IF, 12),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_SUB),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.RETURN_CALL),
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.RETURN),
				).MustBuild(),
			),
		),
		values: []types.Value{types.I32(0)},
	},
	{
		// cross-function tail call: g() = return_call h(); h() = 42.
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{
					Returns: []types.Type{types.TypeI32},
				}).Emit(
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.RETURN_CALL),
				).MustBuild(),
				types.NewFunctionBuilder(&types.FunctionType{
					Returns: []types.Type{types.TypeI32},
				}).Emit(
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.RETURN),
				).MustBuild(),
			),
		),
		values: []types.Value{types.I32(42)},
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
				).MustBuild(),
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
				).MustBuild(),
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
				).MustBuild(),
			),
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
			},
			program.WithConstants(types.NewFunctionBuilder(nil).MustBuild()),
		),
		values: []types.Value{types.NewFunctionBuilder(nil).MustBuild()},
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
		values: []types.Value{types.I1(true)},
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
		values: []types.Value{types.I1(true)},
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
		values: []types.Value{types.I1(false)},
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
		values: []types.Value{types.I1(false)},
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
		values: []types.Value{types.I1(true)},
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
		values: []types.Value{types.I1(true)},
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
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.REF_IS_NULL),
			},
			program.WithConstants(types.String("foo")),
		),
		values: []types.Value{types.I1(false)},
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
		values: []types.Value{types.I1(true)},
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
		values: []types.Value{types.I1(false)},
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
	// width-closed bitwise ops preserve a shared narrow kind (Rust/Swift):
	// i8&i8 → i8, i1^i1 → i1, i1|i1 → i1; a mixed pair widens to i32.
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.I32_AND),
			},
			program.WithConstants(types.I8(127), types.I8(15)),
		),
		values: []types.Value{types.I8(15)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.I32_XOR),
			},
			program.WithConstants(types.I1(true), types.I1(false)),
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.I32_OR),
			},
			program.WithConstants(types.I1(false), types.I1(true)),
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, 15),
				instr.New(instr.I32_AND),
			},
			program.WithConstants(types.I8(127)),
		),
		values: []types.Value{types.I32(15)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CLZ),
			},
		),
		values: []types.Value{types.I32(31)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 8),
				instr.New(instr.I32_CTZ),
			},
		),
		values: []types.Value{types.I32(3)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 7),
				instr.New(instr.I32_POPCNT),
			},
		),
		values: []types.Value{types.I32(3)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_ROTL),
			},
		),
		values: []types.Value{types.I32(2)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_ROTR),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 255),
				instr.New(instr.I32_EXTEND8_S),
			},
		),
		values: []types.Value{types.I32(-1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 65535),
				instr.New(instr.I32_EXTEND16_S),
			},
		),
		values: []types.Value{types.I32(-1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_EQZ),
			},
		),
		values: []types.Value{types.I1(false)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_EQ),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_NE),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_LT_S),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 0xFFFFFFFF),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_LT_S),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_LT_U),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_GT_S),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_GT_U),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_LE_S),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_LE_U),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_GE_S),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_GE_U),
			},
		),
		values: []types.Value{types.I1(true)},
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
				instr.New(instr.I64_CONST, 12),
				instr.New(instr.I64_CONST, 10),
				instr.New(instr.I64_XOR),
			},
		),
		values: []types.Value{types.I64(6)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 12),
				instr.New(instr.I64_CONST, 10),
				instr.New(instr.I64_AND),
			},
		),
		values: []types.Value{types.I64(8)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 12),
				instr.New(instr.I64_CONST, 10),
				instr.New(instr.I64_OR),
			},
		),
		values: []types.Value{types.I64(14)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_CLZ),
			},
		),
		values: []types.Value{types.I64(63)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 8),
				instr.New(instr.I64_CTZ),
			},
		),
		values: []types.Value{types.I64(3)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 7),
				instr.New(instr.I64_POPCNT),
			},
		),
		values: []types.Value{types.I64(3)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_ROTL),
			},
		),
		values: []types.Value{types.I64(2)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_ROTR),
			},
		),
		values: []types.Value{types.I64(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 255),
				instr.New(instr.I64_EXTEND8_S),
			},
		),
		values: []types.Value{types.I64(-1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 65535),
				instr.New(instr.I64_EXTEND16_S),
			},
		),
		values: []types.Value{types.I64(-1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 4294967295),
				instr.New(instr.I64_EXTEND32_S),
			},
		),
		values: []types.Value{types.I64(-1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_EQZ),
			},
		),
		values: []types.Value{types.I1(false)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_EQ),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_NE),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_LT_S),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_LT_U),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_GT_S),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_GT_U),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_LE_S),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_LE_U),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_GE_S),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_GE_U),
			},
		),
		values: []types.Value{types.I1(true)},
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
				instr.New(instr.F32_CONST, uint64(math.Float32bits(-3.5))),
				instr.New(instr.F32_ABS),
			},
		),
		values: []types.Value{types.F32(3.5)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(3.5))),
				instr.New(instr.F32_NEG),
			},
		),
		values: []types.Value{types.F32(-3.5)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(9.0))),
				instr.New(instr.F32_SQRT),
			},
		),
		values: []types.Value{types.F32(3.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.2))),
				instr.New(instr.F32_CEIL),
			},
		),
		values: []types.Value{types.F32(2.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.8))),
				instr.New(instr.F32_FLOOR),
			},
		),
		values: []types.Value{types.F32(1.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.8))),
				instr.New(instr.F32_TRUNC),
			},
		),
		values: []types.Value{types.F32(1.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.5))),
				instr.New(instr.F32_NEAREST),
			},
		),
		values: []types.Value{types.F32(2.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_MIN),
			},
		),
		values: []types.Value{types.F32(1.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_MAX),
			},
		),
		values: []types.Value{types.F32(2.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(-1.0))),
				instr.New(instr.F32_COPYSIGN),
			},
		),
		values: []types.Value{types.F32(-3.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
				instr.New(instr.F32_EQ),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
				instr.New(instr.F32_NE),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_LT),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_GT),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_LE),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
				instr.New(instr.F32_GE),
			},
		),
		values: []types.Value{types.I1(true)},
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
				instr.New(instr.F64_CONST, math.Float64bits(-3.5)),
				instr.New(instr.F64_ABS),
			},
		),
		values: []types.Value{types.F64(3.5)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(3.5)),
				instr.New(instr.F64_NEG),
			},
		),
		values: []types.Value{types.F64(-3.5)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(9.0)),
				instr.New(instr.F64_SQRT),
			},
		),
		values: []types.Value{types.F64(3.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(1.2)),
				instr.New(instr.F64_CEIL),
			},
		),
		values: []types.Value{types.F64(2.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(1.8)),
				instr.New(instr.F64_FLOOR),
			},
		),
		values: []types.Value{types.F64(1.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(1.8)),
				instr.New(instr.F64_TRUNC),
			},
		),
		values: []types.Value{types.F64(1.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(2.5)),
				instr.New(instr.F64_NEAREST),
			},
		),
		values: []types.Value{types.F64(2.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(1.0)),
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_MIN),
			},
		),
		values: []types.Value{types.F64(1.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(1.0)),
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_MAX),
			},
		),
		values: []types.Value{types.F64(2.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(3.0)),
				instr.New(instr.F64_CONST, math.Float64bits(-1.0)),
				instr.New(instr.F64_COPYSIGN),
			},
		),
		values: []types.Value{types.F64(-3.0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(1.0)),
				instr.New(instr.F64_CONST, math.Float64bits(1.0)),
				instr.New(instr.F64_EQ),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_CONST, math.Float64bits(1.0)),
				instr.New(instr.F64_NE),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(1.0)),
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_LT),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(3.0)),
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_GT),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_LE),
			},
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(3.0)),
				instr.New(instr.F64_CONST, math.Float64bits(2.0)),
				instr.New(instr.F64_GE),
			},
		),
		values: []types.Value{types.I1(true)},
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
				instr.New(instr.F64_CONST, math.Float64bits(math.NaN())),
				instr.New(instr.F64_TO_I32_S),
			},
		),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(math.Inf(1))),
				instr.New(instr.F64_TO_I32_S),
			},
		),
		values: []types.Value{types.I32(math.MaxInt32)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(math.Inf(-1))),
				instr.New(instr.F64_TO_I32_S),
			},
		),
		values: []types.Value{types.I32(math.MinInt32)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(-1.0)),
				instr.New(instr.F64_TO_I32_U),
			},
		),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(math.NaN())),
				instr.New(instr.F64_TO_I64_S),
			},
		),
		values: []types.Value{types.I64(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(math.Inf(1))),
				instr.New(instr.F64_TO_I64_S),
			},
		),
		values: []types.Value{types.I64(math.MaxInt64)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(math.Inf(1))))),
				instr.New(instr.F32_TO_I32_S),
			},
		),
		values: []types.Value{types.I32(math.MaxInt32)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(math.NaN())))),
				instr.New(instr.F32_TO_I32_U),
			},
		),
		values: []types.Value{types.I32(0)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))),
				instr.New(instr.I32_REINTERPRET_F32),
			},
		),
		values: []types.Value{types.I32(int32(math.Float32bits(1.5)))},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, uint64(math.Float32bits(1.5))),
				instr.New(instr.F32_REINTERPRET_I32),
			},
		),
		values: []types.Value{types.F32(1.5)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(1.5)),
				instr.New(instr.I64_REINTERPRET_F64),
			},
		),
		values: []types.Value{types.I64(int64(math.Float64bits(1.5)))},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, math.Float64bits(1.5)),
				instr.New(instr.F64_REINTERPRET_I64),
			},
		),
		values: []types.Value{types.F64(1.5)},
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
		values: []types.Value{types.I1(true)},
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
		values: []types.Value{types.I1(false)},
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
		values: []types.Value{types.I1(false)},
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
		values: []types.Value{types.I1(true)},
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
		values: []types.Value{types.I1(false)},
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
		values: []types.Value{types.I1(true)},
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
		values: []types.Value{types.I8(-1)},
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
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.MAP_KEYS),
				instr.New(instr.ARRAY_LEN),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)),
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.MAP_ITER),
				instr.New(instr.CORO_VALUE),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)),
		),
		values: []types.Value{types.I32(5)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, uint64(int64(1<<50))),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.MAP_ITER),
				instr.New(instr.CORO_VALUE),
			},
			program.WithTypes(types.NewMapType(types.TypeI64, types.TypeI32)),
		),
		values: []types.Value{types.I64(1 << 50)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.MAP_ITER),
				instr.New(instr.CORO_VALUE),
			},
			program.WithConstants(types.String("key")),
			program.WithTypes(types.NewMapType(types.TypeString, types.TypeI32)),
		),
		values: []types.Value{types.String("key")},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.MAP_ITER),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.RESUME),
				instr.New(instr.CORO_DONE),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)),
		),
		values: []types.Value{types.I1(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.MAP_NEW, 0),
				instr.New(instr.MAP_KEYS),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.ARRAY_GET),
			},
			program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)),
		),
		values: []types.Value{types.I32(5)},
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
		values: []types.Value{types.I1(true), types.I32(42)},
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
		values: []types.Value{types.I1(false), types.I32(0)},
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
				).MustBuild(),
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
				).MustBuild(),
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
				).MustBuild(),
				types.NewFunctionBuilder(&types.FunctionType{
					Returns: []types.Type{types.TypeI32},
				}).WithCaptures(types.TypeRef).Emit(
					instr.New(instr.UPVAL_GET, 0),
					instr.New(instr.REF_GET),
					instr.New(instr.RETURN),
				).MustBuild(),
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
				).MustBuild(),
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
					instr.New(instr.BR_IF, 20),
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
				).MustBuild(),
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

func requireJIT(t *testing.T) {
	t.Helper()
	if runtime.GOARCH != "arm64" {
		t.Skip("jit is not available on this architecture")
	}
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

	t.Run("returns heap exhaustion", func(t *testing.T) {
		i := New(program.New(nil), WithMaxHeap(1))
		defer i.Close()

		require.ErrorIs(t, i.Push(types.String("blocked")), ErrHeapExhausted)
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

func TestInterpreter_Peek(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(1)))
		require.NoError(t, i.Push(types.I32(2)))

		got, err := i.Peek(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(2), got)
		got, err = i.Peek(1)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(1), got)
		require.Equal(t, 2, i.Len())
	})

	t.Run("underflow", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		for _, n := range []int{-1, 0} {
			_, err := i.Peek(n)
			require.ErrorIs(t, err, ErrStackUnderflow)
		}
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

	t.Run("returns heap exhaustion", func(t *testing.T) {
		i := New(program.New(nil), WithMaxHeap(1))
		defer i.Close()

		_, err := i.Alloc(types.NewArray(types.NewArrayType(types.TypeI32)))
		require.ErrorIs(t, err, ErrHeapExhausted)
	})

	t.Run("function is callable", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.CALL)})
		i := New(prog)
		defer i.Close()

		fn := types.NewFunction(
			&types.FunctionType{Returns: []types.Type{types.TypeI32}},
			nil,
			[]instr.Instruction{instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN)},
		)
		addr, err := i.Alloc(fn)
		require.NoError(t, err)
		require.NoError(t, i.Push(types.BoxRef(addr)))
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), got)

		i.fr.ip = 0
		reused, err := i.Alloc(types.I32(9))
		require.NoError(t, err)
		require.Equal(t, addr, reused)
		require.NoError(t, i.Push(types.BoxRef(reused)))
		require.ErrorIs(t, i.Run(context.Background()), ErrTypeMismatch)
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
	t.Run("boxed ref resolves before storing", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		dst, err := i.Alloc(types.I32(7))
		require.NoError(t, err)
		src, err := i.Alloc(types.String("value"))
		require.NoError(t, err)

		require.NoError(t, i.Store(dst, types.BoxRef(src)))
		v, err := i.Load(dst)
		require.NoError(t, err)
		require.Equal(t, types.String("value"), v)
	})
	t.Run("boxed primitive unboxes before storing", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.I32(0))
		require.NoError(t, err)

		require.NoError(t, i.Store(addr, types.BoxI32(5)))
		v, err := i.Load(addr)
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})
	t.Run("segfault", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		for _, addr := range []int{-1, 9999} {
			require.ErrorIs(t, i.Store(addr, types.I32(1)), ErrSegmentationFault)
		}
	})
	t.Run("boxed ref segfault", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		addr, err := i.Alloc(types.I32(0))
		require.NoError(t, err)

		require.ErrorIs(t, i.Store(addr, types.BoxRef(9999)), ErrSegmentationFault)
	})
	t.Run("host stores callable function", func(t *testing.T) {
		host := NewHostFunction(
			&types.FunctionType{Returns: []types.Type{types.TypeRef}},
			func(i *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				addr, err := i.Alloc(types.I32(0))
				if err != nil {
					return nil, err
				}
				fn := types.NewFunction(
					&types.FunctionType{Returns: []types.Type{types.TypeI32}},
					nil,
					[]instr.Instruction{instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)},
				)
				if err := i.Store(addr, fn); err != nil {
					return nil, err
				}
				return []types.Boxed{types.BoxRef(addr)}, nil
			},
		)
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
				instr.New(instr.CALL),
			},
			program.WithConstants(host),
		)
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), got)
	})
	t.Run("replaces function body", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.CALL)})
		i := New(prog)
		defer i.Close()

		addr, err := i.Alloc(types.I32(0))
		require.NoError(t, err)
		first := types.NewFunction(
			&types.FunctionType{Returns: []types.Type{types.TypeI32}},
			nil,
			[]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN)},
		)
		require.NoError(t, i.Store(addr, first))
		_, err = i.Retain(addr)
		require.NoError(t, err)
		require.NoError(t, i.Push(types.BoxRef(addr)))
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1), got)

		i.fr.ip = 0
		second := types.NewFunction(
			&types.FunctionType{Returns: []types.Type{types.TypeI32}},
			nil,
			[]instr.Instruction{instr.New(instr.I32_CONST, 2), instr.New(instr.RETURN)},
		)
		require.NoError(t, i.Store(addr, second))
		_, err = i.Retain(addr)
		require.NoError(t, err)
		require.NoError(t, i.Push(types.BoxRef(addr)))
		require.NoError(t, i.Run(context.Background()))
		got, err = i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(2), got)
	})
	t.Run("non-function replacement is not callable", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.CALL)})
		i := New(prog)
		defer i.Close()

		addr, err := i.Alloc(types.I32(0))
		require.NoError(t, err)
		fn := types.NewFunction(
			&types.FunctionType{Returns: []types.Type{types.TypeI32}},
			nil,
			[]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN)},
		)
		require.NoError(t, i.Store(addr, fn))
		require.NoError(t, i.Store(addr, types.I32(9)))
		require.NoError(t, i.Push(types.BoxRef(addr)))

		require.ErrorIs(t, i.Run(context.Background()), ErrTypeMismatch)
	})
	t.Run("clears stale jit traces", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.CALL)})
		i := New(prog)
		defer i.Close()

		addr, err := i.Alloc(types.I32(0))
		require.NoError(t, err)
		fn := types.NewFunction(
			&types.FunctionType{Returns: []types.Type{types.TypeI32}},
			nil,
			[]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN)},
		)
		require.NoError(t, i.Store(addr, fn))
		a := anchor{addr: addr, ip: 0}
		i.tracer.trees[a] = &tree{root: &trace{anchor: a, kind: returned}}
		i.tracer.blacklist[a] = true
		i.tracer.loops[addr] = []int{0}

		require.NoError(t, i.Store(addr, types.I32(9)))

		require.Nil(t, i.tracer.trees[a])
		require.False(t, i.tracer.blacklist[a])
		require.Nil(t, i.tracer.loops[addr])
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

// gcCloser is a leaf heap value that records how many times it was finalized,
// so a test can prove the tracing sweep runs io.Closer.Close on collected nodes.
type gcCloser struct{ closed *int }

func (gcCloser) Kind() types.Kind { return types.KindRef }
func (gcCloser) Type() types.Type { return types.TypeRef }
func (gcCloser) String() string   { return "gcCloser" }
func (c gcCloser) Close() error   { *c.closed++; return nil }

func TestInterpreter_gc(t *testing.T) {
	oneRef := types.NewStructType(types.NewStructField(types.TypeRef))
	twoRef := types.NewStructType(types.NewStructField(types.TypeRef), types.NewStructField(types.TypeRef))

	// link wires src.field -> dst as a counted reference, mirroring a STRUCT_SET:
	// it stores the ref and retains the target the way the interpreter would.
	link := func(i *Interpreter, src, field, dst int) {
		i.heap[src].(*types.Struct).SetField(field, types.BoxRef(dst))
		i.retain(dst)
	}

	t.Run("collects a cycle", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		a, _ := i.Alloc(types.NewStruct(oneRef))
		b, _ := i.Alloc(types.NewStruct(oneRef))
		link(i, a, 0, b)
		link(i, b, 0, a)
		// Drop the allocation temporaries; only the cross-edges remain, so plain
		// refcounting can never reclaim the pair.
		i.release(a)
		i.release(b)
		require.Equal(t, 1, i.rc[a])
		require.Equal(t, 1, i.rc[b])

		i.gc()

		require.Nil(t, i.heap[a])
		require.Nil(t, i.heap[b])
		require.Zero(t, i.rc[a])
		require.Zero(t, i.rc[b])
		require.Contains(t, i.free, a)
		require.Contains(t, i.free, b)
	})

	t.Run("keeps a rooted cycle", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		a, _ := i.Alloc(types.NewStruct(oneRef))
		b, _ := i.Alloc(types.NewStruct(oneRef))
		link(i, a, 0, b)
		link(i, b, 0, a)
		i.release(a)
		i.release(b)
		i.root(types.BoxRef(a)) // reachable from a GC root

		i.gc()

		require.NotNil(t, i.heap[a])
		require.NotNil(t, i.heap[b])
		require.NotContains(t, i.free, a)
		require.NotContains(t, i.free, b)
	})

	t.Run("interned string in a cycle is re-internable", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		str := i.intern("ghost") // weak-held: only the cycle keeps it live
		require.Contains(t, i.interned, "ghost")
		require.Equal(t, 1, i.rc[int(str)])

		a, _ := i.Alloc(types.NewStruct(twoRef))
		b, _ := i.Alloc(types.NewStruct(oneRef))
		link(i, a, 0, b)
		link(i, b, 0, a)
		// a -> str reuses the intern's own count as the edge; no extra retain.
		i.heap[a].(*types.Struct).SetField(1, types.BoxRef(int(str)))
		i.release(a)
		i.release(b)

		i.gc()

		require.Nil(t, i.heap[a])
		require.Nil(t, i.heap[b])
		require.Nil(t, i.heap[int(str)])
		require.NotContains(t, i.interned, "ghost")

		again := i.intern("ghost")
		require.Equal(t, types.String("ghost"), i.heap[int(again)])
		require.Contains(t, i.interned, "ghost")
	})

	t.Run("does not inflate survivor refcount", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.String("live")))
		top, _ := i.Peek(0)
		s := top.Ref() // rooted from the stack, rc == 1

		d1, _ := i.Alloc(types.NewStruct(twoRef))
		d2, _ := i.Alloc(types.NewStruct(oneRef))
		link(i, d1, 0, d2)
		link(i, d1, 1, s) // dead garbage also references the survivor
		link(i, d2, 0, d1)
		i.release(d1)
		i.release(d2)
		require.Equal(t, 2, i.rc[s]) // stack + dead edge

		i.gc()

		require.Nil(t, i.heap[d1])
		require.Nil(t, i.heap[d2])
		require.NotNil(t, i.heap[s])
		require.Equal(t, 1, i.rc[s]) // dead edge removed, not pinned

		_, err := i.Pop() // releasing the last root now frees it
		require.NoError(t, err)
		require.Nil(t, i.heap[s])
		require.Contains(t, i.free, s)
	})

	t.Run("finalizes a closer in a dead cycle", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		closed := 0
		c, _ := i.Alloc(gcCloser{closed: &closed}) // leaf, rc == 1 == d1 -> c edge
		d1, _ := i.Alloc(types.NewStruct(twoRef))
		d2, _ := i.Alloc(types.NewStruct(oneRef))
		link(i, d1, 0, d2)
		i.heap[d1].(*types.Struct).SetField(1, types.BoxRef(c))
		link(i, d2, 0, d1)
		i.release(d1)
		i.release(d2)

		i.gc()

		require.Nil(t, i.heap[c])
		require.Equal(t, 1, closed)
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

func TestInterpreter_Local(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(42)))
		v, err := i.Local(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(42), v)
	})

	t.Run("segfault", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		for _, idx := range []int{-1, 0} {
			_, err := i.Local(idx)
			require.ErrorIs(t, err, ErrSegmentationFault)
		}
	})
}

func TestInterpreter_SetLocal(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.I32(1)))
		require.NoError(t, i.SetLocal(0, types.BoxI32(2)))
		v, err := i.Local(0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(2), v)
	})

	t.Run("releases old ref", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.NoError(t, i.Push(types.String("old")))
		old, err := i.Local(0)
		require.NoError(t, err)
		require.Equal(t, 1, i.rc[old.Ref()])

		require.NoError(t, i.SetLocal(0, types.BoxI32(2)))
		require.Zero(t, i.rc[old.Ref()])
	})

	t.Run("segfault", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		for _, idx := range []int{-1, 0} {
			require.ErrorIs(t, i.SetLocal(idx, types.BoxI32(0)), ErrSegmentationFault)
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
	t.Run("releases old ref", func(t *testing.T) {
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.REF_NULL),
				instr.New(instr.GLOBAL_SET, 0),
			},
		)
		i := New(prog, WithGlobals(1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))

		addr, err := i.Alloc(types.String("old"))
		require.NoError(t, err)
		require.NoError(t, i.SetGlobal(0, types.BoxRef(addr)))
		require.Equal(t, 1, i.rc[addr])

		require.NoError(t, i.SetGlobal(0, types.BoxI32(1)))
		require.Zero(t, i.rc[addr])
	})
	t.Run("segfault negative", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.ErrorIs(t, i.SetGlobal(-1, types.BoxI32(0)), ErrSegmentationFault)
	})
	t.Run("segfault upper bound", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.ErrorIs(t, i.SetGlobal(9999, types.BoxI32(0)), ErrSegmentationFault)
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
	modes := []struct {
		name string
		opts []func(*option)
	}{
		{name: "default"},
		{name: "jit", opts: []func(*option){WithTick(1), WithThreshold(1)}},
	}
	for _, mode := range modes {
		mode := mode
		t.Run(mode.name, func(t *testing.T) {
			for _, tt := range tests {
				tt := tt
				t.Run(tt.program.String(), func(t *testing.T) {
					i := New(tt.program, mode.opts...)
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
	}

	t.Run("yield from root frame escapes to host", func(t *testing.T) {
		// A YIELD in the entry frame suspends the whole interpreter: Run returns
		// ErrYield with the yielded value left on the stack, and the next Run
		// resumes after the YIELD with the host-pushed value as its result.
		for _, mode := range modes {
			mode := mode
			t.Run(mode.name, func(t *testing.T) {
				prog := program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.YIELD),
				})
				i := New(prog, mode.opts...)
				defer i.Close()

				err := i.Run(context.Background())
				require.ErrorIs(t, err, ErrYield)

				v, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I32(42), v)

				require.NoError(t, i.Push(types.I32(7)))
				require.NoError(t, i.Run(context.Background()))
				got, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I32(7), got)
			})
		}
	})

	t.Run("custom iterator resumes through coroutine opcodes", func(t *testing.T) {
		for _, mode := range modes {
			mode := mode
			t.Run(mode.name, func(t *testing.T) {
				it := &testIterator{values: []types.Value{types.I32(7), types.I32(11)}}
				require.True(t, it.Next())
				prog := program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.RESUME),
					instr.New(instr.CORO_VALUE),
				}, program.WithConstants(it))

				i := New(prog, mode.opts...)
				defer i.Close()
				require.NoError(t, i.Run(context.Background()))
				got, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I32(11), got)
			})
		}
	})

	t.Run("custom iterator reports done", func(t *testing.T) {
		for _, mode := range modes {
			mode := mode
			t.Run(mode.name, func(t *testing.T) {
				it := &testIterator{values: []types.Value{types.I32(7)}}
				require.True(t, it.Next())
				prog := program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.RESUME),
					instr.New(instr.CORO_DONE),
				}, program.WithConstants(it))

				i := New(prog, mode.opts...)
				defer i.Close()
				require.NoError(t, i.Run(context.Background()))
				got, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I1(true), got)
			})
		}
	})

	t.Run("call starts coroutine and first yield delivers a value", func(t *testing.T) {
		// CALL of a coroutine-function (one containing YIELD) produces a Coroutine
		// handle instead of return values; coro.value reads the first yielded value.
		for _, mode := range modes {
			mode := mode
			t.Run(mode.name, func(t *testing.T) {
				producer := types.NewFunctionBuilder(&types.FunctionType{
					Returns: []types.Type{types.TypeI32},
				}).Emit(
					instr.New(instr.I32_CONST, 7),
					instr.New(instr.YIELD),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.RETURN),
				).MustBuild()
				prog := program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
					instr.New(instr.CORO_VALUE),
				}, program.WithConstants(producer))

				i := New(prog, mode.opts...)
				defer i.Close()
				require.NoError(t, i.Run(context.Background()))
				got, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I32(7), got)
			})
		}
	})

	t.Run("resume passes a value in and runs to completion", func(t *testing.T) {
		// RESUME delivers its value as the result of the pending YIELD; the
		// coroutine adds it to its retained local and returns, and coro.value reads
		// the final return.
		for _, mode := range modes {
			mode := mode
			t.Run(mode.name, func(t *testing.T) {
				producer := types.NewFunctionBuilder(&types.FunctionType{
					Params:  []types.Type{types.TypeI32},
					Returns: []types.Type{types.TypeI32},
				}).Emit(
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.YIELD),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I32_ADD),
					instr.New(instr.RETURN),
				).MustBuild()
				prog := program.New([]instr.Instruction{
					instr.New(instr.I32_CONST, 5),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
					instr.New(instr.I32_CONST, 100),
					instr.New(instr.RESUME),
					instr.New(instr.CORO_VALUE),
				}, program.WithConstants(producer))

				i := New(prog, mode.opts...)
				defer i.Close()
				require.NoError(t, i.Run(context.Background()))
				got, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I32(105), got)
			})
		}
	})

	t.Run("coro.done reports suspended then finished", func(t *testing.T) {
		for _, mode := range modes {
			mode := mode
			t.Run(mode.name, func(t *testing.T) {
				producer := types.NewFunctionBuilder(&types.FunctionType{
					Returns: []types.Type{types.TypeI32},
				}).Emit(
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.YIELD),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.RETURN),
				).MustBuild()

				suspended := program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
					instr.New(instr.CORO_DONE),
				}, program.WithConstants(producer))
				i := New(suspended, mode.opts...)
				defer i.Close()
				require.NoError(t, i.Run(context.Background()))
				got, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I1(false), got)

				finished := program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.RESUME),
					instr.New(instr.CORO_DONE),
				}, program.WithConstants(producer))
				j := New(finished, mode.opts...)
				defer j.Close()
				require.NoError(t, j.Run(context.Background()))
				got, err = j.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I1(true), got)
			})
		}
	})

	t.Run("resume after done errors", func(t *testing.T) {
		for _, mode := range modes {
			mode := mode
			t.Run(mode.name, func(t *testing.T) {
				producer := types.NewFunctionBuilder(&types.FunctionType{
					Returns: []types.Type{types.TypeI32},
				}).Emit(
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.YIELD),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.RETURN),
				).MustBuild()
				prog := program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.RESUME),
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.RESUME),
				}, program.WithConstants(producer))

				i := New(prog, mode.opts...)
				defer i.Close()
				err := i.Run(context.Background())
				require.ErrorIs(t, err, ErrCoroutineDone)
			})
		}
	})

	t.Run("nested coroutine yields through its caller", func(t *testing.T) {
		// An outer coroutine resumes an inner coroutine, reads its yielded value,
		// and re-yields it to main, exercising a coroutine driving a coroutine.
		for _, mode := range modes {
			mode := mode
			t.Run(mode.name, func(t *testing.T) {
				inner := types.NewFunctionBuilder(&types.FunctionType{
					Returns: []types.Type{types.TypeI32},
				}).Emit(
					instr.New(instr.I32_CONST, 11),
					instr.New(instr.YIELD),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.RETURN),
				).MustBuild()
				outer := types.NewFunctionBuilder(&types.FunctionType{
					Returns: []types.Type{types.TypeI32},
				}).Emit(
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
					instr.New(instr.CORO_VALUE),
					instr.New(instr.YIELD),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.RETURN),
				).MustBuild()
				prog := program.New([]instr.Instruction{
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.CALL),
					instr.New(instr.CORO_VALUE),
				}, program.WithConstants(inner, outer))

				i := New(prog, mode.opts...)
				defer i.Close()
				require.NoError(t, i.Run(context.Background()))
				got, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I32(11), got)
			})
		}
	})

	t.Run("jit lowers coro.value in a hot loop", func(t *testing.T) {
		requireJIT(t)

		// A coroutine suspended at its first YIELD exposes the yielded value
		// through CORO_VALUE. The poller reads it n times in a loop hot enough to
		// JIT-compile, so the trace must lower CORO_VALUE natively instead of
		// aborting. Each read returns the yielded 1, so the sum is n.
		producer := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.YIELD),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.RETURN),
		).MustBuild()

		pb := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeRef, types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32)
		header := pb.Label()
		exit := pb.Label()
		poller := pb.Emit(
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 2),
		).Bind(header).Emit(
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_EQZ),
		).BrIf(exit).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.CORO_VALUE),
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_SET, 1),
		).Br(header).Bind(exit).Emit(
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.RETURN),
		).MustBuild()

		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.I32_CONST, 300),
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
		}, program.WithConstants(producer, poller))

		threaded := New(prog, WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(300), want)

		jit := New(prog, WithTick(1), WithThreshold(1))
		defer jit.Close()
		require.NoError(t, jit.Run(context.Background()))
		got, err := jit.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Greater(t, jit.local.Value("vm_jit_emits_total"), float64(0))
	})

	t.Run("jit lowers coro.done in a hot loop", func(t *testing.T) {
		requireJIT(t)

		// RESUME drives the coroutine to completion before the loop, so CORO_DONE
		// reports 1 on every poll. The loop is hot enough to JIT-compile, so the
		// trace must lower CORO_DONE (a heap read plus an itab guard) natively.
		// Each poll returns 1, so the sum is n.
		producer := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.YIELD),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.RETURN),
		).MustBuild()

		pb := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeRef, types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32)
		header := pb.Label()
		exit := pb.Label()
		poller := pb.Emit(
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 2),
		).Bind(header).Emit(
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_EQZ),
		).BrIf(exit).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.CORO_DONE),
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_SET, 1),
		).Br(header).Bind(exit).Emit(
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.RETURN),
		).MustBuild()

		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.RESUME),
			instr.New(instr.I32_CONST, 300),
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
		}, program.WithConstants(producer, poller))

		threaded := New(prog, WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(300), want)

		jit := New(prog, WithTick(1), WithThreshold(1))
		defer jit.Close()
		require.NoError(t, jit.Run(context.Background()))
		got, err := jit.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Greater(t, jit.local.Value("vm_jit_emits_total"), float64(0))
	})

	t.Run("jit lowers narrow bitwise in a hot loop", func(t *testing.T) {
		requireJIT(t)

		// A hot loop masks an i8 accumulator with an i8 constant each iteration.
		// The trace must lower i32.and natively while keeping the i8 kind, so the
		// result round-trips as i8 (0x7F & 0x3F == 0x3F), not i32.
		fb := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI8},
			Returns: []types.Type{types.TypeI8},
		}).WithLocals(types.TypeI32)
		header := fb.Label()
		exit := fb.Label()
		fn := fb.Emit(
			instr.New(instr.I32_CONST, 300),
			instr.New(instr.LOCAL_SET, 1),
		).Bind(header).Emit(
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_EQZ),
		).BrIf(exit).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.CONST_GET, 2),
			instr.New(instr.I32_AND),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_SET, 1),
		).Br(header).Bind(exit).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.RETURN),
		).MustBuild()

		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn, types.I8(0x7F), types.I8(0x3F)))

		threaded := New(prog, WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I8(0x3F), want)

		jit := New(prog, WithTick(1), WithThreshold(1))
		defer jit.Close()
		require.NoError(t, jit.Run(context.Background()))
		got, err := jit.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Greater(t, jit.local.Value("vm_jit_emits_total"), float64(0))
	})

	t.Run("jit deopts on resume in a hot driver loop", func(t *testing.T) {
		requireJIT(t)

		// An infinite generator yields 1 on every resume. The driver loop resumes
		// it n times and sums the yielded values, so the RESUME lands on a hot
		// back-edge and the trace records it. RESUME cannot run natively, so the
		// trace must terminate at it with a deopt to the threaded handler; sum is n.
		gb := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		})
		gloop := gb.Label()
		generator := gb.Bind(gloop).Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.YIELD),
			instr.New(instr.DROP),
		).Br(gloop).MustBuild()

		pb := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeRef, types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32)
		header := pb.Label()
		exit := pb.Label()
		driver := pb.Emit(
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 2),
		).Bind(header).Emit(
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_EQZ),
		).BrIf(exit).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.RESUME),
			instr.New(instr.DROP),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.CORO_VALUE),
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_SET, 1),
		).Br(header).Bind(exit).Emit(
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.RETURN),
		).MustBuild()

		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.I32_CONST, 300),
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
		}, program.WithConstants(generator, driver))

		threaded := New(prog, WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(300), want)

		jit := New(prog, WithTick(1), WithThreshold(1))
		defer jit.Close()
		require.NoError(t, jit.Run(context.Background()))
		got, err := jit.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Greater(t, jit.local.Value("vm_jit_emits_total"), float64(0))
	})

	t.Run("jit deopts on yield reached from a hot driver loop", func(t *testing.T) {
		requireJIT(t)

		// The driver loop calls a fresh coroutine each iteration; every call runs
		// the coroutine to its first YIELD, so the coroutine entry (and the YIELD)
		// becomes hot and the trace records the YIELD. YIELD cannot run natively, so
		// the trace must terminate at it with a deopt; each call yields 7, sum 7*n.
		producer := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.YIELD),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.RETURN),
		).MustBuild()

		pb := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32)
		header := pb.Label()
		exit := pb.Label()
		driver := pb.Emit(
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 1),
		).Bind(header).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_EQZ),
		).BrIf(exit).Emit(
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.CORO_VALUE),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_SET, 0),
		).Br(header).Bind(exit).Emit(
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.RETURN),
		).MustBuild()

		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 200),
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
		}, program.WithConstants(producer, driver))

		threaded := New(prog, WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1400), want)

		jit := New(prog, WithTick(1), WithThreshold(1))
		defer jit.Close()
		require.NoError(t, jit.Run(context.Background()))
		got, err := jit.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Greater(t, jit.local.Value("vm_jit_emits_total"), float64(0))
	})

	t.Run("root-frame yield returns ErrYield under jit and threaded", func(t *testing.T) {
		// A root-frame YIELD (fp==1) panics errYield and Run returns ErrYield. The
		// JIT config must preserve that after any deopt, identical to threaded.
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.YIELD),
		})

		threaded := New(prog, WithThreshold(-1))
		defer threaded.Close()
		require.ErrorIs(t, threaded.Run(context.Background()), ErrYield)

		jit := New(prog, WithTick(1), WithThreshold(1))
		defer jit.Close()
		require.ErrorIs(t, jit.Run(context.Background()), ErrYield)
	})

	t.Run("local.get const binop superinstruction", func(t *testing.T) {
		// The loop body fuses `local.get 0; i32.const 1; i32.sub` into one
		// dispatch; run it in pure threaded mode and assert the exact sum so a
		// miscompiled superinstruction is caught. sum(1..200) = 20100.
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32).Emit(
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0), // IP 7 header
			instr.New(instr.I32_EQZ),
			instr.New(instr.BR_IF, 20), // -> exit
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.BR, 0xFFE6), // -26 -> header
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 200),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		i := New(prog, WithThreshold(-1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(20100), got)
	})

	t.Run("local.get i64 const binop superinstruction", func(t *testing.T) {
		// local.get 0 (i64); i64.const 3; i64.mul fuses into one dispatch. A
		// non-boxable arg forces the local into a heap-promoted KindRef box,
		// exercising the retain that keeps the i64 folder's unbox/release
		// balanced; a missing retain would over-release and corrupt the heap.
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI64},
			Returns: []types.Type{types.TypeI64},
		}).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I64_CONST, 3),
			instr.New(instr.I64_MUL),
			instr.New(instr.RETURN),
		).MustBuild()
		const big = int64(1) << 50
		prog := program.New([]instr.Instruction{
			instr.New(instr.I64_CONST, uint64(big)),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		i := New(prog, WithThreshold(-1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I64(big*3), got)
	})

	t.Run("local.get f32 const binop superinstruction", func(t *testing.T) {
		// local.get 0 (f32); f32.const 1.5; f32.sub fuses into one dispatch.
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeF32},
			Returns: []types.Type{types.TypeF32},
		}).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))),
			instr.New(instr.F32_SUB),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(10.5))),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		i := New(prog, WithThreshold(-1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.F32(9.0), got)
	})

	t.Run("local.get f64 const binop superinstruction", func(t *testing.T) {
		// local.get 0 (f64); f64.const 2.5; f64.add fuses into one dispatch.
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeF64},
			Returns: []types.Type{types.TypeF64},
		}).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.F64_CONST, math.Float64bits(2.5)),
			instr.New(instr.F64_ADD),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.F64_CONST, math.Float64bits(39.5)),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		i := New(prog, WithThreshold(-1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.F64(42.0), got)
	})

	t.Run("jit loop matches threaded", func(t *testing.T) {
		requireJIT(t)

		// A function summing n..1 via a loop (header IP 7, back-edge BR at IP 30)
		// compiles as a framed native loop; threaded and JIT must agree.
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32).Emit(
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0), // IP 7 header
			instr.New(instr.I32_EQZ),
			instr.New(instr.BR_IF, 20), // -> exit
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.BR, 0xFFE6), // -26 -> header
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 200),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		threaded := New(prog, WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)

		jit := New(prog, WithTick(1), WithThreshold(1))
		defer jit.Close()
		require.NoError(t, jit.Run(context.Background()))
		got, err := jit.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("jit loop honors cancel", func(t *testing.T) {
		requireJIT(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1<<30),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0), // IP 8 header
			instr.New(instr.I32_EQZ),
			instr.New(instr.BR_IF, 15), // -> exit (end)
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.BR, 0xFFEA), // -22 -> header
		}), WithTick(1), WithThreshold(1), WithHook(func(*Interpreter) error {
			calls++
			if calls == 5000 {
				cancel()
			}
			return nil
		}))
		defer i.Close()
		require.ErrorIs(t, i.Run(ctx), context.Canceled)
		require.Equal(t, 5000, calls)
	})

	t.Run("heap exhaustion includes runtime frames", func(t *testing.T) {
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW, 0),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeI32)),
		)
		i := New(prog, WithMaxHeap(2), WithThreshold(-1))
		defer i.Close()

		err := i.Run(context.Background())
		require.ErrorIs(t, err, ErrHeapExhausted)
		var runtimeErr *RuntimeError
		require.ErrorAs(t, err, &runtimeErr)
		require.Equal(t, []FrameInfo{{Func: 0, IP: 23}}, runtimeErr.Frames)
		require.Contains(t, runtimeErr.Error(), "fn=0 ip=23")
	})

	t.Run("heap limit allows reuse after release", func(t *testing.T) {
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW, 0),
				instr.New(instr.DROP),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW, 0),
			},
			program.WithTypes(types.NewArrayType(types.TypeI32)),
		)
		i := New(prog, WithMaxHeap(2), WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int32]{2}, got)
	})

	t.Run("runtime error includes nested frames", func(t *testing.T) {
		inner := types.NewFunctionBuilder(&types.FunctionType{}).Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_DIV_S),
			instr.New(instr.RETURN),
		).MustBuild()
		outer := types.NewFunctionBuilder(&types.FunctionType{}).Emit(
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(outer, inner),
		)
		i := New(prog, WithThreshold(-1))
		defer i.Close()

		err := i.Run(context.Background())
		require.ErrorIs(t, err, ErrDivideByZero)
		var runtimeErr *RuntimeError
		require.ErrorAs(t, err, &runtimeErr)
		require.Equal(t, []FrameInfo{
			{Func: 2, IP: 5},
			{Func: 1, IP: 4},
			{Func: 0, IP: 4},
		}, runtimeErr.Frames)
		require.Contains(t, runtimeErr.Error(), "fn=2 ip=5")
	})

	t.Run("jit main loop reenters natively", func(t *testing.T) {
		requireJIT(t)

		// A loop in the main body (addr 0) compiles via the non-framed blocks path.
		// It drains a global from 300 to 0 and pushes the result; a clean 0 proves
		// the yield/re-entry preserved state across many native safepoints.
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 300),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0), // IP 8 header
			instr.New(instr.I32_EQZ),
			instr.New(instr.BR_IF, 15), // -> exit (final GLOBAL_GET)
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.BR, 0xFFEA),    // -22 -> header
			instr.New(instr.GLOBAL_GET, 0), // exit: push final value
		}), WithTick(1), WithThreshold(1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(0), v)
	})

	t.Run("jit guards heap-promoted i64 local", func(t *testing.T) {
		// f(n) = n > 0 ? n + f(n-step) : 0, with n seeded above the 49-bit
		// boxable range so the i64 param heap-promotes. Each level does
		// LOCAL_GET 0 on the promoted param, exercising the JIT i64 load
		// guard; the threaded interpreter is the ground truth.
		const step = 1 << 45
		const depth = 64

		body := []instr.Instruction{
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I64_CONST, step),
			instr.New(instr.I64_SUB),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.I64_ADD),
			instr.New(instr.RETURN),
		}
		skip := 0
		for _, in := range body {
			skip += in.Width()
		}
		code := append([]instr.Instruction{
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I64_CONST, 0),
			instr.New(instr.I64_LE_S),
			instr.New(instr.BR_IF, uint64(skip)),
		}, body...)
		code = append(code,
			instr.New(instr.I64_CONST, 0),
			instr.New(instr.RETURN),
		)
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI64},
			Returns: []types.Type{types.TypeI64},
		}).Emit(code...).MustBuild()

		want := types.I64(int64(depth) * (depth + 1) / 2 * step)

		for _, mode := range modes {
			t.Run(mode.name, func(t *testing.T) {
				i := New(program.New(
					[]instr.Instruction{
						instr.New(instr.I64_CONST, depth*step),
						instr.New(instr.CONST_GET, 0),
						instr.New(instr.CALL),
					},
					program.WithConstants(fn),
				), mode.opts...)
				defer i.Close()

				require.NoError(t, i.Run(context.Background()))
				require.Equal(t, 1, i.FP())
				v, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, want, v)
			})
		}
	})

	t.Run("jit direct recursion preserves frame overflow", func(t *testing.T) {
		requireJIT(t)

		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_EQZ),
			instr.New(instr.BR_IF, 13),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.RETURN),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.RETURN),
		).MustBuild()
		p := prof.NewCollector()
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn),
		), withLocal(p), WithFrame(2))
		defer func() {
			i.fp = 1
			require.NoError(t, i.Close())
		}()

		addr := i.constants[0].Ref()
		require.NoError(t, i.compile(addr))
		require.ErrorIs(t, i.Run(context.Background()), ErrFrameOverflow)
	})

	t.Run("jit global get falls back when runtime value is ref", func(t *testing.T) {
		requireJIT(t)

		fn := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.RETURN),
		).MustBuild()
		p := prof.NewCollector()
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn),
		), withLocal(p), WithGlobals(1))
		defer i.Close()
		i.globals = append(i.globals, types.BoxI32(1))

		addr := i.constants[0].Ref()
		require.NoError(t, i.compile(addr))
		ref, err := i.Alloc(types.String("live"))
		require.NoError(t, err)
		require.NoError(t, i.SetGlobal(0, types.BoxRef(ref)))

		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 2, i.rc[ref])
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.String("live"), value)
		_, err = i.Load(ref)
		require.NoError(t, err)
	})

	t.Run("nested return restores caller frame for locals", func(t *testing.T) {
		callee := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.RETURN),
		).MustBuild()
		caller := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32).Emit(
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.RETURN),
		).MustBuild()

		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.CALL),
			},
			program.WithConstants(callee, caller),
		), WithThreshold(-1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), v)
	})

	t.Run("fused direct call clears stale release flag", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{}).Emit(
			instr.New(instr.RETURN),
		).MustBuild()
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.NOP),
				instr.New(instr.CALL),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn),
		))
		defer i.Close()
		addr := i.constants[0].Ref()

		require.NoError(t, i.Run(context.Background()))
		_, err := i.Load(addr)
		require.NoError(t, err)
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
		).MustBuild()
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
		}))), WithTick(1), WithThreshold(1), WithHook(func(i *Interpreter) error {
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
		p := prof.NewCollector()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		}), withLocal(p), WithTick(1), WithThreshold(-1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		require.Zero(t, p.Value("vm_jit_attempts_total"))
	})

	t.Run("zero attempts jit on first sample", func(t *testing.T) {
		requireJIT(t)
		p := prof.NewCollector()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		}), withLocal(p), WithTick(1), WithThreshold(0))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, float64(1), p.Value("vm_jit_attempts_total"))
	})
}

func TestInterpreter_withLocal(t *testing.T) {
	t.Run("records opcode samples", func(t *testing.T) {
		p := prof.NewCollector()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.DROP),
		}), withLocal(p), WithTick(1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))

		require.Equal(t, uint64(2), p.Total())
		require.Equal(t, uint64(2), p.Samples(0))
		require.Equal(t, uint64(1), p.IP(0, 0))
		require.Equal(t, uint64(1), p.IP(0, 5))
		require.Equal(t, uint64(1), p.Opcode(byte(instr.I32_CONST)))
		require.Equal(t, uint64(1), p.Opcode(byte(instr.DROP)))
	})

	t.Run("records jit counters", func(t *testing.T) {
		requireJIT(t)
		p := prof.NewCollector()
		fn := types.NewFunctionBuilder(nil).WithReturns(types.TypeI32).Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).MustBuild()
		var addr int
		i := New(program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn)), withLocal(p), WithTick(1), WithThreshold(3), WithHook(func(i *Interpreter) error {
			if addr == 0 || i.Func() != addr || i.IP() != 0 {
				return nil
			}
			_, err := i.tracer.capture(i, anchor{addr: addr, ip: 0})
			return err
		}))
		defer i.Close()
		addr = i.constants[0].Ref()
		require.NoError(t, i.Run(context.Background()))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(3), value)
		require.Equal(t, float64(1), p.Value("vm_jit_attempts_total"))
		require.NotZero(t, p.Value("vm_jit_emits_total"))
		require.NotZero(t, p.Value("vm_jit_links_total"))
		require.NotZero(t, p.Value("vm_jit_bytes_total"))
	})

	t.Run("samples jit loop", func(t *testing.T) {
		requireJIT(t)
		p := prof.NewCollector()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 256),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0), // IP 8 header
			instr.New(instr.I32_EQZ),
			instr.New(instr.BR_IF, 15), // -> exit (end)
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.BR, 0xFFEA), // -22 -> header
		}), withLocal(p), WithTick(1), WithThreshold(1))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		// JIT fires at the first sample; further samples accrue only if the
		// safepoint keeps sampling through the native loop.
		require.Greater(t, p.Samples(0), uint64(1))
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
		).MustBuild()
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
		).MustBuild()
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
		}))), WithTick(1), WithThreshold(1), WithFuel(1))
		defer i.Close()
		require.ErrorIs(t, i.Run(context.Background()), ErrFuelExhausted)
	})

	t.Run("exhausts jit loop", func(t *testing.T) {
		requireJIT(t)

		// The loop would run 2^30 iterations; fuel stops it inside the native loop.
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1<<30),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0), // IP 8 header
			instr.New(instr.I32_EQZ),
			instr.New(instr.BR_IF, 15), // -> exit (end)
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.BR, 0xFFEA), // -22 -> header
		}), WithTick(1), WithThreshold(1), WithFuel(500))
		defer i.Close()
		require.ErrorIs(t, i.Run(context.Background()), ErrFuelExhausted)
	})

	t.Run("reset restores fuel", func(t *testing.T) {
		// Fuel is sized to complete exactly one run. A second run succeeds only if
		// Reset restored the budget the first run consumed.
		i := New(program.New([]instr.Instruction{
			instr.New(instr.NOP),
			instr.New(instr.I32_CONST, 7),
		}), WithTick(1), WithFuel(2))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		i.Reset()
		require.NoError(t, i.Run(context.Background()))
	})
}

func TestInterpreter_JIT(t *testing.T) {
	t.Run("records linear trace before native install", func(t *testing.T) {
		p := prof.NewCollector()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.I32_ADD),
		}), withLocal(p), WithTick(1), WithThreshold(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(12), value)

		require.NotNil(t, i.tracer)
		tree := i.tracer.trees[anchor{addr: 0, ip: 0}]
		require.NotNil(t, tree)
		require.Equal(t, linear, tree.root.kind)
		require.Len(t, tree.root.ops, 3)
		require.Equal(t, instr.I32_CONST, tree.root.ops[0].op)
		require.Equal(t, instr.I32_ADD, tree.root.ops[2].op)
		require.Equal(t, []int{0, 5, 10}, i.hot(0))
	})

	t.Run("records branch direction before native install", func(t *testing.T) {
		i := New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.BR_IF, 5),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 42),
		}), WithTick(1), WithThreshold(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), value)

		tree := i.tracer.trees[anchor{addr: 0, ip: 0}]
		require.NotNil(t, tree)
		require.GreaterOrEqual(t, len(tree.root.ops), 2)
		require.Equal(t, instr.BR_IF, tree.root.ops[1].op)
		require.True(t, tree.root.ops[1].taken)
	})

	t.Run("records observed call target and inline depth", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).WithReturns(types.TypeI32).Emit(
			instr.New(instr.I32_CONST, 9),
			instr.New(instr.RETURN),
		).MustBuild()
		i := New(program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn)), WithTick(1), WithThreshold(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(9), value)

		tree := i.tracer.trees[anchor{addr: 0, ip: 0}]
		require.NotNil(t, tree)
		require.GreaterOrEqual(t, len(tree.root.ops), 4)
		require.Equal(t, instr.CALL, tree.root.ops[1].op)
		require.Equal(t, types.KindRef, tree.root.ops[1].seen.Kind())
		require.Equal(t, 1, tree.root.ops[1].callee)
		require.Equal(t, instr.I32_CONST, tree.root.ops[2].op)
		require.Equal(t, 1, tree.root.ops[2].depth)
	})

	t.Run("records observed closure call target and inline depth", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).WithCaptures(types.TypeI32).Emit(
			instr.New(instr.UPVAL_GET, 0),
			instr.New(instr.RETURN),
		).MustBuild()
		closure := types.NewClosure(fn.Typ, 1, []types.Boxed{types.BoxI32(11)})
		i := New(program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
		}, program.WithConstants(fn, closure)), WithTick(1), WithThreshold(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(11), value)

		tree := i.tracer.trees[anchor{addr: 0, ip: 0}]
		require.NotNil(t, tree)
		require.Equal(t, instr.CALL, tree.root.ops[1].op)
		require.Equal(t, types.KindRef, tree.root.ops[1].seen.Kind())
		require.Equal(t, 1, tree.root.ops[1].callee)
		require.Equal(t, instr.UPVAL_GET, tree.root.ops[2].op)
		require.Equal(t, 1, tree.root.ops[2].depth)
	})

	t.Run("traces recursive i64 factorial natively", func(t *testing.T) {
		requireJIT(t)
		// fact(n) = n<=1 ? 1 : n * fact(n-1). fact(20) overflows the 49-bit
		// boxable range mid-product, so the i64 multiply guard deopts to the
		// interpreter (heap promotion) yet still yields the correct value.
		fact := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI64},
			Returns: []types.Type{types.TypeI64},
		}).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I64_CONST, 1),
			instr.New(instr.I64_LE_S),
			instr.New(instr.BR_IF, 20),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I64_CONST, 1),
			instr.New(instr.I64_SUB),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.I64_MUL),
			instr.New(instr.RETURN),
			instr.New(instr.I64_CONST, 1),
			instr.New(instr.RETURN),
		).MustBuild()
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 20),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fact),
		), WithTick(1), WithThreshold(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I64(2432902008176640000), value)

		addr := i.constants[0].Ref()
		tree := i.tracer.trees[anchor{addr: addr, ip: 0}]
		require.NotNil(t, tree)
		require.NotNil(t, tree.root)
		require.NotEqual(t, aborted, tree.root.kind)
		require.NotZero(t, i.local.Value("vm_jit_emits_total"))
	})

	t.Run("traces recursive global accumulation natively", func(t *testing.T) {
		requireJIT(t)
		// sumto(n): if n<=0 return; global[0] += n; sumto(n-1). sumto(5) => 15.
		sumto := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_LE_S),
			instr.New(instr.BR_IF, 22),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_ADD),
			instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.RETURN),
			instr.New(instr.RETURN),
		).MustBuild()
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
				instr.New(instr.GLOBAL_GET, 0),
			},
			program.WithConstants(sumto),
		), WithTick(1), WithThreshold(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(15), value)

		addr := i.constants[0].Ref()
		tree := i.tracer.trees[anchor{addr: addr, ip: 0}]
		require.NotNil(t, tree)
		require.NotNil(t, tree.root)
		require.NotEqual(t, aborted, tree.root.kind)
		require.NotZero(t, i.local.Value("vm_jit_emits_total"))
	})

	t.Run("traces recursive f32 accumulation natively", func(t *testing.T) {
		requireJIT(t)
		// acc(n) = n<=0 ? 0.0 : 1.5 + acc(n-1); acc(20) == 30.0.
		acc := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeF32},
		}).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_LE_S),
			instr.New(instr.BR_IF, 19),
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.F32_ADD),
			instr.New(instr.RETURN),
			instr.New(instr.F32_CONST, uint64(math.Float32bits(0))),
			instr.New(instr.RETURN),
		).MustBuild()
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 20),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(acc),
		), WithTick(1), WithThreshold(1))
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.F32(30), value)

		addr := i.constants[0].Ref()
		tree := i.tracer.trees[anchor{addr: addr, ip: 0}]
		require.NotNil(t, tree)
		require.NotNil(t, tree.root)
		require.NotEqual(t, aborted, tree.root.kind)
		require.NotZero(t, i.local.Value("vm_jit_emits_total"))
	})

	t.Run("traces tail calls natively", func(t *testing.T) {
		requireJIT(t)

		patch := func(code []instr.Instruction, branch, target int) {
			start := len(instr.Marshal(code[:branch]))
			end := start + len(code[branch])
			dst := len(instr.Marshal(code[:target]))
			code[branch].SetOperand(0, uint64(uint16(int16(dst-end))))
		}
		requireEntry := func(t *testing.T, i *Interpreter, addr int) {
			t.Helper()
			tree := i.tracer.trees[anchor{addr: addr, ip: 0}]
			require.NotNil(t, tree)
			require.NotNil(t, tree.root)
			require.Equal(t, returned, tree.root.kind)
			require.NotZero(t, i.local.Value("vm_jit_emits_total"))
		}

		t.Run("self tail-call loops natively without growing frames", func(t *testing.T) {
			// f(n) = n==0 ? 0 : return_call f(n-1). n=200 exceeds the 128 frame
			// budget, so it only completes by reusing the frame; the JIT lowers the
			// self tail-call as a native loop back-edge anchored at the entry.
			body := []instr.Instruction{
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_EQZ),
				instr.New(instr.BR_IF, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.RETURN_CALL),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.RETURN),
			}
			patch(body, 2, 8)
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			}).Emit(body...).MustBuild()
			i := New(program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 200),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(fn),
			), WithTick(1), WithThreshold(1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(0), value)
			requireEntry(t, i, i.constants[0].Ref())
		})

		t.Run("self tail-call carries an accumulator and resets extra locals", func(t *testing.T) {
			// f(n, acc) = n==0 ? acc : return_call f(n-1, acc+n). The unread extra
			// local exercises the back-edge zero-fill of non-param locals.
			// f(200, 0) sums 1..200.
			body := []instr.Instruction{
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_EQZ),
				instr.New(instr.BR_IF, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SUB),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_ADD),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.RETURN_CALL),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.RETURN),
			}
			patch(body, 2, 11)
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32, types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			}).WithLocals(types.TypeI32).Emit(body...).MustBuild()
			i := New(program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 200),
					instr.New(instr.I32_CONST, 0),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(fn),
			), WithTick(1), WithThreshold(1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(20100), value)
			requireEntry(t, i, i.constants[0].Ref())
		})

		t.Run("self tail-call yields across the safepoint budget", func(t *testing.T) {
			// n far exceeds loopBudget, so the native back-edge spends its budget
			// and yields to the safepoint mid-recursion, deopting and re-entering
			// at the reused frame each time. The result still resolves to 0.
			body := []instr.Instruction{
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_EQZ),
				instr.New(instr.BR_IF, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.RETURN_CALL),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.RETURN),
			}
			patch(body, 2, 8)
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			}).Emit(body...).MustBuild()
			i := New(program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 20000),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(fn),
			), WithTick(1), WithThreshold(1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(0), value)
			requireEntry(t, i, i.constants[0].Ref())
		})

		t.Run("cross-function tail call morphs the frame in place", func(t *testing.T) {
			// Mutual recursion: isEven(n) tail-calls isOdd(n-1) and isOdd(n) tail-
			// calls isEven(n-1). Traced from isEven's entry, the isEven->isOdd leg
			// morphs the frame in place and the isOdd->isEven leg closes the loop at
			// the anchor. isEven(200) == 1.
			even := []instr.Instruction{
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_EQZ),
				instr.New(instr.BR_IF, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.RETURN_CALL),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.RETURN),
			}
			patch(even, 2, 8)
			odd := []instr.Instruction{
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_EQZ),
				instr.New(instr.BR_IF, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.RETURN_CALL),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.RETURN),
			}
			patch(odd, 2, 8)
			isEven := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			}).Emit(even...).MustBuild()
			isOdd := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			}).Emit(odd...).MustBuild()
			i := New(program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 200),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(isEven, isOdd),
			), WithTick(1), WithThreshold(1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(1), value)
			requireEntry(t, i, i.constants[0].Ref())
		})
	})

	t.Run("traces loops natively", func(t *testing.T) {
		requireJIT(t)

		patch := func(code []instr.Instruction, branch, target int) {
			start := len(instr.Marshal(code[:branch]))
			end := start + len(code[branch])
			dst := len(instr.Marshal(code[:target]))
			code[branch].SetOperand(0, uint64(uint16(int16(dst-end))))
		}
		// requireLoop asserts a non-aborted loop trace was recorded at one of the
		// function's loop headers and that native code was emitted.
		requireLoop := func(t *testing.T, i *Interpreter, addr int) {
			t.Helper()
			found := false
			for _, h := range i.tracer.headers(i, addr) {
				tree := i.tracer.trees[anchor{addr: addr, ip: h}]
				if tree != nil && tree.root != nil && tree.root.kind == loop {
					found = true
				}
			}
			require.True(t, found, "no loop trace recorded at a header")
			require.NotZero(t, i.local.Value("vm_jit_emits_total"))
		}

		t.Run("unconditional back-edge sums a counted loop", func(t *testing.T) {
			// sum = n + (n-1) + ... + 1, looping with a BR back-edge and a forward
			// BR_IF loop-exit guard (the typed-array-sum loop shape).
			body := []instr.Instruction{
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.LOCAL_SET, 1),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_LE_S),
				instr.New(instr.BR_IF, 0),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_ADD),
				instr.New(instr.LOCAL_SET, 1),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SUB),
				instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.BR, 0),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.RETURN),
			}
			patch(body, 5, 15) // forward exit guard
			patch(body, 14, 2) // back-edge to header
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			}).WithLocals(types.TypeI32, types.TypeI32).Emit(body...).MustBuild()
			i := New(program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1000),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(fn),
			), WithTick(1), WithThreshold(1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(500500), value)
			requireLoop(t, i, i.constants[0].Ref())
		})

		t.Run("conditional back-edge counts down", func(t *testing.T) {
			// counter loops while non-zero via a BR_IF back-edge (the
			// closure-counter loop shape) and returns the iteration count. Local
			// setup precedes the loop so its header sits past the function entry.
			body := []instr.Instruction{
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.LOCAL_SET, 1),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_ADD),
				instr.New(instr.LOCAL_SET, 1),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SUB),
				instr.New(instr.LOCAL_TEE, 0),
				instr.New(instr.BR_IF, 0),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.RETURN),
			}
			patch(body, 10, 2) // back-edge to header (loop top)
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			}).WithLocals(types.TypeI32, types.TypeI32).Emit(body...).MustBuild()
			i := New(program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1000),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(fn),
			), WithTick(1), WithThreshold(1))
			defer i.Close()

			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(1000), value)
			requireLoop(t, i, i.constants[0].Ref())
		})

		t.Run("native loop yields to a canceled context", func(t *testing.T) {
			// A long loop must poll the safepoint across its back-edge so a
			// canceled context still stops it rather than spinning natively.
			body := []instr.Instruction{
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.LOCAL_SET, 1),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SUB),
				instr.New(instr.LOCAL_TEE, 0),
				instr.New(instr.BR_IF, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.RETURN),
			}
			patch(body, 6, 2) // back-edge to header past the entry
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			}).WithLocals(types.TypeI32, types.TypeI32).Emit(body...).MustBuild()
			i := New(program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2000000000),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(fn),
			), WithTick(1), WithThreshold(1))
			defer i.Close()

			ctx, cancel := context.WithCancel(context.Background())
			// Cancel only once the loop runs natively, so the stop must come from
			// the native back-edge polling the safepoint, not the threaded warmup.
			i.hook = func(i *Interpreter) error {
				if i.local.Value("vm_jit_emits_total") > 0 {
					cancel()
				}
				return nil
			}
			require.ErrorIs(t, i.Run(ctx), context.Canceled)
		})
	})

	t.Run("trace lowerer covers phase 3 op classes", func(t *testing.T) {
		requireJIT(t)

		t.Run("captured closure call", func(t *testing.T) {
			body := types.NewFunctionBuilder(&types.FunctionType{
				Returns: []types.Type{types.TypeI32},
			}).WithCaptures(types.TypeI32).Emit(
				instr.New(instr.UPVAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_ADD),
				instr.New(instr.RETURN),
			).MustBuild()
			caller := types.NewFunctionBuilder(&types.FunctionType{
				Returns: []types.Type{types.TypeI32},
			}).Emit(
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.CALL),
				instr.New(instr.RETURN),
			).MustBuild()
			closure := types.NewClosure(body.Typ, 1, []types.Boxed{types.BoxI32(41)})
			var addr int
			i := New(program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 2),
					instr.New(instr.CALL),
				},
				program.WithConstants(body, closure, caller),
			), WithTick(1), WithThreshold(-1), WithHook(func(i *Interpreter) error {
				if addr == 0 || i.Func() != addr || i.IP() != 0 {
					return nil
				}
				_, err := i.tracer.capture(i, anchor{addr: addr, ip: 0})
				return err
			}))
			defer i.Close()
			addr = i.constants[2].Ref()

			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(42), v)

			compiler, err := newCompiler()
			require.NoError(t, err)
			require.NotNil(t, compiler)
			defer compiler.Close()
			def, ok := i.function(addr)
			require.True(t, ok)
			mod := &module{entries: map[anchor]asm.Callable{}, loops: map[anchor]bool{}}
			ok, err = compiler.emit(i, addr, def, mod)
			require.NoError(t, err)
			require.True(t, ok)
			require.NotNil(t, mod.entries[anchor{addr: addr, ip: 0}])
		})

		t.Run("indirect function call", func(t *testing.T) {
			add := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			}).Emit(
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_ADD),
				instr.New(instr.RETURN),
			).MustBuild()
			caller := types.NewFunctionBuilder(&types.FunctionType{
				Returns: []types.Type{types.TypeI32},
			}).WithLocals(types.TypeRef).Emit(
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.I32_CONST, 41),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.CALL),
				instr.New(instr.RETURN),
			).MustBuild()
			var addr int
			i := New(program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.CALL),
				},
				program.WithConstants(add, caller),
			), WithTick(1), WithThreshold(-1), WithHook(func(i *Interpreter) error {
				if addr == 0 || i.Func() != addr || i.IP() != 0 {
					return nil
				}
				_, err := i.tracer.capture(i, anchor{addr: addr, ip: 0})
				return err
			}))
			defer i.Close()
			addr = i.constants[1].Ref()

			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(42), v)

			compiler, err := newCompiler()
			require.NoError(t, err)
			require.NotNil(t, compiler)
			defer compiler.Close()
			def, ok := i.function(addr)
			require.True(t, ok)
			mod := &module{entries: map[anchor]asm.Callable{}, loops: map[anchor]bool{}}
			ok, err = compiler.emit(i, addr, def, mod)
			require.NoError(t, err)
			require.True(t, ok)
			require.NotNil(t, mod.entries[anchor{addr: addr, ip: 0}])
		})

		t.Run("heap reads", func(t *testing.T) {
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeRef, types.TypeRef, types.TypeRef},
				Returns: []types.Type{types.TypeI32},
			}).Emit(
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.REF_GET),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.ARRAY_LEN),
				instr.New(instr.I32_ADD),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_GET),
				instr.New(instr.I32_ADD),
				instr.New(instr.LOCAL_GET, 2),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.STRUCT_GET),
				instr.New(instr.I32_ADD),
				instr.New(instr.RETURN),
			).MustBuild()
			var addr int
			i := New(program.New(
				[]instr.Instruction{
					instr.New(instr.GLOBAL_GET, 0),
					instr.New(instr.GLOBAL_GET, 1),
					instr.New(instr.GLOBAL_GET, 2),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(fn),
			), WithTick(1), WithThreshold(-1), WithHook(func(i *Interpreter) error {
				if addr == 0 || i.Func() != addr || i.IP() != 0 {
					return nil
				}
				_, err := i.tracer.capture(i, anchor{addr: addr, ip: 0})
				return err
			}))
			defer i.Close()
			addr = i.constants[0].Ref()
			cell, err := i.Alloc(types.I32(5))
			require.NoError(t, err)
			array, err := i.Alloc(types.TypedArray[int32]{10, 20})
			require.NoError(t, err)
			st, err := i.Alloc(types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32)), types.BoxI32(15)))
			require.NoError(t, err)
			i.globals = append(i.globals, types.BoxRef(cell), types.BoxRef(array), types.BoxRef(st))

			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(42), v)

			compiler, err := newCompiler()
			require.NoError(t, err)
			require.NotNil(t, compiler)
			defer compiler.Close()
			def, ok := i.function(addr)
			require.True(t, ok)
			mod := &module{entries: map[anchor]asm.Callable{}, loops: map[anchor]bool{}}
			ok, err = compiler.emit(i, addr, def, mod)
			require.NoError(t, err)
			require.True(t, ok)
			require.NotNil(t, mod.entries[anchor{addr: addr, ip: 0}])
		})

		t.Run("br table", func(t *testing.T) {
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Returns: []types.Type{types.TypeI32},
			}).Emit(
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_TABLE, 2, 0, 8, 16),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR, 16),
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.BR, 8),
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.BR, 0),
				instr.New(instr.NOP),
				instr.New(instr.RETURN),
			).MustBuild()
			var addr int
			i := New(program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(fn),
			), WithTick(1), WithThreshold(-1), WithHook(func(i *Interpreter) error {
				if addr == 0 || i.Func() != addr || i.IP() != 0 {
					return nil
				}
				_, err := i.tracer.capture(i, anchor{addr: addr, ip: 0})
				return err
			}))
			defer i.Close()
			addr = i.constants[0].Ref()

			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(42), v)

			compiler, err := newCompiler()
			require.NoError(t, err)
			require.NotNil(t, compiler)
			defer compiler.Close()
			def, ok := i.function(addr)
			require.True(t, ok)
			mod := &module{entries: map[anchor]asm.Callable{}, loops: map[anchor]bool{}}
			ok, err = compiler.emit(i, addr, def, mod)
			require.NoError(t, err)
			require.True(t, ok)
			require.NotNil(t, mod.entries[anchor{addr: addr, ip: 0}])
		})
	})

	t.Run("updates entry slot for direct call", func(t *testing.T) {
		requireJIT(t)
		callee := types.NewFunctionBuilder(nil).WithParams(types.TypeI32).WithReturns(types.TypeI32).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_MUL),
			instr.New(instr.RETURN),
		).MustBuild()
		caller := types.NewFunctionBuilder(nil).WithParams(types.TypeI32).WithReturns(types.TypeI32).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.RETURN),
		).MustBuild()
		p := prof.NewCollector()
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 21),
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.CALL),
			},
			program.WithConstants(callee, caller),
		), withLocal(p))
		defer i.Close()

		require.NoError(t, i.compile(i.constants[0].Ref()))
		require.NoError(t, i.compile(i.constants[1].Ref()))
		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 1, i.FP())
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), value)
	})

	t.Run("entry fallback restores frame metadata", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{}).Emit(
			instr.New(instr.NOP),
			instr.New(instr.RETURN),
		).MustBuild()
		i := New(program.New(nil, program.WithConstants(fn)))
		defer i.Close()

		addr := i.constants[0].Ref()
		i.fallbacks[anchor{addr: addr, ip: 0}] = i.code[addr][0]
		i.frames[1] = frame{addr: addr, ref: addr, code: i.code[addr]}
		i.fp = 2
		i.fr = &i.frames[1]

		wrapped := i.entry(callableFunc(func(ctx uintptr) error {
			i.journal[journalSP] = 0
			i.journal[journalDepth] = 1
			i.journal[journalHead+recordIP] = 0
			i.journal[journalTrap] = trapFallback
			return nil
		}))
		wrapped(i)

		require.Same(t, &i.frames[1], i.fr)
		require.Len(t, i.fr.code, len(i.code[addr]))
		require.NotNil(t, i.fr.code[0])
		require.Nil(t, i.fr.upvals)
		require.Equal(t, 1, i.fr.ip)
		require.Equal(t, 2, i.fp)
	})

	t.Run("restore frame metadata keeps closure upvals", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{}).WithCaptures(types.TypeI32).Emit(
			instr.New(instr.RETURN),
		).MustBuild()
		i := New(program.New(nil, program.WithConstants(fn)))
		defer i.Close()

		addr := i.constants[0].Ref()
		upvals := []types.Boxed{types.BoxI32(7)}
		closure := types.NewClosure(fn.Typ, types.Ref(addr), upvals)
		ref := i.keep(closure)
		f := &frame{addr: addr, ref: ref}

		i.restore(f, addr)

		require.Len(t, f.code, len(i.code[addr]))
		require.NotNil(t, f.code[0])
		require.Equal(t, upvals, f.upvals)
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
		).MustBuild()
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn, gate),
		), WithFrame(1024), WithTick(1), WithThreshold(1))
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

	t.Run("lowers new ops natively and matches threaded", func(t *testing.T) {
		requireJIT(t)

		// Each case is a counted loop (counter local 0, accumulator local 1 seeded
		// from param 1) whose body applies one new op to the accumulator. Running
		// the same program threaded and JIT-compiled must agree, and a non-zero
		// emit count proves the body's op actually lowered to native code rather
		// than silently falling back to threaded dispatch.
		seedPush := func(accType types.Type, seed uint64) instr.Instruction {
			switch accType.Kind() {
			case types.KindI32:
				return instr.New(instr.I32_CONST, seed)
			case types.KindI64:
				return instr.New(instr.I64_CONST, seed)
			case types.KindF32:
				return instr.New(instr.F32_CONST, seed)
			default:
				return instr.New(instr.F64_CONST, seed)
			}
		}
		// The function takes the i32 counter as param 0 and holds the accumulator
		// in local 1, seeded by a prologue. The prologue keeps the loop header off
		// instruction 0 (a loop header at the function entry is not compiled), and
		// because the prologue width cancels out, the relative branch offsets do
		// not depend on it: BR_IF skips the body plus the 13-byte decrement tail,
		// and the back-edge BR rewinds the whole header-to-BR span.
		build := func(accType types.Type, seed uint64, body []instr.Instruction) *program.Program {
			var b int
			for _, in := range body {
				b += in.Width()
			}
			loop := []instr.Instruction{
				seedPush(accType, seed),
				instr.New(instr.LOCAL_SET, 1),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_EQZ),
				instr.New(instr.BR_IF, uint64(b+13)),
			}
			loop = append(loop, body...)
			loop = append(loop,
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SUB),
				instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.BR, uint64(uint16(-(19+b)))),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.RETURN),
			)
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{accType},
			}).WithLocals(accType).Emit(loop...).MustBuild()
			return program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 200),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			}, program.WithConstants(fn))
		}

		tests := []struct {
			name    string
			accType types.Type
			seed    uint64
			body    []instr.Instruction
		}{
			{"i64.and", types.TypeI64, 0xF0F0, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I64_CONST, 0xFF0F), instr.New(instr.I64_AND), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i64.or", types.TypeI64, 0xF0, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I64_CONST, 0x0F), instr.New(instr.I64_OR), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i64.xor", types.TypeI64, 0x1234, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I64_CONST, 0x00FF), instr.New(instr.I64_XOR), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i32.clz", types.TypeI32, 0x0FFF, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CLZ), instr.New(instr.I32_CONST, 7), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i64.ctz", types.TypeI64, 0x1000, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I64_CTZ), instr.New(instr.I64_CONST, 9), instr.New(instr.I64_ADD), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i32.popcnt", types.TypeI32, 0x7F3, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_POPCNT), instr.New(instr.I32_CONST, 17), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i64.popcnt", types.TypeI64, 0xABCD, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I64_POPCNT), instr.New(instr.I64_CONST, 21), instr.New(instr.I64_ADD), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i32.rotl", types.TypeI32, 0x12345678, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_ROTL), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i32.rotr", types.TypeI32, 0x12345678, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 5), instr.New(instr.I32_ROTR), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i64.rotl", types.TypeI64, 0x9, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I64_CONST, 1), instr.New(instr.I64_ROTL), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_AND), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i64.rotr", types.TypeI64, 0x40, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I64_CONST, 1), instr.New(instr.I64_ROTR), instr.New(instr.I64_CONST, 7), instr.New(instr.I64_AND), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i32.extend8_s", types.TypeI32, 0x1FF, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_EXTEND8_S), instr.New(instr.I32_CONST, 0x100), instr.New(instr.I32_OR), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i64.extend32_s", types.TypeI64, 0x1FFFF, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I64_EXTEND16_S), instr.New(instr.I64_CONST, 0xFFFF), instr.New(instr.I64_AND), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i32.reinterpret roundtrip", types.TypeI32, 0x40490FDB, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.F32_REINTERPRET_I32), instr.New(instr.I32_REINTERPRET_F32), instr.New(instr.LOCAL_SET, 1),
			}},
			{"i64.reinterpret roundtrip", types.TypeI64, 0x3FF0000000000000 & 0x1FFFFFFFFFFFF, []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.F64_REINTERPRET_I64), instr.New(instr.I64_REINTERPRET_F64), instr.New(instr.LOCAL_SET, 1),
			}},
			{"f64.sqrt", types.TypeF64, math.Float64bits(2.0), []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.F64_SQRT), instr.New(instr.LOCAL_SET, 1),
			}},
			{"f64.abs/neg", types.TypeF64, math.Float64bits(3.5), []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.F64_NEG), instr.New(instr.F64_ABS), instr.New(instr.LOCAL_SET, 1),
			}},
			{"f64.ceil/floor/trunc/nearest", types.TypeF64, math.Float64bits(2.5), []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.F64_CEIL), instr.New(instr.F64_FLOOR), instr.New(instr.F64_TRUNC), instr.New(instr.F64_NEAREST), instr.New(instr.LOCAL_SET, 1),
			}},
			{"f64.min", types.TypeF64, math.Float64bits(10.0), []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.F64_CONST, math.Float64bits(4.0)), instr.New(instr.F64_MIN), instr.New(instr.LOCAL_SET, 1),
			}},
			{"f64.max", types.TypeF64, math.Float64bits(1.0), []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.F64_CONST, math.Float64bits(4.0)), instr.New(instr.F64_MAX), instr.New(instr.LOCAL_SET, 1),
			}},
			{"f64.copysign", types.TypeF64, math.Float64bits(7.5), []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.F64_CONST, math.Float64bits(-1.0)), instr.New(instr.F64_COPYSIGN), instr.New(instr.LOCAL_SET, 1),
			}},
			{"f32.sqrt", types.TypeF32, uint64(math.Float32bits(2.0)), []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.F32_SQRT), instr.New(instr.LOCAL_SET, 1),
			}},
			{"f32.min/copysign", types.TypeF32, uint64(math.Float32bits(9.0)), []instr.Instruction{
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.F32_CONST, uint64(math.Float32bits(4.0))), instr.New(instr.F32_MIN), instr.New(instr.F32_CONST, uint64(math.Float32bits(-1.0))), instr.New(instr.F32_COPYSIGN), instr.New(instr.LOCAL_SET, 1),
			}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				prog := build(tt.accType, tt.seed, tt.body)

				threaded := New(prog, WithThreshold(-1))
				defer threaded.Close()
				require.NoError(t, threaded.Run(context.Background()))
				want, err := threaded.Pop()
				require.NoError(t, err)

				jit := New(prog, WithTick(1), WithThreshold(1))
				defer jit.Close()
				require.NoError(t, jit.Run(context.Background()))
				got, err := jit.Pop()
				require.NoError(t, err)

				require.Equal(t, want, got)
				require.NotZero(t, jit.local.Value("vm_jit_emits_total"))
			})
		}
	})
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
			{true, types.I1(true)},
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

	t.Run("returns heap exhaustion", func(t *testing.T) {
		i := New(program.New(nil), WithMaxHeap(1))
		defer i.Close()

		_, err := i.Marshal([]string{"blocked"})
		require.ErrorIs(t, err, ErrHeapExhausted)
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

		got, err = i.Marshal([]int16{1, -1})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int32]{1, -1}, got)

		got, err = i.Marshal([]uint8{0x00, 0x7F, 0xFF})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int8]{0, 0x7F, -1}, got)

		got, err = i.Marshal([]uint16{0, math.MaxUint16})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int32]{0, math.MaxUint16}, got)

		got, err = i.Marshal([]byte{0xAB, 0xCD})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int8]{-0x55, -0x33}, got)

		got, err = i.Marshal([]float32{1.5})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[float32]{1.5}, got)

		got, err = i.Marshal([]float64{2.5})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[float64]{2.5}, got)
	})

	t.Run("primitive arrays", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal([2]int32{1, 2})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int32]{1, 2}, got)

		got, err = i.Marshal([2]uint8{0x7F, 0xFF})
		require.NoError(t, err)
		require.Equal(t, types.TypedArray[int8]{0x7F, -1}, got)
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

		f32, err := i.Marshal(map[float32]int32{1.5: 2})
		require.NoError(t, err)
		mF32, ok := f32.(*types.TypedMap[float32])
		require.True(t, ok)
		gotF32, ok := mF32.Get(1.5)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), gotF32)
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

	t.Run("struct scalar fields", func(t *testing.T) {
		type sample struct {
			Bool   bool
			I8     int8
			I16    int16
			I32    int32
			I      int
			I64    int64
			U8     uint8
			U16    uint16
			U32    uint32
			U      uint
			U64    uint64
			Uintpt uintptr
			F32    float32
			F64    float64
			Text   string
		}
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(sample{
			Bool:   true,
			I8:     -8,
			I16:    -16,
			I32:    -32,
			I:      -64,
			I64:    -128,
			U8:     8,
			U16:    16,
			U32:    32,
			U:      64,
			U64:    128,
			Uintpt: 256,
			F32:    1.25,
			F64:    2.5,
			Text:   "go",
		})
		require.NoError(t, err)

		s, ok := got.(*types.Struct)
		require.True(t, ok)
		require.Equal(t, types.BoxedTrue, s.FieldByName("Bool"))
		require.Equal(t, types.BoxI8(-8), s.FieldByName("I8"))
		require.Equal(t, types.BoxI32(-16), s.FieldByName("I16"))
		require.Equal(t, types.BoxI32(-32), s.FieldByName("I32"))
		require.Equal(t, types.BoxI64(-64), s.FieldByName("I"))
		require.Equal(t, types.BoxI64(-128), s.FieldByName("I64"))
		require.Equal(t, types.BoxI32(8), s.FieldByName("U8"))
		require.Equal(t, types.BoxI32(16), s.FieldByName("U16"))
		require.Equal(t, types.BoxI32(32), s.FieldByName("U32"))
		require.Equal(t, types.BoxI64(64), s.FieldByName("U"))
		require.Equal(t, types.BoxI64(128), s.FieldByName("U64"))
		require.Equal(t, types.BoxI64(256), s.FieldByName("Uintpt"))
		require.Equal(t, types.BoxF32(1.25), s.FieldByName("F32"))
		require.Equal(t, types.BoxF64(2.5), s.FieldByName("F64"))

		text := s.FieldByName("Text")
		require.Equal(t, types.KindRef, text.Kind())
		loaded, err := i.Load(text.Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("go"), loaded)
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

	t.Run("time.Time marshals to i64 nanos", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		ts := time.Date(2026, 6, 14, 1, 2, 3, 4, time.UTC)
		got, err := i.Marshal(ts)
		require.NoError(t, err)
		require.Equal(t, types.I64(ts.UnixNano()), got)
	})

	t.Run("time.Duration stays i64", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(3 * time.Second)
		require.NoError(t, err)
		require.Equal(t, types.I64(3*time.Second), got)
	})

	t.Run("complex128 marshals to struct", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(complex(1.5, -2.5))
		require.NoError(t, err)
		st, ok := got.(*types.Struct)
		require.True(t, ok)
		require.Equal(t, types.BoxF64(1.5), st.FieldByName("Real"))
		require.Equal(t, types.BoxF64(-2.5), st.FieldByName("Imag"))
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

		var f32 float32
		require.NoError(t, i.Unmarshal(types.F32(1.5), &f32))
		require.Equal(t, float32(1.5), f32)

		var f64 float64
		require.NoError(t, i.Unmarshal(types.F64(2.5), &f64))
		require.Equal(t, 2.5, f64)
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

		var f32s []float32
		require.NoError(t, i.Unmarshal(types.TypedArray[float32]{1.5}, &f32s))
		require.Equal(t, []float32{1.5}, f32s)

		var f64s []float64
		require.NoError(t, i.Unmarshal(types.TypedArray[float64]{2.5}, &f64s))
		require.Equal(t, []float64{2.5}, f64s)
	})

	t.Run("array", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var out [2]int32
		require.NoError(t, i.Unmarshal(types.TypedArray[int32]{1, 2}, &out))
		require.Equal(t, [2]int32{1, 2}, out)

		var bytes [2]byte
		require.NoError(t, i.Unmarshal(types.TypedArray[int8]{0x7F, -1}, &bytes))
		require.Equal(t, [2]byte{0x7F, 0xFF}, bytes)

		var tooShort [1]int32
		require.ErrorIs(t, i.Unmarshal(types.TypedArray[int32]{1, 2}, &tooShort), ErrValueOverflow)
	})

	t.Run("array", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var bs [3]byte
		require.NoError(t, i.Unmarshal(types.TypedArray[int8]{0x00, 0x7F, -1}, &bs))
		require.Equal(t, [3]byte{0x00, 0x7F, 0xFF}, bs)

		var i8s [2]int8
		require.NoError(t, i.Unmarshal(types.TypedArray[int8]{-1, 0x7F}, &i8s))
		require.Equal(t, [2]int8{-1, 0x7F}, i8s)

		var mismatch [2]byte
		err := i.Unmarshal(types.TypedArray[int8]{1, 2, 3}, &mismatch)
		require.ErrorIs(t, err, ErrValueOverflow)
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

		float32s := map[float32]int32{1.5: 2}
		got, err = i.Marshal(float32s)
		require.NoError(t, err)
		var outFloat32s map[float32]int32
		require.NoError(t, i.Unmarshal(got, &outFloat32s))
		require.Equal(t, float32s, outFloat32s)
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
		var ch chan int
		require.ErrorIs(t, i.Unmarshal(types.I32(1), &ch), ErrUnsupportedMarshalType)
	})

	t.Run("time.Time round-trips through i64", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		ts := time.Date(2026, 6, 14, 1, 2, 3, 4, time.UTC)
		var out time.Time
		require.NoError(t, i.Unmarshal(types.I64(ts.UnixNano()), &out))
		require.True(t, out.Equal(ts))
	})

	t.Run("complex128 round-trips through struct", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		src, err := i.Marshal(complex(1.5, -2.5))
		require.NoError(t, err)

		var out complex128
		require.NoError(t, i.Unmarshal(src, &out))
		require.Equal(t, complex(1.5, -2.5), out)
	})

	t.Run("complex mismatch errors", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var out complex128
		require.ErrorIs(t, i.Unmarshal(types.I32(1), &out), ErrTypeMismatch)
	})
}

func TestValueMarshaler(t *testing.T) {
	t.Run("marshals via MarshalVM ahead of struct routing", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(vmPoint{X: 3, Y: 4})
		require.NoError(t, err)
		require.Equal(t, types.String("3,4"), got)
	})

	t.Run("nested element converts through MarshalVM", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal([]vmPoint{{X: 1, Y: 2}})
		require.NoError(t, err)
		arr, ok := got.(*types.Array)
		require.True(t, ok)

		elem, err := i.Load(arr.Elems[0].Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("1,2"), elem)
	})

	t.Run("missing UnmarshalVM errors on unmarshal", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(vmOnlyMarshal{})
		require.NoError(t, err)
		require.Equal(t, types.I32(7), got)

		var out vmOnlyMarshal
		require.ErrorIs(t, i.Unmarshal(types.I32(7), &out), ErrUnsupportedMarshalType)
	})
}

func TestValueUnmarshaler(t *testing.T) {
	t.Run("round-trips via UnmarshalVM", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var out vmPoint
		require.NoError(t, i.Unmarshal(types.String("5,6"), &out))
		require.Equal(t, vmPoint{X: 5, Y: 6}, out)
	})

	t.Run("pointer receiver mutates destination", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		out := &vmPoint{X: 9, Y: 9}
		require.NoError(t, i.Unmarshal(types.String("1,2"), out))
		require.Equal(t, &vmPoint{X: 1, Y: 2}, out)
	})
}

func TestWithConverter(t *testing.T) {
	extType := reflect.TypeOf(extPoint{})

	t.Run("marshals and unmarshals an external type", func(t *testing.T) {
		i := New(program.New(nil), WithConverter(extType, extPointConverter()))
		defer i.Close()

		got, err := i.Marshal(extPoint{X: 3, Y: 4})
		require.NoError(t, err)
		require.Equal(t, types.String("3:4"), got)

		var out extPoint
		require.NoError(t, i.Unmarshal(types.String("5:6"), &out))
		require.Equal(t, extPoint{X: 5, Y: 6}, out)
	})

	t.Run("applies to a nested struct field", func(t *testing.T) {
		i := New(program.New(nil), WithConverter(extType, extPointConverter()))
		defer i.Close()

		got, err := i.Marshal(struct{ P extPoint }{P: extPoint{X: 1, Y: 2}})
		require.NoError(t, err)
		st, ok := got.(*types.Struct)
		require.True(t, ok)
		field, err := i.Load(st.FieldByName("P").Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("1:2"), field)
	})

	t.Run("overrides a builtin converter", func(t *testing.T) {
		secs := Converter{
			VMType: types.TypeI64,
			Marshal: func(_ *Interpreter, v any) (types.Value, error) {
				return types.I64(v.(time.Time).Unix()), nil
			},
		}
		ts := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
		i := New(program.New(nil), WithConverter(reflect.TypeOf(time.Time{}), secs))
		defer i.Close()

		got, err := i.Marshal(ts)
		require.NoError(t, err)
		require.Equal(t, types.I64(ts.Unix()), got)
	})

	t.Run("nil direction stays unsupported", func(t *testing.T) {
		marshalOnly := extPointConverter()
		marshalOnly.Unmarshal = nil
		i := New(program.New(nil), WithConverter(extType, marshalOnly))
		defer i.Close()

		var out extPoint
		require.ErrorIs(t, i.Unmarshal(types.String("1:2"), &out), ErrUnsupportedMarshalType)
	})

	t.Run("ignored when a custom marshaler is set", func(t *testing.T) {
		i := New(program.New(nil),
			WithMarshaler(&recordingMarshaler{}),
			WithConverter(extType, extPointConverter()),
		)
		defer i.Close()

		got, err := i.Marshal(extPoint{X: 1, Y: 2})
		require.NoError(t, err)
		require.Equal(t, types.I32(9), got)
	})
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
				b.StopTimer()
				require.NoError(b, i.Run(ctx))
			})
		}
	})

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

				i := New(tt.program, WithThreshold(-1))
				defer i.Close()
				for _, constant := range i.constants {
					if constant.Kind() != types.KindRef {
						continue
					}
					if _, ok := i.function(constant.Ref()); ok {
						require.NoError(b, i.compile(constant.Ref()))
					}
				}

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

// Verification is no longer part of the interpreter: New trusts prog and runs
// malformed bytecode until it traps. Callers reject bad bytecode up front with
// program.Verify (covered by program's TestVerify). This documents the trusting
// default — a malformed function stored at runtime executes and traps.
func TestInterpreter_RunsUnverifiedBytecode(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.CALL)})
	i := New(prog)
	defer i.Close()

	addr, err := i.Alloc(types.I32(0))
	require.NoError(t, err)
	fn := types.NewFunction(
		&types.FunctionType{Returns: []types.Type{types.TypeI32}},
		nil,
		[]instr.Instruction{instr.New(instr.I32_ADD), instr.New(instr.RETURN)},
	)
	require.NoError(t, i.Store(addr, fn))
	require.NoError(t, i.Push(types.BoxRef(addr)))
	require.ErrorIs(t, i.Run(context.Background()), ErrStackUnderflow)
}

func TestInterpreter_Throw(t *testing.T) {
	t.Run("explicit throw lands on handler", func(t *testing.T) {
		b := instr.NewBuilder()
		start, end, catch := b.Label(), b.Label(), b.Label()
		b.Bind(start)
		b.Emit(instr.I32_CONST, 1)
		b.Emit(instr.THROW)
		b.Bind(end)
		b.Bind(catch)
		b.Try(start, end, catch, 0)
		instrs, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(instrs, program.WithHandlers(b.Handlers()...))

		i := New(prog)
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1), v)
	})

	t.Run("runtime trap is caught as an error value", func(t *testing.T) {
		b := instr.NewBuilder()
		start, end, catch := b.Label(), b.Label(), b.Label()
		b.Bind(start)
		b.Emit(instr.I32_CONST, 10)
		b.Emit(instr.I32_CONST, 0)
		b.Emit(instr.I32_DIV_S)
		b.Bind(end)
		b.Bind(catch)
		b.Try(start, end, catch, 0)
		instrs, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(instrs, program.WithHandlers(b.Handlers()...))

		i := New(prog)
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		e, ok := v.(*types.Error)
		require.True(t, ok)
		require.ErrorIs(t, e, ErrDivideByZero)
	})

	t.Run("host error is caught", func(t *testing.T) {
		host := NewHostFunction(
			&types.FunctionType{Returns: []types.Type{types.TypeI32}},
			func(_ *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
				return nil, errors.New("boom")
			},
		)
		b := instr.NewBuilder()
		start, end, catch := b.Label(), b.Label(), b.Label()
		b.Bind(start)
		b.Emit(instr.CONST_GET, 0)
		b.Emit(instr.CALL)
		b.Bind(end)
		b.Bind(catch)
		b.Try(start, end, catch, 0)
		instrs, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(instrs, program.WithConstants(host), program.WithHandlers(b.Handlers()...))

		i := New(prog)
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		e, ok := v.(*types.Error)
		require.True(t, ok)
		require.Contains(t, e.Error(), "boom")
	})

	t.Run("propagates across call frames", func(t *testing.T) {
		callee := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Emit(instr.New(instr.I32_CONST, 99), instr.New(instr.THROW)).
			MustBuild()
		b := instr.NewBuilder()
		start, end, catch := b.Label(), b.Label(), b.Label()
		b.Bind(start)
		b.Emit(instr.CONST_GET, 0)
		b.Emit(instr.CALL)
		b.Bind(end)
		b.Bind(catch)
		b.Try(start, end, catch, 0)
		instrs, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(instrs, program.WithConstants(callee), program.WithHandlers(b.Handlers()...))

		i := New(prog)
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(99), v)
	})

	t.Run("rethrow reaches the enclosing handler", func(t *testing.T) {
		b := instr.NewBuilder()
		outerStart, outerEnd, outerCatch := b.Label(), b.Label(), b.Label()
		innerStart, innerEnd, innerCatch := b.Label(), b.Label(), b.Label()
		b.Bind(outerStart)
		b.Bind(innerStart)
		b.Emit(instr.I32_CONST, 5)
		b.Emit(instr.THROW)
		b.Bind(innerEnd)
		b.Bind(innerCatch)
		b.Emit(instr.THROW) // rethrow the caught value
		b.Bind(outerEnd)
		b.Bind(outerCatch)
		b.Try(innerStart, innerEnd, innerCatch, 0) // innermost first
		b.Try(outerStart, outerEnd, outerCatch, 0)
		instrs, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(instrs, program.WithHandlers(b.Handlers()...))

		i := New(prog)
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(5), v)
	})

	t.Run("error.new and error.get round-trip", func(t *testing.T) {
		b := instr.NewBuilder()
		start, end, catch := b.Label(), b.Label(), b.Label()
		b.Bind(start)
		b.Emit(instr.CONST_GET, 0)
		b.Emit(instr.ERROR_NEW)
		b.Emit(instr.THROW)
		b.Bind(end)
		b.Bind(catch)
		b.Emit(instr.ERROR_GET)
		b.Try(start, end, catch, 0)
		instrs, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(instrs, program.WithConstants(types.String("data")), program.WithHandlers(b.Handlers()...))

		i := New(prog)
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.String("data"), v)
	})

	t.Run("uncaught throw surfaces as a runtime error", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.THROW),
		})
		i := New(prog)
		defer i.Close()
		err := i.Run(context.Background())
		require.ErrorIs(t, err, ErrUncaughtException)
		var re *RuntimeError
		require.ErrorAs(t, err, &re)
	})

	t.Run("uncaught error value preserves its unwrap chain", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.ERROR_NEW),
			instr.New(instr.THROW),
		}, program.WithConstants(types.String("kaboom")))
		i := New(prog)
		defer i.Close()
		err := i.Run(context.Background())
		var e *types.Error
		require.ErrorAs(t, err, &e)
		require.Equal(t, "kaboom", e.Error())
	})

	t.Run("jit lowers error.get in a hot loop", func(t *testing.T) {
		requireJIT(t)

		b := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeRef, types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32)
		header := b.Label()
		exit := b.Label()
		sum := b.Emit(
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 2),
		).Bind(header).Emit(
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_EQZ),
		).BrIf(exit).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.ERROR_GET),
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_SET, 1),
		).Br(header).Bind(exit).Emit(
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.I32_CONST, 300),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(sum))

		threaded := New(prog, WithThreshold(-1))
		defer threaded.Close()
		errAddr, err := threaded.Alloc(types.NewError("one", types.BoxI32(1)))
		require.NoError(t, err)
		threaded.globals = append(threaded.globals, types.BoxRef(errAddr))
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(300), want)

		jit := New(prog, WithTick(1), WithThreshold(1))
		defer jit.Close()
		errAddr, err = jit.Alloc(types.NewError("one", types.BoxI32(1)))
		require.NoError(t, err)
		jit.globals = append(jit.globals, types.BoxRef(errAddr))
		require.NoError(t, jit.Run(context.Background()))
		got, err := jit.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Greater(t, jit.local.Value("vm_jit_emits_total"), float64(0))
	})

	t.Run("jit error.get retains ref payloads", func(t *testing.T) {
		requireJIT(t)

		b := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeRef, types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32)
		header := b.Label()
		exit := b.Label()
		dropper := b.Emit(
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 2),
		).Bind(header).Emit(
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_EQZ),
		).BrIf(exit).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.ERROR_GET),
			instr.New(instr.DROP),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_SET, 1),
		).Br(header).Bind(exit).Emit(
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.I32_CONST, 300),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(dropper))

		threaded := New(prog, WithThreshold(-1))
		defer threaded.Close()
		payload, err := threaded.Alloc(types.String("payload"))
		require.NoError(t, err)
		errAddr, err := threaded.Alloc(types.NewError("payload", types.BoxRef(payload)))
		require.NoError(t, err)
		threaded.globals = append(threaded.globals, types.BoxRef(errAddr))
		require.NoError(t, threaded.Run(context.Background()))
		_, err = threaded.Pop()
		require.NoError(t, err)
		wantPayloadRC := threaded.rc[payload]
		wantErrorRC := threaded.rc[errAddr]

		jit := New(prog, WithTick(1), WithThreshold(1))
		defer jit.Close()
		payload, err = jit.Alloc(types.String("payload"))
		require.NoError(t, err)
		errAddr, err = jit.Alloc(types.NewError("payload", types.BoxRef(payload)))
		require.NoError(t, err)
		jit.globals = append(jit.globals, types.BoxRef(errAddr))
		require.NoError(t, jit.Run(context.Background()))
		_, err = jit.Pop()
		require.NoError(t, err)
		require.Equal(t, wantPayloadRC, jit.rc[payload])
		require.Equal(t, wantErrorRC, jit.rc[errAddr])
		require.Greater(t, jit.local.Value("vm_jit_emits_total"), float64(0))
	})

	t.Run("jit deopts on error.new terminal", func(t *testing.T) {
		requireJIT(t)

		makeErr := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.I32_CONST, 42),
			instr.New(instr.ERROR_NEW),
			instr.New(instr.ERROR_GET),
			instr.New(instr.RETURN),
		).MustBuild()
		b := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32)
		header := b.Label()
		exit := b.Label()
		driver := b.Emit(
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 1),
		).Bind(header).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_EQZ),
		).BrIf(exit).Emit(
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_SET, 0),
		).Br(header).Bind(exit).Emit(
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 200),
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
		}, program.WithConstants(makeErr, driver))

		threaded := New(prog, WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(8400), want)

		jit := New(prog, WithTick(1), WithThreshold(1))
		defer jit.Close()
		require.NoError(t, jit.Run(context.Background()))
		got, err := jit.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Greater(t, jit.local.Value("vm_jit_emits_total"), float64(0))
	})

	t.Run("jit deopts on throw terminal", func(t *testing.T) {
		requireJIT(t)

		b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}})
		start, end, catch := b.Label(), b.Label(), b.Label()
		thrower := b.Bind(start).Emit(
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.THROW),
		).Bind(end).Bind(catch).Emit(
			instr.New(instr.RETURN),
		).Try(start, end, catch, 0).MustBuild()
		db := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32)
		header := db.Label()
		exit := db.Label()
		driver := db.Emit(
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 1),
		).Bind(header).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_EQZ),
		).BrIf(exit).Emit(
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_SET, 0),
		).Br(header).Bind(exit).Emit(
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 200),
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
		}, program.WithConstants(thrower, driver))

		threaded := New(prog, WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1000), want)

		jit := New(prog, WithTick(1), WithThreshold(1))
		defer jit.Close()
		require.NoError(t, jit.Run(context.Background()))
		got, err := jit.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Greater(t, jit.local.Value("vm_jit_emits_total"), float64(0))
	})

	t.Run("jit deopted arithmetic trap is caught", func(t *testing.T) {
		requireJIT(t)

		b := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32, types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		})
		start, end, catch := b.Label(), b.Label(), b.Label()
		div := b.Bind(start).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_DIV_S),
			instr.New(instr.RETURN),
		).Bind(end).Bind(catch).Emit(
			instr.New(instr.RETURN),
		).Try(start, end, catch, 2).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.GLOBAL_GET, 1),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(div))

		jit := New(prog, WithTick(1), WithThreshold(1))
		defer jit.Close()
		jit.globals = append(jit.globals, types.BoxI32(10), types.BoxI32(2))
		require.NoError(t, jit.Run(context.Background()))
		value, err := jit.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(5), value)
		require.Greater(t, jit.local.Value("vm_jit_emits_total"), float64(0))

		jit.Reset()
		jit.globals = append(jit.globals, types.BoxI32(10), types.BoxI32(0))
		require.NoError(t, jit.Run(context.Background()))
		value, err = jit.Pop()
		require.NoError(t, err)
		exc, ok := value.(*types.Error)
		require.True(t, ok)
		require.ErrorIs(t, exc, ErrDivideByZero)
	})

	// Fusion (error.new;throw and string;error.new) is active in non-precise mode
	// and bypassed under WithTick(1); both must behave identically.
	for _, mode := range []struct {
		name string
		opts []func(*option)
	}{
		{name: "fused"},
		{name: "precise", opts: []func(*option){WithTick(1)}},
	} {
		mode := mode
		t.Run("error.new;throw fusion parity/"+mode.name, func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 7),
				instr.New(instr.ERROR_NEW),
				instr.New(instr.THROW),
			})
			i := New(prog, mode.opts...)
			defer i.Close()
			err := i.Run(context.Background())
			var e *types.Error
			require.ErrorAs(t, err, &e)
			require.Equal(t, "7", e.Error())
			require.Equal(t, types.BoxI32(7), e.Value())
		})

		t.Run("string;error.new fusion parity/"+mode.name, func(t *testing.T) {
			b := instr.NewBuilder()
			start, end, catch := b.Label(), b.Label(), b.Label()
			b.Bind(start)
			b.Emit(instr.CONST_GET, 0)
			b.Emit(instr.ERROR_NEW)
			b.Emit(instr.THROW)
			b.Bind(end)
			b.Bind(catch)
			b.Emit(instr.ERROR_GET)
			b.Try(start, end, catch, 0)
			instrs, err := b.Assemble()
			require.NoError(t, err)
			prog := program.New(instrs, program.WithConstants(types.String("x")), program.WithHandlers(b.Handlers()...))

			i := New(prog, mode.opts...)
			defer i.Close()
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.String("x"), v)
		})
	}
}
