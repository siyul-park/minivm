package interp_test

import (
	"context"
	"runtime"
	"sync"
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
		p := interp.NewPool(prog, 2, interp.WithThreshold(1), interp.WithProfiler(profiler))
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
		poll(t, func() bool {
			first.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles == 1
		})

		require.NoError(t, second.Run(context.Background()))
		_, err = second.Pop()
		require.NoError(t, err)
		second.Flush()

		compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
		require.Equal(t, float64(1), compiles)

		p.Put(first)
		p.Put(second)
	})

	t.Run("pooled interpreters observing different callees match threaded with bounded deopts", func(t *testing.T) {
		native(t)
		const calls = 5
		const rounds = 600
		prog := applyGlobalProgram(t, calls)

		var wantInc, wantDec int32
		for i := int32(0); i < calls; i++ {
			wantInc += i + 1
			wantDec += i - 1
		}

		profiler := prof.New()
		p := interp.NewPool(prog, 2, interp.WithThreshold(1), interp.WithProfiler(profiler))
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

		for round := 1; round <= rounds; round++ {
			run(a, 0, wantInc, round)
			run(b, 1, wantDec, round)
			a.Reset()
			b.Reset()
		}
		p.Put(a)
		p.Put(b)

		deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		// Retired sites stop deopting long before every round does.
		require.Less(t, deopts, float64(2*rounds))
		require.Greater(t, deopts, float64(0))
	})

	t.Run("pooled interpreters entering and retiring shared code concurrently match threaded", func(t *testing.T) {
		native(t)
		const calls = 200
		const rounds = 300
		prog := applyGlobalProgram(t, calls)

		var wantInc, wantDec int32
		for i := int32(0); i < calls; i++ {
			wantInc += i + 1
			wantDec += i - 1
		}

		p := interp.NewPool(prog, 2, interp.WithThreshold(1))
		defer p.Close()

		// a always calls inc and b always dec, through one dynamic CALL site:
		// each refutes and retires code the other may be running.
		a, err := p.Get(context.Background())
		require.NoError(t, err)
		b, err := p.Get(context.Background())
		require.NoError(t, err)

		var got [2][]types.Value
		var errs [2]error
		var wg sync.WaitGroup
		for k, vm := range []*interp.Interpreter{a, b} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range rounds {
					if errs[k] = vm.SetGlobal(1, types.BoxI32(int32(k))); errs[k] != nil {
						return
					}
					if errs[k] = vm.Run(context.Background()); errs[k] != nil {
						return
					}
					var v types.Value
					if v, errs[k] = vm.Pop(); errs[k] != nil {
						return
					}
					got[k] = append(got[k], v)
					vm.Reset()
				}
			}()
		}
		wg.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		for round := range rounds {
			require.Equal(t, types.I32(wantInc), got[0][round], "round %d", round)
			require.Equal(t, types.I32(wantDec), got[1][round], "round %d", round)
		}

		p.Put(a)
		p.Put(b)
	})

	t.Run("a pooled interpreter promotes code another interpreter drained", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(sumFunction(t)))

		profiler := prof.New()
		p := interp.NewPool(prog, 2, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer p.Close()
		compiles := func(tier string) float64 {
			v, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: tier}, prof.Label{Key: "outcome", Value: "ok"})
			return v
		}
		call := func(vm *interp.Interpreter) error {
			defer vm.Reset()
			if err := vm.Run(context.Background()); err != nil {
				return err
			}
			_, err := vm.Pop()
			vm.Flush()
			return err
		}

		// first alone drains the Baseline compile, calling too rarely to promote it.
		first, err := p.Get(context.Background())
		require.NoError(t, err)
		poll(t, func() bool {
			err = call(first)
			return err != nil || compiles("baseline") == 1
		})
		require.NoError(t, err)

		// second never drains a Baseline job; its own calls must promote it.
		second, err := p.Get(context.Background())
		require.NoError(t, err)
		poll(t, func() bool {
			err = call(second)
			return err != nil || compiles("optimized") >= 1
		})
		require.NoError(t, err)

		p.Put(first)
		p.Put(second)
	})

	t.Run("pooled interpreters alternating Runs publish code after about threshold total entries", func(t *testing.T) {
		native(t)
		// Loop-free module code: its only OSR site is ip 0, entry-site
		// threshold max(n, 2), cadence 1 (Entries table).
		b := instr.NewBuilder()
		big, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 7).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 5).Emit(instr.I32_GT_S).BrIf(big)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2).Emit(instr.I32_MUL).Br(done)
		b.Bind(big).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 3).Emit(instr.I32_MUL)
		b.Bind(done)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32))

		const threshold = 8
		profiler := prof.New()
		p := interp.NewPool(prog, 2, interp.WithThreshold(threshold), interp.WithProfiler(profiler))
		defer p.Close()

		a, err := p.Get(context.Background())
		require.NoError(t, err)
		b2, err := p.Get(context.Background())
		require.NoError(t, err)

		run := func(vm *interp.Interpreter) error {
			if err := vm.Run(context.Background()); err != nil {
				return err
			}
			_, err := vm.Pop()
			vm.Reset()
			vm.Flush()
			return err
		}

		var runs int
		var runErr error
		var entries float64
		poll(t, func() bool {
			vm := a
			if runs%2 == 1 {
				vm = b2
			}
			runErr = run(vm)
			runs++
			if runErr != nil {
				return true
			}
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.Greater(t, entries, float64(0))
		// A single, unshared interpreter would need threshold Runs of its
		// own; two alternating interpreters would need about 2x threshold
		// total without pool-wide aggregation. The small constant covers
		// strict alternation's worst case: only the interpreter whose turn
		// crosses the total submits, so the other's turns are wasted retries
		// until submission's owner returns to drain the compile (one turn)
		// and then enter it (one more).
		require.LessOrEqual(t, runs, threshold+8)

		p.Put(a)
		p.Put(b2)
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
		resource := &trackedValue{}
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

// applyGlobalProgram reads global 1 (the caller's selector: 0 for inc, 1 for
// dec) once, stores the matching constant-pool address into global 0, then
// calls apply(i, global 0) calls times through the same dynamic CALL site,
// the callee read fresh from global 0 on every call. Constants are [apply,
// inc, dec]. Module locals [0] the counter and [1] the running sum, left on
// the stack. The constant-pool address survives Reset, unlike a heap Alloc,
// so repeated rounds observe the same callee address for the same selector.
func applyGlobalProgram(t *testing.T, calls int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	useDec, selected := b.Label(), b.Label()
	b.Emit(instr.GLOBAL_GET, 1).BrIf(useDec)
	b.Emit(instr.CONST_GET, 1)
	b.Br(selected)
	b.Bind(useDec).Emit(instr.CONST_GET, 2)
	b.Bind(selected).Emit(instr.GLOBAL_SET, 0)

	loop, done := b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(calls)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithGlobals(types.TypeAny, types.TypeI32),
		program.WithConstants(applyFunction(), incFunction(), decFunction()))
}
