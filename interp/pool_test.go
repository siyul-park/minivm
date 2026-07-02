package interp

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
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

		p := NewPool(prog, 4, WithTick(1), WithThreshold(0))
		defer p.Close()

		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range exitThreshold * 4 {
					i, err := p.Get(context.Background())
					if err != nil {
						errs <- err
						return
					}
					if err := i.Push(types.I32(3)); err != nil {
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
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
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
