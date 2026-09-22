package compile

import (
	"sync"
	"sync/atomic"

	"github.com/siyul-park/minivm/internal/jit"
)

// Job is one finished compile: its Unit and either its Code or why not.
type Job struct {
	Unit Unit
	Code *jit.Code
	Err  error
}

// Queue compiles at most one unit per address at a time.
type Queue struct {
	machine func() Machine

	pending []Unit
	// active holds every address queued, compiling, or finished but not
	// yet drained; Submit refuses a second one while it is set.
	active map[int]bool
	done   []Job
	closed bool
	// ready is len(done), readable without the lock so Drain on an empty
	// queue — every interpreted call's case — takes no lock.
	ready atomic.Int64

	mu   sync.Mutex
	cond *sync.Cond
	wg   sync.WaitGroup
}

// NewQueue returns a Queue whose workers each own one machine(). A nil
// machine or fewer than one worker is a programmer error and panics.
func NewQueue(machine func() Machine, workers int) *Queue {
	if machine == nil {
		panic("compile: nil machine factory")
	}
	if workers < 1 {
		panic("compile: at least one worker is required")
	}
	q := &Queue{machine: machine, active: map[int]bool{}}
	q.cond = sync.NewCond(&q.mu)
	q.wg.Add(workers)
	for range workers {
		go q.work()
	}
	return q
}

// Submit queues u for compilation. It reports false, never blocking, when a
// unit of u.Address is queued, compiling, or finished but not drained, or
// the queue is closed.
func (q *Queue) Submit(u Unit) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.active[u.Address] {
		return false
	}
	q.active[u.Address] = true
	q.pending = append(q.pending, u)
	q.cond.Signal()
	return true
}

// Drain returns every finished job in finish order and frees its address
// for Submit.
func (q *Queue) Drain() []Job {
	if q.ready.Load() == 0 {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	jobs := q.done
	q.done = nil
	q.ready.Store(0)
	for _, j := range jobs {
		delete(q.active, j.Unit.Address)
	}
	return jobs
}

// Close stops accepting units, lets units already queued finish, stops the
// workers, and returns the jobs finished but not yet drained.
func (q *Queue) Close() []Job {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()

	q.wg.Wait()
	return q.Drain()
}

func (q *Queue) work() {
	defer q.wg.Done()
	m := q.machine()
	for {
		q.mu.Lock()
		for len(q.pending) == 0 && !q.closed {
			q.cond.Wait()
		}
		if len(q.pending) == 0 {
			q.mu.Unlock()
			return
		}
		u := q.pending[0]
		q.pending = q.pending[1:]
		q.mu.Unlock()

		code, err := Compile(u, m)

		q.mu.Lock()
		q.done = append(q.done, Job{Unit: u, Code: code, Err: err})
		q.ready.Add(1)
		q.mu.Unlock()
	}
}
