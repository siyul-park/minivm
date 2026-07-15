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

func TestCache_Close(t *testing.T) {
	cache := NewCache(program.New(nil))
	i := New(program.New(nil), WithCache(cache))

	require.NoError(t, cache.Close())
	require.NoError(t, cache.Close())
	require.NoError(t, i.Close())
}

func TestCache_Request(t *testing.T) {
	t.Run("queues distinct loop roots", func(t *testing.T) {
		cache := NewCache(program.New(nil))
		defer cache.Close()

		first := cacheRequest{root: anchor{ip: 4}, trigger: prof.TriggerHot}
		second := cacheRequest{root: anchor{ip: 8}, trigger: prof.TriggerHot}
		cache.request(first)
		cache.request(second)

		request, ok := cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, first, request)
		cache.fail(0)

		request, ok = cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, second, request)
	})

	t.Run("prioritizes side exits", func(t *testing.T) {
		cache := NewCache(program.New(nil))
		defer cache.Close()

		hot := cacheRequest{root: anchor{ip: 4}, trigger: prof.TriggerHot}
		upgraded := cacheRequest{root: anchor{ip: 8}, trigger: prof.TriggerSideExit}
		cache.request(hot)
		cache.request(cacheRequest{root: upgraded.root, trigger: prof.TriggerHot})
		cache.request(upgraded)

		request, ok := cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, upgraded, request)
		cache.fail(0)

		request, ok = cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, hot, request)
	})

	t.Run("deduplicates active hot root", func(t *testing.T) {
		cache := NewCache(program.New(nil))
		defer cache.Close()

		request := cacheRequest{root: anchor{ip: 4}, trigger: prof.TriggerHot}
		cache.request(request)
		claimed, ok := cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, request, claimed)

		cache.request(request)
		cache.fail(0)
		_, ok = cache.claim(0, 1)
		require.False(t, ok)
	})

	t.Run("queues entry root", func(t *testing.T) {
		cache := NewCache(program.New(nil))
		defer cache.Close()

		request := cacheRequest{root: anchor{}, trigger: prof.TriggerHot}
		cache.request(request)
		claimed, ok := cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, request, claimed)
	})
}
