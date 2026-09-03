package compile

import (
	"sync"
	"sync/atomic"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/jit"
)

// Store is the native code the interpreters sharing one program install from,
// together with the executable buffers that code was linked into. It is
// append-only and reference counted: a published mapping stays executable for
// as long as any holder can still dispatch into it, so the buffers are freed
// only once the last one detaches.
type Store struct {
	code   atomic.Pointer[[]*jit.Code]
	bufs   []*asm.Buffer
	refs   atomic.Int64
	shared atomic.Bool
	closed bool

	mu sync.Mutex
}

// NewStore builds a store held by its creator alone.
func NewStore() *Store {
	s := &Store{}
	s.refs.Store(1)
	code := []*jit.Code{}
	s.code.Store(&code)
	return s
}

// Attach admits one more holder, or reports false once the store is closed.
func (s *Store) Attach() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return false
	}
	s.refs.Add(1)
	s.shared.Store(true)
	return true
}

// Detach drops one holder, freeing every buffer once the last one leaves.
func (s *Store) Detach() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.release()
}

// Close drops the creator's own hold and refuses further Attach. It is
// idempotent.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	return s.release()
}

// Shared reports whether anyone but the creator ever held the store. Code can
// then arrive from another goroutine, so a holder has to poll Code for it;
// the creator of a store nobody else attached to sees only what it published
// itself.
func (s *Store) Shared() bool {
	return s.shared.Load()
}

// Publish appends code and takes over buf's lifetime. Code that emitted no
// entry publishes nothing, but its buffer is still retained, because a
// rejected build may have already linked a callable into it.
func (s *Store) Publish(code *jit.Code, buf *asm.Buffer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if buf != nil {
		s.bufs = append(s.bufs, buf)
	}
	if code == nil || len(code.Entries) == 0 {
		return
	}
	published := s.code.Load()
	next := make([]*jit.Code, 0, len(*published)+1)
	next = append(next, (*published)...)
	next = append(next, code)
	s.code.Store(&next)
}

// Code returns the gen'th published code, or false once gen has caught up
// with everything published so far.
func (s *Store) Code(gen int) (*jit.Code, bool) {
	published := s.code.Load()
	if published == nil || gen < 0 || gen >= len(*published) {
		return nil, false
	}
	return (*published)[gen], true
}

func (s *Store) release() error {
	if s.refs.Add(-1) > 0 {
		return nil
	}
	var err error
	for _, buf := range s.bufs {
		if e := buf.Free(); e != nil && err == nil {
			err = e
		}
	}
	s.bufs = nil
	return err
}
