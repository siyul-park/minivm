package vm

import (
	"math"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

var tests = []struct {
	program *program.Program
	values  []types.Value
}{
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.NOP),
			},
			nil,
		),
		values: nil,
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.DROP),
			},
			nil,
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.DUP),
			},
			nil,
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
			nil,
		),
		values: []types.Value{types.I32(1), types.I32(2)},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.BR, 5),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
			},
			nil,
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
			nil,
		),
		values: []types.Value{types.I32(3)},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.FN_CONST, 0),
				instr.New(instr.CALL),
			},
			[]types.Value{
				types.NewFunction(
					[]instr.Instruction{
						instr.New(instr.I32_CONST, 1),
					},
				),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.FN_CONST, 0),
				instr.New(instr.CALL),
			},
			[]types.Value{
				types.NewFunction(
					[]instr.Instruction{
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.RETURN),
					},
					types.FunctionWithReturns(1),
				),
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
			nil,
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.GLOBAL_SET, 0),
				instr.New(instr.GLOBAL_GET, 0),
			},
			nil,
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.GLOBAL_TEE, 0),
				instr.New(instr.GLOBAL_GET, 0),
			},
			nil,
		),
		values: []types.Value{types.I32(1), types.I32(1)},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.FN_CONST, 0),
				instr.New(instr.CALL),
			},
			[]types.Value{
				types.NewFunction(
					[]instr.Instruction{
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.LOCAL_SET, 0),
					},
					types.FunctionWithLocals(1),
				),
			},
		),
		values: nil,
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.FN_CONST, 0),
				instr.New(instr.CALL),
			},
			[]types.Value{
				types.NewFunction(
					[]instr.Instruction{
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.LOCAL_SET, 0),
						instr.New(instr.LOCAL_GET, 0),
					},
					types.FunctionWithLocals(1),
				),
			},
		),
		values: []types.Value{types.I32(1)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.FN_CONST, 0),
				instr.New(instr.CALL),
			},
			[]types.Value{
				types.NewFunction(
					[]instr.Instruction{
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.LOCAL_TEE, 0),
						instr.New(instr.LOCAL_GET, 0),
					},
					types.FunctionWithLocals(1),
				),
			},
		),
		values: []types.Value{types.I32(1), types.I32(1)},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.FN_CONST, 0),
			},
			[]types.Value{types.NewFunction(nil)},
		),
		values: []types.Value{&types.Closure{Function: types.NewFunction(nil)}},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
			},
			nil,
		),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_STORE),
			},
			nil,
		),
		values: []types.Value{types.I32(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 42),
				instr.New(instr.I32_STORE),
				instr.New(instr.I32_LOAD),
			},
			nil,
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
			nil,
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
			nil,
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
			nil,
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
			nil,
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
			nil,
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
			nil,
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
			nil,
		),
		values: []types.Value{types.I32(1)},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_EQ),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_NE),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_LT_S),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_GT_S),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_LE_S),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_GE_S),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 42),
			},
			nil,
		),
		values: []types.Value{types.I64(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 42),
				instr.New(instr.I64_STORE),
			},
			nil,
		),
		values: []types.Value{types.I64(42)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 42),
				instr.New(instr.I64_STORE),
				instr.New(instr.I64_LOAD),
			},
			nil,
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
			nil,
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
			nil,
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
			nil,
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
			nil,
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
			nil,
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
			nil,
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
			nil,
		),
		values: []types.Value{types.I64(1)},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_EQ),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_NE),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_LT_S),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_GT_S),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_LE_S),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.I64_CONST, 3),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_GE_S),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(3.14))),
			},
			nil,
		),
		values: []types.Value{types.F32(3.14)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(3.14))),
				instr.New(instr.F32_STORE),
			},
			nil,
		),
		values: []types.Value{types.F32(3.14)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(3.14))),
				instr.New(instr.F32_STORE),
				instr.New(instr.F32_LOAD),
			},
			nil,
		),
		values: []types.Value{types.F32(3.14)},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))),
				instr.New(instr.F32_CONST, uint64(math.Float32bits(2.5))),
				instr.New(instr.F32_ADD),
			},
			nil,
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
			nil,
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
			nil,
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
			nil,
		),
		values: []types.Value{types.F32(5.0)},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, 1.0),
				instr.New(instr.F32_CONST, 1.0),
				instr.New(instr.F32_EQ),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, 2.0),
				instr.New(instr.F32_CONST, 1.0),
				instr.New(instr.F32_NE),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, 1.0),
				instr.New(instr.F32_CONST, 2.0),
				instr.New(instr.F32_LT),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, 3.0),
				instr.New(instr.F32_CONST, 2.0),
				instr.New(instr.F32_GT),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, 2.0),
				instr.New(instr.F32_CONST, 2.0),
				instr.New(instr.F32_LE),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F32_CONST, 3.0),
				instr.New(instr.F32_CONST, 2.0),
				instr.New(instr.F32_GE),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(3.14)),
			},
			nil,
		),
		values: []types.Value{types.F64(3.14)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(3.14)),
				instr.New(instr.F64_STORE),
			},
			nil,
		),
		values: []types.Value{types.F64(3.14)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(3.14)),
				instr.New(instr.F64_STORE),
				instr.New(instr.F64_LOAD),
			},
			nil,
		),
		values: []types.Value{types.F64(3.14)},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(1.5)),
				instr.New(instr.F64_CONST, math.Float64bits(2.5)),
				instr.New(instr.F64_ADD),
			},
			nil,
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
			nil,
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
			nil,
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
			nil,
		),
		values: []types.Value{types.F64(5.0)},
	},

	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, 1.0),
				instr.New(instr.F64_CONST, 1.0),
				instr.New(instr.F64_EQ),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, 2.0),
				instr.New(instr.F64_CONST, 1.0),
				instr.New(instr.F64_NE),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, 1.0),
				instr.New(instr.F64_CONST, 2.0),
				instr.New(instr.F64_LT),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, 3.0),
				instr.New(instr.F64_CONST, 2.0),
				instr.New(instr.F64_GT),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, 2.0),
				instr.New(instr.F64_CONST, 2.0),
				instr.New(instr.F64_LE),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
	{
		program: program.New(
			[]instr.Instruction{
				instr.New(instr.F64_CONST, 3.0),
				instr.New(instr.F64_CONST, 2.0),
				instr.New(instr.F64_GE),
			},
			nil,
		),
		values: []types.Value{types.Bool(true)},
	},
}

