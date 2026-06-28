package optimize

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
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
				).MustBuild(),
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
				).MustBuild(),
			),
		)
		_, err := o.Optimize(prog)
		require.NoError(t, err)
	})

	t.Run("O3 eliminates a common subexpression and preserves semantics", func(t *testing.T) {
		build := func() *program.Program {
			return program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_CONST, 4),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(
					types.NewFunctionBuilder(&types.FunctionType{
						Params:  []types.Type{types.TypeI32, types.TypeI32},
						Returns: []types.Type{types.TypeI32},
					}).Emit(
						instr.New(instr.LOCAL_GET, 0),
						instr.New(instr.LOCAL_GET, 1),
						instr.New(instr.I32_ADD),
						instr.New(instr.LOCAL_GET, 0),
						instr.New(instr.LOCAL_GET, 1),
						instr.New(instr.I32_ADD),
						instr.New(instr.I32_ADD),
						instr.New(instr.RETURN),
					).MustBuild(),
				),
			)
		}

		optimized, err := NewOptimizer(O3).Optimize(build())
		require.NoError(t, err)

		fn := optimized.Constants[0].(*types.Function)
		require.Len(t, fn.Locals, 1, "common subexpression captured into a fresh local")
		require.Contains(t, instr.Format(fn.Code), "local.tee")

		require.Equal(t, run(t, build()), run(t, optimized))
	})

	t.Run("O3 repairs branch offsets after shrinking", func(t *testing.T) {
		build := func() *program.Program {
			fb := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32, types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			})
			l := fb.Label()
			fb.Emit(
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.I32_ADD),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.I32_ADD),
				instr.New(instr.I32_ADD),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_GT_S),
			)
			fb.BrIf(l)
			fb.Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN))
			fb.Bind(l)
			fb.Emit(instr.New(instr.RETURN))

			return program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_CONST, 4),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(fb.MustBuild()),
			)
		}

		optimized, err := NewOptimizer(O3).Optimize(build())
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))
		require.Equal(t, run(t, build()), run(t, optimized))
	})

	t.Run("O3 eliminates a redundancy across a control-flow merge", func(t *testing.T) {
		build := func() *program.Program {
			fb := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32, types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			})
			then, merge := fb.Label(), fb.Label()
			fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_GT_S))
			fb.BrIf(then)
			fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.DROP))
			fb.Br(merge)
			fb.Bind(then)
			fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.DROP))
			fb.Bind(merge)
			fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN))

			return program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_CONST, 4),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(fb.MustBuild()),
			)
		}

		optimized, err := NewOptimizer(O3).Optimize(build())
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))

		fn := optimized.Constants[0].(*types.Function)
		require.Len(t, fn.Locals, 1, "merge redundancy captured into a fresh local")
		require.Equal(t, run(t, build()), run(t, optimized))
	})

	t.Run("O3 preserves types and handlers", func(t *testing.T) {
		build := func() *program.Program {
			fb := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32, types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			})
			start, end, catch := fb.Label(), fb.Label(), fb.Label()
			fb.Bind(start)
			fb.Emit(
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.I32_ADD),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.I32_ADD),
				instr.New(instr.I32_ADD),
			)
			fb.Bind(end)
			fb.Emit(instr.New(instr.RETURN))
			fb.Bind(catch)
			fb.Emit(instr.New(instr.DROP), instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN))
			fb.Try(start, end, catch, 2)

			return program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.ARRAY_NEW_DEFAULT, 0),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_CONST, 4),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CALL),
				},
				program.WithConstants(fb.MustBuild()),
				program.WithTypes(types.NewArrayType(types.TypeI32)),
			)
		}

		optimized, err := NewOptimizer(O3).Optimize(build())
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))
		require.Len(t, optimized.Types, 1, "a referenced program type is not dropped")

		fn := optimized.Constants[0].(*types.Function)
		require.Len(t, fn.Handlers, 1, "function handlers are not dropped")
		require.Equal(t, run(t, build()), run(t, optimized))
	})
}

func run(t *testing.T, prog *program.Program) types.Value {
	t.Helper()
	i := interp.New(prog)
	defer i.Close()
	require.NoError(t, i.Run(context.Background()))
	v, err := i.Pop()
	require.NoError(t, err)
	return v
}
