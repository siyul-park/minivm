package interp_test

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	interp "github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type poolTrackedValue struct {
	closed int
}

func TestNewPool(t *testing.T) {
	t.Run("normalizes non-positive size", func(t *testing.T) {
		p := interp.NewPool(program.New([]instr.Instruction{instr.New(instr.NOP)}), 0)
		defer p.Close()
		first, err := p.Get(context.Background())
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = p.Get(ctx)
		require.ErrorIs(t, err, context.Canceled)
		p.Put(first)
	})
}

func TestPool_Get(t *testing.T) {

	t.Run("reuses an idle interpreter", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		p := interp.NewPool(prog, 1)
		defer p.Close()

		i1, err := p.Get(context.Background())
		require.NoError(t, err)
		p.Put(i1)

		i2, err := p.Get(context.Background())
		require.NoError(t, err)
		require.Same(t, i1, i2)
	})

	t.Run("returns context error while waiting", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		p := interp.NewPool(prog, 1)
		defer p.Close()

		i, err := p.Get(context.Background())
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})
		result := make(chan error, 1)
		go func() {
			close(started)
			_, err := p.Get(ctx)
			result <- err
		}()
		<-started
		cancel()
		require.ErrorIs(t, <-result, context.Canceled)

		p.Put(i)
	})

	t.Run("returns ErrPoolClosed once closed", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		p := interp.NewPool(prog, 1)
		require.NoError(t, p.Close())

		_, err := p.Get(context.Background())
		require.ErrorIs(t, err, interp.ErrPoolClosed)
	})

	t.Run("shares one native JIT runtime across pooled interpreters, compiling once", func(t *testing.T) {
		native(t)
		prog := fibFlatCallsProgram(t, 1000)
		profiler := prof.New()
		p := interp.NewPool(prog, 2, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer p.Close()

		first, err := p.Get(context.Background())
		require.NoError(t, err)
		second, err := p.Get(context.Background())
		require.NoError(t, err)

		require.NoError(t, first.Run(context.Background()))
		_, err = first.Pop()
		require.NoError(t, err)
		first.Flush()

		var compiles float64
		require.Eventually(t, func() bool {
			first.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles == 1
		}, 5*time.Second, time.Millisecond)

		require.NoError(t, second.Run(context.Background()))
		_, err = second.Pop()
		require.NoError(t, err)
		second.Flush()

		compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
		require.Equal(t, float64(1), compiles)

		p.Put(first)
		p.Put(second)
	})

	t.Run("pooled interpreters observing different callees converge", func(t *testing.T) {
		native(t)
		// Baseline and Optimized each pay one refute cycle, in separate waves.
		const calls = 5
		const rounds = 600
		const refute = 8 // interp/native.go's unexported refute constant.
		prog := applyGlobalProgram(t, calls)

		var wantInc, wantDec int32
		for i := int32(0); i < calls; i++ {
			wantInc += i + 1
			wantDec += i - 1
		}

		profiler := prof.New()
		p := interp.NewPool(prog, 2, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer p.Close()

		// a always calls inc and b always dec, through one dynamic CALL site.
		a, err := p.Get(context.Background())
		require.NoError(t, err)
		b, err := p.Get(context.Background())
		require.NoError(t, err)

		run := func(vm *interp.Interpreter, selector int32, want int32, round int) {
			require.NoError(t, vm.SetGlobal(1, types.BoxI32(selector)), "round %d", round)
			require.NoError(t, vm.Run(context.Background()), "round %d", round)
			got, err := vm.Pop()
			require.NoError(t, err, "round %d", round)
			require.Equal(t, types.I32(want), got, "round %d", round)
			vm.Flush()
		}

		var entriesAtHalf float64
		for round := 1; round <= rounds; round++ {
			run(a, 0, wantInc, round)
			run(b, 1, wantDec, round)
			a.Reset()
			b.Reset()

			if round == rounds/2 {
				entriesAtHalf, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			}
		}
		p.Put(a)
		p.Put(b)

		deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		require.LessOrEqual(t, deopts, float64(2*refute), "deopts must stay bounded, not run away")
		require.Greater(t, deopts, float64(0), "the divergence must have been exercised")

		entries, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
		require.Greater(t, entries, entriesAtHalf, "at least one interpreter keeps entering native code")
	})
}

func TestPool_Put(t *testing.T) {
	t.Run("returns interpreter to idle after resetting state", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1)})
		p := interp.NewPool(prog, 1)
		defer p.Close()

		i, err := p.Get(context.Background())
		require.NoError(t, err)
		require.NoError(t, i.Run(context.Background()))
		require.Equal(t, 1, i.Len())

		p.Put(i)

		i2, err := p.Get(context.Background())
		require.NoError(t, err)
		require.Same(t, i, i2)
		require.Equal(t, 0, i2.Len())
	})

	t.Run("nil is a no-op", func(t *testing.T) {
		p := interp.NewPool(program.New([]instr.Instruction{instr.New(instr.NOP)}), 1)
		defer p.Close()
		p.Put(nil)
	})
}

