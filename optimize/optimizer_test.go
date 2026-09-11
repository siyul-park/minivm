package optimize_test

import (
	"context"
	"strings"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	ssapass "github.com/siyul-park/minivm/internal/ssa/transform"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/optimize"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	optimizer := optimize.New(optimize.O2)
	require.Equal(t, optimize.O2, optimizer.Level())

	// New wires the analyses and the level's transform pipeline, so the
	// returned optimizer must be usable without any further registration.
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1)})
	optimized, err := optimizer.Optimize(prog)
	require.NoError(t, err)
	require.NotNil(t, optimized)
}

func TestOptimizer_Optimize(t *testing.T) {
	t.Run("O0 passthrough", func(t *testing.T) {
		o := optimize.New(optimize.O0)
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		result, err := o.Optimize(prog)
		require.NoError(t, err)
		require.Equal(t, prog.String(), result.String())
	})

	t.Run("O1 folds a constant window and drops what nothing reads", func(t *testing.T) {
		// The addition is dead. Folding alone would keep the folded constant,
		// because dropping a value the code pushes is not something a peephole
		// over an operand stack can decide; over SSA it is ordinary liveness,
		// so the whole phrase goes.
		prog := program.New(
			[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_ADD),
					instr.New(instr.DROP),
					instr.New(instr.I32_CONST, 5),
					instr.New(instr.RETURN),
				).MustBuild(),
			),
		)

		optimized, err := optimize.New(optimize.O1).Optimize(prog)
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))
		require.Equal(t, instr.Format(instr.Marshal([]instr.Instruction{
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.RETURN),
		})), instr.Format(optimized.Constants[0].(*types.Function).Code))

		vm := interp.New(optimized)
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(5), value)
	})

	t.Run("O1", func(t *testing.T) {
		o := optimize.New(optimize.O1)
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
		o := optimize.New(optimize.O2)
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

	t.Run("O3 preserves a top-level branch to the program end", func(t *testing.T) {
		b := program.NewBuilder()
		end := b.Label()
		b.Emit(instr.I32_CONST, 1)
		b.BrIf(end)
		b.Emit(instr.UNREACHABLE)
		b.Bind(end)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, program.Verify(prog))

		got, err := optimize.New(optimize.O3).Optimize(prog)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))

		vm := interp.New(got)
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
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

		before := build()
		optimized, err := optimize.New(optimize.O3).Optimize(build())
		require.NoError(t, err)

		fn := optimized.Constants[0].(*types.Function)
		require.Len(t, fn.Locals, 1)
		require.Contains(t, instr.Format(fn.Code), "local.tee")

		beforeVM := interp.New(before)
		defer beforeVM.Close()
		require.NoError(t, beforeVM.Run(context.Background()))
		beforeValue, err := beforeVM.Pop()
		require.NoError(t, err)

		optimizedVM := interp.New(optimized)
		defer optimizedVM.Close()
		require.NoError(t, optimizedVM.Run(context.Background()))
		optimizedValue, err := optimizedVM.Pop()
		require.NoError(t, err)
		require.Equal(t, beforeValue, optimizedValue)
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

		before := build()
		optimized, err := optimize.New(optimize.O3).Optimize(build())
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))

		beforeVM := interp.New(before)
		defer beforeVM.Close()
		require.NoError(t, beforeVM.Run(context.Background()))
		beforeValue, err := beforeVM.Pop()
		require.NoError(t, err)

		optimizedVM := interp.New(optimized)
		defer optimizedVM.Close()
		require.NoError(t, optimizedVM.Run(context.Background()))
		optimizedValue, err := optimizedVM.Pop()
		require.NoError(t, err)
		require.Equal(t, beforeValue, optimizedValue)
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

		before := build()
		optimized, err := optimize.New(optimize.O3).Optimize(build())
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))

		// Both arms drop what they compute, so the redundancy is not captured
		// and reloaded but removed outright, and only the merge's own addition
		// survives.
		fn := optimized.Constants[0].(*types.Function)
		require.Empty(t, fn.Locals)
		require.Equal(t, 1, strings.Count(instr.Format(fn.Code), "i32.add"))

		beforeVM := interp.New(before)
		defer beforeVM.Close()
		require.NoError(t, beforeVM.Run(context.Background()))
		beforeValue, err := beforeVM.Pop()
		require.NoError(t, err)

		optimizedVM := interp.New(optimized)
		defer optimizedVM.Close()
		require.NoError(t, optimizedVM.Run(context.Background()))
		optimizedValue, err := optimizedVM.Pop()
		require.NoError(t, err)
		require.Equal(t, beforeValue, optimizedValue)
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

		before := build()
		optimized, err := optimize.New(optimize.O3).Optimize(build())
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))
		require.Len(t, optimized.Types, 1)

		fn := optimized.Constants[0].(*types.Function)
		require.Len(t, fn.Handlers, 1)

		beforeVM := interp.New(before)
		defer beforeVM.Close()
		require.NoError(t, beforeVM.Run(context.Background()))
		beforeValue, err := beforeVM.Pop()
		require.NoError(t, err)

		optimizedVM := interp.New(optimized)
		defer optimizedVM.Close()
		require.NoError(t, optimizedVM.Run(context.Background()))
		optimizedValue, err := optimizedVM.Pop()
		require.NoError(t, err)
		require.Equal(t, beforeValue, optimizedValue)
	})

	parity := []struct {
		name string
		prog *program.Program
	}{
		{
			name: "constant arithmetic",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 20),
				instr.New(instr.I32_CONST, 22),
				instr.New(instr.I32_ADD),
			}),
		},
		{
			name: "conditional branch",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_IF, 5),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 7),
			}),
		},
		{
			name: "array access",
			prog: program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_GET),
			}, program.WithConstants(types.TypedArray[int32]{10, 20, 30})),
		},
	}
	for _, tt := range parity {
		t.Run("semantic parity "+tt.name, func(t *testing.T) {
			original := interp.New(tt.prog, interp.WithThreshold(-1))
			defer original.Close()
			require.NoError(t, original.Run(context.Background()))
			var want []types.Value
			for original.Len() > 0 {
				value, err := original.Pop()
				require.NoError(t, err)
				want = append(want, value)
			}

			optimized, err := optimize.New(optimize.O3).Optimize(tt.prog)
			require.NoError(t, err)
			got := interp.New(optimized, interp.WithThreshold(-1))
			defer got.Close()
			require.NoError(t, got.Run(context.Background()))
			var values []types.Value
			for got.Len() > 0 {
				value, err := got.Pop()
				require.NoError(t, err)
				values = append(values, value)
			}
			require.Equal(t, want, values)
		})
	}
}

func TestOptimizer_Level(t *testing.T) {
	for _, level := range []optimize.Level{optimize.O0, optimize.O1, optimize.O2, optimize.O3} {
		require.Equal(t, level, optimize.New(level).Level())
	}
}

func TestOptimizer_Add(t *testing.T) {
	pipeline := pass.NewPipeline[*ssa.Function]()
	pipeline.Add(ssapass.NewFoldPass())

	o := optimize.New(optimize.O0)
	o.Add(transform.NewSSAPass(pipeline))

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
