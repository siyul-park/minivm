package interp_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestWithThresholdFuel(t *testing.T) {
	t.Run("exhausts fuel inside a native loop", func(t *testing.T) {
		native(t)
		// Fewer ticks of fuel than the loop has iterations: no tick mapping
		// lets the loop finish.
		prog := iterativeFibProgram(t, 1<<22)
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithFuel(1<<20))
		defer vm.Close()
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			vm.Flush()
			return runErr != interp.ErrFuelExhausted || nativeEntries(profiler) > 0
		})
		require.Equal(t, interp.ErrFuelExhausted, runErr)
	})

	t.Run("runs WithTick(1) exactly, without native code", func(t *testing.T) {
		native(t)
		prog := iterativeFibProgram(t, 1<<12)
		count := func(opts ...interp.Option) int {
			calls := 0
			vm := interp.New(prog, append(opts, interp.WithTick(1), interp.WithHook(func(*interp.Interpreter) error {
				calls++
				return nil
			}))...)
			defer vm.Close()
			for range 8 {
				require.NoError(t, vm.Run(context.Background()))
				vm.Reset()
			}
			return calls
		}
		want := count(interp.WithThreshold(-1))

		profiler := prof.New()
		got := count(interp.WithThreshold(1), interp.WithProfiler(profiler))

		require.Equal(t, want, got)
		require.Zero(t, nativeEntries(profiler))
	})

	t.Run("cancels a long self tail call", func(t *testing.T) {
		native(t)
		prog := tailRefProgram(t, math.MaxInt32)

		var runErr error
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			runErr = vm.Run(ctx)
			if !errors.Is(runErr, context.DeadlineExceeded) {
				return false
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			return exits > 0
		})
		require.ErrorIs(t, runErr, context.DeadlineExceeded)
	})

	t.Run("escapes guest handlers on a cancelled context", func(t *testing.T) {
		native(t)
		// The final call repeats natively until the timer cancels it.
		sum := sumFunction(t)
		b := instr.NewBuilder()
		loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Bind(start).Emit(instr.I32_CONST, uint64(2_000_000_000)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Br(start)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Try(start, end, catch, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sum), program.WithHandlers(b.Handlers()...))

		var runErr error
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				time.Sleep(time.Second)
				cancel()
			}()
			runErr = vm.Run(ctx)
			if !errors.Is(runErr, context.Canceled) {
				return false
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			return exits > 0
		})
		require.Error(t, runErr)
		require.ErrorIs(t, runErr, context.Canceled)
	})

}
