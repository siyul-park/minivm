package optimize

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func FuzzOptimizerParity(f *testing.F) {
	f.Add(byte(0), int32(20), int32(22))
	f.Add(byte(1), int32(-10), int32(3))
	f.Add(byte(4), int32(7), int32(0))

	f.Fuzz(func(t *testing.T, operation byte, left, right int32) {
		ops := []instr.Opcode{
			instr.I32_ADD,
			instr.I32_SUB,
			instr.I32_MUL,
			instr.I32_XOR,
			instr.I32_EQ,
			instr.I32_LT_S,
		}
		op := ops[int(operation)%len(ops)]
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, uint64(uint32(left))),
			instr.New(instr.I32_CONST, uint64(uint32(right))),
			instr.New(op),
		})
		require.NoError(t, program.Verify(prog))

		run := func(prog *program.Program) types.Value {
			vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			value, err := vm.Pop()
			require.NoError(t, err)
			return value
		}

		want := run(prog)
		optimized, err := NewOptimizer(O3).Optimize(prog)
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))
		require.Equal(t, want, run(optimized))
	})
}
