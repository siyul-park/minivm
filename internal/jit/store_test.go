package jit_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/jit"
)

// code returns a fresh, unpublished Code at address of tier.
func code(t *testing.T, address int, tier jit.Tier) *jit.Code {
	t.Helper()
	c, err := jit.NewCode(address, tier, ret(), nil)
	require.NoError(t, err)
	return c
}

func TestNewStore(t *testing.T) {
	t.Run("serves the given number of addresses", func(t *testing.T) {
		s := jit.NewStore(4)
		require.Nil(t, s.Code(3))
		require.NoError(t, s.Close())
	})

	t.Run("rejects a non-positive size", func(t *testing.T) {
		require.Panics(t, func() { jit.NewStore(0) })
	})
}

func TestStore_Natives(t *testing.T) {
	s := jit.NewStore(2)
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	require.NotZero(t, s.Natives())
	require.Equal(t, s.Natives(), s.Natives())
}

func TestStore_Code(t *testing.T) {
	s := jit.NewStore(2)
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	require.Nil(t, s.Code(0))
	c := code(t, 0, jit.Baseline)
	require.True(t, s.Publish(c))
	require.Equal(t, c, s.Code(0))
}

func TestStore_Publish(t *testing.T) {
	t.Run("installs at its address and a higher tier replaces it", func(t *testing.T) {
		s := jit.NewStore(2)
		t.Cleanup(func() { require.NoError(t, s.Close()) })

		low := code(t, 1, jit.Baseline)
		require.True(t, s.Publish(low))
		require.Equal(t, low, s.Code(1))

		high := code(t, 1, jit.Optimized)
		require.True(t, s.Publish(high))
		require.Equal(t, high, s.Code(1))
		require.Equal(t, low, s.Find(low.Entry()))
	})

	t.Run("refuses and frees a tier no higher than the published one", func(t *testing.T) {
		s := jit.NewStore(2)
		t.Cleanup(func() { require.NoError(t, s.Close()) })

		first := code(t, 0, jit.Optimized)
		require.True(t, s.Publish(first))

		stale := code(t, 0, jit.Optimized)
		require.False(t, s.Publish(stale))
		require.Equal(t, first, s.Code(0))
		require.NoError(t, stale.Free())
	})

	t.Run("refuses a zero tier even when nothing is published", func(t *testing.T) {
		s := jit.NewStore(1)
		t.Cleanup(func() { require.NoError(t, s.Close()) })

		c := code(t, 0, 0)
		require.False(t, s.Publish(c))
		require.Nil(t, s.Code(0))
	})

	t.Run("refuses an out-of-range address", func(t *testing.T) {
		s := jit.NewStore(1)
		t.Cleanup(func() { require.NoError(t, s.Close()) })

		c := code(t, 1, jit.Baseline)
		require.False(t, s.Publish(c))
		require.NoError(t, c.Free())
	})
}

func TestStore_Retire(t *testing.T) {
	t.Run("clears the published code and keeps it findable while retired", func(t *testing.T) {
		s := jit.NewStore(1)
		t.Cleanup(func() { require.NoError(t, s.Close()) })

		c := code(t, 0, jit.Optimized)
		require.True(t, s.Publish(c))

		s.Retire(0)
		require.Nil(t, s.Code(0))
		require.Equal(t, c, s.Find(c.Entry()))

		// Nothing is published at 0 anymore, so even a lower tier installs.
		low := code(t, 0, jit.Baseline)
		require.True(t, s.Publish(low))
	})

	t.Run("is a no-op when nothing is published", func(t *testing.T) {
		s := jit.NewStore(1)
		t.Cleanup(func() { require.NoError(t, s.Close()) })

		s.Retire(0)
		require.Nil(t, s.Code(0))
	})
}

func TestStore_Find(t *testing.T) {
	s := jit.NewStore(1)
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	c := code(t, 0, jit.Baseline)
	require.True(t, s.Publish(c))

	require.Equal(t, c, s.Find(c.Entry()))
	require.Equal(t, c, s.Find(c.Entry()+3))
	require.Nil(t, s.Find(c.Entry()+4))
	require.Nil(t, s.Find(c.Entry()-1))
}

func TestStore_Reclaim(t *testing.T) {
	s := jit.NewStore(1)
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	c := code(t, 0, jit.Baseline)
	require.True(t, s.Publish(c))
	s.Retire(0)

	s.Enter()
	require.NoError(t, s.Reclaim())
	require.Equal(t, c, s.Find(c.Entry()))

	s.Leave()
	require.NoError(t, s.Reclaim())
	require.Nil(t, s.Find(c.Entry()))
}

func TestStore_Close(t *testing.T) {
	s := jit.NewStore(2)
	base := code(t, 0, jit.Baseline)
	require.True(t, s.Publish(base))
	opt := code(t, 0, jit.Optimized)
	require.True(t, s.Publish(opt))

	require.NoError(t, s.Close())
}

func TestStore_Race(t *testing.T) {
	s := jit.NewStore(4)
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	var wg sync.WaitGroup
	for addr := range 4 {
		for _, tier := range []jit.Tier{jit.Baseline, jit.Optimized} {
			c := code(t, addr, tier)
			wg.Add(1)
			go func(c *jit.Code) {
				defer wg.Done()
				s.Publish(c)
			}(c)
		}
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Enter()
			s.Find(0)
			s.Leave()
			_ = s.Reclaim()
		}()
	}
	wg.Wait()
}
