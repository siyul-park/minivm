package optimize_test

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/optimize"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func FuzzOptimizerParity(f *testing.F) {
	f.Add(byte(0), int64(20), int64(22))
	f.Add(byte(1), int64(-10), int64(3))
	f.Add(byte(4), int64(7), int64(0))
	f.Add(byte(9), int64(-3750763034362895579), int64(1)<<62)
	f.Add(byte(11), int64(1)<<62, int64(3))

	f.Fuzz(func(t *testing.T, operation byte, left, right int64) {
		// An even operation picks an i32 op over the operands' low halves,
		// an odd one an i64 op.
		ops := [2][]instr.Opcode{
			{instr.I32_ADD, instr.I32_SUB, instr.I32_MUL, instr.I32_XOR, instr.I32_EQ, instr.I32_LT_S},
			{instr.I64_ADD, instr.I64_SUB, instr.I64_MUL, instr.I64_XOR, instr.I64_SHL, instr.I64_EQ, instr.I64_LT_S},
		}[operation&1]
		code, a, b := instr.I64_CONST, uint64(left), uint64(right)
		if operation&1 == 0 {
			code, a, b = instr.I32_CONST, uint64(uint32(left)), uint64(uint32(right))
		}
		prog := program.New([]instr.Instruction{
			instr.New(code, a), instr.New(code, b), instr.New(ops[int(operation/2)%len(ops)])})
		require.NoError(t, program.Verify(prog))

		run := func(prog *program.Program) types.Value {
			vm := interp.New(prog, interp.WithTick(1))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			value, err := vm.Pop()
			require.NoError(t, err)
			return value
		}

		want := run(prog)
		optimized, err := optimize.New(optimize.O3).Optimize(prog)
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))
		require.Equal(t, want, run(optimized))
	})
}
