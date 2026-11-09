package jit

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Cache owns published code and the native entry table.
//
// Invalidated code is reclaimed by quiescent states: each invalidation advances the
// cache's epoch and stamps the code with it, and each attached Reader
// publishes the epoch it observed at its last quiescent point. A code is
// freed once every Reader has observed its epoch.
type Cache struct {
	// natives is the table read by native CALLs.
	natives []uintptr
	// codes publishes one atomic pointer per address.
	codes []atomic.Pointer[Code]
	// osr publishes OSR code per (address, IP): never a call target, so it
	// is never in natives, and is looked up only through Find or CodeAt.
	osr map[key]*Code
	// invalidated is in invalidation order, so its epochs ascend.
	invalidated []*Code
	readers     []*Reader
	// epoch counts invalidations; written under mu.
	epoch atomic.Uint64
	// pending is len(invalidated), readable without mu so Reclaim with nothing
	// invalidated — the common case — takes no lock.
	pending atomic.Int64

	mu sync.Mutex
	// freeErr records the first Free failure from Publish's own uninstall
	// free; Reclaim and Close surface it.
	freeErr atomic.Pointer[error]
}

// Reader is one interpreter's registration with a Cache: Reclaim frees no
// code invalidated after the Reader's last quiescent point.
type Reader struct {
	cache *Cache
	// seen is the cache epoch at the Reader's last quiescent point.
	seen atomic.Uint64
}

// key identifies one OSR unit's published code.
type key struct {
	address, ip int
}

// NewCache returns a Cache serving addresses [0, size).
func NewCache(size int) *Cache {
	if size < 1 {
		panic("jit: cache size must be positive")
	}
	return &Cache{natives: make([]uintptr, size), codes: make([]atomic.Pointer[Code], size), osr: map[key]*Code{}}
}

// Natives returns the address of the natives table, stable for the Cache's
// life: Context.Natives.
func (s *Cache) Natives() uintptr {
	return uintptr(unsafe.Pointer(&s.natives[0]))
}

// Code returns the published code at address, or nil. It never blocks.
func (s *Cache) Code(address int) *Code {
	if address < 0 || address >= len(s.codes) {
		return nil
	}
	return s.codes[address].Load()
}

// CodeAt returns the published OSR code at (address, ip), or nil. Unlike
// Code, it takes the mutex.
func (s *Cache) CodeAt(address, ip int) *Code {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.osr[key{address, ip}]
}

// Find returns the published or invalidated code whose range holds pc, or nil.
func (s *Cache) Find(pc uintptr) *Code {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.codes {
		if c := s.codes[i].Load(); c != nil && c.holds(pc) {
			return c
		}
	}
	for _, c := range s.osr {
		if c.holds(pc) {
			return c
		}
	}
	for _, c := range s.invalidated {
		if c.holds(pc) {
			return c
		}
	}
	return nil
}

// Publish takes ownership of c. An OSR code (c.OSR) is never a call target:
// it installs into the (address, IP) map, once, instead of natives[c.Address].
// Otherwise it installs c — natives[c.Address] = c.Native() — when c.Tier is
// above the published code's tier (none published counts as zero), invalidating
// the code it replaces. Either way it reports whether c installed; a stale c,
// or one whose address is out of range, is freed instead.
func (s *Cache) Publish(c *Code) bool {
	installed := false
	if c.Address >= 0 && c.Address < len(s.natives) {
		s.mu.Lock()
		if c.OSR {
			k := key{c.Address, c.IP}
			if installed = s.osr[k] == nil; installed {
				s.osr[k] = c
			}
		} else {
			old := s.codes[c.Address].Load()
			var published Tier
			if old != nil {
				published = old.Tier
			}
			if installed = c.Tier > published; installed {
				atomic.StoreUintptr(&s.natives[c.Address], c.Native())
				s.codes[c.Address].Store(c)
				if old != nil {
					s.invalidate(old)
				}
			}
		}
		s.mu.Unlock()
	}
	if !installed {
		if err := c.Free(); err != nil {
			s.freeErr.CompareAndSwap(nil, &err)
		}
	}
	return installed
}

