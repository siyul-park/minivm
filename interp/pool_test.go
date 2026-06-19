package interp

import (
	"context"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func label(key, value string) prof.Label {
	return prof.Label{Key: key, Value: value}
}

func mustMetric(t *testing.T, p *prof.Profiler, name string, labels ...prof.Label) float64 {
	t.Helper()
	v, ok := p.Metric(name, labels...)
	require.True(t, ok, "metric %s missing", name)
	return v
}

func TestNewPool(t *testing.T) {
	t.Run("normalizes non-positive size", func(t *testing.T) {
		p := NewPool(program.New(nil), 0)
		defer p.Close()

		i, err := p.Get(context.Background())
		require.NoError(t, err)
		p.Put(i)
	})

	t.Run("forwards interpreter options", func(t *testing.T) {
		p := NewPool(program.New(nil), 1, WithStack(2048), WithHeap(64))
		defer p.Close()

		i, err := p.Get(context.Background())
		require.NoError(t, err)
		defer p.Put(i)
		require.Equal(t, 2048, len(i.stack))
		require.Equal(t, 64, cap(i.heap))
	})

	t.Run("does not allocate interpreters eagerly", func(t *testing.T) {
		p := NewPool(program.New(nil), 4)
		defer p.Close()
		require.Equal(t, int64(0), p.live.Load())
	})

	t.Run("shares default marshaler within pool", func(t *testing.T) {
		p1 := NewPool(program.New(nil), 2)
		defer p1.Close()

		i1, err := p1.Get(context.Background())
		require.NoError(t, err)
		defer p1.Put(i1)

		i2, err := p1.Get(context.Background())
		require.NoError(t, err)
		defer p1.Put(i2)

		m1, ok := i1.marshaler.(*codec)
		require.True(t, ok)
		m2, ok := i2.marshaler.(*codec)
		require.True(t, ok)
		require.Same(t, m1, m2)

		p2 := NewPool(program.New(nil), 1)
		defer p2.Close()

		i3, err := p2.Get(context.Background())
		require.NoError(t, err)
		defer p2.Put(i3)

		m3, ok := i3.marshaler.(*codec)
		require.True(t, ok)
		require.NotSame(t, m1, m3)
	})

	t.Run("custom marshaler overrides pool default", func(t *testing.T) {
		custom := &recordingMarshaler{}
		p := NewPool(program.New(nil), 1, WithMarshaler(custom))
		defer p.Close()

		i, err := p.Get(context.Background())
		require.NoError(t, err)
		defer p.Put(i)

		require.Same(t, custom, i.marshaler)
	})
}

func TestPool_Get(t *testing.T) {
	t.Run("creates under cap", func(t *testing.T) {
		p := NewPool(program.New(nil), 2)
		defer p.Close()

		i1, err := p.Get(context.Background())
		require.NoError(t, err)
		i2, err := p.Get(context.Background())
		require.NoError(t, err)
		require.NotSame(t, i1, i2)

		p.Put(i1)
		p.Put(i2)
	})

	t.Run("reuses idle", func(t *testing.T) {
		p := NewPool(program.New(nil), 2)
		defer p.Close()

		i1, err := p.Get(context.Background())
		require.NoError(t, err)
		p.Put(i1)

		i2, err := p.Get(context.Background())
		require.NoError(t, err)
		require.Same(t, i1, i2)
		p.Put(i2)
	})

	t.Run("blocks at cap then unblocks on put", func(t *testing.T) {
		p := NewPool(program.New(nil), 1)
		defer p.Close()

		i1, err := p.Get(context.Background())
		require.NoError(t, err)

		done := make(chan *Interpreter, 1)
		go func() {
			i2, err := p.Get(context.Background())
			require.NoError(t, err)
			done <- i2
		}()

		select {
		case <-done:
			t.Fatal("Get returned before Put")
		case <-time.After(50 * time.Millisecond):
		}

		p.Put(i1)
		select {
		case i2 := <-done:
			require.Same(t, i1, i2)
			p.Put(i2)
		case <-time.After(time.Second):
			t.Fatal("Get did not unblock after Put")
		}
	})

	t.Run("context canceled while blocked", func(t *testing.T) {
		p := NewPool(program.New(nil), 1)
		defer p.Close()

		i, err := p.Get(context.Background())
		require.NoError(t, err)
		defer p.Put(i)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		_, err = p.Get(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("after close returns ErrPoolClosed", func(t *testing.T) {
		p := NewPool(program.New(nil), 1)
		require.NoError(t, p.Close())

		_, err := p.Get(context.Background())
		require.ErrorIs(t, err, ErrPoolClosed)
	})
}

func TestPool_Put(t *testing.T) {
	t.Run("resets borrowed interpreter", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 42),
		})
		p := NewPool(prog, 1)
		defer p.Close()

		i, err := p.Get(context.Background())
		require.NoError(t, err)
		require.NoError(t, i.Run(context.Background()))
		require.Greater(t, i.Len(), 0)

		p.Put(i)

		i2, err := p.Get(context.Background())
		require.NoError(t, err)
		defer p.Put(i2)
		require.Same(t, i, i2)
		require.Equal(t, 0, i2.Len())
	})

	t.Run("nil is ignored", func(t *testing.T) {
		p := NewPool(program.New(nil), 1)
		defer p.Close()
		require.NotPanics(t, func() { p.Put(nil) })
	})

	t.Run("after close closes interpreter", func(t *testing.T) {
		p := NewPool(program.New(nil), 2)

		i, err := p.Get(context.Background())
		require.NoError(t, err)
		require.NoError(t, p.Close())

		require.NotPanics(t, func() { p.Put(i) })
		require.Equal(t, int64(0), p.live.Load())
	})
}

