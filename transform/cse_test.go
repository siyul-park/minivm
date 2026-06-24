package transform

import (
	"testing"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestCommonSubexpressionEliminationPass_Run(t *testing.T) {
	i32 := func(n int) []types.Type {
		ls := make([]types.Type, n)
		for i := range ls {
			ls[i] = types.TypeI32
		}
		return ls
	}

	t.Run("reuses an existing local home", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).WithLocals(i32(3)...).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
		).MustBuild()
		prog := program.New(nil, program.WithConstants(fn))

		runCSE(t, prog)

		want := instr.Marshal([]instr.Instruction{
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 2),
		})
		require.Equal(t, instr.Format(want), instr.Format(fn.Code))
		require.Len(t, fn.Locals, 3)
	})

	t.Run("captures into a fresh local", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).WithLocals(i32(2)...).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
		).MustBuild()
		prog := program.New(nil, program.WithConstants(fn))

		runCSE(t, prog)

		want := instr.Marshal([]instr.Instruction{
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_TEE, 2),
			instr.New(instr.LOCAL_GET, 2),
		})
		require.Equal(t, instr.Format(want), instr.Format(fn.Code))
		require.Equal(t, []types.Type{types.TypeI32, types.TypeI32, types.TypeI32}, fn.Locals)
	})

	t.Run("top-level reuses a home in place", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
		})

		runCSE(t, prog)

		want := instr.Marshal([]instr.Instruction{
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 2),
		})
		require.Equal(t, instr.Format(want), instr.Format(prog.Code))
	})

	t.Run("top-level cannot capture a fresh local", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
		})
		before := instr.Format(prog.Code)

		runCSE(t, prog)
		require.Equal(t, before, instr.Format(prog.Code))
	})
}

func runCSE(t *testing.T, prog *program.Program) {
	t.Helper()
	m := pass.NewManager()
	pass.Register(m, analysis.NewBasicBlocksAnalysis())
	pass.Register(m, analysis.NewValueNumberingAnalysis())
	_, err := NewCommonSubexpressionEliminationPass().Run(m, prog)
	require.NoError(t, err)
}