func TestPool_Close(t *testing.T) {
	t.Run("releases idle interpreters and is idempotent", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		p := interp.NewPool(prog, 1)

		i, err := p.Get(context.Background())
		require.NoError(t, err)
		p.Put(i)

		require.NoError(t, p.Close())
		require.NoError(t, p.Close())
	})

	t.Run("closes an outstanding interpreter when returned", func(t *testing.T) {
		p := interp.NewPool(program.New(nil), 1)
		vm, err := p.Get(context.Background())
		require.NoError(t, err)
		resource := &poolTrackedValue{}
		_, err = vm.Alloc(resource)
		require.NoError(t, err)

		require.NoError(t, p.Close())
		require.Zero(t, resource.closed)

		p.Put(vm)
		require.Equal(t, 1, resource.closed)
	})

}

func BenchmarkPool_Get(b *testing.B) {
	b.Run("Uncontended", func(b *testing.B) {
		pool := interp.NewPool(program.New(nil), 1)
		defer pool.Close()
		vm, err := pool.Get(context.Background())
		require.NoError(b, err)
		pool.Put(vm)

		var elapsed time.Duration
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			start := time.Now()
			vm, err = pool.Get(context.Background())
			elapsed += time.Since(start)
			require.NoError(b, err)
			pool.Put(vm)
		}
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
	})

	b.Run("Miss", func(b *testing.B) {
		var vm *interp.Interpreter
		var err error
		var elapsed time.Duration
		var bytes, allocs uint64
		var sampled bool
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			pool := interp.NewPool(program.New(nil), 1)
			var before runtime.MemStats
			if !sampled {
				runtime.ReadMemStats(&before)
			}
			start := time.Now()
			vm, err = pool.Get(context.Background())
			elapsed += time.Since(start)
			if !sampled {
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				bytes = after.TotalAlloc - before.TotalAlloc
				allocs = after.Mallocs - before.Mallocs
				sampled = true
			}
			require.NoError(b, err)
			pool.Put(vm)
			require.NoError(b, pool.Close())
		}
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
		b.ReportMetric(float64(bytes), "B/op")
		b.ReportMetric(float64(allocs), "allocs/op")
	})

	b.Run("ParallelRoundTrip", func(b *testing.B) {
		pool := interp.NewPool(program.New(nil), runtime.GOMAXPROCS(0))
		defer pool.Close()
		var failed atomic.Bool
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				vm, err := pool.Get(context.Background())
				if err != nil {
					failed.Store(true)
					continue
				}
				pool.Put(vm)
			}
		})
		require.False(b, failed.Load())
	})
}

func BenchmarkPool_Put(b *testing.B) {
	b.Run("Uncontended", func(b *testing.B) {
		pool := interp.NewPool(program.New(nil), 1)
		defer pool.Close()
		vm, err := pool.Get(context.Background())
		require.NoError(b, err)

		var elapsed time.Duration
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			start := time.Now()
			pool.Put(vm)
			elapsed += time.Since(start)
			vm, err = pool.Get(context.Background())
			require.NoError(b, err)
		}
		b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
		pool.Put(vm)
	})
}
func (*poolTrackedValue) Kind() types.Kind { return types.KindRef }
func (*poolTrackedValue) Type() types.Type { return types.TypeAny }
func (*poolTrackedValue) String() string   { return "tracked" }

func (v *poolTrackedValue) Close() error {
	v.closed++
	return nil
}
