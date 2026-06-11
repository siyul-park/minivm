package interp

import (
	"sync"
	"sync/atomic"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
)

type Cache struct {
	stats *prof.Stats
	mods  atomic.Pointer[[]*jitModule]
	bufs  []*asm.Buffer
	hits  []atomic.Int64
	state []atomic.Int32
	refs  atomic.Int64

	mu     sync.Mutex
	closed bool
}

const (
	cold int32 = iota
	build
	ready
)

func NewCache(prog *program.Program) *Cache {
	size := len(prog.Constants) + 1
	mods := []*jitModule{}
	c := &Cache{
		stats: prof.New(),
		hits:  make([]atomic.Int64, size),
		state: make([]atomic.Int32, size),
	}
	c.refs.Store(1)
	c.mods.Store(&mods)
	return c
}

func (c *Cache) Profile() prof.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats.Snapshot()
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
	if c.state[addr].Load() != cold {
		return false
	}
	return c.hits[addr].Add(1) >= threshold && c.state[addr].CompareAndSwap(cold, build)
}

func (c *Cache) fail(addr int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.done(addr)
}

func (c *Cache) flush(stats *prof.Stats) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats.Merge(stats.Snapshot())
	stats.Reset()
}

func (c *Cache) publish(addr int, mod *jitModule, buf *asm.Buffer) {
	if buf != nil {
		c.bufs = append(c.bufs, buf)
	}
	if mod != nil {
		c.stats.JITAdd(prof.JIT{
			Emits: uint64(mod.emits),
			Links: uint64(mod.links),
			Skips: uint64(mod.skips),
			Bytes: uint64(mod.bytes),
		})
		if len(mod.entries) > 0 || len(mod.segments) > 0 {
			mods := c.mods.Load()
			next := make([]*jitModule, 0, len(*mods)+1)
			next = append(next, (*mods)...)
			next = append(next, mod)
			c.mods.Store(&next)
		}
		for target := range mod.entries {
			c.done(target)
		}
		for _, seg := range mod.segments {
			c.done(seg.addr)
		}
	}
	c.done(addr)
}

func (c *Cache) release() error {
	if c.refs.Add(-1) > 0 {
		return nil
	}
	var err error
	for _, buf := range c.bufs {
		if e := buf.Free(); e != nil && err == nil {
			err = e
		}
	}
	c.bufs = nil
	return err
}

func (c *Cache) done(addr int) {
	if addr >= 0 && addr < len(c.state) {
		c.state[addr].Store(ready)
	}
}
