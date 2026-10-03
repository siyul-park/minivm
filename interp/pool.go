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

// ErrPoolClosed is returned by Get once the pool is closed.
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

// share adopts a matching interpreter JIT runtime into the pool. The pool owns
// its reference; a mismatched runtime stays private. Interpreters without JIT
// have no shared runtime.
func (p *Pool) share(i *Interpreter) {
	// A pool exists to reuse its program: its members skip dormancy, which
	// may also leave a program without a JIT.
	if i.attach() != nil {
		i.spend(dormancy)
	}
	if i.native == nil {
		return
	}
	p.sharedMu.Lock()
	defer p.sharedMu.Unlock()

	if p.shared == nil {
		p.shared = i.native.shared.retain()
		return
	}
	// transform.Module is rebuilt per interpreter (newModule) rather than
	// canonically shared, so cheap pointer equality cannot tell whether two
	// runtimes serve the same program: DeepEqual on the module content is
	// the only check that can.
	if !reflect.DeepEqual(i.native.shared.module, p.shared.module) {
		return
	}
	// i's own runtime is unpublished and unreferenced from here: i runs on
	// the pool runtime, so a release failure only leaks its mappings and no
	// caller can recover it.
	_ = i.native.join(p.shared.retain())
}

func (p *Pool) drop(i *Interpreter) {
	// Put has no error channel and i is discarded either way.
	_ = i.Close()
	p.live.Add(-1)
}
