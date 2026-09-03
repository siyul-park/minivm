package interp

import (
	"math"
	"sync/atomic"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/internal/jit/tier"
	"github.com/siyul-park/minivm/prof"
)

type counters struct {
	entry  *prof.Counter
	yields *prof.Counter
	exits  []*prof.Counter
}

// entryWarmup is how many hot events capture an entry trace before the entry
// compile fires. The compile is one-shot per root, so a trace has to already be
// published when it runs; capturing from the first few events records the
// shallowest states the function ever has. tracer.attemptLimit bounds the cost
// independently.
const entryWarmup = 8

// coolLimit is the number of consecutive unproductive observations - every
// compilation root already tried and nothing installed - before a function
// is cooled and stops being instrumented. See checkCool.
const coolLimit = 2

// nativeFrameLimit caps generated call depth to the stack space reserved by
// the ARM64 invoke trampoline (see arm64.StackReserve and
// TestARM64_StackReserve in tier_test.go). Deeper calls trap before moving
// SP.
const nativeFrameLimit = 128

// loopBudget is how many native loop back-edges run between safepoints. It is
// independent of tick so a hot loop amortizes the deopt/re-enter cost of a
// yield over many iterations while still polling for cancellation and fuel.
const loopBudget = 1 << 13

func (m counters) exit(encoded uint64) {
	if encoded == 0 {
		return
	}
	id := int(encoded - 1)
	if id >= 0 && id < len(m.exits) {
		m.exits[id].Inc()
	}
}

func (m counters) enter() {
	if m.entry != nil {
		m.entry.Inc()
	}
}

func (m counters) yield() {
	if m.yields != nil {
		m.yields.Inc()
	}
}

// entered records one call into the current function. Threaded handlers report
// failures by panicking so dispatch can annotate them at the interpreter boundary.
func (i *Interpreter) entered() {
	if err := i.hit(); err != nil {
		panic(err)
	}
}

// hit records one hot event. The count is this interpreter's own, and gates
// entry capture and cooling; the queue keeps the count everyone sharing it
// raised, and gates compilation.
func (i *Interpreter) hit() error {
	addr := i.fr.addr
	if i.trigger == 0 || addr < 0 || addr >= len(i.entries) {
		return nil
	}
	hits := i.entries[addr]
	if hits < math.MaxUint64 {
		hits++
		i.entries[addr] = hits
	}
	if hits <= entryWarmup || hits >= i.trigger {
		i.warm(addr, hits)
	}
	if !i.queue.Hit(addr, i.threshold) {
		return nil
	}
	return i.claim(addr)
}

// warm handles entry tracing and requests the entry compile once an event
// reaches the warmup window or threshold. Entry capture records the shallowest
// runtime state.
//
// Only an event raised at the entry itself may capture. A back edge is a hot
// event for the same function but stands mid-body, and recording the entry root
// from there replays the entry instructions against loop-carried locals - a
// state the function never reaches on entry, whose trace then plans nothing.
func (i *Interpreter) warm(addr int, hits uint64) {
	if i.isCold(addr) {
		return
	}
	root := jit.Anchor{Addr: addr}
	if hits <= entryWarmup && i.fr.ip == 0 {
		i.tracer.capture(i, root)
	}
	if hits >= i.trigger && !i.tried[root] && i.settled(addr, hits) {
		i.tried[root] = true
		i.queue.Add(compile.Request{Root: root, Trigger: prof.TriggerHot})
	}
	i.checkCool(addr, root)
}

// request queues a compile request and serves it here when this interpreter
// wins the queue for its function. A solo interpreter always wins, because
// nothing else holds its queue.
func (i *Interpreter) request(req compile.Request) error {
	i.queue.Add(req)
	return i.claim(req.Root.Addr)
}

// claim serves the next request queued for addr, or does nothing when another
// interpreter sharing the queue is already building addr.
func (i *Interpreter) claim(addr int) error {
	req, ok := i.queue.Claim(addr, i.threshold)
	if !ok {
		return nil
	}
	return i.serve(req)
}

