package vm

import (
	"context"
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
		program: program.New(instr.New(instr.I32_CONST, 42)),
		values:  []types.Value{types.I32(42)},
	},
	{
		program: program.New(instr.New(instr.I64_CONST, 123456789)),
		values:  []types.Value{types.I64(123456789)},
	},
	{
		program: program.New(instr.New(instr.F32_CONST, uint64(math.Float32bits(3.14)))),
		values:  []types.Value{types.F32(3.14)},
	},
	{
		program: program.New(instr.New(instr.F64_CONST, math.Float64bits(3.14))),
		values:  []types.Value{types.F64(3.14)},
	},
}

func TestVM_RunWithContext(t *testing.T) {
	for _, tt := range tests {
		t.Run(tt.program.String(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			vm := New(tt.program)
			err := vm.RunWithContext(ctx)
			require.NoError(t, err)

			for _, val := range tt.values {
				v, err := vm.Pop()
				require.NoError(t, err)
				require.Equal(t, val.Interface(), v.Interface())
			}
		})
	}
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

func BenchmarkVM_RunWithContext(b *testing.B) {
	for _, tt := range tests {
		b.Run(tt.program.String(), func(b *testing.B) {
			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			vm := New(tt.program)
			err := vm.RunWithContext(ctx)
			require.NoError(b, err)

			for _, val := range tt.values {
				v, err := vm.Pop()
				require.NoError(b, err)
				require.Equal(b, val.Interface(), v.Interface())
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
