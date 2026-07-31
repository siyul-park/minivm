package transform_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	transform "github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewGVNPass(t *testing.T) {
	require.NotNil(t, transform.NewGVNPass())
}

func TestGVNPass_Run(t *testing.T) {
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
		fn.Handlers = []instr.Handler{{Start: 5, End: 10, Catch: 11}}
		prog := program.New(nil, program.WithConstants(fn))

		manager := pass.NewManager()
		pass.Register(manager, analysis.NewBlocksAnalysis())
		pass.Register(manager, analysis.NewGVNAnalysis())
		_, err := transform.NewGVNPass().Run(manager, prog)
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
		require.Equal(t, []instr.Handler{{Start: 7, End: 9, Catch: 10, Depth: 1}}, fn.Handlers)
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
		pass.Register(manager, analysis.NewBlocksAnalysis())
		pass.Register(manager, analysis.NewGVNAnalysis())
		_, err := transform.NewGVNPass().Run(manager, prog)
		require.NoError(t, err)

		want := types.NewFunctionBuilder(i32t).Locals(types.TypeI32)
		wantThen, wantMerge := want.Label(), want.Label()
		want.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_GT_S))
		want.BrIf(wantThen)
		want.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_TEE, 2), instr.New(instr.DROP))
		want.Br(wantMerge)
		want.Bind(wantThen)
		want.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_TEE, 2), instr.New(instr.DROP))
		want.Bind(wantMerge)
		want.Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
		require.Equal(t, want.MustBuild(), fn)
		require.NoError(t, program.Verify(prog))
	})

	t.Run("leaves the function unchanged when branch repair overflows", func(t *testing.T) {
		typ := &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32, types.TypeI32}}
		body := types.NewFunctionBuilder(typ)
		then, merge := body.Label(), body.Label()
		body.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_GT_S))
		body.BrIf(then)
		body.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.DROP))
		body.Br(merge)
		body.Bind(then)
		body.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.DROP))
		body.Bind(merge)
		body.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.DROP))
		bodyCode := body.MustBuild().Code

		prefix := instr.Marshal([]instr.Instruction{
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.BR_IF, uint64(^uint16(0)>>1)),
		})
		branchTarget := len(prefix) + int(^uint16(0)>>1)
		code := append(prefix, bodyCode...)
		code = append(code, make([]byte, branchTarget-len(code))...)
		code = append(code, instr.Marshal([]instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)})...)
		fn := &types.Function{Typ: typ, Code: code}
		prog := program.New(nil, program.WithConstants(fn))
		before := append([]byte(nil), fn.Code...)

		manager := pass.NewManager()
		pass.Register(manager, analysis.NewBlocksAnalysis())
		pass.Register(manager, analysis.NewGVNAnalysis())
		_, err := transform.NewGVNPass().Run(manager, prog)
		require.NoError(t, err)

		require.True(t, bytes.Equal(before, fn.Code))
		require.Empty(t, fn.Locals)
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
		pass.Register(manager, analysis.NewBlocksAnalysis())
		pass.Register(manager, analysis.NewGVNAnalysis())
		_, err := transform.NewGVNPass().Run(manager, prog)
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
		pass.Register(manager, analysis.NewBlocksAnalysis())
		pass.Register(manager, analysis.NewGVNAnalysis())
		_, err = transform.NewGVNPass().Run(manager, prog)
		require.NoError(t, err)
		after := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer after.Close()
		require.NoError(t, after.Run(context.Background()))
		got, err := after.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
}