// settled reports whether addr's entry root is ready to be compiled. A loop
// header is the better root of the two - it carries the hoisting and the native
// back-edge that the enclosing entry's copy of the same blocks does not - and an
// installed entry runs its loops inside itself, so their back edges stop
// reporting and they can never earn one. Headers therefore go first.
//
// The wait is bounded, because a header the function never actually reaches
// would otherwise hold its entry back forever.
func (i *Interpreter) settled(addr int, hits uint64) bool {
	if hits >= i.trigger+entryWarmup {
		return true
	}
	for _, l := range i.tracer.headers(i.instrs, addr) {
		// A header at ip 0 is this very root, not a separate one to wait for.
		if l.header != 0 && !i.tried[jit.Anchor{Addr: addr, IP: l.header}] {
			return false
		}
	}
	return true
}

// checkCool counts one unproductive observation when addr's entry root and
// every loop header returned by i.tracer.headers have already been attempted
// (recorded in i.tried). coolLimit consecutive such observations cool addr,
// which permanently stops instrumenting it. A root still waiting to be
// attempted resets nothing and costs one map lookup.
//
// Whether anything installed is deliberately not consulted. Once every root
// has been attempted there is nothing further to compile, so continuing to
// sample, capture, and observe back-edges only costs dispatch overhead - a
// function that installed native code pays that overhead in the tier that
// wins, and its installed entries keep running. checkRetire, not this, is what
// reacts to native code that turned out not to pay for itself.
func (i *Interpreter) checkCool(addr int, root jit.Anchor) {
	if !i.tried[root] {
		return
	}
	for _, l := range i.tracer.headers(i.instrs, addr) {
		if !i.tried[jit.Anchor{Addr: addr, IP: l.header}] {
			return
		}
	}
	if addr < 0 || addr >= len(i.misses) {
		return
	}
	if i.misses[addr] < math.MaxUint8 {
		i.misses[addr]++
	}
	if i.misses[addr] >= coolLimit {
		i.cool(addr)
	}
}

// backedge receives the exact target of a warmed backward branch, which is the
// loop header itself - no header scan is needed here. The arrival is also a hot
// event for the enclosing function, so a loop makes its own function's entry
// eligible without any instruction sampling. It drives cooling too: once every
// root of this function has been attempted, repeated arrivals are what
// eventually retire its instrumentation.
func (i *Interpreter) backedge(f *frame) error {
	if f.ip <= 0 {
		return nil
	}
	if i.isCold(f.addr) {
		return nil
	}
	if err := i.hit(); err != nil {
		return err
	}
	// The loop root waits for the same hot count as the entry root, so the
	// whole-function plan is always attempted first. Compiling the header first
	// would install a native loop that stops running this hook, and the function
	// it belongs to would never accumulate the events its own entry needs.
	if f.addr >= 0 && f.addr < len(i.entries) && i.entries[f.addr] < i.trigger {
		if live, ok := i.live[jit.Anchor{Addr: f.addr}]; !ok || live.Frontend != prof.FrontendStatic {
			return nil
		}
	}
	err := i.trace(f)
	i.checkCool(f.addr, jit.Anchor{Addr: f.addr})
	return err
}

// yielded reports a native loop yield through the same back-edge path as the threaded loop.
func (i *Interpreter) yielded() error {
	if err := i.backedge(i.fr); err != nil {
		return err
	}
	return i.safepoint()
}

func (i *Interpreter) trace(f *frame) error {
	root := jit.Anchor{Addr: f.addr, IP: f.ip}
	if i.exits[root] != nil || i.tried[root] {
		return nil
	}
	i.tried[root] = true
	result := i.tracer.capture(i, root)
	if result.trace == nil {
		return nil
	}
	if err := i.request(compile.Request{Root: root, Trigger: prof.TriggerHot}); err != nil {
		return err
	}
	// A loop header reached on the iteration that exits records the path out of
	// the loop rather than the loop, and the walk ends cut at some later header -
	// a partial. That plans nothing, and keeping it would serve the same
	// recording to every later arrival, so forget it and let the next arrival
	// record again; tracer.attemptLimit bounds the retries. A recording that
	// completed and still planned nothing is a real answer about this header, not
	// bad luck, and retrying only spends clones to reach the same plan.
	if root.IP != 0 && i.exits[root] == nil && result.outcome == prof.CaptureOutcomePartial {
		i.tracer.forget(root)
		delete(i.tried, root)
	}
	return nil
}

