package jit

import (
	"sync"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
)

// Slots is a writable indirection table that caller-JIT code reads when
// emitting a CALL to a statically-known target. Each slot holds the native
// entry address of the target function; before the target compiles, every
// slot points at a single fallback stub that returns control to the
// interpreter.
//
// Slot pointers are stable for the lifetime of the underlying Data region.
type Slots struct {
	slots map[int]unsafe.Pointer

	fallback asm.Callable
	data     *asm.Data

	mu sync.Mutex
}

// NewSlots builds a Slots backed by data. fallback is the address every
// freshly allocated slot points at until Set replaces it.
func NewSlots(data *asm.Data, fallback asm.Callable) *Slots {
	return &Slots{
		data:     data,
		fallback: fallback,
		slots:    make(map[int]unsafe.Pointer),
	}
}

// For returns the stable address of the slot for addr, lazily allocating it
// and initializing it with the fallback target on first use.
func (s *Slots) For(addr int) (unsafe.Pointer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.slots[addr]; ok {
		return p, nil
	}
	p, err := s.data.Alloc()
	if err != nil {
		return nil, err
	}
	s.data.Set(p, s.fallback.Addr())
	s.slots[addr] = p
	return p, nil
}

// Set atomically points the slot for addr at entry's native entry address.
func (s *Slots) Set(addr int, entry asm.Callable) error {
	p, err := s.For(addr)
	if err != nil {
		return err
	}
	s.data.Set(p, entry.Addr())
	return nil
}
