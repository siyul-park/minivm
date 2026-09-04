package interp_test

import (
	"context"
	"fmt"
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

type poolTrackedValue struct {
	closed int
}

func (*poolTrackedValue) Kind() types.Kind { return types.KindRef }
func (*poolTrackedValue) Type() types.Type { return types.TypeAny }
func (*poolTrackedValue) String() string   { return "tracked" }

func (v *poolTrackedValue) Close() error {
	v.closed++
	return nil
}

// native reports how many native entries every pool member flushed into
// metrics, which is how a test waits for a build the pool's worker ran to
// reach the interpreter that claimed it.
func native(metrics *prof.Profiler) float64 {
	var total float64
	for _, metric := range metrics.Metrics() {
		if metric.Name == "vm_jit_native_entries_total" {
			total += metric.Value
		}
	}
	return total
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

	t.Run("compiles a shared branch tree concurrently without racing", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}

		// A called function with a branch tree: members share one tracer, so one
		// interpreter warming a side exit (tracer.branch mutating tree.branches/hits)
		// runs concurrently with another lowering the same root (rootAt reading it).
		// Before the fix that races the shared tree; the snapshot isolates the reader.
		b := types.NewFunctionBuilder(nil).Params(types.TypeI32).Returns(types.TypeI32)
		neg := b.Label()
		small := b.Label()
		tiny := b.Label()
		b.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.I32_LT_S)).
			BrIf(neg).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 10)).
			Emit(instr.New(instr.I32_LT_S)).
			BrIf(small).
			Emit(instr.New(instr.I32_CONST, 2)).
			Emit(instr.New(instr.RETURN)).
			Bind(neg).
			Emit(instr.New(instr.I32_CONST, 0xffffffff)).
			Emit(instr.New(instr.RETURN)).
			Bind(small).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 5)).
			Emit(instr.New(instr.I32_LT_S)).
			BrIf(tiny).
			Emit(instr.New(instr.I32_CONST, 1)).
			Emit(instr.New(instr.RETURN)).
			Bind(tiny).
			Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.RETURN))
		eval, err := b.Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(eval))

		metrics := prof.New()
		p := interp.NewPool(prog, 12, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(metrics))
		defer p.Close()

		var wg sync.WaitGroup
		errs := make(chan error, 12)
		for worker := range 12 {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				for n := range 256 {
					i, err := p.Get(context.Background())
					if err != nil {
						errs <- err
						return
					}
					value := int32((worker+n)%24 - 8)
					if err := i.Push(types.I32(value)); err != nil {
						p.Put(i)
						errs <- err
						return
					}
					if err := i.Run(context.Background()); err != nil {
						p.Put(i)
						errs <- err
						return
					}
					p.Put(i)
				}
			}(worker)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		emits, ok := metrics.Metric("vm_jit_emits_total")
		require.True(t, ok)
		require.Greater(t, emits, float64(0))
	})

	t.Run("runs a spilled shared native entry from fresh goroutines", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}

		const workers = 8
		const values = 256
		const rounds = 8
		want := types.I32(values * (values + 1) / 2)

		b := program.NewBuilder()
		for value := range values {
			b.Emit(instr.I32_CONST, uint64(value+1))
		}
		for range values - 1 {
			b.Emit(instr.I32_ADD)
		}
		prog, err := b.Build()
		require.NoError(t, err)

		metrics := prof.New()
		p := interp.NewPool(prog, workers, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(metrics))
		defer p.Close()
		// The pool builds on its own worker, so the shared native entry lands a
		// few rounds after the first of them asked for it. Keep going until a
		// full measurement window of rounds has run against installed native
		// code rather than against the threaded closures it replaces.
		installed := 0
		for range 1 << 10 {
			ready := make(chan struct{}, workers)
			start := make(chan struct{})
			results := make(chan error, workers)
			for range workers {
				go func() {
					i, err := p.Get(context.Background())
					ready <- struct{}{}
					<-start
					if err != nil {
						results <- err
						return
					}
					err = i.Run(context.Background())
					if err == nil {
						var value types.Value
						value, err = i.Pop()
						if err == nil && value != want {
							err = fmt.Errorf("got %v, want %v", value, want)
						}
					}
					p.Put(i)
					results <- err
				}()
			}
			for range workers {
				<-ready
			}
			close(start)
			for range workers {
				require.NoError(t, <-results)
			}

			if native(metrics) > 0 {
				installed++
			}
			if installed == rounds {
				break
			}
		}

		require.Equal(t, rounds, installed)
		emits, ok := metrics.Metric("vm_jit_emits_total")
		require.True(t, ok)
		require.Greater(t, emits, float64(0))
	})

	t.Run("adopts a build the worker finished at a safepoint", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		b := program.NewBuilder()
		loop := b.Label()
		b.Locals(types.TypeI32).
			Emit(instr.I32_CONST, 0).
			Emit(instr.LOCAL_SET, 0).
			Bind(loop).
			Emit(instr.LOCAL_GET, 0).
			Emit(instr.I32_CONST, 1).
			Emit(instr.I32_ADD).
			Emit(instr.LOCAL_TEE, 0).
			Emit(instr.I32_CONST, 1<<22).
			Emit(instr.I32_LT_S).
			BrIf(loop).
			Emit(instr.LOCAL_GET, 0)
		prog, err := b.Build()
		require.NoError(t, err)
		metrics := prof.New()
		p := interp.NewPool(prog, 1, interp.WithProfiler(metrics), interp.WithThreshold(1))
		defer func() { require.NoError(t, p.Close()) }()

		vm, err := p.Get(context.Background())
		require.NoError(t, err)
		defer p.Put(vm)

		// One Run, so nothing between runs can install anything: the pool
		// builds on its own worker, and the only place this interpreter looks
		// for what that worker published is a safepoint inside this loop.
		require.NoError(t, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1<<22), value)

		vm.Flush()
		require.Positive(t, native(metrics))
	})

	t.Run("does not recompile a known loop exit", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		const runs = 64

		b := program.NewBuilder()
		loop := b.Label()
		b.Locals(types.TypeI32).
			Emit(instr.I32_CONST, 0).
			Emit(instr.LOCAL_SET, 0).
			Bind(loop).
			Emit(instr.LOCAL_GET, 0).
			Emit(instr.I32_CONST, 1).
			Emit(instr.I32_ADD).
			Emit(instr.LOCAL_TEE, 0).
			Emit(instr.I32_CONST, 4).
			Emit(instr.I32_LT_S).
			BrIf(loop).
			Emit(instr.LOCAL_GET, 0)
		prog, err := b.Build()
		require.NoError(t, err)
		metrics := prof.New()
		p := interp.NewPool(prog, 1, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(metrics))

		// The pool builds on its own worker, so the program settles a few runs
		// after it asked to. Run until a whole window of runs has gone by with
		// native code installed and nothing new attempted: the loop's own exit
		// is a leg the trace tree learns, so once it is known neither the
		// header nor the exit is ever rebuilt.
		var settled float64
		stable := 0
		for range 1 << 14 {
			i, err := p.Get(context.Background())
			require.NoError(t, err)
			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(4), value)
			i.Flush()
			p.Put(i)

			attempts, _ := metrics.Metric("vm_jit_attempts_total")
			if attempts != settled || native(metrics) == 0 {
				settled, stable = attempts, 0
				continue
			}
			stable++
			if stable == runs {
				break
			}
		}
		require.NoError(t, p.Close())

		require.Equal(t, runs, stable)
	})

	t.Run("accounts only shared cache winners across flush and recompile", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		const guardFailuresPerMember = 8

		b := types.NewFunctionBuilder(nil).Params(types.TypeI32, types.TypeI32).Returns(types.TypeI32)
		b.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.LOCAL_GET, 1)).
			Emit(instr.New(instr.I32_DIV_S)).
			Emit(instr.New(instr.RETURN))
		divide, err := b.Build()
		require.NoError(t, err)
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		}, program.WithConstants(divide))
		metrics := prof.New()
		p := interp.NewPool(prog, 2, interp.WithProfiler(metrics), interp.WithTick(1), interp.WithThreshold(0))
		first, err := p.Get(context.Background())
		require.NoError(t, err)
		second, err := p.Get(context.Background())
		require.NoError(t, err)

		run := func(i *interp.Interpreter, divisor int32) (types.Value, error) {
			i.Reset()
			if err := i.Push(types.I32(8)); err != nil {
				return nil, err
			}
			if err := i.Push(types.I32(divisor)); err != nil {
				return nil, err
			}
			if err := i.Run(context.Background()); err != nil {
				return nil, err
			}
			return i.Pop()
		}

		ready := make(chan struct{}, 2)
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, i := range []*interp.Interpreter{first, second} {
			go func(i *interp.Interpreter) {
				ready <- struct{}{}
				<-start
				value, err := run(i, 2)
				if err == nil && value != types.I32(4) {
					err = fmt.Errorf("got %v, want %v", value, types.I32(4))
				}
				results <- err
			}(i)
		}
		<-ready
		<-ready
		close(start)
		require.NoError(t, <-results)
		require.NoError(t, <-results)
		// Both members must be running native code before a guard failure can
		// deopt out of one, and the pool built on its own worker, so give each
		// of them runs until it is the one contributing native entries.
		for _, i := range []*interp.Interpreter{first, second} {
			before := native(metrics)
			for range 1024 {
				value, err := run(i, 2)
				require.NoError(t, err)
				require.Equal(t, types.I32(4), value)
				i.Flush()
				if native(metrics) > before {
					break
				}
			}
		}
		for range guardFailuresPerMember {
			ready := make(chan struct{}, 2)
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, i := range []*interp.Interpreter{first, second} {
				go func(i *interp.Interpreter) {
					ready <- struct{}{}
					<-start
					_, err := run(i, 0)
					results <- err
				}(i)
			}
			<-ready
			<-ready
			close(start)
			require.ErrorIs(t, <-results, interp.ErrDivideByZero)
			require.ErrorIs(t, <-results, interp.ErrDivideByZero)
		}
		resultValue, err := run(second, 2)
		require.NoError(t, err)
		require.Equal(t, types.I32(4), resultValue)

		p.Put(first)
		p.Put(second)
		require.NoError(t, p.Close())

		var hotCompiles, sideExitCompiles, guardExits float64
		for _, metric := range metrics.Metrics() {
			trigger := ""
			reason := ""
			for _, label := range metric.Labels {
				switch label.Key {
				case "trigger":
					trigger = label.Value
				case "reason":
					reason = label.Value
				}
			}
			switch {
			case metric.Name == "vm_jit_compiles_total" && trigger == "hot":
				hotCompiles += metric.Value
			case metric.Name == "vm_jit_compiles_total" && trigger == "side-exit":
				sideExitCompiles += metric.Value
			case metric.Name == "vm_jit_native_exits_total" && reason == "guard-value":
				guardExits += metric.Value
			}
		}
		// Both entry roots are built. The queue admits one winner per function,
		// so a root a build emitted code for is never built twice; the module
		// root emits nothing, is therefore never recorded as built, and the
		// member that lost the race may still ask for it again.
		require.GreaterOrEqual(t, hotCompiles, 2.0)
		// Every guard failure deopts, and a hot exit asks for its root to be
		// rebuilt with that leg folded in. How many rebuilds run depends on how
		// the members' requests overlap, because one raised while the same
		// rebuild is already in flight is coalesced into it, but at least one
		// must.
		require.Positive(t, sideExitCompiles)
		require.Equal(t, float64(guardFailuresPerMember*2), guardExits)
		// Compile and emission rows follow compilation ownership: every attempt
		// is recorded once, by the member that claimed it, and a member that
		// only installed what a peer published adds neither.
		attempts, ok := metrics.Metric("vm_jit_attempts_total")
		require.True(t, ok)
		require.Equal(t, hotCompiles+sideExitCompiles, attempts)
		emits, ok := metrics.Metric("vm_jit_emits_total")
		require.True(t, ok)
		// The module root emits nothing, because a top-level CALL has no native
		// framed ABI, so the function root and each rebuild replacing it are
		// the whole emission count.
		require.Equal(t, sideExitCompiles+1, emits)
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

	t.Run("stops the worker with builds in flight", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		const members = 4
		const runs = 64

		b := program.NewBuilder()
		loop := b.Label()
		b.Locals(types.TypeI32).
			Emit(instr.I32_CONST, 0).
			Emit(instr.LOCAL_SET, 0).
			Bind(loop).
			Emit(instr.LOCAL_GET, 0).
			Emit(instr.I32_CONST, 1).
			Emit(instr.I32_ADD).
			Emit(instr.LOCAL_TEE, 0).
			Emit(instr.I32_CONST, 1<<10).
			Emit(instr.I32_LT_S).
			BrIf(loop).
			Emit(instr.LOCAL_GET, 0)
		prog, err := b.Build()
		require.NoError(t, err)
		p := interp.NewPool(prog, members, interp.WithTick(1), interp.WithThreshold(1))

		borrowed := make([]*interp.Interpreter, 0, members)
		for range members {
			vm, err := p.Get(context.Background())
			require.NoError(t, err)
			borrowed = append(borrowed, vm)
		}

		// Every member keeps asking for builds while Close stops the worker,
		// so a build is in flight across the shutdown and whatever the worker
		// had already taken must still publish before any buffer is freed.
		var wg sync.WaitGroup
		results := make(chan error, members)
		for _, vm := range borrowed {
			wg.Add(1)
			go func(vm *interp.Interpreter) {
				defer wg.Done()
				for range runs {
					vm.Reset()
					if err := vm.Run(context.Background()); err != nil {
						results <- err
						return
					}
					value, err := vm.Pop()
					if err != nil {
						results <- err
						return
					}
					if value != types.I32(1<<10) {
						results <- fmt.Errorf("got %v, want %v", value, types.I32(1<<10))
						return
					}
				}
				results <- nil
			}(vm)
		}
		require.NoError(t, p.Close())
		wg.Wait()
		for range members {
			require.NoError(t, <-results)
		}

		// Each member still holds the store, so the buffers its published code
		// lives in are freed only as the last one is handed back.
		for _, vm := range borrowed {
			p.Put(vm)
		}
		require.NoError(t, p.Close())
	})

	t.Run("keeps native code alive for an outstanding interpreter", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT requires arm64")
		}
		var code []instr.Instruction
		for range 64 {
			code = append(code,
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_ADD),
				instr.New(instr.DROP),
			)
		}
		code = append(code, instr.New(instr.I32_CONST, 42))
		metrics := prof.New()
		p := interp.NewPool(program.New(code), 1,
			interp.WithProfiler(metrics),
			interp.WithTick(1),
			interp.WithThreshold(0),
		)
		var vm *interp.Interpreter
		defer func() {
			p.Put(vm)
			require.NoError(t, p.Close())
		}()

		var err error
		vm, err = p.Get(context.Background())
		require.NoError(t, err)
		// The pool builds on its own worker, so the module entry becomes native
		// some runs after the one that asked for it.
		for range 1024 {
			vm.Reset()
			require.NoError(t, vm.Run(context.Background()))
			value, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(42), value)
			vm.Flush()
			if native(metrics) > 0 {
				break
			}
		}
		p.Put(vm)
		vm = nil

		emits, ok := metrics.Metric("vm_jit_emits_total")
		require.True(t, ok)
		require.Greater(t, emits, float64(0))
		require.Greater(t, native(metrics), float64(0))

		vm, err = p.Get(context.Background())
		require.NoError(t, err)
		require.NoError(t, p.Close())
		require.NoError(t, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), value)

		p.Put(vm)
		vm = nil
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

	b.Run("SharedJITMiss", func(b *testing.B) {
		if runtime.GOARCH != "arm64" {
			b.Skip("native JIT requires arm64")
		}
		var code []instr.Instruction
		for range 64 {
			code = append(code,
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_ADD),
				instr.New(instr.DROP),
			)
		}
		code = append(code, instr.New(instr.I32_CONST, 42))
		prog := program.New(code)
		var second *interp.Interpreter
		b.ReportAllocs()
		b.ResetTimer()
		b.StopTimer()
		for range b.N {
			metrics := prof.New()
			pool := interp.NewPool(prog, 2, interp.WithProfiler(metrics), interp.WithTick(1), interp.WithThreshold(0))
			first, err := pool.Get(context.Background())
			require.NoError(b, err)
			for range 16 {
				first.Reset()
				require.NoError(b, first.Run(context.Background()))
				value, err := first.Pop()
				require.NoError(b, err)
				require.Equal(b, types.I32(42), value)
			}
			pool.Put(first)
			emits, ok := metrics.Metric("vm_jit_emits_total")
			require.True(b, ok)
			require.Greater(b, emits, float64(0))
			attempts, ok := metrics.Metric("vm_jit_attempts_total")
			require.True(b, ok)

			first, err = pool.Get(context.Background())
			require.NoError(b, err)
			var nativeEntriesBefore float64
			for _, metric := range metrics.Metrics() {
				if metric.Name == "vm_jit_native_entries_total" {
					nativeEntriesBefore += metric.Value
				}
			}
			require.Greater(b, nativeEntriesBefore, float64(0))

			b.StartTimer()
			second, err = pool.Get(context.Background())
			b.StopTimer()
			require.NoError(b, err)
			for range 2 {
				second.Reset()
				require.NoError(b, second.Run(context.Background()))
				value, err := second.Pop()
				require.NoError(b, err)
				require.Equal(b, types.I32(42), value)
			}
			pool.Put(first)
			pool.Put(second)
			require.NoError(b, pool.Close())

			attemptsAfter, ok := metrics.Metric("vm_jit_attempts_total")
			require.True(b, ok)
			require.Equal(b, attempts, attemptsAfter)
			var nativeEntriesAfter float64
			for _, metric := range metrics.Metrics() {
				if metric.Name == "vm_jit_native_entries_total" {
					nativeEntriesAfter += metric.Value
				}
			}
			require.Greater(b, nativeEntriesAfter, nativeEntriesBefore)
		}
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
