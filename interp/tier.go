package interp

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/prof"
)

type counters struct {
	entry  *prof.Counter
	yields *prof.Counter
	exits  []*prof.Counter
}

// watchdog counts native entries, give-up exits, and bridge cycles for one
// installed anchor. Unlike counters, it is always live regardless of
// i.profiler: a give-up exit is one where the entry abandoned the work it was
// compiled for (see givesUp) rather than completing its job or leaving
// through a healthy loop-exit edge, so a high rate of it — not a high exit rate
// alone — is the signal that the installed entry should be retired (see
// Interpreter.retire). A bridge cycle (see Interpreter.bridge) is productive
// work and must never count as one, but an anchor that spends most of its
// entries bridging pays the same deopt/re-enter cost, so it is tracked and
// retired the same way, just through its own counter.
type watchdog struct {
	gaveUp  []bool // exit descriptor ID -> givesUp(reason)
	entries uint32
	giveUps uint32
	bridges uint32

	probe        probePhase
	probeSize    uint32
	probeCount   uint32
	probeRound   uint8
	probeStart   time.Time
	probeNative  time.Duration
	probeMean    float64
	probeM2      float64
	probeRetire  bool
	probePending bool
}

type probePhase uint8

const (
	probeWarm probePhase = iota
	probeNative
	probeShadow
	probeDecided
)

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

// retireWindow is the number of native entries a watchdog observes before
// judging whether an installed anchor is paying for itself (see watchdog).
const retireWindow = 1024

// retireGiveUpThreshold is the minimum count of give-up exits (see givesUp)
// within one retireWindow that marks the anchor as a net loss rather than a
// healthy kernel's normal loop-exit traffic.
const retireGiveUpThreshold = retireWindow / 4

const (
	probeWindowMin = 32
	probeWindowMax = 256
	probeRoundsMin = 3
	probeRoundsMax = 6
	probeMinGain   = 0.01
	probeZ         = 1.96
	probeError     = 0.05
)

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

// newWatchdog precomputes, for each of entry's exit descriptors, whether taking
// it means the entry gave up, so the watchdog's hot path only ever indexes a
// []bool keyed by descriptor ID.
func newWatchdog(entry jit.Entry) *watchdog {
	gaveUp := make([]bool, len(entry.Exits))
	for id, exit := range entry.Exits {
		gaveUp[id] = givesUp(exit.Reason)
	}
	wd := &watchdog{gaveUp: gaveUp, probeSize: probeWindowMin}
	if entry.Kind != jit.EntryFunction {
		wd.probe = probeDecided
	}
	return wd
}

// givesUp reports whether taking this exit means the native entry abandoned the
// work it was compiled for. A guard failure and a cold branch both say the
// recording predicted the program wrong, and a trace cut says the code knowingly
// stopped mid-function; each pays full bailout and re-entry for nothing. A loop
// exit is how a loop normally ends and a terminal op is a deopt the plan
// intended, so neither counts.
func givesUp(reason prof.ExitReason) bool {
	switch reason {
	case prof.ExitTraceCut, prof.ExitColdBranch,
		prof.ExitGuardKind, prof.ExitGuardShape, prof.ExitGuardBounds, prof.ExitGuardValue:
		return true
	default:
		return false
	}
}

// enter counts one invocation of the installed native entry.
func (w *watchdog) enter() {
	w.entries++
	if w.probe == probeWarm {
		w.probeCount++
		if w.probeCount == probeWindowMin {
			w.probe = probeNative
			w.probeCount = 0
		}
		return
	}
	if w.probe != probeNative {
		return
	}
	if w.probeCount == 0 {
		w.probeStart = time.Now()
	}
	w.probeCount++
	if w.probeCount == w.probeSize {
		w.probeNative = time.Since(w.probeStart)
		w.probe = probeShadow
		w.probeCount = 0
		w.probePending = true
	}
}

// shadowReach records one shadow reach. It returns true after the final reach
// so the caller can restore the native call-fast-path and decide the round.
func (w *watchdog) shadowReach() bool {
	if w.probe != probeShadow {
		return false
	}
	if w.probeCount == 0 {
		w.probeStart = time.Now()
	}
	w.probeCount++
	if w.probeCount != w.probeSize {
		return false
	}
	shadow := time.Since(w.probeStart)
	native := w.probeNative
	w.probeRound++
	if w.probeRound == 1 {
		w.probe = probeNative
		w.probeCount = 0
		return true
	}
	samples := w.probeRound - 1
	diff := 1 - float64(shadow)/float64(native)
	delta := diff - w.probeMean
	w.probeMean += delta / float64(samples)
	w.probeM2 += delta * (diff - w.probeMean)
	if samples >= probeRoundsMin {
		variance := w.probeM2 / float64(samples-1)
		bound := probeZ * math.Sqrt(variance/float64(samples))
		if w.probeMean-bound > probeMinGain {
			w.probeRetire = true
			w.probe = probeDecided
		} else if w.probeMean+bound < -probeMinGain {
			w.probe = probeDecided
		} else if samples == probeRoundsMax {
			w.probe = probeDecided
		} else if bound > probeError {
			w.probeSize = min(w.probeSize*2, uint32(probeWindowMax))
			w.probe = probeNative
		} else {
			w.probe = probeNative
		}
	} else {
		w.probe = probeNative
	}
	w.probeCount = 0
	return true
}

