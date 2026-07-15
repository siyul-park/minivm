package interp

import (
	"slices"
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
	active  []cacheRequest
	pending [][]cacheRequest
	refs    atomic.Int64
	closed  bool

	mu sync.Mutex
}

type cacheRequest struct {
	root    anchor
	trigger prof.Trigger
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
		hits:    make([]atomic.Int64, size),
		state:   make([]atomic.Int32, size),
		active:  make([]cacheRequest, size),
		pending: make([][]cacheRequest, size),
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

func (c *Cache) claim(addr int, threshold int64) (cacheRequest, bool) {
	if threshold < 0 || addr < 0 || addr >= len(c.hits) {
		return cacheRequest{}, false
	}
	if c.state[addr].Load() != cacheCold {
		return cacheRequest{}, false
	}
	if c.hits[addr].Add(1) < threshold {
		return cacheRequest{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state[addr].Load() != cacheCold {
		return cacheRequest{}, false
	}
	request := cacheRequest{root: anchor{addr: addr}, trigger: prof.TriggerHot}
	if len(c.pending[addr]) > 0 {
		request = c.pending[addr][0]
		c.pending[addr] = c.pending[addr][1:]
	}
	c.active[addr] = request
	c.state[addr].Store(cacheBuild)
	return request, true
}

// rearm queues a side-exit build request without disturbing an active owner.
func (c *Cache) rearm(root anchor) {
	c.request(cacheRequest{root: root, trigger: prof.TriggerSideExit})
}

func (c *Cache) request(next cacheRequest) {
	addr := next.root.addr
	if addr < 0 || addr >= len(c.state) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	active := c.active[addr]
	sameRoot := active.trigger != prof.TriggerNone && active.root == next.root
	if sameRoot && (active.trigger == prof.TriggerSideExit || next.trigger == prof.TriggerHot) {
		return
	}
	pending := c.pending[addr]
	for idx := range pending {
		if pending[idx].root != next.root {
			continue
		}
		if pending[idx].trigger == prof.TriggerSideExit || next.trigger == prof.TriggerHot {
			return
		}
		copy(pending[idx:], pending[idx+1:])
		pending = pending[:len(pending)-1]
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

func (c *Cache) fail(addr int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finishLocked(addr)
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
			if target.addr >= 0 && target.addr < len(c.state) && target.addr != addr &&
				c.state[target.addr].Load() == cacheCold && len(c.pending[target.addr]) == 0 {
				c.state[target.addr].Store(cacheReady)
			}
		}
	}
	c.finishLocked(addr)
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

func (c *Cache) finishLocked(addr int) {
	if addr >= 0 && addr < len(c.state) {
		c.active[addr] = cacheRequest{}
		if len(c.pending[addr]) == 0 {
			c.state[addr].Store(cacheReady)
		} else {
			c.state[addr].Store(cacheCold)
		}
	}
}
