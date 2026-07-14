package transform

import (
	"context"
	"strings"
	"testing"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewGlobalValueNumberingPass(t *testing.T) {
	require.NotNil(t, NewGlobalValueNumberingPass())
}

func TestGlobalValueNumberingPass_Run(t *testing.T) {
	i32t := &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}}

	t.Run("captures a within-block subexpression like CSE", func(t *testing.T) {
		fn := types.NewFunctionBuilder(i32t).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New(nil, program.WithConstants(fn))

		manager := pass.NewManager()
		pass.Register(manager, analysis.NewBasicBlocksAnalysis())
		pass.Register(manager, analysis.NewGlobalValueNumberingAnalysis())
		_, err := NewGlobalValueNumberingPass().Run(manager, prog)
		require.NoError(t, err)

		want := instr.Marshal([]instr.Instruction{
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_TEE, 2),
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		})
		require.Equal(t, instr.Format(want), instr.Format(fn.Code))
		require.Len(t, fn.Locals, 1)
		require.NoError(t, program.Verify(prog))
	})

	t.Run("captures a value recomputed at a control-flow merge", func(t *testing.T) {
		fb := types.NewFunctionBuilder(i32t)
		then, merge := fb.Label(), fb.Label()
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_GT_S))
		fb.BrIf(then)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.DROP))
		fb.Br(merge)
		fb.Bind(then)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.DROP))
		fb.Bind(merge)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN))
		fn := fb.MustBuild()
		prog := program.New(nil, program.WithConstants(fn))

		manager := pass.NewManager()
		pass.Register(manager, analysis.NewBasicBlocksAnalysis())
		pass.Register(manager, analysis.NewGlobalValueNumberingAnalysis())
		_, err := NewGlobalValueNumberingPass().Run(manager, prog)
		require.NoError(t, err)

		code := instr.Format(fn.Code)
		require.Equal(t, 2, strings.Count(code, "local.tee"), "captured at both arms")
		require.Len(t, fn.Locals, 1)
		require.NoError(t, program.Verify(prog))
	})

	t.Run("top-level body cannot capture across blocks", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.DROP),
			instr.New(instr.BR, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.DROP),
		})
		before := instr.Format(prog.Code)

		manager := pass.NewManager()
		pass.Register(manager, analysis.NewBasicBlocksAnalysis())
		pass.Register(manager, analysis.NewGlobalValueNumberingAnalysis())
		_, err := NewGlobalValueNumberingPass().Run(manager, prog)
		require.NoError(t, err)
		require.Equal(t, before, instr.Format(prog.Code), "no locals to allocate at the top level")
	})

	t.Run("preserves execution", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New(
			[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
			program.WithConstants(fn),
		)
		before := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer before.Close()
		require.NoError(t, before.Run(context.Background()))
		want, err := before.Pop()
		require.NoError(t, err)

		manager := pass.NewManager()
		pass.Register(manager, analysis.NewBasicBlocksAnalysis())
		pass.Register(manager, analysis.NewGlobalValueNumberingAnalysis())
		_, err = NewGlobalValueNumberingPass().Run(manager, prog)
		require.NoError(t, err)
		after := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer after.Close()
		require.NoError(t, after.Run(context.Background()))
		got, err := after.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
}
