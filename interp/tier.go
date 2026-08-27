package interp

import "github.com/siyul-park/minivm/prof"

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
}

// nativeFrameLimit caps generated call depth to the stack space reserved by
// the ARM64 invoke trampoline. Deeper calls trap before moving SP.
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
func newWatchdog(entry native) *watchdog {
	gaveUp := make([]bool, len(entry.exits))
	for id, exit := range entry.exits {
		gaveUp[id] = givesUp(exit.reason)
	}
	return &watchdog{gaveUp: gaveUp}
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
