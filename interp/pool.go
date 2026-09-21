package interp

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/siyul-park/minivm/program"
)

// Pool hands out Interpreter instances bound to a shared Program for use across
// goroutines. Each Interpreter owns its runtime state; callers must borrow one
// per goroutine via Get/Put or Run.
type Pool struct {
	prog *program.Program
	opts []Option
	size int

	idle chan *Interpreter
	live atomic.Int64

	shared   *shared
	sharedMu sync.Mutex

	mu     sync.RWMutex
	closed bool
}

var ErrPoolClosed = errors.New("pool closed")

// NewPool builds a pool that lends up to size Interpreters constructed from
// prog with opts. size <= 0 is normalized to 1. Interpreters are created lazily
// on Get.
func NewPool(prog *program.Program, size int, opts ...Option) *Pool {
	if size <= 0 {
		size = 1
	}
	return &Pool{
		prog: prog,
		opts: append([]Option(nil), opts...),
		size: size,
		idle: make(chan *Interpreter, size),
	}
}

// Get returns an Interpreter ready for use. It reuses an idle one if available,
// otherwise creates a new one when below the size cap, otherwise blocks until
// another goroutine calls Put or ctx is canceled. Returns ErrPoolClosed once
// the pool is closed.
func (p *Pool) Get(ctx context.Context) (*Interpreter, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, ErrPoolClosed
	}

	select {
	case i := <-p.idle:
		p.mu.RUnlock()
		return i, nil
	default:
	}

	if i := p.grow(); i != nil {
		p.mu.RUnlock()
		return i, nil
	}
	p.mu.RUnlock()

	return p.wait(ctx)
}

// Put returns i to the pool after resetting its runtime state. If the pool is
// closed or already holds size idle Interpreters, i is closed instead.
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
	p.sharedMu.Lock()
	if p.shared != nil {
		errs = append(errs, p.shared.release())
		p.shared = nil
	}
	p.sharedMu.Unlock()
	return errors.Join(errs...)
}

// grow reserves a slot below the size cap and returns a fresh Interpreter, or
// nil when the cap is reached so the caller waits.
func (p *Pool) grow() *Interpreter {
	for {
		live := p.live.Load()
		if live >= int64(p.size) {
			return nil
		}
		if p.live.CompareAndSwap(live, live+1) {
			i := New(p.prog, p.opts...)
			p.share(i)
			return i
		}
	}
}

// share gives i's native JIT runtime to the pool: the first Interpreter's is
// adopted, with a reference of the pool's own that Close drops, as the pool's
// shared runtime, and a later one swaps onto it once
// its own freshly loaded constants match — which every Interpreter built
// from the same Program always does, so a mismatch leaves i on its own
// runtime instead of risking a shared one that does not actually agree with
// it. A no-op when i was built without WithThreshold.
func (p *Pool) share(i *Interpreter) {
	if i.native == nil {
		return
	}
	p.sharedMu.Lock()
	defer p.sharedMu.Unlock()

	if p.shared == nil {
		p.shared = i.native.shared.retain()
		return
	}
	if !reflect.DeepEqual(i.native.shared.module, p.shared.module) {
		return
	}
	own := i.native.shared
	i.native.shared = p.shared.retain()
	_ = own.release()
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
