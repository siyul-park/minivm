package interp

import (
	"testing"

	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestNewCache(t *testing.T) {
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

	t.Run("prioritizes side exits in arrival order", func(t *testing.T) {
		cache := NewCache(program.New(nil))
		defer cache.Close()

		hot := cacheRequest{root: anchor{ip: 4}, trigger: prof.TriggerHot}
		first := cacheRequest{root: anchor{ip: 8}, trigger: prof.TriggerSideExit}
		second := cacheRequest{root: anchor{ip: 12}, trigger: prof.TriggerSideExit}
		cache.request(hot)
		cache.request(cacheRequest{root: first.root, trigger: prof.TriggerHot})
		cache.request(first)
		cache.request(second)

		request, ok := cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, first, request)
		cache.fail(0)

		request, ok = cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, second, request)
		cache.fail(0)

		request, ok = cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, hot, request)
	})

	t.Run("coalesces pending roots", func(t *testing.T) {
		for _, trigger := range []prof.Trigger{prof.TriggerHot, prof.TriggerSideExit} {
			cache := NewCache(program.New(nil))
			request := cacheRequest{root: anchor{ip: 4}, trigger: trigger}
			cache.request(request)
			cache.request(request)

			claimed, ok := cache.claim(0, 1)
			require.True(t, ok, "trigger=%v", trigger)
			require.Equal(t, request, claimed, "trigger=%v", trigger)
			cache.fail(0)
			_, ok = cache.claim(0, 1)
			require.False(t, ok, "trigger=%v", trigger)
			require.NoError(t, cache.Close())
		}
	})

	t.Run("queues side exit behind active hot root", func(t *testing.T) {
		cache := NewCache(program.New(nil))
		defer cache.Close()

		hot := cacheRequest{root: anchor{ip: 4}, trigger: prof.TriggerHot}
		sideExit := cacheRequest{root: hot.root, trigger: prof.TriggerSideExit}
		cache.request(hot)
		claimed, ok := cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, hot, claimed)

		cache.request(sideExit)
		cache.fail(0)
		claimed, ok = cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, sideExit, claimed)
	})

	t.Run("deduplicates active roots", func(t *testing.T) {
		for _, trigger := range []prof.Trigger{prof.TriggerHot, prof.TriggerSideExit} {
			cache := NewCache(program.New(nil))
			request := cacheRequest{root: anchor{ip: 4}, trigger: trigger}
			cache.request(request)
			claimed, ok := cache.claim(0, 1)
			require.True(t, ok, "trigger=%v", trigger)
			require.Equal(t, request, claimed, "trigger=%v", trigger)

			cache.request(request)
			cache.fail(0)
			_, ok = cache.claim(0, 1)
			require.False(t, ok, "trigger=%v", trigger)
			require.NoError(t, cache.Close())
		}
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

func TestCache_Close(t *testing.T) {
	cache := NewCache(program.New(nil))
	i := New(program.New(nil), WithCache(cache))

	require.NoError(t, cache.Close())
	require.NoError(t, cache.Close())
	require.NoError(t, i.Close())
}
