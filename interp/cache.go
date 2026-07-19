package interp

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
)

// cache is the shared store of compiled native modules for a pool. It owns the
// per-function compile state machine (cold → build → ready), the executable
// buffers, and the append-only module list members install from. Profiling and
// trace recording live in the tracer, not here.
type cache struct {
	modules atomic.Pointer[[]*module]
	buffers []*asm.Buffer
	hits    []atomic.Int64
	state   []atomic.Int32
	active  []request
	pending [][]request
	refs    atomic.Int64
	closed  bool

	mu sync.Mutex
}

type request struct {
	root    anchor
	trigger prof.Trigger
}

const (
	cacheCold int32 = iota
	cacheBuild
	cacheReady
)

func newCache(prog *program.Program) *cache {
	size := len(prog.Constants) + 1
	mods := []*module{}
	c := &cache{
		hits:    make([]atomic.Int64, size),
		state:   make([]atomic.Int32, size),
		active:  make([]request, size),
		pending: make([][]request, size),
	}
	c.refs.Store(1)
	c.modules.Store(&mods)
	return c
}

func (c *cache) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.release()
}

func (c *cache) attach() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.refs.Add(1)
	return true
}

func (c *cache) detach() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.release()
}

func (c *cache) claim(addr int, threshold int64) (request, bool) {
	if threshold < 0 || addr < 0 || addr >= len(c.hits) {
		return request{}, false
	}
	if c.state[addr].Load() != cacheCold {
		return request{}, false
	}
	if c.hits[addr].Add(1) < threshold {
		return request{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state[addr].Load() != cacheCold {
		return request{}, false
	}
	next := request{root: anchor{addr: addr}, trigger: prof.TriggerHot}
	if len(c.pending[addr]) > 0 {
		next = c.pending[addr][0]
		c.pending[addr] = c.pending[addr][1:]
	}
	c.active[addr] = next
	c.state[addr].Store(cacheBuild)
	return next, true
}

func (c *cache) request(next request) {
	addr := next.root.addr
	if addr < 0 || addr >= len(c.state) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	active := c.active[addr]
	if active.covers(next) {
		return
	}
	pending := c.pending[addr]
	for idx, prior := range pending {
		if prior.covers(next) {
			return
		}
		if prior.root != next.root {
			continue
		}
		pending = slices.Delete(pending, idx, idx+1)
		break
	}
	if next.trigger == prof.TriggerSideExit {
		insert := 0
		for insert < len(pending) && pending[insert].trigger == prof.TriggerSideExit {
			insert++
		}
		pending = slices.Insert(pending, insert, next)
	} else {
		pending = append(pending, next)
	}
	c.pending[addr] = pending
	if c.state[addr].Load() == cacheReady {
		c.state[addr].Store(cacheCold)
	}
}

// covers reports whether prior already requests the same root with equal or
// higher priority than next. Side exits replace queued hot roots but never the
// reverse, so a hot request cannot displace either an active or pending exit.
func (r request) covers(next request) bool {
	return r.trigger != prof.TriggerNone && r.root == next.root &&
		(r.trigger == prof.TriggerSideExit || next.trigger == prof.TriggerHot)
}

func (c *cache) fail(addr int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finishLocked(addr)
}

func (c *cache) publish(addr int, mod *module, buf *asm.Buffer) {
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
			if target.addr >= 0 && target.addr < len(c.state) && target.addr != addr &&
				c.state[target.addr].Load() == cacheCold && len(c.pending[target.addr]) == 0 {
				c.state[target.addr].Store(cacheReady)
			}
		}
	}
	c.finishLocked(addr)
}

func (c *cache) release() error {
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

func (c *cache) finishLocked(addr int) {
	if addr >= 0 && addr < len(c.state) {
		c.active[addr] = request{}
		if len(c.pending[addr]) == 0 {
			c.state[addr].Store(cacheReady)
		} else {
			c.state[addr].Store(cacheCold)
		}
	}
}
