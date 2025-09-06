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
		program: program.New(instr.New(instr.NOP)),
		values:  nil,
	},
	{
		program: program.New(instr.New(instr.I32_CONST, 42), instr.New(instr.DROP)),
		values:  nil,
	},
	{
		program: program.New(instr.New(instr.I32_CONST, 42), instr.New(instr.DUP)),
		values:  []types.Value{types.I32(42), types.I32(42)},
	},
	{
		program: program.New(instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.SWAP)),
		values:  []types.Value{types.I32(1), types.I32(2)},
	},
	{
		program: program.New(instr.New(instr.JMP, 5), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2)),
		values:  []types.Value{types.I32(2)},
	},
	{
		program: program.New(instr.New(instr.I32_CONST, 1), instr.New(instr.JMP_IF, 5), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_CONST, 3)),
		values:  []types.Value{types.I32(3)},
	},

	{
		program: program.New(instr.New(instr.I32_CONST, 42)),
		values:  []types.Value{types.I32(42)},
	},
	{
		program: program.New(instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD)),
		values:  []types.Value{types.I32(3)},
	},
	{
		program: program.New(instr.New(instr.I32_CONST, 5), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_SUB)),
		values:  []types.Value{types.I32(2)},
	},
	{
		program: program.New(instr.New(instr.I32_CONST, 4), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_MUL)),
		values:  []types.Value{types.I32(12)},
	},
	{
		program: program.New(instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_DIV_S)),
		values:  []types.Value{types.I32(5)},
	},
	{
		program: program.New(instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_DIV_U)),
		values:  []types.Value{types.I32(3)},
	},
	{
		program: program.New(instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_REM_S)),
		values:  []types.Value{types.I32(1)},
	},
	{
		program: program.New(instr.New(instr.I32_CONST, 10), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_REM_U)),
		values:  []types.Value{types.I32(1)},
	},

	{
		program: program.New(instr.New(instr.I64_CONST, 123456789)),
		values:  []types.Value{types.I64(123456789)},
	},
	{
		program: program.New(instr.New(instr.I64_CONST, 1), instr.New(instr.I64_CONST, 2), instr.New(instr.I64_ADD)),
		values:  []types.Value{types.I64(3)},
	},
	{
		program: program.New(instr.New(instr.I64_CONST, 5), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_SUB)),
		values:  []types.Value{types.I64(2)},
	},
	{
		program: program.New(instr.New(instr.I64_CONST, 4), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_MUL)),
		values:  []types.Value{types.I64(12)},
	},
	{
		program: program.New(instr.New(instr.I64_CONST, 10), instr.New(instr.I64_CONST, 2), instr.New(instr.I64_DIV_S)),
		values:  []types.Value{types.I64(5)},
	},
	{
		program: program.New(instr.New(instr.I64_CONST, 10), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_DIV_U)),
		values:  []types.Value{types.I64(3)},
	},
	{
		program: program.New(instr.New(instr.I64_CONST, 10), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_REM_S)),
		values:  []types.Value{types.I64(1)},
	},
	{
		program: program.New(instr.New(instr.I64_CONST, 10), instr.New(instr.I64_CONST, 3), instr.New(instr.I64_REM_U)),
		values:  []types.Value{types.I64(1)},
	},

	{
		program: program.New(instr.New(instr.F32_CONST, uint64(math.Float32bits(3.14)))),
		values:  []types.Value{types.F32(3.14)},
	},
	{
		program: program.New(instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2.5))), instr.New(instr.F32_ADD)),
		values:  []types.Value{types.F32(4.0)},
	},
	{
		program: program.New(instr.New(instr.F32_CONST, uint64(math.Float32bits(5.5))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))), instr.New(instr.F32_SUB)),
		values:  []types.Value{types.F32(3.5)},
	},
	{
		program: program.New(instr.New(instr.F32_CONST, uint64(math.Float32bits(4.0))), instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))), instr.New(instr.F32_MUL)),
		values:  []types.Value{types.F32(12.0)},
	},
	{
		program: program.New(instr.New(instr.F32_CONST, uint64(math.Float32bits(10.0))), instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))), instr.New(instr.F32_DIV)),
		values:  []types.Value{types.F32(5.0)},
	},

	{
		program: program.New(instr.New(instr.F64_CONST, math.Float64bits(3.14))),
		values:  []types.Value{types.F64(3.14)},
	},
	{
		program: program.New(instr.New(instr.F64_CONST, math.Float64bits(1.5)), instr.New(instr.F64_CONST, math.Float64bits(2.5)), instr.New(instr.F64_ADD)),
		values:  []types.Value{types.F64(4.0)},
	},
	{
		program: program.New(instr.New(instr.F64_CONST, math.Float64bits(5.5)), instr.New(instr.F64_CONST, math.Float64bits(2.0)), instr.New(instr.F64_SUB)),
		values:  []types.Value{types.F64(3.5)},
	},
	{
		program: program.New(instr.New(instr.F64_CONST, math.Float64bits(4.0)), instr.New(instr.F64_CONST, math.Float64bits(3.0)), instr.New(instr.F64_MUL)),
		values:  []types.Value{types.F64(12.0)},
	},
	{
		program: program.New(instr.New(instr.F64_CONST, math.Float64bits(10.0)), instr.New(instr.F64_CONST, math.Float64bits(2.0)), instr.New(instr.F64_DIV)),
		values:  []types.Value{types.F64(5.0)},
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
			err := vm.Run()
			require.NoError(b, err)

			for _, val := range tt.values {
				v, err := vm.Pop()
				require.NoError(b, err)
				require.Equal(b, val.Interface(), v.Interface())
			}
		})
	}
}