// Invalidate clears natives[address] and moves the published code to the
// invalidated list; a no-op when nothing is published at address. A invalidated
// code stays findable until Reclaim: suspended activations still run it.
func (s *Cache) Invalidate(address int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if address < 0 || address >= len(s.codes) {
		return
	}
	c := s.codes[address].Load()
	if c == nil {
		return
	}
	atomic.StoreUintptr(&s.natives[address], 0)
	s.codes[address].Store(nil)
	s.invalidate(c)
}

// InvalidateAt moves the published OSR code at (address, ip) to the invalidated
// list; a no-op when nothing is published there. A invalidated code stays
// findable until Reclaim.
func (s *Cache) InvalidateAt(address, ip int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{address, ip}
	c, ok := s.osr[k]
	if !ok {
		return
	}
	delete(s.osr, k)
	s.invalidate(c)
}

// Attach registers a Reader that has observed the current epoch: no code
// invalidated so far is reachable to it.
func (s *Cache) Attach() *Reader {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := newReader(s, s.currentEpoch())
	s.readers = append(s.readers, r)
	return r
}

// Reclaim frees every invalidated code each attached Reader has passed a
// quiescent point since; with no Reader attached, every invalidated code.
func (s *Cache) Reclaim() error {
	if s.pending.Load() == 0 {
		if p := s.freeErr.Swap(nil); p != nil {
			return *p
		}
		return nil
	}
	s.mu.Lock()
	floor := s.epoch.Load()
	for _, r := range s.readers {
		floor = min(floor, r.seenEpoch())
	}
	n := 0
	for n < len(s.invalidated) && s.invalidated[n].invalidatedAt() <= floor {
		n++
	}
	freed := slices.Clone(s.invalidated[:n])
	s.invalidated = slices.Delete(s.invalidated, 0, n)
	s.pending.Add(-int64(n))
	s.mu.Unlock()

	var err error
	for _, c := range freed {
		err = errors.Join(err, c.Free())
	}
	if p := s.freeErr.Swap(nil); p != nil {
		err = errors.Join(err, *p)
	}
	return err
}

// Close frees every published and invalidated code; the caller must ensure no
// interpreter is inside native code first.
func (s *Cache) Close() error {
	s.mu.Lock()
	codes, osr, invalidated := s.codes, s.osr, s.invalidated
	s.codes, s.osr, s.invalidated = nil, nil, nil
	s.mu.Unlock()

	var err error
	for i := range codes {
		if c := codes[i].Load(); c != nil {
			err = errors.Join(err, c.Free())
		}
	}
	for _, c := range osr {
		err = errors.Join(err, c.Free())
	}
	for _, c := range invalidated {
		err = errors.Join(err, c.Free())
	}
	if p := s.freeErr.Swap(nil); p != nil {
		err = errors.Join(err, *p)
	}
	s.pending.Store(0)
	return err
}

// Quiesce marks a quiescent point: r holds no code it read before, so
// Reclaim may free every code invalidated up to now. Entry re-checks a cached
// code's Invalidated after it. It stores only when the epoch moved, so a
// steady quiescent point issues no store.
func (r *Reader) Quiesce() {
	e := r.cache.currentEpoch()
	if r.seenEpoch() != e {
		r.seen.Store(e)
	}
}

// Detach unregisters r; its interpreter must hold no code from then on.
func (r *Reader) Detach() {
	r.cache.detach(r)
}

func newReader(cache *Cache, epoch uint64) *Reader {
	r := &Reader{cache: cache}
	r.seen.Store(epoch)
	return r
}

func (s *Cache) invalidate(c *Code) {
	e := s.currentEpoch() + 1
	c.invalidate(e)
	s.epoch.Store(e)
	s.invalidated = append(s.invalidated, c)
	s.pending.Add(1)
}

func (s *Cache) currentEpoch() uint64 {
	return s.epoch.Load()
}

func (s *Cache) detach(r *Reader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k := slices.Index(s.readers, r); k >= 0 {
		s.readers = slices.Delete(s.readers, k, k+1)
	}
}

func (r *Reader) seenEpoch() uint64 {
	return r.seen.Load()
}
