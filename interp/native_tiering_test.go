package interp_test

import (
	"context"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestWithThresholdTiering(t *testing.T) {
	t.Run("enters a hot recursive function's native code", func(t *testing.T) {
		native(t)
		prog := fibCallsProgram(t, 20)
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("runs a hot function natively by default", func(t *testing.T) {
		native(t)
		prog := fibCallsProgram(t, 20)
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries = nativeEntries(profiler)
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("enters native code within one long profiled Run by default", func(t *testing.T) {
		native(t)
		prog := fibCallsProgram(t, 2048)
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.Pop()
		require.NoError(t, err)
		vm.Flush()

		require.Equal(t, want, got)
		require.Greater(t, nativeEntries(profiler), float64(0))
	})

	t.Run("enters native code within one long Run without safepoints by default", func(t *testing.T) {
		native(t)
		for _, prog := range []*program.Program{
			fibCallsProgram(t, 2048),
			iterativeFibProgram(t, 4<<20),
		} {
			want := runProgram(t, prog)

			// A tick past the Run's length: no safepoint ever runs.
			profiler := prof.New()
			vm := interp.New(prog, interp.WithProfiler(profiler), interp.WithTick(1<<30))
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			vm.Flush()
			require.NoError(t, vm.Close())

			require.Equal(t, want, got)
			require.Greater(t, nativeEntries(profiler), float64(0))
		}
	})

	t.Run("recompiles a retired function only after more calls than its first compile", func(t *testing.T) {
		native(t)
		// apply(x, fn) calls fn(x) through a dynamic CALL; inc and dec never
		// compile, so every ok Baseline compile is apply's.
		inc := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		refuse(inc)
		inc.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN))
		dec := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		refuse(dec)
		dec.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.RETURN))
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 7).Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithGlobals(types.TypeAny),
			program.WithConstants(applyFunction(), inc.MustBuild(), dec.MustBuild()))

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		// calls runs the program with callee constant c until apply has
		// compiled ok compiles in total, and returns how many calls it took.
		const step = 8
		var runErr error
		calls := func(c, ok int) int {
			callee, err := vm.Const(c)
			require.NoError(t, err)
			n := 0
			poll(t, func() bool {
				for range step {
					// Reset zeroes globals: the callee is set before every Run.
					if runErr = vm.SetGlobal(0, callee); runErr != nil {
						return true
					}
					if runErr = vm.Run(context.Background()); runErr != nil {
						return true
					}
					vm.Reset()
				}
				n += step
				vm.Flush()
				v, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
				return v >= float64(ok)
			})
			require.NoError(t, runErr)
			return n
		}
		first := calls(1, 1)
		second := calls(2, 2)

		require.Greater(t, second, 2*first)
	})

	t.Run("runs code shorter than a Go entry threaded by default", func(t *testing.T) {
		native(t)
		for _, prog := range []*program.Program{
			program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.NOP)}),
			program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(incFunction())),
		} {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithProfiler(profiler))
			// Far past waking and every threshold, with time to compile.
			for range 8 {
				for range 512 {
					require.NoError(t, vm.Run(context.Background()))
					vm.Reset()
				}
				time.Sleep(5 * time.Millisecond)
			}
			vm.Flush()
			require.NoError(t, vm.Close())

			require.Zero(t, nativeEntries(profiler))
		}
	})

	t.Run("runs a self tail call as a native loop", func(t *testing.T) {
		native(t)
		prog := tailRefProgram(t, 1000)
		threaded := interp.New(prog, interp.WithThreshold(-1))
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		held, err := threaded.Const(1)
		require.NoError(t, err)
		wantCount, err := threaded.RefCount(held.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		var runErr, popErr, countErr error
		var result types.Value
		var count int
		var entries float64
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			count, countErr = vm.RefCount(held.Ref())
			vm.Reset()
			vm.Flush()
			entries = nativeEntries(profiler)
			return entries >= 8
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, countErr)
		require.Equal(t, want, result)
		require.Equal(t, wantCount, count)

		before := entries
		for range 16 {
			require.NoError(t, vm.Run(context.Background()))
			result, err = vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, result)
			count, err = vm.RefCount(held.Ref())
			require.NoError(t, err)
			require.Equal(t, wantCount, count)
			vm.Reset()
		}
		vm.Flush()
		require.Greater(t, nativeEntries(profiler), before)
		deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		require.Zero(t, deopts)
	})

	t.Run("completes a deep self tail call without growing frames", func(t *testing.T) {
		native(t)
		prog := tailRefProgram(t, 1_000_000)
		want := runProgram(t, prog)

		vm := interp.New(prog, interp.WithThreshold(1), interp.WithFrame(4))
		defer vm.Close()
		for range 8 {
			require.NoError(t, vm.Run(context.Background()))
			result, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, result)
			vm.Reset()
			vm.Flush()
		}
	})

	t.Run("keeps a bridged loop-free module native", func(t *testing.T) {
		native(t)
		record := node()
		b := instr.NewBuilder()
		b.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.DROP)
		for range 16 {
			b.Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		}
		b.Emit(instr.I32_CONST, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithTypes(record), program.WithConstants(incFunction()))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()

		var result types.Value
		var runErr, popErr error
		var entries float64
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries >= 64
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
		require.GreaterOrEqual(t, entries, float64(64))

		recent := make([]float64, 0, 32)
		for range 32 {
			require.NoError(t, vm.Run(context.Background()))
			result, err = vm.Pop()
			require.NoError(t, err)
			vm.Flush()
			count, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			recent = append(recent, count)
			vm.Reset()
		}
		require.Equal(t, want, result)
		for i := 1; i < len(recent); i++ {
			require.Greater(t, recent[i], recent[i-1])
		}
		deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		require.Zero(t, deopts)
	})

	t.Run("retires a loop that only bridges and releases", func(t *testing.T) {
		native(t)
		record := node()
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 100).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithTypes(record))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()

		var result types.Value
		var runErr, popErr error
		var bridges float64
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return bridges > 1
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
		require.Greater(t, bridges, float64(0))

		for range 32 {
			require.NoError(t, vm.Run(context.Background()))
			result, err = vm.Pop()
			require.NoError(t, err)
			vm.Reset()
		}
		vm.Flush()
		before, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		for range 32 {
			require.NoError(t, vm.Run(context.Background()))
			result, err = vm.Pop()
			require.NoError(t, err)
			vm.Reset()
		}
		vm.Flush()
		after, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		require.Equal(t, want, result)
		require.Equal(t, before, after)
	})

	t.Run("refills a loop's budget at a safepoint", func(t *testing.T) {
		native(t)
		sum := sumFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, uint64(200000)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sum))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("retires a caller whose served calls outweigh its work", func(t *testing.T) {
		native(t)
		const runs = 16
		// held(x) = x never compiles, so a native caller always reaches it
		// through ExitCall.
		held := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		refuse(held)
		held.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
		served := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		served.Emit(instr.New(instr.I32_CONST, 0))
		for range 16 {
			served.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.CALL), instr.New(instr.I32_ADD))
		}
		served.Emit(instr.New(instr.RETURN))

		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(served.MustBuild(), held.MustBuild()))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		calls := func() float64 {
			vm.Flush()
			v, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return v
		}
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || calls() > 0
		})
		require.NoError(t, runErr)
		// Until the ledger retires served.
		poll(t, func() bool {
			before := calls()
			for range runs {
				if runErr = vm.Run(context.Background()); runErr != nil {
					return true
				}
				vm.Reset()
			}
			return calls() == before
		})
		require.NoError(t, runErr)

		before := calls()
		for range runs {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
		}
		require.Equal(t, before, calls())
	})

	t.Run("tiers a hot function up to Optimized", func(t *testing.T) {
		native(t)
		prog := fibFlatCallsProgram(t, 2000)
		want := runProgram(t, prog)

		var got types.Value
		var compiles float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("keeps a caller entering past a callee's caught traps", func(t *testing.T) {
		native(t)
		const rounds, batches = 32, 64
		b := instr.NewBuilder()
		loop, done, start, end, catch, next := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(rounds)).Emit(instr.I32_GE_S).BrIf(done)
		b.Bind(start).Emit(instr.I32_CONST, uint64(100)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Bind(end).Br(next)
		b.Bind(catch).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Bind(next)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1)
		b.Try(start, end, catch, 2)
		code, err := b.Assemble()
		require.NoError(t, err)
		rareBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).Locals(types.TypeI32, types.TypeI32)
		rareLoop, rareDone := rareBuilder.Label(), rareBuilder.Label()
		rareBuilder.Bind(rareLoop).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_GE_S)).BrIf(rareDone)
		rareBuilder.Emit(
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.I32_NE),
			instr.New(instr.CONST_GET, uint64(1)), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1),
		).Br(rareLoop)
		rareBuilder.Bind(rareDone).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
		rare := rareBuilder.MustBuild()
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32),
			program.WithConstants(rare, divFunction(t)), program.WithHandlers(b.Handlers()...))

		threaded := interp.New(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(rounds), want)
		counts := func(vm *interp.Interpreter) []int {
			var out []int
			for index := range 2 {
				c, err := vm.Const(index)
				require.NoError(t, err)
				count, err := vm.RefCount(c.Ref())
				require.NoError(t, err)
				out = append(out, count)
			}
			return out
		}
		wantCounts := counts(threaded)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) +
				metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}) > 0
		})
		require.NoError(t, runErr)

		before := entries()
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantCounts, counts(vm))
			vm.Reset()
		}
		require.GreaterOrEqual(t, entries()-before, float64(batches*rounds))
	})

	t.Run("keeps a protected hot loop native past caught traps", func(t *testing.T) {
		native(t)
		const rounds, batches = 32, 64
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(rounds)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, uint64(100)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, uint64(100)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		protectedBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).
			Locals(types.TypeI32, types.TypeI32, types.TypeString)
		protectedLoop, protectedDone, start, end, catch, next := protectedBuilder.Label(), protectedBuilder.Label(), protectedBuilder.Label(), protectedBuilder.Label(), protectedBuilder.Label(), protectedBuilder.Label()
		protectedBuilder.Emit(instr.New(instr.CONST_GET, 1), instr.New(instr.LOCAL_SET, 3))
		protectedBuilder.Bind(protectedLoop).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_GE_S)).BrIf(protectedDone)
		protectedBuilder.Bind(start).Emit(
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 6400),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.I32_CONST, 63), instr.New(instr.I32_AND),
			instr.New(instr.I32_DIV_S), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2))
		protectedBuilder.Bind(end).Br(next)
		protectedBuilder.Bind(catch).Emit(instr.New(instr.DROP), instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1000), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2))
		protectedBuilder.Bind(next).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1)).Br(protectedLoop)
		protectedBuilder.Bind(protectedDone).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
		protectedBuilder.Try(start, end, catch, 4)
		protected := protectedBuilder.MustBuild()
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(protected, types.String("owned")))

		threaded := interp.New(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		count := func(vm *interp.Interpreter) int {
			c, err := vm.Const(1)
			require.NoError(t, err)
			n, err := vm.RefCount(c.Ref())
			require.NoError(t, err)
			return n
		}
		wantCount := count(threaded)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) +
				metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}) > 0
		})
		require.NoError(t, runErr)

		before := entries()
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantCount, count(vm))
			vm.Reset()
		}
		require.GreaterOrEqual(t, entries()-before, float64(batches*rounds))
	})

	t.Run("enters a protected module loop and reports a trap outside it", func(t *testing.T) {
		native(t)
		const n = 200_000
		want := 0
		for i := range n {
			if d := (i + 1) & 63; d == 0 {
				want += 1000
			} else {
				want += 6400 / d
			}
		}
		b := instr.NewBuilder()
		loop, done, start, end, catch, next := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
		b.Bind(start)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 6400)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.I32_CONST, 63).Emit(instr.I32_AND)
		b.Emit(instr.I32_DIV_S).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Bind(end).Br(next)
		b.Bind(catch).Emit(instr.DROP).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1000).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Bind(next).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(want)).Emit(instr.I32_SUB).Emit(instr.I32_DIV_S)
		b.Try(start, end, catch, 2)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithHandlers(b.Handlers()...))
		wantErr := runProgramErr(t, prog, interp.WithThreshold(-1))
		require.ErrorIs(t, wantErr, interp.ErrDivideByZero)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		var runErr error
		var entries float64
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.Greater(t, entries, float64(0))
		require.EqualError(t, runErr, wantErr.Error())
	})

	t.Run("recompiles a null array's refuted container guard with its site bridged", func(t *testing.T) {
		native(t)
		const rounds, batches = 64, 8
		at := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.NewArrayType(types.TypeI32), types.TypeI32}, Returns: []types.Type{types.TypeI32},
		}).Locals(types.TypeI32)
		count, counted := at.Label(), at.Label()
		at.Bind(count).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 8), instr.New(instr.I32_GE_S)).BrIf(counted)
		at.Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2)).Br(count)
		at.Bind(counted).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.ARRAY_GET), instr.New(instr.RETURN))
		arr := types.NewArray(types.NewArrayType(types.TypeI32), types.BoxI32(1), types.BoxI32(2), types.BoxI32(3))

		b := instr.NewBuilder()
		loop, done, live, start, end, catch, next, null, picked := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 1).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(rounds)).Emit(instr.I32_GE_S).BrIf(done)
		// A read no run reaches keeps the module loop threaded.
		b.Emit(instr.I32_CONST, 1).BrIf(live)
		b.Emit(instr.REF_NULL).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.DROP)
		b.Bind(live).Bind(start)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 15).Emit(instr.I32_AND).Emit(instr.I32_CONST, 15).Emit(instr.I32_EQ).BrIf(null)
		b.Emit(instr.LOCAL_GET, 2).Br(picked)
		b.Bind(null).Emit(instr.LOCAL_GET, 3)
		b.Bind(picked).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Bind(end).Br(next)
		b.Bind(catch).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Bind(next)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1)
		b.Try(start, end, catch, 4)
		code, err := b.Assemble()
		require.NoError(t, err)
		array := types.NewArrayType(types.TypeI32)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, array, array),
			program.WithConstants(at.MustBuild(), arr), program.WithHandlers(b.Handlers()...))
		want := runProgram(t, prog)
		require.Equal(t, types.I32(rounds/16), want)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) +
				metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}) > 0
		})
		require.NoError(t, runErr)
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			vm.Reset()
			time.Sleep(time.Millisecond)
		}

		before := entries()
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
		}
		require.GreaterOrEqual(t, entries()-before, float64(batches*rounds))
	})

	t.Run("keeps a loop with a never-true null array read native", func(t *testing.T) {
		native(t)
		const runs = 32
		// A null []i32 local read only under a branch no iteration takes.
		b := instr.NewBuilder()
		loop, skip, done := b.Label(), b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 20_000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 0).Emit(instr.I32_GE_S).BrIf(skip)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Bind(skip)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 2)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.NewArrayType(types.TypeI32), types.TypeI32, types.TypeI32))
		want := runProgram(t, prog)

		var errs []error
		var results []types.Value
		var entries, deopts float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			errs, results = nil, nil
			for range runs {
				if err := vm.Run(context.Background()); err != nil {
					errs = append(errs, err)
					return true
				}
				result, err := vm.Pop()
				if err != nil {
					errs = append(errs, err)
					return true
				}
				results = append(results, result)
				vm.Reset()
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return entries >= runs/2
		})
		require.Empty(t, errs)
		require.Len(t, results, runs)
		for _, result := range results {
			require.Equal(t, want, result)
		}
		require.Less(t, deopts, float64(2))
	})

	t.Run("guards an array replaced inside an Optimized loop afresh", func(t *testing.T) {
		native(t)
		// The []i32 local is replaced by a fresh array of varying length every iteration.
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 200_000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 3).Emit(instr.I32_AND).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 3).Emit(instr.I32_AND).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 3).Emit(instr.I32_AND).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 2)
		code, err := b.Assemble()
		require.NoError(t, err)
		elem := types.NewArrayType(types.TypeI32)
		prog := program.New(code, program.WithLocals(elem, types.TypeI32, types.TypeI32), program.WithTypes(elem))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("reports no vm_jit metrics for a short first Run by default", func(t *testing.T) {
		profiler := prof.New()
		vm := interp.New(fibCallsProgram(t, 3), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		vm.Flush()

		for _, m := range profiler.Metrics() {
			require.NotContains(t, m.Name, "vm_jit_")
		}
	})

	t.Run("enters a module loop by OSR", func(t *testing.T) {
		native(t)
		prog := iterativeFibProgram(t, 200_000)
		want := runProgram(t, prog)

		var got types.Value
		var entries float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("compiles a module loop straight to Optimized", func(t *testing.T) {
		native(t)
		const n = 20000
		want := runProgram(t, concat(t, n))
		prog := concat(t, n)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.Pop()
		require.NoError(t, err)
		vm.Flush()

		require.Equal(t, want, got)
		compiles, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
		require.Greater(t, compiles, float64(0))
	})

	t.Run("tiers up a callee reached only from native code", func(t *testing.T) {
		native(t)
		const n = 200_000
		want := runProgram(t, fib(t, n))
		prog := fib(t, n)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.Pop()
		require.NoError(t, err)
		vm.Flush()

		require.Equal(t, want, got)
		// One Optimized compile is the header's own OSR unit; a second is
		// only possible if fib, called solely from that native loop, also
		// reached the promote threshold.
		compiles, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
		require.GreaterOrEqual(t, compiles, float64(2))
	})

	t.Run("tiers up a native-only callee across short Runs", func(t *testing.T) {
		native(t)
		// inc is entered once per iteration: its 1024 Baseline entries
		// arrive only after the loop itself runs natively.
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 100).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(incFunction()))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()

		var got types.Value
		var runErr, popErr error
		var compiles float64
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles >= 2
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.GreaterOrEqual(t, compiles, float64(2))
	})

	t.Run("enters a function's loop header mid-call by OSR", func(t *testing.T) {
		native(t)
		// warmCalls*warmEach back edges warm sum's own loop-header OSR site
		// across calls too few (warmCalls) to ever reach its entry-0 CALL
		// threshold on their own, so entry-0 never compiles: the final call
		// starts threaded and can only enter native code through OSR, mid-call,
		// once the site resolves.
		const threshold, warmCalls, warmEach, n = 1000, 20, 300, 200_000
		sum := sumFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warmCalls)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, uint64(warmEach)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sum))
		want := runProgram(t, prog)

		var got types.Value
		var entries float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(threshold), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("unwraps an OSR loop header that cannot compile", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		header, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0) // counter = 0
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1) // flag = 0
		b.Bind(header)
		b.Emit(instr.LOCAL_GET, 1).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)                // counter++
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).Emit(instr.LOCAL_SET, 1) // flag = counter >= 200_000
		b.Br(header)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32))
		want := runProgram(t, prog)

		var got types.Value
		var unsupported float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			unsupported, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "unsupported"})
			return unsupported > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		// A site submits its OSR unit once: the failed compile is never
		// resubmitted, however many further back edges the loop takes.
		require.Equal(t, float64(1), unsupported)

		// The failed site restores its threaded handler and stops polling:
		// a second Run on the same interpreter takes the same many back
		// edges again, and the metric does not move.
		require.NoError(t, vm.Run(context.Background()))
		got2, popErr2 := vm.Pop()
		require.NoError(t, popErr2)
		require.Equal(t, want, got2)
		vm.Flush()
		again, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "unsupported"})
		require.Equal(t, float64(1), again)
	})

	t.Run("retires an OSR site whose loop body always bridges", func(t *testing.T) {
		native(t)
		const n = 600
		b := instr.NewBuilder()
		header, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0) // i = 0
		b.Bind(header)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 0).Emit(instr.MAP_NEW_DEFAULT, 0).Emit(instr.MAP_KEYS).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(header)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		ctx := context.Background()

		// The header's own back edges cross its cadence within the first
		// Run and it retires (refute deopts) within a handful of Runs. The
		// ip-0 site's submit waits for the header to resolve (resolved),
		// then needs its own cadence (interval) of Runs for a submit
		// window and again for its first lookup to drain and publish the
		// compile: at n=600 back edges per Run that lands around Run 512.
		var got types.Value
		var runErr, popErr error
		var compiles float64
		poll(t, func() bool {
			runErr = vm.Run(ctx)
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles >= 2
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		// Two submits, two compiles: the header's own OSR unit, and module
		// code's own ip-0 unit (reachable prefix and loop both, including
		// the same always-bridging op); every native entry bridges immediately.
		require.Equal(t, float64(2), compiles)

		// The ip-0 unit's own bridge-and-deopt count starts fresh once
		// published (materializing hides the rest of a Run from its
		// observer, so it takes one Run per deopt, like the header's own
		// retirement): run comfortably past refute (8) deopts so it
		// retires too, then confirm two further runs report the same
		// steady-state exits count.
		for range 12 {
			require.NoError(t, vm.Run(ctx))
			_, err := vm.Pop()
			require.NoError(t, err)
			vm.Reset()
		}
		vm.Flush()
		exits, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})

		require.NoError(t, vm.Run(ctx))
		got2, err2 := vm.Pop()
		require.NoError(t, err2)
		require.Equal(t, want, got2)
		vm.Reset()
		vm.Flush()
		compilesAgain, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
		exitsAgain, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		require.Equal(t, float64(2), compilesAgain)
		require.Equal(t, exits, exitsAgain)
	})

	t.Run("reuses a resolved OSR site after Reset", func(t *testing.T) {
		native(t)
		prog := iterativeFibProgram(t, 200_000)
		want := runProgram(t, prog)

		profiler := prof.New()
		pool := interp.NewPool(prog, 2, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer pool.Close()
		first, err := pool.Get(context.Background())
		require.NoError(t, err)
		second, err := pool.Get(context.Background())
		require.NoError(t, err)

		var gotFirst, gotSecond types.Value
		var entries float64
		var runErr, firstErr, secondErr error
		poll(t, func() bool {
			runErr = first.Run(context.Background())
			if runErr != nil {
				return true
			}
			gotFirst, firstErr = first.Pop()
			if firstErr != nil {
				return true
			}
			runErr = second.Run(context.Background())
			if runErr != nil {
				return true
			}
			gotSecond, secondErr = second.Pop()
			if secondErr != nil {
				return true
			}
			first.Reset()
			second.Reset()
			first.Flush()
			second.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, firstErr)
		require.NoError(t, secondErr)
		require.Equal(t, want, gotFirst)
		require.Equal(t, want, gotSecond)
	})

	t.Run("recompiles a unit retired by a later-recorded site", func(t *testing.T) {
		native(t)
		prog := indirectFibCallsProgram(t, 20, 50)
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		run := func() {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
		}
		compiled := prof.Label{Key: "tier", Value: "baseline"}
		ok := prof.Label{Key: "outcome", Value: "ok"}
		poll(t, func() bool {
			err := vm.Run(context.Background())
			vm.Reset()
			return err != nil || metric("vm_jit_compiles_total", compiled, ok) >= 2
		})
		// Settled: the recompiled code traps at no site.
		for range 16 {
			run()
			time.Sleep(time.Millisecond)
		}
		deopts := metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		for range 16 {
			run()
		}
		require.Equal(t, deopts, metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}))
	})

	t.Run("compiles a unit before its dynamic sites ran", func(t *testing.T) {
		native(t)
		prog := indirectFibCallsProgram(t, 20, 50)
		want := runProgram(t, prog)

		var runErr, popErr error
		var got types.Value
		var unsupported, ok float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			unsupported, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "unsupported"})
			ok, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			return ok >= 1
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.Zero(t, unsupported)
	})

	t.Run("retires a speculated callee refuted by a second function", func(t *testing.T) {
		native(t)
		prog := applyProgram(t, 3000)

		wantVM := interp.New(applyProgram(t, 3000), interp.WithThreshold(-1))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)
		wantInc, err := wantVM.Const(1)
		require.NoError(t, err)
		wantIncRC, err := wantVM.RefCount(wantInc.Ref())
		require.NoError(t, err)
		wantDec, err := wantVM.Const(2)
		require.NoError(t, err)
		wantDecRC, err := wantVM.RefCount(wantDec.Ref())
		require.NoError(t, err)

		var runErr, popErr, rcErr error
		var got types.Value
		var gotIncRC, gotDecRC int
		var deopts float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			gotInc, err := vm.Const(1)
			if err != nil {
				popErr = err
				return true
			}
			gotIncRC, rcErr = vm.RefCount(gotInc.Ref())
			if rcErr != nil {
				return true
			}
			gotDec, err := vm.Const(2)
			if err != nil {
				popErr = err
				return true
			}
			gotDecRC, rcErr = vm.RefCount(gotDec.Ref())
			if rcErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return deopts >= 1
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, want, got)
		require.Equal(t, wantIncRC, gotIncRC)
		require.Equal(t, wantDecRC, gotDecRC)
		require.GreaterOrEqual(t, deopts, float64(1))
		// A retired site stops deopting long before its 3000 refuting calls end.
		require.Less(t, deopts, float64(3000))
	})

	t.Run("recompiles a polymorphic speculated site generic", func(t *testing.T) {
		native(t)
		const n, runs = 200, 16
		b := instr.NewBuilder()
		loop, done, inner, counted, high, chosen := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 3).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(counted)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Br(inner)
		b.Bind(counted)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.GLOBAL_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_AND).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(high)
		b.Emit(instr.CONST_GET, 1).Br(chosen)
		b.Bind(high).Emit(instr.CONST_GET, 2)
		b.Bind(chosen).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, types.TypeString, types.TypeI32), program.WithGlobals(types.TypeI32),
			program.WithConstants(relayDynamicFunction(), bumpFunction(1), bumpFunction(3), types.String("s")))

		threaded := interp.New(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.SetGlobal(0, types.BoxI32(1)))
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		deopts := func() float64 { return metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}) }
		calls := func() float64 { return metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"}) }
		// Reset clears globals: every polymorphic Run sets global 0 again.
		run := func() {
			require.NoError(t, vm.SetGlobal(0, types.BoxI32(1)))
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
		}
		// Monomorphic until relay compiles with its one observed callee.
		compiled := prof.Label{Key: "tier", Value: "baseline"}
		ok := prof.Label{Key: "outcome", Value: "ok"}
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_compiles_total", compiled, ok) >= 2
		})
		require.NoError(t, runErr)
		for range runs {
			require.NoError(t, vm.Run(context.Background()))
			vm.Reset()
		}

		turned := deopts()
		for range runs {
			run()
			time.Sleep(time.Millisecond)
		}
		// Fewer than refute: the moved feedback retires the code at once.
		require.Less(t, deopts()-turned, float64(8))

		settled, served := deopts(), calls()
		for range runs {
			run()
		}
		require.Equal(t, settled, deopts())
		require.Greater(t, calls(), served)
	})

	t.Run("keeps a caller entering while its closure callee's Baseline is pending", func(t *testing.T) {
		native(t)
		// The callee counts only on its ExitCalls, so at a threshold
		// above refute its Baseline is pending for more entries than a
		// refuted caller survives.
		prog := counterProgram(t)
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(2*8), interp.WithProfiler(profiler))
		defer vm.Close()

		metric := func(name string, label prof.Label) float64 {
			v, _ := profiler.Metric(name, label)
			return v
		}
		entry, call := prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "kind", Value: "call"}

		var got types.Value
		var runErr, popErr error
		var entries, called float64
		poll(t, func() bool {
			vm.Flush()
			entered, exited := metric("vm_jit_entries_total", entry), metric("vm_jit_exits_total", call)
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if got, popErr = vm.Pop(); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, called = metric("vm_jit_entries_total", entry)-entered, metric("vm_jit_exits_total", call)-exited
			return entries > 0 && called == 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.Positive(t, entries)
		require.Zero(t, called)
	})

	t.Run("compiles a closure body reached only from native code", func(t *testing.T) {
		native(t)
		counter := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Captures(types.TypeI32).
			Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.DUP), instr.New(instr.UPVAL_SET, 0), instr.New(instr.RETURN)).
			MustBuild()
		gb := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).Locals(types.TypeAny, types.TypeI32)
		loop, done := gb.Label(), gb.Label()
		gb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.LOCAL_SET, 1))
		gb.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 64), instr.New(instr.I32_GE_S)).BrIf(done)
		gb.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL), instr.New(instr.DROP))
		gb.Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2)).Br(loop)
		gb.Bind(done).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL), instr.New(instr.RETURN))
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 7).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(counter, gb.MustBuild()))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		baseline, optimized := prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "tier", Value: "optimized"}
		call := prof.Label{Key: "kind", Value: "call"}
		entered := func() float64 {
			return metric("vm_jit_entries_total", baseline) + metric("vm_jit_entries_total", optimized)
		}

		var got types.Value
		var runErr, popErr error
		var compiled, entries, called float64
		poll(t, func() bool {
			vm.Flush()
			before, exited := entered(), metric("vm_jit_exits_total", call)
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if got, popErr = vm.Pop(); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiled = metric("vm_jit_compiles_total", baseline, prof.Label{Key: "outcome", Value: "ok"})
			entries, called = entered()-before, metric("vm_jit_exits_total", call)-exited
			return compiled >= 2 && entries > 0 && called == 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.GreaterOrEqual(t, compiled, float64(2))
		require.Positive(t, entries)
		require.Zero(t, called)
	})

}
