package optimize

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestOptimizer_Level(t *testing.T) {
	o := NewOptimizer(O0)
	require.Equal(t, O0, o.Level())
}

func TestOptimizer_AddPass(t *testing.T) {
	o := NewOptimizer(O0)
	o.AddPass(transform.NewConstantFoldingPass())

	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1),
		instr.New(instr.I32_CONST, 2),
		instr.New(instr.I32_ADD),
	})
	before := prog.String()

	got, err := o.Optimize(prog)
	require.NoError(t, err)
	require.NotEqual(t, before, got.String())
}

func TestOptimizer_Optimize(t *testing.T) {
	t.Run("O0 passthrough", func(t *testing.T) {
		o := NewOptimizer(O0)
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		result, err := o.Optimize(prog)
		require.NoError(t, err)
		require.Equal(t, prog.String(), result.String())
	})

	t.Run("O1", func(t *testing.T) {
		o := NewOptimizer(O1)
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 20),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{
					Params:  []types.Type{types.TypeI64},
					Returns: []types.Type{types.TypeI64},
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
				).Build(),
			),
		)
		_, err := o.Optimize(prog)
		require.NoError(t, err)
	})

	t.Run("O2", func(t *testing.T) {
		o := NewOptimizer(O2)
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 20),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{
					Params:  []types.Type{types.TypeI64},
					Returns: []types.Type{types.TypeI64},
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
				).Build(),
			),
		)
		_, err := o.Optimize(prog)
		require.NoError(t, err)
	})
}
