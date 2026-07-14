package interp

import (
	"testing"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewCache(t *testing.T) {
	cache := NewCache(program.New([]instr.Instruction{
		instr.New(instr.NOP),
	}))
	defer cache.Close()

	i := New(program.New([]instr.Instruction{
		instr.New(instr.NOP),
	}), WithCache(cache))
	defer i.Close()

	require.Same(t, cache, i.cache)
}

func TestCache_Close(t *testing.T) {
	cache := NewCache(program.New(nil))
	i := New(program.New(nil), WithCache(cache))

	require.NoError(t, cache.Close())
	require.NoError(t, cache.Close())
	require.NoError(t, i.Close())
}

func TestCache_StateTransitions(t *testing.T) {
	t.Run("closed cache rejects attach", func(t *testing.T) {
		cache := NewCache(program.New(nil))

		require.NoError(t, cache.Close())
		require.False(t, cache.attach())
	})

	t.Run("fail marks address ready", func(t *testing.T) {
		cache := NewCache(program.New([]instr.Instruction{
			instr.New(instr.NOP),
		}))
		defer cache.Close()

		require.True(t, cache.due(0, 1))

		cache.fail(0)

		require.Equal(t, ready, cache.state[0].Load())
		require.False(t, cache.due(0, 1))
	})

	t.Run("publish records module and completes all targets", func(t *testing.T) {
		cache := NewCache(program.New(nil, program.WithConstants(
			types.I32(1),
			types.I32(2),
		)))
		defer cache.Close()

		mod := &jitModule{
			entries: map[int]asm.Callable{1: noopCallable{}},
			segments: []jitSegment{
				{addr: 2},
			},
			emits: 1,
			links: 2,
			skips: 3,
			bytes: 4,
		}

		cache.publish(0, mod, nil)

		require.Len(t, *cache.mods.Load(), 1)
		require.Same(t, mod, (*cache.mods.Load())[0])
		require.Equal(t, ready, cache.state[0].Load())
		require.Equal(t, ready, cache.state[1].Load())
		require.Equal(t, ready, cache.state[2].Load())
		require.Equal(t, prof.JIT{
			Emits: 1,
			Links: 2,
			Skips: 3,
			Bytes: 4,
		}, cache.Profile().JIT)
	})
}

type noopCallable struct{}

func (noopCallable) Call([]uint64) error { return nil }