// exit counts one arrival at a native side exit and, every time the count
// crosses the threshold again, asks for its root to be rebuilt with the exit's
// leg folded in. A rebuild is retried rather than attempted once, because the
// trace tree keeps learning: a rebuild the tree could not plan at the first
// crossing regularly plans at a later one.
//
// A side-exit request is the one request the queue never discards as already
// built, because rebuilding a root that is already native is its whole point.
func (i *Interpreter) exit(root jit.Anchor) {
	hits := i.tracer.branch(i, root, jit.Anchor{Addr: i.fr.addr, IP: i.fr.ip})
	if hits < exitThreshold || hits%exitThreshold != 0 {
		return
	}
	if err := i.request(compile.Request{Root: root, Trigger: prof.TriggerSideExit}); err != nil {
		panic(err)
	}
}

// cool permanently stops instrumenting addr once every compilation root has
// been tried and nothing installed: further entry capture and back-edge
// observation would only pay dispatch overhead for no benefit (see checkCool).
// Reverting the function to the zero-overhead BR handler is what actually
// removes the loop instrumentation, since back-edge hooks are installed by
// default rather than upgraded into; sync still runs afterward so a peer's
// later publish can still be adopted (see install).
func (i *Interpreter) cool(addr int) {
	if addr < 0 || addr >= len(i.cold) || i.cold[addr] {
		return
	}
	i.cold[addr] = true
	if i.backedges[addr] {
		i.rethread(addr, false)
	}
}

// checkRetire advances the throughput probe after a native dispatch has
// finished, while keeping the existing give-up and bridge retirement window
// unchanged. clearNatives is true only for a function-entry anchor.
//
// The pending native call-fast-path clear is gated only on the bounds check,
// not on clearNatives: TakePending can only ever report true for a
// function-entry anchor (only EntryFunction anchors run the throughput probe
// at all, see tier.New), and clearNatives is exactly entry.Kind ==
// EntryFunction for the same anchor at every call site, so the two are
// already equivalent whenever the flag is set.
func (i *Interpreter) checkRetire(a jit.Anchor, wd *tier.Watchdog, clearNatives bool) {
	if wd.TakePending() && a.Addr < len(i.natives) {
		atomic.StorePointer(&i.natives[a.Addr], nil)
	}
	if wd.Retire() {
		i.retire(a, clearNatives)
	}
}

// retire undoes install for anchor a once either tier.Watchdog signal finds a net
// loss: the existing give-up/bridge window or the function-entry throughput
// probe. It restores the shadowed threaded handler saved in i.exits,
// clears a's function-entry call-fast-path slot in i.natives when
// clearNatives is set (a null slot already makes callers fall back at the
// CALL, see install), and calls cool so addr is neither re-instrumented nor
// recompiled. The threaded handler must be restored before cool, because
// cool may rethread addr's whole table and preserves whatever i.code
// currently holds at every live anchor (see rethread).
//
// retire only ever writes i.code, i.natives, and i.cold, all local to this
// Interpreter, so it never reaches into a pool's shared published module (see
// sync). It is safe to call from inside the very wrapper closure it replaces:
// the caller is already done making this dispatch's progress and is about to
// return to the Run loop.
func (i *Interpreter) retire(a jit.Anchor, clearNatives bool) {
	if a.Addr < 0 || a.Addr >= len(i.code) || a.IP < 0 || a.IP >= len(i.code[a.Addr]) {
		return
	}
	entry, live := i.live[a]
	if fn := i.exits[a]; fn != nil {
		i.code[a.Addr][a.IP] = fn
	}
	if live && i.profiler != nil {
		i.samples.RegisterRetirement(a.Addr, a.IP, entry.Kind.Profile(), entry.Frontend).Inc()
	}
	delete(i.live, a)
	delete(i.watchdogs, a)
	if clearNatives && a.Addr < len(i.natives) {
		atomic.StorePointer(&i.natives[a.Addr], nil)
	}
	i.cool(a.Addr)
}