func TestPool_Close(t *testing.T) {
	t.Run("drains idle", func(t *testing.T) {
		p := NewPool(program.New(nil), 3)

		var borrowed []*Interpreter
		for j := 0; j < 3; j++ {
			i, err := p.Get(context.Background())
			require.NoError(t, err)
			borrowed = append(borrowed, i)
		}
		for _, i := range borrowed {
			p.Put(i)
		}

		require.NoError(t, p.Close())
		require.Equal(t, int64(0), p.live.Load())
	})

	t.Run("idempotent", func(t *testing.T) {
		p := NewPool(program.New(nil), 1)
		require.NoError(t, p.Close())
		require.NoError(t, p.Close())
	})

	t.Run("outstanding interpreter closed on later put", func(t *testing.T) {
		p := NewPool(program.New(nil), 1)

		i, err := p.Get(context.Background())
		require.NoError(t, err)
		require.Equal(t, int64(1), p.live.Load())

		require.NoError(t, p.Close())
		p.Put(i)
		require.Equal(t, int64(0), p.live.Load())
	})
}

func TestPool_Profile(t *testing.T) {
	t.Run("aggregates profiles on put", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.DROP),
		})
		pr := prof.New()
		p := NewPool(prog, 2, WithProfiler(pr), WithTick(1), WithThreshold(-1))
		defer p.Close()

		for range 2 {
			i, err := p.Get(context.Background())
			require.NoError(t, err)
			require.NoError(t, i.Run(context.Background()))
			p.Put(i)
		}

		require.Equal(t, float64(4), mustMetric(t, pr, "vm_samples_total"))
		require.Equal(t, float64(4), mustMetric(t, pr, "vm_func_samples_total", label("func", "0")))
		require.Equal(t, float64(2), mustMetric(t, pr, "vm_func_ip_samples_total", label("func", "0"), label("ip", "0")))
		require.Equal(t, float64(2), mustMetric(t, pr, "vm_func_ip_samples_total", label("func", "0"), label("ip", "5")))
	})

	t.Run("does not double count repeated put cycles", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
		})
		pr := prof.New()
		p := NewPool(prog, 1, WithProfiler(pr), WithTick(1), WithThreshold(-1))
		defer p.Close()

		i, err := p.Get(context.Background())
		require.NoError(t, err)
		require.NoError(t, i.Run(context.Background()))
		p.Put(i)
		require.Equal(t, float64(1), mustMetric(t, pr, "vm_samples_total"))

		i, err = p.Get(context.Background())
		require.NoError(t, err)
		p.Put(i)
		require.Equal(t, float64(1), mustMetric(t, pr, "vm_samples_total"))
	})

	t.Run("shares one jit attempt across pool", func(t *testing.T) {
		requireJIT(t)
		fn := types.NewFunctionBuilder(nil).WithReturns(types.TypeI32).Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.NOP),
			instr.New(instr.NOP),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))
		var addr int
		pr := prof.New()
		p := NewPool(prog, 2, WithProfiler(pr), WithTick(1), WithThreshold(5), WithHook(func(i *Interpreter) error {
			if addr == 0 || i.Func() != addr || i.IP() != 0 {
				return nil
			}
			_, err := i.tracer.capture(i, anchor{addr: addr, ip: 0})
			return err
		}))
		defer p.Close()

		first, err := p.Get(context.Background())
		require.NoError(t, err)
		addr = first.constants[0].Ref()
		second, err := p.Get(context.Background())
		require.NoError(t, err)

		require.NoError(t, first.Run(context.Background()))
		value, err := first.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(3), value)
		require.NoError(t, second.Run(context.Background()))
		value, err = second.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(3), value)

		p.Put(first)
		p.Put(second)

		require.Equal(t, float64(1), mustMetric(t, pr, "vm_jit_attempts_total"))
		require.NotZero(t, mustMetric(t, pr, "vm_jit_emits_total"))
		require.NotZero(t, mustMetric(t, pr, "vm_jit_links_total"))
		require.NotZero(t, mustMetric(t, pr, "vm_jit_bytes_total"))
	})

	t.Run("late borrower installs published jit without compiling", func(t *testing.T) {
		requireJIT(t)
		fn := types.NewFunctionBuilder(nil).WithReturns(types.TypeI32).Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.NOP),
			instr.New(instr.NOP),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))
		var addr int
		pr := prof.New()
		p := NewPool(prog, 2, WithProfiler(pr), WithTick(1), WithThreshold(5), WithHook(func(i *Interpreter) error {
			if addr == 0 || i.Func() != addr || i.IP() != 0 {
				return nil
			}
			_, err := i.tracer.capture(i, anchor{addr: addr, ip: 0})
			return err
		}))
		defer p.Close()

		first, err := p.Get(context.Background())
		require.NoError(t, err)
		addr = first.constants[0].Ref()
		require.NoError(t, first.Run(context.Background()))
		first.flush()
		require.Equal(t, float64(1), mustMetric(t, pr, "vm_jit_attempts_total"))

		second, err := p.Get(context.Background())
		require.NoError(t, err)
		require.NoError(t, second.Run(context.Background()))
		value, err := second.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(3), value)
		p.Put(first)
		p.Put(second)
		require.Equal(t, float64(1), mustMetric(t, pr, "vm_jit_attempts_total"))
	})
}

