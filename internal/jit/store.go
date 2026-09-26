package jit

import (
	"errors"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Store owns published code and the native entry table.
type Store struct {
	// natives is the table read by native CALLs.
	natives []uintptr
	// codes publishes one atomic pointer per address.
	codes []atomic.Pointer[Code]
	// osr publishes OSR code per (address, IP): never a call target, so it
	// is never in natives, and is looked up only through Find or CodeAt.
	osr     map[key]*Code
	retired []*Code
	active  atomic.Int64
	// pending is len(retired), readable without mu so Reclaim with nothing
	// retired — the common case — takes no lock.
	pending atomic.Int64

	mu sync.Mutex
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
	return &Store{natives: make([]uintptr, size), codes: make([]atomic.Pointer[Code], size)}
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
// Otherwise it installs c — natives[c.Address] = c.Native() — when c.Address
// is in range and c.Tier is above the published code's tier (none published
// counts as zero), retiring the code it replaces. Either way it reports
// whether c installed; a stale c is freed instead.
func (s *Store) Publish(c *Code) bool {
	s.mu.Lock()
	installed := c.Address >= 0 && c.Address < len(s.natives)
	if installed && c.OSR {
		k := key{c.Address, c.IP}
		if _, ok := s.osr[k]; ok {
			installed = false
		} else {
			if s.osr == nil {
				s.osr = map[key]*Code{}
			}
			s.osr[k] = c
		}
		s.mu.Unlock()
		if !installed {
			_ = c.Free()
		}
		return installed
	}
	var old *Code
	if installed {
		old = s.codes[c.Address].Load()
		var published Tier
		if old != nil {
			published = old.Tier
		}
		installed = c.Tier > published
	}
	if installed {
		atomic.StoreUintptr(&s.natives[c.Address], c.Native())
		s.codes[c.Address].Store(c)
		if old != nil {
			old.retired.Store(true)
			s.retired = append(s.retired, old)
			s.pending.Add(1)
		}
	}
	s.mu.Unlock()

	if !installed {
		_ = c.Free()
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
	c.retired.Store(true)
	s.retired = append(s.retired, c)
	s.pending.Add(1)
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
	c.retired.Store(true)
	s.retired = append(s.retired, c)
	s.pending.Add(1)
}

// Enter brackets an interpreter's native execution, suspended exits
// included. An interpreter MUST Enter before it uses a code it read: a code
// is retired only after its natives entry is cleared, so an interpreter
// that saw the old entry is counted.
func (s *Store) Enter() {
	s.active.Add(1)
}

// Leave ends the bracket Enter began.
func (s *Store) Leave() {
	s.active.Add(-1)
}

// Reclaim frees every retired code when no interpreter is inside native
// code, else does nothing. It takes the retired list before it reads the
// count: a code retired later may be running in an interpreter that entered
// after the read.
func (s *Store) Reclaim() error {
	if s.pending.Load() == 0 {
		return nil
	}
	s.mu.Lock()
	retired := s.retired
	if s.active.Load() != 0 {
		s.mu.Unlock()
		return nil
	}
	s.retired = nil
	s.pending.Add(-int64(len(retired)))
	s.mu.Unlock()

	var err error
	for _, c := range retired {
		err = errors.Join(err, c.Free())
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
	return err
}
