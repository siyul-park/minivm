package jit

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Store owns published code and the native entry table.
//
// Retired code is reclaimed by quiescent states: each retire advances the
// store's epoch and stamps the code with it, and each attached Reader
// publishes the epoch it observed at its last quiescent point. A code is
// freed once every Reader has observed its epoch.
type Store struct {
	// natives is the table read by native CALLs.
	natives []uintptr
	// codes publishes one atomic pointer per address.
	codes []atomic.Pointer[Code]
	// osr publishes OSR code per (address, IP): never a call target, so it
	// is never in natives, and is looked up only through Find or CodeAt.
	osr map[key]*Code
	// retired is in retire order, so its epochs ascend.
	retired []*Code
	readers []*Reader
	// epoch counts retires; written under mu.
	epoch atomic.Uint64
	// pending is len(retired), readable without mu so Reclaim with nothing
	// retired — the common case — takes no lock.
	pending atomic.Int64

	mu sync.Mutex
	// freeErr records the first Free failure from Publish's own uninstall
	// free; Reclaim and Close surface it.
	freeErr atomic.Pointer[error]
}

// Reader is one interpreter's registration with a Store: Reclaim frees no
// code retired after the Reader's last quiescent point.
type Reader struct {
	store *Store
	// seen is the store epoch at the Reader's last quiescent point.
	seen atomic.Uint64
}

// key identifies one OSR unit's published code.
type key struct {
	address, ip int
}

// NewStore returns a Store serving addresses [0, size).
func NewStore(size int) *Store {
	if size < 1 {
		panic("jit: store size must be positive")
	}
	return &Store{natives: make([]uintptr, size), codes: make([]atomic.Pointer[Code], size), osr: map[key]*Code{}}
}

// Natives returns the address of the natives table, stable for the Store's
// life: Context.Natives.
func (s *Store) Natives() uintptr {
	return uintptr(unsafe.Pointer(&s.natives[0]))
}

// Code returns the published code at address, or nil. It never blocks.
func (s *Store) Code(address int) *Code {
	if address < 0 || address >= len(s.codes) {
		return nil
	}
	return s.codes[address].Load()
}

// CodeAt returns the published OSR code at (address, ip), or nil. Unlike
// Code, it takes the mutex.
func (s *Store) CodeAt(address, ip int) *Code {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.osr[key{address, ip}]
}

// Find returns the published or retired code whose range holds pc, or nil.
func (s *Store) Find(pc uintptr) *Code {
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
	for _, c := range s.retired {
		if c.holds(pc) {
			return c
		}
	}
	return nil
}

// Publish takes ownership of c. An OSR code (c.OSR) is never a call target:
// it installs into the (address, IP) map, once, instead of natives[c.Address].
// Otherwise it installs c — natives[c.Address] = c.Native() — when c.Tier is
// above the published code's tier (none published counts as zero), retiring
// the code it replaces. Either way it reports whether c installed; a stale c,
// or one whose address is out of range, is freed instead.
func (s *Store) Publish(c *Code) bool {
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
					s.retire(old)
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

// Retire clears natives[address] and moves the published code to the
// retired list; a no-op when nothing is published at address. A retired
// code stays findable until Reclaim: suspended activations still run it.
func (s *Store) Retire(address int) {
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
	s.retire(c)
}

// RetireAt moves the published OSR code at (address, ip) to the retired
// list; a no-op when nothing is published there. A retired code stays
// findable until Reclaim.
func (s *Store) RetireAt(address, ip int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{address, ip}
	c, ok := s.osr[k]
	if !ok {
		return
	}
	delete(s.osr, k)
	s.retire(c)
}

// Attach registers a Reader that has observed the current epoch: no code
// retired so far is reachable to it.
func (s *Store) Attach() *Reader {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := newReader(s, s.currentEpoch())
	s.readers = append(s.readers, r)
	return r
}

// Reclaim frees every retired code each attached Reader has passed a
// quiescent point since; with no Reader attached, every retired code.
func (s *Store) Reclaim() error {
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
	for n < len(s.retired) && s.retired[n].retiredAt() <= floor {
		n++
	}
	freed := slices.Clone(s.retired[:n])
	s.retired = slices.Delete(s.retired, 0, n)
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

// Close frees every published and retired code; the caller must ensure no
// interpreter is inside native code first.
func (s *Store) Close() error {
	s.mu.Lock()
	codes, osr, retired := s.codes, s.osr, s.retired
	s.codes, s.osr, s.retired = nil, nil, nil
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
	for _, c := range retired {
		err = errors.Join(err, c.Free())
	}
	if p := s.freeErr.Swap(nil); p != nil {
		err = errors.Join(err, *p)
	}
	s.pending.Store(0)
	return err
}

// Quiesce marks a quiescent point: r holds no code it read before, so
// Reclaim may free every code retired up to now. Entry re-checks a cached
// code's Retired after it. It stores only when the epoch moved, so a
// steady quiescent point issues no store.
func (r *Reader) Quiesce() {
	e := r.store.currentEpoch()
	if r.seenEpoch() != e {
		r.seen.Store(e)
	}
}

// Detach unregisters r; its interpreter must hold no code from then on.
func (r *Reader) Detach() {
	r.store.detach(r)
}

func (s *Store) retire(c *Code) {
	e := s.currentEpoch() + 1
	c.retire(e)
	s.epoch.Store(e)
	s.retired = append(s.retired, c)
	s.pending.Add(1)
}

func newReader(store *Store, epoch uint64) *Reader {
	r := &Reader{store: store}
	r.seen.Store(epoch)
	return r
}

func (s *Store) currentEpoch() uint64 {
	return s.epoch.Load()
}

func (r *Reader) seenEpoch() uint64 {
	return r.seen.Load()
}

func (s *Store) detach(r *Reader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k := slices.Index(s.readers, r); k >= 0 {
		s.readers = slices.Delete(s.readers, k, k+1)
	}
}