func BenchmarkPool_GetPut(b *testing.B) {
	b.Run("default", func(b *testing.B) {
		for _, tt := range tests {
			b.Run(tt.program.String(), func(b *testing.B) {
				ctx, cancel := context.WithCancel(context.TODO())
				defer cancel()

				p := NewPool(tt.program, 256)
				defer p.Close()

				b.ReportAllocs()
				b.ResetTimer()

				b.RunParallel(func(pb *testing.PB) {
					b.StopTimer()
					i, err := p.Get(ctx)
					require.NoError(b, err)
					for pb.Next() {
						b.StartTimer()
						require.NoError(b, i.Run(ctx))
						b.StopTimer()
						i.Reset()
					}
					p.Put(i)
					b.StartTimer()
				})
			})
		}
	})

	b.Run("threaded", func(b *testing.B) {
		for _, tt := range tests {
			b.Run(tt.program.String(), func(b *testing.B) {
				ctx, cancel := context.WithCancel(context.TODO())
				defer cancel()

				p := NewPool(tt.program, 256, WithThreshold(-1))
				defer p.Close()

				b.ReportAllocs()
				b.ResetTimer()

				b.RunParallel(func(pb *testing.PB) {
					b.StopTimer()
					i, err := p.Get(ctx)
					require.NoError(b, err)
					for pb.Next() {
						b.StartTimer()
						require.NoError(b, i.Run(ctx))
						b.StopTimer()
						i.Reset()
					}
					p.Put(i)
					b.StartTimer()
				})
			})
		}
	})
}