// exit counts one give-up fallback exit. encoded is i.journal[journal.CellExitID]
// exactly as counters.exit consumes it: the exit descriptor ID plus one, zero
// meaning no descriptor.
func (w *watchdog) exit(encoded uint64) {
	if encoded == 0 {
		return
	}
	id := int(encoded - 1)
	if id >= 0 && id < len(w.gaveUp) && w.gaveUp[id] {
		w.giveUps++
	}
}

// bridge counts one bridge cycle (see Interpreter.bridge). It is tracked
// separately from exit so a bridge never counts toward the give-up rate.
func (w *watchdog) bridge() {
	w.bridges++
}

// failed reports whether this anchor lost the window it just completed: its
// give-up rate or bridge rate reached retireGiveUpThreshold, so it is retired
// (see Interpreter.retire). A window shorter than retireWindow has decided
// nothing yet; a completed one always resets every counter, so the next window
// starts clean whichever way it went.
func (w *watchdog) failed() bool {
	if w.entries < retireWindow {
		return false
	}
	bad := w.giveUps >= retireGiveUpThreshold || w.bridges >= retireGiveUpThreshold
	w.entries, w.giveUps, w.bridges = 0, 0, 0
	return bad
}

// entered records one call into the current function. Threaded handlers report
// failures by panicking so dispatch can annotate them at the interpreter boundary.
func (i *Interpreter) entered() {
	if err := i.hit(); err != nil {
		panic(err)
	}
}

// hit records one hot event.
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
		if err := i.warm(addr, hits); err != nil {
			return err
		}
	}
	if i.cache != nil {
		request, ok := i.cache.claim(addr, i.threshold)
		if ok {
			return i.shared(request.root, request.trigger)
		}
	}
	return nil
}

// warm handles entry tracing and compilation once an event reaches the warmup
// window or threshold. Entry capture records the shallowest runtime state.
//
// Only an event raised at the entry itself may capture. A back edge is a hot
// event for the same function but stands mid-body, and recording the entry root
// from there replays the entry instructions against loop-carried locals - a
// state the function never reaches on entry, whose trace then plans nothing.
func (i *Interpreter) warm(addr int, hits uint64) error {
	if i.isCold(addr) {
		return nil
	}
	root := jit.Anchor{Addr: addr}
	if hits <= entryWarmup && i.fr.ip == 0 {
		i.tracer.capture(i, root)
	}
	if hits >= i.trigger && i.cache == nil && !i.tried[root] && i.settled(addr, hits) {
		i.tried[root] = true
		if err := i.compile(root); err != nil {
			return err
		}
	}
	i.checkCool(addr, root)
	return nil
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
	for _, l := range i.tracer.headers(i, addr) {
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
	for _, l := range i.tracer.headers(i, addr) {
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
	if i.cache != nil {
		i.cache.request(request{root: root, trigger: prof.TriggerHot})
		return nil
	}
	if err := i.compile(root); err != nil {
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

func (i *Interpreter) exit(root jit.Anchor) {
	hits := i.tracer.branch(i, root, jit.Anchor{Addr: i.fr.addr, IP: i.fr.ip})
	if i.cache != nil {
		if hits < exitThreshold || hits%exitThreshold != 0 {
			return
		}
		// Queue a side-exit build request without disturbing an active owner.
		i.cache.request(request{root: root, trigger: prof.TriggerSideExit})
		return
	}
	if hits != exitThreshold {
		return
	}
	if i.compiler == nil {
		return
	}
	i.samples.AddMetric("vm_jit_attempts_total", 1)
	result := i.attempt(i.compiler, root, prof.TriggerSideExit)
	if result.Err != nil {
		panic(result.Err)
	}
	if result.Code == nil {
		return
	}
	// A side exit asks for this root to be rebuilt with the exit's leg folded
	// in, and the answer is only an improvement if it is still a recording.
	// When the trace frontend cannot plan the tree any more the compiler falls
	// through to the static plan (see internal/jit/compiler.go's frontend
	// order), and installing that would swap the running recording - folded
	// legs, hoisted container - for the fallback that has neither.
	//
	// Module code keeps the behaviour it had. Its loop root competes with a
	// whole-module plan that is the program rather than one call of it, and
	// holding the recording there costs Control_Sieve about 10%.
	if live, ok := i.live[root]; ok && root.Addr != 0 && live.Frontend == prof.FrontendTrace {
		if rebuilt, ok := result.Code.Entries[root]; ok && rebuilt.Frontend != prof.FrontendTrace {
			// The code was still emitted, so it is still accounted; only the
			// dispatch slot stays with the recording that already owns it.
			i.account(result.Code)
			return
		}
	}
	i.install(result.Code, true)
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
func (i *Interpreter) checkRetire(a jit.Anchor, wd *watchdog, clearNatives bool) {
	if wd.probePending && clearNatives && a.Addr < len(i.natives) {
		wd.probePending = false
		atomic.StorePointer(&i.natives[a.Addr], nil)
	}
	if wd.probeRetire || wd.failed() {
		i.retire(a, clearNatives)
	}
}

// retire undoes install for anchor a once either watchdog signal finds a net
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
