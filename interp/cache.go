package interp

import (
	"sync"
	"sync/atomic"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
)

// Cache is the shared store of compiled native modules for a pool. It owns the
// per-function compile state machine (cold → build → ready), the executable
// buffers, and the append-only module list members install from. Profiling and
// trace recording live in the tracer, not here.
type Cache struct {
	modules atomic.Pointer[[]*module]
	buffers []*asm.Buffer
	hits    []atomic.Int64
	state   []atomic.Int32
	side    []atomic.Bool
	refs    atomic.Int64

	mu     sync.Mutex
	closed bool
}

const (
	cacheCold int32 = iota
	cacheBuild
	cacheReady
)

func NewCache(prog *program.Program) *Cache {
	size := len(prog.Constants) + 1
	mods := []*module{}
	c := &Cache{
		hits:  make([]atomic.Int64, size),
		state: make([]atomic.Int32, size),
		side:  make([]atomic.Bool, size),
	}
	c.refs.Store(1)
	c.modules.Store(&mods)
	return c
}

func (c *Cache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.release()
}

func (c *Cache) attach() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.refs.Add(1)
	return true
}

func (c *Cache) detach() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.release()
}

func (c *Cache) due(addr int, threshold int64) bool {
	if threshold < 0 || addr < 0 || addr >= len(c.hits) {
		return false
	}
	if c.state[addr].Load() != cacheCold {
		return false
	}
	return c.hits[addr].Add(1) >= threshold && c.state[addr].CompareAndSwap(cacheCold, cacheBuild)
}

// rearm returns a ready function to cold so due owns the next build transition
// after a hot side exit grows the trace tree.
func (c *Cache) rearm(addr int) {
	if addr < 0 || addr >= len(c.state) {
		return
	}
	c.side[addr].Store(true)
	if !c.state[addr].CompareAndSwap(cacheReady, cacheCold) && c.state[addr].Load() == cacheReady {
		c.side[addr].Store(false)
	}
}

func (c *Cache) fail(addr int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ready(addr)
}

func (c *Cache) publish(addr int, mod *module, buf *asm.Buffer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if buf != nil {
		c.buffers = append(c.buffers, buf)
	}
	if mod != nil && len(mod.entries) > 0 {
		modules := c.modules.Load()
		next := make([]*module, 0, len(*modules)+1)
		next = append(next, (*modules)...)
		next = append(next, mod)
		c.modules.Store(&next)
		for target := range mod.entries {
			c.ready(target.addr)
		}
	}
	c.ready(addr)
}

func (c *Cache) release() error {
	if c.refs.Add(-1) > 0 {
		return nil
	}
	var err error
	for _, buf := range c.buffers {
		if e := buf.Free(); e != nil && err == nil {
			err = e
		}
	}
	c.buffers = nil
	return err
}

func (c *Cache) ready(addr int) {
	if addr >= 0 && addr < len(c.state) {
		c.state[addr].Store(cacheReady)
		c.side[addr].Store(false)
	}
}

func (c *Cache) trigger(addr int) prof.Trigger {
	if addr >= 0 && addr < len(c.side) && c.side[addr].Load() {
		return prof.TriggerSideExit
	}
	return prof.TriggerHot
}
