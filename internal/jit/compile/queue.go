// Package compile coordinates native compilation between the interpreters
// sharing one program. Queue decides which root a compiler serves next, where
// that build runs, and keeps concurrent requests for one function from racing
// or duplicating one another; Store holds the code those builds publish and
// the executable buffers it was linked into.
//
// Neither type knows what an interpreter is: a job names an anchor and the
// event that raised it, a build is an opaque func, and published code is a
// jit.Code plus the asm.Buffer it lives in. A solo interpreter therefore
// drives the same seam through a private queue and store that a pool drives
// through shared ones.
package compile

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/prof"
)

// Job is one unit of queued compilation: the root to build and the event that
// raised it.
type Job struct {
	Root    jit.Anchor
	Trigger prof.Trigger
}

// Queue admits one build per function at a time and coalesces everything
// raised for that function while a build holds it. It also counts the hot
// events everyone sharing it raised, which is what makes a function's roots
// worth building at all, remembers which roots were built so a second holder
// does not repeat work the first already did, and decides whether a claimed
// build runs on the goroutine that claimed it or on its own worker.
type Queue struct {
	hits    []atomic.Int64
	state   []atomic.Int32
	active  []Job
	pending [][]Job
	built   map[jit.Anchor]bool

	builds []func()
	closed bool

	async bool

	wake *sync.Cond
	wg   sync.WaitGroup
	mu   sync.Mutex
}

// Option configures a Queue at construction.
type Option func(*option)

type option struct {
	async bool
}

// A function is idle with nothing to build, queued once a request waits for
// it, and building while one holder serves that request.
const (
	idle int32 = iota
	queued
	building
)

// WithAsync serves every claimed build on one worker goroutine instead of the
// goroutine that claimed it, so a caller keeps running while its compile does.
// Close stops that goroutine. A queue built without it holds no goroutine and
// needs no shutdown, which is what lets a caller nobody shares pay nothing for
// the seam.
func WithAsync() Option {
	return func(o *option) { o.async = true }
}

// New builds a queue covering size function addresses.
func New(size int, opts ...Option) *Queue {
	var opt option
	for _, o := range opts {
		o(&opt)
	}
	q := &Queue{
		hits:    make([]atomic.Int64, size),
		state:   make([]atomic.Int32, size),
		active:  make([]Job, size),
		pending: make([][]Job, size),
		built:   map[jit.Anchor]bool{},
		async:   opt.async,
	}
	q.wake = sync.NewCond(&q.mu)
	if q.async {
		q.wg.Add(1)
		go q.work()
	}
	return q
}

// Hit counts one hot event for addr and reports whether addr has reached
// threshold with a request waiting and no build in flight. The answer is an
// invitation to Claim rather than the claim itself, so a caller pays for the
// claim only when there is something to serve.
func (q *Queue) Hit(addr int, threshold int64) bool {
	if threshold < 0 || addr < 0 || addr >= len(q.hits) {
		return false
	}
	return q.hits[addr].Add(1) >= threshold && q.state[addr].Load() == queued
}

