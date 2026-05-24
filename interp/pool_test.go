package interp

import (
	"context"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

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

		m1, ok := i1.marshaler.(*marshaler)
		require.True(t, ok)
		m2, ok := i2.marshaler.(*marshaler)
		require.True(t, ok)
		require.Same(t, m1, m2)

		p2 := NewPool(program.New(nil), 1)
		defer p2.Close()

		i3, err := p2.Get(context.Background())
		require.NoError(t, err)
		defer p2.Put(i3)

		m3, ok := i3.marshaler.(*marshaler)
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
