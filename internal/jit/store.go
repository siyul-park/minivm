package jit

import (
	"errors"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Store publishes native code. It owns the natives table native calls read
// and every Code handed to Publish until that Code is freed.
type Store struct {
	// natives is Context.Natives: native code reads its entries, so writes
	// to them go through atomic.StoreUintptr.
	natives []uintptr
	codes   map[int]*Code
	retired []*Code
	active  atomic.Int64

	mu sync.Mutex
}

// NewStore returns a Store serving addresses [0, size).
func NewStore(size int) *Store {
	if size < 1 {
		panic("jit: store size must be positive")
	}
	return &Store{natives: make([]uintptr, size), codes: map[int]*Code{}}
}

// Natives returns the address of the natives table, stable for the Store's
// life: Context.Natives.
func (s *Store) Natives() uintptr {
	return uintptr(unsafe.Pointer(&s.natives[0]))
}

// Code returns the published code at address, or nil.
func (s *Store) Code(address int) *Code {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.codes[address]
}

// Find returns the published or retired code whose range holds pc, or nil.
func (s *Store) Find(pc uintptr) *Code {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.codes {
		if holds(c, pc) {
			return c
		}
	}
	for _, c := range s.retired {
		if holds(c, pc) {
			return c
		}
	}
	return nil
}

// Publish takes ownership of c. It installs c — natives[c.Address] =
// c.Entry() — when c.Address is in range and c.Tier is above the published
// code's tier (none published counts as zero), retiring the code it
// replaces, and reports true. A stale c is freed instead and reports false.
func (s *Store) Publish(c *Code) bool {
	s.mu.Lock()
	installed := c.Address >= 0 && c.Address < len(s.natives)
	var old *Code
	if installed {
		old = s.codes[c.Address]
		var published Tier
		if old != nil {
			published = old.Tier
		}
		installed = c.Tier > published
	}
	if installed {
		atomic.StoreUintptr(&s.natives[c.Address], c.entry)
		s.codes[c.Address] = c
		if old != nil {
			s.retired = append(s.retired, old)
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
	c, ok := s.codes[address]
	if !ok {
		return
	}
	atomic.StoreUintptr(&s.natives[address], 0)
	delete(s.codes, address)
	s.retired = append(s.retired, c)
}

// Enter brackets an interpreter's native execution, suspended exits
// included. An interpreter MUST Enter before it reads Natives or Code to
// enter native code: a code is retired only after its natives entry is
// cleared, so an interpreter that saw the old entry is counted.
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
	s.mu.Lock()
	retired := s.retired
	if s.active.Load() != 0 {
		s.mu.Unlock()
		return nil
	}
	s.retired = nil
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
	codes, retired := s.codes, s.retired
	s.codes, s.retired = nil, nil
	s.mu.Unlock()

	var err error
	for _, c := range codes {
		err = errors.Join(err, c.Free())
	}
	for _, c := range retired {
		err = errors.Join(err, c.Free())
	}
	return err
}

func holds(c *Code, pc uintptr) bool {
	return pc >= c.entry && pc < c.entry+uintptr(c.size)
}
