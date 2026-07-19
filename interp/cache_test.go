package interp

import (
	"testing"

	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestCache(t *testing.T) {
	t.Run("keeps shared state alive until attached members detach", func(t *testing.T) {
		cache := newCache(program.New(nil))
		require.Equal(t, int64(1), cache.refs.Load())

		require.True(t, cache.attach())
		require.Equal(t, int64(2), cache.refs.Load())

		require.NoError(t, cache.close())
		require.True(t, cache.closed)
		require.Equal(t, int64(1), cache.refs.Load())
		require.False(t, cache.attach())

		require.NoError(t, cache.detach())
		require.Zero(t, cache.refs.Load())
		require.NoError(t, cache.close())
		require.Zero(t, cache.refs.Load())
	})

	t.Run("queues distinct loop roots", func(t *testing.T) {
		cache := newCache(program.New(nil))
		defer cache.close()

		first := request{root: anchor{ip: 4}, trigger: prof.TriggerHot}
		second := request{root: anchor{ip: 8}, trigger: prof.TriggerHot}
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
		cache := newCache(program.New(nil))
		defer cache.close()

		hot := request{root: anchor{ip: 4}, trigger: prof.TriggerHot}
		first := request{root: anchor{ip: 8}, trigger: prof.TriggerSideExit}
		second := request{root: anchor{ip: 12}, trigger: prof.TriggerSideExit}
		cache.request(hot)
		cache.request(request{root: first.root, trigger: prof.TriggerHot})
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
			cache := newCache(program.New(nil))
			next := request{root: anchor{ip: 4}, trigger: trigger}
			cache.request(next)
			cache.request(next)

			claimed, ok := cache.claim(0, 1)
			require.True(t, ok, "trigger=%v", trigger)
			require.Equal(t, next, claimed, "trigger=%v", trigger)
			cache.fail(0)
			_, ok = cache.claim(0, 1)
			require.False(t, ok, "trigger=%v", trigger)
			require.NoError(t, cache.close())
		}
	})

	t.Run("queues side exit behind active hot root", func(t *testing.T) {
		cache := newCache(program.New(nil))
		defer cache.close()

		hot := request{root: anchor{ip: 4}, trigger: prof.TriggerHot}
		sideExit := request{root: hot.root, trigger: prof.TriggerSideExit}
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

	t.Run("replaces a queued hot root with its side exit", func(t *testing.T) {
		cache := newCache(program.New(nil))
		defer cache.close()

		hot := request{root: anchor{ip: 4}, trigger: prof.TriggerHot}
		sideExit := request{root: hot.root, trigger: prof.TriggerSideExit}
		cache.request(hot)
		cache.request(sideExit)

		claimed, ok := cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, sideExit, claimed)
		cache.fail(0)
		_, ok = cache.claim(0, 1)
		require.False(t, ok)
	})

	t.Run("suppresses a hot root behind an active side exit", func(t *testing.T) {
		cache := newCache(program.New(nil))
		defer cache.close()

		sideExit := request{root: anchor{ip: 4}, trigger: prof.TriggerSideExit}
		hot := request{root: sideExit.root, trigger: prof.TriggerHot}
		cache.request(sideExit)
		claimed, ok := cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, sideExit, claimed)

		cache.request(hot)
		cache.fail(0)
		_, ok = cache.claim(0, 1)
		require.False(t, ok)
	})

	t.Run("deduplicates active roots", func(t *testing.T) {
		for _, trigger := range []prof.Trigger{prof.TriggerHot, prof.TriggerSideExit} {
			cache := newCache(program.New(nil))
			next := request{root: anchor{ip: 4}, trigger: trigger}
			cache.request(next)
			claimed, ok := cache.claim(0, 1)
			require.True(t, ok, "trigger=%v", trigger)
			require.Equal(t, next, claimed, "trigger=%v", trigger)

			cache.request(next)
			cache.fail(0)
			_, ok = cache.claim(0, 1)
			require.False(t, ok, "trigger=%v", trigger)
			require.NoError(t, cache.close())
		}
	})

	t.Run("queues entry root", func(t *testing.T) {
		cache := newCache(program.New(nil))
		defer cache.close()

		next := request{root: anchor{}, trigger: prof.TriggerHot}
		cache.request(next)
		claimed, ok := cache.claim(0, 1)
		require.True(t, ok)
		require.Equal(t, next, claimed)
	})
}
