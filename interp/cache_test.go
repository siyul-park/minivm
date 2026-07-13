package interp

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
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

func TestCache_Due(t *testing.T) {
	cache := NewCache(program.New([]instr.Instruction{
		instr.New(instr.NOP),
	}))
	defer cache.Close()

	_, ok := cache.claim(0, 2)
	require.False(t, ok)
	request, ok := cache.claim(0, 2)
	require.True(t, ok)
	require.Equal(t, cacheRequest{root: anchor{}, trigger: prof.TriggerHot}, request)
	_, ok = cache.claim(0, 2)
	require.False(t, ok)
}

func TestCache_Rearm(t *testing.T) {
	cache := NewCache(program.New([]instr.Instruction{
		instr.New(instr.NOP),
	}))
	defer cache.Close()

	_, ok := cache.claim(0, 1)
	require.True(t, ok)
	cache.publish(0, nil, nil)
	cache.rearm(anchor{})
	request, ok := cache.claim(0, 1)
	require.True(t, ok)
	require.Equal(t, cacheRequest{root: anchor{}, trigger: prof.TriggerSideExit}, request)
	cache.publish(0, nil, nil)
	cache.rearm(anchor{})
	require.Equal(t, cacheCold, cache.state[0].Load())
	cache.rearm(anchor{addr: 2})

	t.Run("preserves a side exit requested while a build publishes", func(t *testing.T) {
		cache := NewCache(program.New([]instr.Instruction{instr.New(instr.NOP)}))
		defer cache.Close()
		_, ok := cache.claim(0, 1)
		require.True(t, ok)

		publish := make(chan struct{})
		published := make(chan struct{})
		go func() {
			<-publish
			cache.publish(0, nil, nil)
			close(published)
		}()

		root := anchor{ip: 7}
		cache.rearm(root)
		close(publish)
		<-published

		require.Equal(t, cacheCold, cache.state[0].Load())
		request, ok := cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, cacheRequest{root: root, trigger: prof.TriggerSideExit}, request)
	})
}

func TestCache_Close(t *testing.T) {
	cache := NewCache(program.New(nil))
	i := New(program.New(nil), WithCache(cache))

	require.NoError(t, cache.Close())
	require.NoError(t, cache.Close())
	require.NoError(t, i.Close())
}
