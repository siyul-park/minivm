package interp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
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