func TestVM_Run(t *testing.T) {
	for _, tt := range tests {
		t.Run(tt.program.String(), func(t *testing.T) {
			vm := New(tt.program)
			err := vm.Run()
			require.NoError(t, err)

			for _, val := range tt.values {
				v, err := vm.Pop()
				require.NoError(t, err)
				require.Equal(t, val.Interface(), v.Interface())
			}
		})
	}
}

func BenchmarkVM_Run(b *testing.B) {
	for _, tt := range tests {
		b.Run(tt.program.String(), func(b *testing.B) {
			vm := New(tt.program)

			for n := 0; n < b.N; n++ {
				err := vm.Run()
				require.NoError(b, err)

				b.StopTimer()
				vm.Clear()
				b.StartTimer()
			}
		})
	}
}

func BenchmarkFibonacci(b *testing.B) {
	prog := program.New(
		[]instr.Instruction{
			instr.New(instr.FN_CONST, 0),
			instr.New(instr.GLOBAL_SET, 0),

			instr.New(instr.I32_CONST, 20),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.CALL),
		},
		[]types.Value{
			types.NewFunction(
				[]instr.Instruction{
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_LT_S),
					instr.New(instr.BR_IF, 36),

					instr.New(instr.LOCAL_GET, 0),  // +0
					instr.New(instr.I32_CONST, 1),  // +5
					instr.New(instr.I32_SUB),       // +10
					instr.New(instr.GLOBAL_GET, 0), // +11
					instr.New(instr.CALL),          // +16

					instr.New(instr.LOCAL_GET, 0),  // 17
					instr.New(instr.I32_CONST, 2),  //22
					instr.New(instr.I32_SUB),       //27
					instr.New(instr.GLOBAL_GET, 0), //28
					instr.New(instr.CALL),          //33

					instr.New(instr.I32_ADD), //34
					instr.New(instr.RETURN),  //35

					instr.New(instr.LOCAL_GET, 0), //36
					instr.New(instr.RETURN),
				},
				types.FunctionWithParams(1),
				types.FunctionWithReturns(1),
				types.FunctionWithLocals(1),
			),
		},
	)

	vm := New(prog)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		err := vm.Run()
		require.NoError(b, err)

		b.StopTimer()
		vm.Clear()
		b.StartTimer()
	}
}
