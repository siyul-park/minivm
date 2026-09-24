package jit_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/jit"
)

// code returns a fresh, unpublished, non-OSR Code at address of tier.
func code(t *testing.T, address int, tier jit.Tier) *jit.Code {
	t.Helper()
	c, err := jit.NewCode(address, 0, false, tier, 0, ret(), nil, 0)
	require.NoError(t, err)
	return c
}

// osrCode returns a fresh, unpublished OSR Code at address rooted at ip.
func osrCode(t *testing.T, address, ip int, tier jit.Tier) *jit.Code {
	t.Helper()
	c, err := jit.NewCode(address, ip, true, tier, 0, ret(), nil, 0)
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

	t.Run("installs an OSR code per (address, IP), leaving natives untouched", func(t *testing.T) {
		s := jit.NewStore(2)
		t.Cleanup(func() { require.NoError(t, s.Close()) })

		c := osrCode(t, 1, 12, jit.Optimized)
		require.True(t, s.Publish(c))
		require.Equal(t, c, s.CodeAt(1, 12))
		require.Nil(t, s.Code(1))
	})

	t.Run("refuses a second OSR publish at the same (address, IP)", func(t *testing.T) {
		s := jit.NewStore(1)
		t.Cleanup(func() { require.NoError(t, s.Close()) })

		first := osrCode(t, 0, 12, jit.Optimized)
		require.True(t, s.Publish(first))

		second := osrCode(t, 0, 12, jit.Optimized)
		require.False(t, s.Publish(second))
		require.Equal(t, first, s.CodeAt(0, 12))
		require.NoError(t, second.Free())
	})

	t.Run("distinguishes two OSR headers of the same function", func(t *testing.T) {
		s := jit.NewStore(1)
		t.Cleanup(func() { require.NoError(t, s.Close()) })

		a := osrCode(t, 0, 12, jit.Optimized)
		b := osrCode(t, 0, 40, jit.Optimized)
		require.True(t, s.Publish(a))
		require.True(t, s.Publish(b))
		require.Equal(t, a, s.CodeAt(0, 12))
		require.Equal(t, b, s.CodeAt(0, 40))
	})

	t.Run("installs an OSR code whose header sits at IP 0 as OSR, not as ordinary entry code", func(t *testing.T) {
		s := jit.NewStore(1)
		t.Cleanup(func() { require.NoError(t, s.Close()) })

		c := osrCode(t, 0, 0, jit.Baseline)
		require.True(t, s.Publish(c))
		require.Equal(t, c, s.CodeAt(0, 0))
		require.Nil(t, s.Code(0))
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

func TestStore_CodeAt(t *testing.T) {
	s := jit.NewStore(2)
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	require.Nil(t, s.CodeAt(1, 12))
	c := osrCode(t, 1, 12, jit.Optimized)
	require.True(t, s.Publish(c))
	require.Equal(t, c, s.CodeAt(1, 12))
	// An OSR code is never a call target: it never reaches the ordinary
	// per-address publish path Code reads.
	require.Nil(t, s.Code(1))
}

func TestStore_RetireAt(t *testing.T) {
	t.Run("clears the published OSR code and keeps it findable while retired", func(t *testing.T) {
		s := jit.NewStore(1)
		t.Cleanup(func() { require.NoError(t, s.Close()) })

		c := osrCode(t, 0, 12, jit.Optimized)
		require.True(t, s.Publish(c))

		s.RetireAt(0, 12)
		require.Nil(t, s.CodeAt(0, 12))
		require.Equal(t, c, s.Find(c.Entry()))
	})

	t.Run("is a no-op when nothing is published there", func(t *testing.T) {
		s := jit.NewStore(1)
		t.Cleanup(func() { require.NoError(t, s.Close()) })

		s.RetireAt(0, 12)
		require.Nil(t, s.CodeAt(0, 12))
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
	t.Run("frees a retired code only once no interpreter is inside native code", func(t *testing.T) {
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
	})

	t.Run("frees an OSR code retired through RetireAt", func(t *testing.T) {
		s := jit.NewStore(1)
		t.Cleanup(func() { require.NoError(t, s.Close()) })

		c := osrCode(t, 0, 12, jit.Optimized)
		require.True(t, s.Publish(c))
		s.RetireAt(0, 12)

		require.NoError(t, s.Reclaim())
		require.Nil(t, s.Find(c.Entry()))
	})
}

func TestStore_Close(t *testing.T) {
	s := jit.NewStore(2)
	base := code(t, 0, jit.Baseline)
	require.True(t, s.Publish(base))
	opt := code(t, 0, jit.Optimized)
	require.True(t, s.Publish(opt))
	osr := osrCode(t, 1, 12, jit.Optimized)
	require.True(t, s.Publish(osr))

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
		osr := osrCode(t, addr, 12, jit.Optimized)
		wg.Add(1)
		go func(c *jit.Code) {
			defer wg.Done()
			s.Publish(c)
			_ = s.CodeAt(addr, 12)
			s.RetireAt(addr, 12)
		}(osr)
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