// Add queues next for its function unless the queue already answers it: a root
// that already has native code is discarded, and so is one the build in flight
// or a waiting request already covers. A re-request of a waiting root replaces
// it, and a side exit is inserted ahead of waiting hot roots because it carries
// newer trace work.
//
// A side exit is never discarded as built. It asks for a root already native to
// be rebuilt with the exit's leg folded in, which is the one request whose whole
// point is that the root was built before. A build that emitted nothing is not
// built either, so a caller that learns more about a root - a bounded recording
// it can retry deeper - may ask again.
func (q *Queue) Add(next Job) {
	addr := next.Root.Addr
	if addr < 0 || addr >= len(q.state) {
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if next.Trigger != prof.TriggerSideExit && q.built[next.Root] {
		return
	}
	if q.active[addr].covers(next) {
		return
	}
	pending := q.pending[addr]
	for idx, prior := range pending {
		if prior.covers(next) {
			return
		}
		if prior.Root != next.Root {
			continue
		}
		pending = slices.Delete(pending, idx, idx+1)
		break
	}
	if next.Trigger == prof.TriggerSideExit {
		insert := 0
		for insert < len(pending) && pending[insert].Trigger == prof.TriggerSideExit {
			insert++
		}
		pending = slices.Insert(pending, insert, next)
	} else {
		pending = append(pending, next)
	}
	q.pending[addr] = pending
	if q.state[addr].Load() == idle {
		q.state[addr].Store(queued)
	}
}

// Claim hands the caller the job waiting longest for addr and marks addr
// building, or reports false when addr has not reached threshold, nothing is
// waiting, or another caller already holds the build.
func (q *Queue) Claim(addr int, threshold int64) (Job, bool) {
	if threshold < 0 || addr < 0 || addr >= len(q.hits) {
		return Job{}, false
	}
	if q.state[addr].Load() != queued || q.hits[addr].Load() < threshold {
		return Job{}, false
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.state[addr].Load() != queued || len(q.pending[addr]) == 0 {
		return Job{}, false
	}
	next := q.pending[addr][0]
	q.pending[addr] = q.pending[addr][1:]
	q.active[addr] = next
	q.state[addr].Store(building)
	return next, true
}

// Serve runs the build claimed for one function. An async queue hands it to
// the worker and returns at once, so the caller keeps running while its
// compile does; every other queue - and any queue already closed - runs it on
// the calling goroutine. Either way build ends the claim itself with Done.
func (q *Queue) Serve(build func()) {
	q.mu.Lock()
	if q.async && !q.closed {
		q.builds = append(q.builds, build)
		q.wake.Signal()
		q.mu.Unlock()
		return
	}
	q.mu.Unlock()
	build()
}

// Done ends the build claimed for addr, taking code as what that build
// produced. Every root code emitted an entry for counts as built, so a later
// hot request for any of them is discarded rather than serving the same work
// twice; addr itself returns to queued when requests are still waiting and idle
// otherwise. A nil code ends a build that emitted nothing, which is also how a
// caller releases a claim it could not serve.
func (q *Queue) Done(addr int, code *jit.Code) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if code != nil {
		for root := range code.Entries {
			q.built[root] = true
		}
	}
	if addr < 0 || addr >= len(q.state) {
		return
	}
	q.active[addr] = Job{}
	if len(q.pending[addr]) == 0 {
		q.state[addr].Store(idle)
	} else {
		q.state[addr].Store(queued)
	}
}

// Close stops the worker once it has finished every build already handed to
// it, so nothing is still compiling or publishing when the code those builds
// produced is freed. A build claimed afterwards runs on the claiming
// goroutine, which is how a caller still running keeps compiling for itself.
// Close is idempotent, and a queue that never had a worker has nothing to stop.
func (q *Queue) Close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.wake.Broadcast()
	q.mu.Unlock()

	q.wg.Wait()
}

// covers reports whether j already asks for the same root with equal or higher
// priority than next. Side exits replace queued hot roots but never the
// reverse, so a hot request cannot displace either an active or pending exit.
func (j Job) covers(next Job) bool {
	return j.Trigger != prof.TriggerNone && j.Root == next.Root &&
		(j.Trigger == prof.TriggerSideExit || next.Trigger == prof.TriggerHot)
}

// work runs handed-off builds one at a time until Close has stopped the queue
// and nothing is left. It drains rather than abandons, because every queued
// build still holds a claim to end and an executable buffer to publish or
// free.
func (q *Queue) work() {
	defer q.wg.Done()

	for {
		q.mu.Lock()
		for len(q.builds) == 0 && !q.closed {
			q.wake.Wait()
		}
		if len(q.builds) == 0 {
			q.mu.Unlock()
			return
		}
		build := q.builds[0]
		q.builds[0] = nil
		q.builds = q.builds[1:]
		q.mu.Unlock()

		build()
	}
}
