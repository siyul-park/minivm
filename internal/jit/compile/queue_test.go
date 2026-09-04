package compile_test

import (
	"sync"
	"testing"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/prof"
	"github.com/stretchr/testify/require"
)

func hot(addr, ip int) compile.Job {
	return compile.Job{Root: jit.Anchor{Addr: addr, IP: ip}, Trigger: prof.TriggerHot}
}

func sideExit(addr, ip int) compile.Job {
	return compile.Job{Root: jit.Anchor{Addr: addr, IP: ip}, Trigger: prof.TriggerSideExit}
}

func code(roots ...jit.Anchor) *jit.Code {
	c := &jit.Code{Entries: map[jit.Anchor]jit.Entry{}}
	for _, root := range roots {
		c.Entries[root] = jit.Entry{}
	}
	return c
}

func TestWithAsync(t *testing.T) {
	q := compile.New(4, compile.WithAsync())
	defer q.Close()

	q.Add(hot(1, 0))
	_, ok := q.Claim(1, 0)
	require.True(t, ok)

	hold := make(chan struct{})
	ran := make(chan struct{})
	q.Serve(func() {
		<-hold
		close(ran)
		q.Done(1, nil)
	})

	// Serve has returned while the build is still parked on hold, so the build
	// is running on a goroutine that is not this one.
	select {
	case <-ran:
		require.Fail(t, "the build ran on the calling goroutine")
	default:
	}
	close(hold)
	<-ran
}

func TestNew(t *testing.T) {
	q := compile.New(4)

	_, ok := q.Claim(0, 0)
	require.False(t, ok, "a queue with nothing requested has nothing to serve")
	require.False(t, q.Hit(0, 0))
}

func TestQueue_Hit(t *testing.T) {
	t.Run("invites a claim only once a request is waiting", func(t *testing.T) {
		q := compile.New(4)

		require.False(t, q.Hit(1, 0))
		q.Add(hot(1, 0))
		require.True(t, q.Hit(1, 0))
	})

	t.Run("counts toward the threshold", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 0))

		require.False(t, q.Hit(1, 3))
		require.False(t, q.Hit(1, 3))
		require.True(t, q.Hit(1, 3))
	})

	t.Run("declines a build in flight", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 0))
		_, ok := q.Claim(1, 0)
		require.True(t, ok)

		require.False(t, q.Hit(1, 0))
	})

	t.Run("declines an address outside the program and a disabled threshold", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 0))

		require.False(t, q.Hit(9, 0))
		require.False(t, q.Hit(-1, 0))
		require.False(t, q.Hit(1, -1))
	})
}

func TestQueue_Add(t *testing.T) {
	t.Run("retains distinct roots of one function in order", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 4))
		q.Add(hot(1, 9))

		first, ok := q.Claim(1, 0)
		require.True(t, ok)
		require.Equal(t, hot(1, 4), first)
		q.Done(1, nil)

		second, ok := q.Claim(1, 0)
		require.True(t, ok)
		require.Equal(t, hot(1, 9), second)
	})

	t.Run("discards a duplicate of a waiting root", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 4))
		q.Add(hot(1, 4))

		_, ok := q.Claim(1, 0)
		require.True(t, ok)
		q.Done(1, nil)

		_, ok = q.Claim(1, 0)
		require.False(t, ok)
	})

	t.Run("discards a duplicate of the build in flight", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 4))
		_, ok := q.Claim(1, 0)
		require.True(t, ok)

		q.Add(hot(1, 4))
		q.Done(1, nil)

		_, ok = q.Claim(1, 0)
		require.False(t, ok, "a hot re-request of the active root is already covered")
	})

	t.Run("serves a side exit ahead of waiting hot roots", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 4))
		q.Add(hot(1, 9))
		q.Add(sideExit(1, 12))

		first, ok := q.Claim(1, 0)
		require.True(t, ok)
		require.Equal(t, sideExit(1, 12), first)
	})

	t.Run("keeps a side exit raised behind an active hot build", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 4))
		claimed, ok := q.Claim(1, 0)
		require.True(t, ok)
		require.Equal(t, hot(1, 4), claimed)

		q.Add(sideExit(1, 4))
		q.Done(1, code(jit.Anchor{Addr: 1, IP: 4}))

		next, ok := q.Claim(1, 0)
		require.True(t, ok)
		require.Equal(t, sideExit(1, 4), next, "publication leaves the queued exit for the next winner")
	})

	t.Run("lets a side exit replace a waiting hot root but not the reverse", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 4))
		q.Add(sideExit(1, 4))

		first, ok := q.Claim(1, 0)
		require.True(t, ok)
		require.Equal(t, sideExit(1, 4), first)
		q.Done(1, nil)

		_, ok = q.Claim(1, 0)
		require.False(t, ok, "the hot request it replaced must not still be waiting")

		q.Add(sideExit(2, 4))
		q.Add(hot(2, 4))

		only, ok := q.Claim(2, 0)
		require.True(t, ok)
		require.Equal(t, sideExit(2, 4), only)
		q.Done(2, nil)

		_, ok = q.Claim(2, 0)
		require.False(t, ok, "a hot request cannot displace a waiting exit")
	})

	t.Run("ignores an address outside the program", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(9, 0))
		q.Add(hot(-1, 0))

		_, ok := q.Claim(9, 0)
		require.False(t, ok)
	})
}

