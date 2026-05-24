package interp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/siyul-park/minivm/program"
)

// Pool hands out Interpreter instances bound to a shared Program for use across
// goroutines. Each Interpreter owns its runtime state and JIT buffer; callers
// must borrow one per goroutine via Get/Put or Run.
type Pool struct {
	prog *program.Program
	opts []func(*option)
	size int

	idle chan *Interpreter
	live atomic.Int64

	mu     sync.RWMutex
	closed bool
}

var ErrPoolClosed = errors.New("pool closed")

// NewPool builds a pool that lends up to size Interpreters constructed from
// prog with opts. size <= 0 is normalized to 1. Interpreters are created lazily
// on Get; NewPool itself does not allocate JIT memory.
func NewPool(prog *program.Program, size int, opts ...func(*option)) *Pool {
	if size <= 0 {
		size = 1
	}
	shared := make([]func(*option), 0, len(opts)+1)
	shared = append(shared, WithMarshaler(newMarshaler()))
	shared = append(shared, opts...)
	return &Pool{
		prog: prog,
		opts: shared,
		size: size,
		idle: make(chan *Interpreter, size),
	}
}

// Get returns an Interpreter ready for use. It reuses an idle one if available,
// otherwise creates a new one when below the size cap, otherwise blocks until
// another goroutine calls Put or ctx is canceled. Returns ErrPoolClosed once
// the pool is closed.
func (p *Pool) Get(ctx context.Context) (*Interpreter, error) {
	if p.dead() {
		return nil, ErrPoolClosed
	}

	if i, err := p.take(); i != nil || err != nil {
		return i, err
	}

	if i, ok := p.grow(); ok {
		return i, nil
	}

	return p.wait(ctx)
}

// Put returns i to the pool after resetting its runtime state. If the pool is
// closed or already holds size idle Interpreters, i is closed instead so its
// JIT buffer is released.
func (p *Pool) Put(i *Interpreter) {
	if i == nil {
		return
	}
	i.Reset()

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		p.drop(i)
		return
	}

	select {
	case p.idle <- i:
	default:
		p.drop(i)
	}
}

// Close releases every idle Interpreter and prevents further Get/Put. Outstanding
// Interpreters are closed on their next Put. Close is idempotent; errors from
// individual Interpreter closures are aggregated via errors.Join.
func (p *Pool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.idle)
	p.mu.Unlock()

	var errs []error
	for i := range p.idle {
		if err := i.Close(); err != nil {
			errs = append(errs, err)
		}
		p.live.Add(-1)
	}
	return errors.Join(errs...)
}

func (p *Pool) dead() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.closed
}

// take returns an idle Interpreter without blocking, (nil, nil) if none is
// ready, or (nil, ErrPoolClosed) if Close raced ahead of the dead check.
func (p *Pool) take() (*Interpreter, error) {
	select {
	case i, ok := <-p.idle:
		if !ok {
			return nil, ErrPoolClosed
		}
		return i, nil
	default:
		return nil, nil
	}
}

// grow reserves a slot below the size cap and returns a fresh Interpreter, or
// (nil, false) if the cap is reached.
func (p *Pool) grow() (*Interpreter, bool) {
	for {
		live := p.live.Load()
		if live >= int64(p.size) {
			return nil, false
		}
		if p.live.CompareAndSwap(live, live+1) {
			return New(p.prog, p.opts...), true
		}
	}
}

func (p *Pool) wait(ctx context.Context) (*Interpreter, error) {
	select {
	case i, ok := <-p.idle:
		if !ok {
			return nil, ErrPoolClosed
		}
		return i, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *Pool) drop(i *Interpreter) {
	_ = i.Close()
	p.live.Add(-1)
}
