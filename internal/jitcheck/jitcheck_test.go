// Package jitcheck is a temporary black-box harness proving the JIT still
// lowers, spills, links, and executes through interp's public API.
package jitcheck_test

import (
	"context"
	"math"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

func TestJIT(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	t.Run("iterative loop", func(t *testing.T) {
		b := program.NewBuilder()
		loop := b.Label()
		b.Locals(types.TypeI32, types.TypeF64).
			Emit(instr.I32_CONST, 0).
			Emit(instr.LOCAL_SET, 0).
			Emit(instr.F64_CONST, 0).
			Emit(instr.LOCAL_SET, 1).
			Bind(loop).
			Emit(instr.LOCAL_GET, 1).
			Emit(instr.F64_CONST, math.Float64bits(1)).
			Emit(instr.F64_ADD).
			Emit(instr.LOCAL_SET, 1).
			Emit(instr.LOCAL_GET, 0).
			Emit(instr.I32_CONST, 1).
			Emit(instr.I32_ADD).
			Emit(instr.LOCAL_TEE, 0).
			Emit(instr.I32_CONST, 64).
			Emit(instr.I32_LT_S).
			BrIf(loop).
			Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		profiler := prof.New()
		func() {
			i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer func() { require.NoError(t, i.Close()) }()
			for round := range 24 {
				i.Reset()
				require.NoError(t, i.Run(context.Background()))
				value, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, types.F64(64), value, "round %d", round)
			}
		}()

		emits, ok := profiler.Metric("vm_jit_emits_total")
		require.True(t, ok)
		require.GreaterOrEqual(t, emits, float64(1))
	})

	t.Run("recursive calls", func(t *testing.T) {
		// A self-recursive trace links a BL back to the internal head label,
		// the one backward branch the assembler must resolve inside a build.
		fb := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Params(types.TypeI32)
		base := fb.Label()
		fn := fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).
			BrIf(base).
			Emit(
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
				instr.New(instr.I32_ADD), instr.New(instr.RETURN),
			).
			Bind(base).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
			MustBuild()
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 20),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn),
		)

		profiler := prof.New()
		func() {
			i := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer func() { require.NoError(t, i.Close()) }()
			for round := range 8 {
				i.Reset()
				require.NoError(t, i.Run(context.Background()))
				value, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I32(6765), value, "round %d", round)
			}
		}()
	})

	t.Run("wide live ranges", func(t *testing.T) {
		// 96 simultaneously live locals oversubscribe the integer bank, so a
		// correct sum proves register allocation and any spill frame the
		// lowering needs stay balanced end to end.
		const locals = 96

		locality := make([]types.Type, locals)
		for i := range locality {
			locality[i] = types.TypeI64
		}
		b := program.NewBuilder()
		b.Locals(locality...)
		for i := range locals {
			b.Emit(instr.I64_CONST, uint64(i+1)).Emit(instr.LOCAL_SET, uint64(i))
		}
		b.Emit(instr.LOCAL_GET, 0)
		for i := 1; i < locals; i++ {
			b.Emit(instr.LOCAL_GET, uint64(i)).Emit(instr.I64_ADD)
		}
		prog, err := b.Build()
		require.NoError(t, err)

		want := int64(0)
		for i := range locals {
			want += int64(i + 1)
		}

		profiler := prof.New()
		func() {
			i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer func() { require.NoError(t, i.Close()) }()
			for round := range 24 {
				i.Reset()
				require.NoError(t, i.Run(context.Background()))
				value, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, types.I64(want), value, "round %d", round)
			}
		}()

		emits, ok := profiler.Metric("vm_jit_emits_total")
		require.True(t, ok)
		require.GreaterOrEqual(t, emits, float64(1))
	})
}
