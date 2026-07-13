package interp

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewPool(t *testing.T) {
	p := NewPool(program.New([]instr.Instruction{instr.New(instr.NOP)}), 0)
	defer p.Close()
	require.Equal(t, 1, p.size)
}

func TestPool_Get(t *testing.T) {
	for _, tt := range runTests {
		t.Run(fmt.Sprint(tt.program), func(t *testing.T) {
			p := NewPool(tt.program, 1)
			defer p.Close()

			i, err := p.Get(context.Background())
			require.NoError(t, err)
			defer p.Put(i)

			err = i.Run(context.Background())
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			for _, want := range tt.values {
				got, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
		})
	}

	t.Run("reuses an idle interpreter", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		p := NewPool(prog, 1)
		defer p.Close()

		i1, err := p.Get(context.Background())
		require.NoError(t, err)
		p.Put(i1)

		i2, err := p.Get(context.Background())
		require.NoError(t, err)
		require.Same(t, i1, i2)
	})

	t.Run("blocks until put or context canceled", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		p := NewPool(prog, 1)
		defer p.Close()

		i, err := p.Get(context.Background())
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		_, err = p.Get(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		p.Put(i)
	})

	t.Run("returns ErrPoolClosed once closed", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		p := NewPool(prog, 1)
		require.NoError(t, p.Close())

		_, err := p.Get(context.Background())
		require.ErrorIs(t, err, ErrPoolClosed)
	})

	t.Run("compiles a shared branch tree concurrently without racing", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}

		// A called function with a branch tree: members share one Tracer, so one
		// interpreter warming a side exit (Tracer.exit mutating tree.branches/hits)
		// runs concurrently with another lowering the same root (rootAt reading it).
		// Before the fix that races the shared tree; the snapshot isolates the reader.
		b := types.NewFunctionBuilder(nil).WithParams(types.TypeI32).WithReturns(types.TypeI32)
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
			Emit(instr.New(instr.I32_CONST, i32operand(-1))).
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
		p := NewPool(prog, 12, WithTick(1), WithThreshold(0), WithProfiler(metrics))
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
		p := NewPool(prog, workers, WithTick(1), WithThreshold(0), WithProfiler(metrics))
		defer p.Close()
		for range rounds {
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
		}
		emits, ok := metrics.Metric("vm_jit_emits_total")
		require.True(t, ok)
		require.Greater(t, emits, float64(0))
	})

	t.Run("does not recompile a known loop exit", func(t *testing.T) {
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
			Emit(instr.I32_CONST, 4).
			Emit(instr.I32_LT_S).
			BrIf(loop).
			Emit(instr.LOCAL_GET, 0)
		prog, err := b.Build()
		require.NoError(t, err)
		metrics := prof.New()
		p := NewPool(prog, 1, WithTick(1), WithThreshold(0), WithProfiler(metrics))

		for range exitThreshold * 8 {
			i, err := p.Get(context.Background())
			require.NoError(t, err)
			require.NoError(t, i.Run(context.Background()))
			value, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, types.I32(4), value)
			p.Put(i)
		}
		require.NoError(t, p.Close())
		attempts, ok := metrics.Metric("vm_jit_attempts_total")
		require.True(t, ok)
		require.Equal(t, float64(1), attempts)
	})

	t.Run("accounts only shared cache winners across flush and recompile", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		b := types.NewFunctionBuilder(nil).WithParams(types.TypeI32, types.TypeI32).WithReturns(types.TypeI32)
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
		p := NewPool(prog, 2, WithProfiler(metrics), WithTick(1), WithThreshold(0))
		first, err := p.Get(context.Background())
		require.NoError(t, err)
		second, err := p.Get(context.Background())
		require.NoError(t, err)

		run := func(i *Interpreter, divisor int32) (types.Value, error) {
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
		for _, i := range []*Interpreter{first, second} {
			go func(i *Interpreter) {
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
		for range exitThreshold {
			ready := make(chan struct{}, 2)
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, i := range []*Interpreter{first, second} {
				go func(i *Interpreter) {
					ready <- struct{}{}
					<-start
					_, err := run(i, 0)
					results <- err
				}(i)
			}
			<-ready
			<-ready
			close(start)
			require.ErrorIs(t, <-results, ErrDivideByZero)
			require.ErrorIs(t, <-results, ErrDivideByZero)
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
		require.Equal(t, float64(2), hotCompiles)
		require.Equal(t, float64(2), sideExitCompiles)
		require.Equal(t, float64(exitThreshold*2), guardExits)
		attempts, ok := metrics.Metric("vm_jit_attempts_total")
		require.True(t, ok)
		require.Equal(t, float64(4), attempts)
		emits, ok := metrics.Metric("vm_jit_emits_total")
		require.True(t, ok)
		require.Equal(t, float64(3), emits)
	})
}

func TestPool_Put(t *testing.T) {
	t.Run("returns interpreter to idle after resetting state", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1)})
		p := NewPool(prog, 1)
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
		p := NewPool(program.New([]instr.Instruction{instr.New(instr.NOP)}), 1)
		defer p.Close()
		p.Put(nil)
	})

	t.Run("drops interpreter beyond idle capacity", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		p := NewPool(prog, 1)
		defer p.Close()

		i, err := p.Get(context.Background())
		require.NoError(t, err)

		p.Put(i)
		p.Put(i)

		require.Equal(t, int64(0), p.live.Load())
	})

	t.Run("closes interpreter when pool is closed", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		p := NewPool(prog, 1)

		i, err := p.Get(context.Background())
		require.NoError(t, err)
		require.NoError(t, p.Close())

		p.Put(i)
		require.Equal(t, int64(0), p.live.Load())
	})
}

func TestPool_Close(t *testing.T) {
	t.Run("releases idle interpreters and is idempotent", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		p := NewPool(prog, 1)

		i, err := p.Get(context.Background())
		require.NoError(t, err)
		p.Put(i)

		require.NoError(t, p.Close())
		require.NoError(t, p.Close())
	})

	t.Run("outstanding interpreter closes on its next Put", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
		p := NewPool(prog, 1)

		i, err := p.Get(context.Background())
		require.NoError(t, err)

		require.NoError(t, p.Close())
		p.Put(i)
		require.Equal(t, int64(0), p.live.Load())
	})
}