func TestQueue_Claim(t *testing.T) {
	t.Run("admits one holder at a time", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 0))
		q.Add(hot(1, 4))

		_, ok := q.Claim(1, 0)
		require.True(t, ok)
		_, ok = q.Claim(1, 0)
		require.False(t, ok, "a second holder must wait for the first to finish")
	})

	t.Run("waits for the threshold", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 0))

		_, ok := q.Claim(1, 3)
		require.False(t, ok)
		q.Hit(1, 3)
		q.Hit(1, 3)
		q.Hit(1, 3)
		_, ok = q.Claim(1, 3)
		require.True(t, ok)
	})

	t.Run("hands one request to exactly one of many callers", func(t *testing.T) {
		const callers = 8
		q := compile.New(4)
		q.Add(hot(1, 0))

		var wg sync.WaitGroup
		claims := make(chan compile.Job, callers)
		for range callers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if job, ok := q.Claim(1, 0); ok {
					claims <- job
				}
			}()
		}
		wg.Wait()
		close(claims)

		require.Len(t, claims, 1)
		require.Equal(t, hot(1, 0), <-claims)
	})
}

func TestQueue_Serve(t *testing.T) {
	t.Run("runs the build on the calling goroutine", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 0))
		_, ok := q.Claim(1, 0)
		require.True(t, ok)

		ran := false
		q.Serve(func() {
			ran = true
			q.Done(1, nil)
		})

		require.True(t, ran, "a queue with no worker must finish the build before Serve returns")
		q.Add(hot(1, 4))
		_, ok = q.Claim(1, 0)
		require.True(t, ok, "the build ended its claim")
	})

	t.Run("runs the build on the worker", func(t *testing.T) {
		q := compile.New(4, compile.WithAsync())
		defer q.Close()
		q.Add(hot(1, 0))
		_, ok := q.Claim(1, 0)
		require.True(t, ok)

		ran := make(chan struct{})
		q.Serve(func() {
			q.Done(1, nil)
			close(ran)
		})

		<-ran
		q.Add(hot(1, 4))
		_, ok = q.Claim(1, 0)
		require.True(t, ok, "the build ended its claim")
	})
}

func TestQueue_Done(t *testing.T) {
	t.Run("releases the claim for the next winner", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 0))
		q.Add(hot(1, 4))
		_, ok := q.Claim(1, 0)
		require.True(t, ok)

		q.Done(1, nil)

		next, ok := q.Claim(1, 0)
		require.True(t, ok)
		require.Equal(t, hot(1, 4), next)
	})

	t.Run("discards a later hot request for a root it published", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 0))
		_, ok := q.Claim(1, 0)
		require.True(t, ok)
		q.Done(1, code(jit.Anchor{Addr: 1}, jit.Anchor{Addr: 2}))

		q.Add(hot(1, 0))
		_, ok = q.Claim(1, 0)
		require.False(t, ok)

		q.Add(hot(2, 0))
		_, ok = q.Claim(2, 0)
		require.False(t, ok, "a root another build already emitted needs no build of its own")
	})

	t.Run("keeps a root a build emitted nothing for requestable", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 4))
		_, ok := q.Claim(1, 0)
		require.True(t, ok)
		q.Done(1, nil)

		q.Add(hot(1, 4))
		again, ok := q.Claim(1, 0)
		require.True(t, ok, "a caller that learns more about the root may ask again")
		require.Equal(t, hot(1, 4), again)
	})

	t.Run("keeps serving side exits for a published root", func(t *testing.T) {
		q := compile.New(4)
		q.Add(hot(1, 4))
		_, ok := q.Claim(1, 0)
		require.True(t, ok)
		q.Done(1, code(jit.Anchor{Addr: 1, IP: 4}))

		q.Add(sideExit(1, 4))
		rebuild, ok := q.Claim(1, 0)
		require.True(t, ok, "rebuilding a native root with the exit's leg folded in is the point")
		require.Equal(t, sideExit(1, 4), rebuild)
	})
}

func TestQueue_Close(t *testing.T) {
	t.Run("is idempotent with no worker to stop", func(t *testing.T) {
		q := compile.New(4)

		q.Close()
		q.Close()
	})

	t.Run("finishes every build already handed to the worker", func(t *testing.T) {
		q := compile.New(4, compile.WithAsync())
		q.Add(hot(1, 0))
		_, ok := q.Claim(1, 0)
		require.True(t, ok)
		q.Add(hot(2, 0))
		_, ok = q.Claim(2, 0)
		require.True(t, ok)

		started := make(chan struct{})
		hold := make(chan struct{})
		var mu sync.Mutex
		var ran []int
		q.Serve(func() {
			close(started)
			<-hold
			mu.Lock()
			ran = append(ran, 1)
			mu.Unlock()
			q.Done(1, nil)
		})
		q.Serve(func() {
			mu.Lock()
			ran = append(ran, 2)
			mu.Unlock()
			q.Done(2, nil)
		})

		// The worker is inside the first build with the second still queued
		// behind it, so Close can only return once it has drained both.
		<-started
		go close(hold)
		q.Close()
		q.Close()

		mu.Lock()
		defer mu.Unlock()
		require.Equal(t, []int{1, 2}, ran)
	})

	t.Run("runs a later build on the calling goroutine", func(t *testing.T) {
		q := compile.New(4, compile.WithAsync())
		q.Add(hot(1, 0))
		_, ok := q.Claim(1, 0)
		require.True(t, ok)
		q.Close()

		ran := false
		q.Serve(func() {
			ran = true
			q.Done(1, nil)
		})

		require.True(t, ran, "a closed queue has no worker left to hand the build to")
	})
}
