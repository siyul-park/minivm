// Package tier holds the throughput/give-up probe that decides whether an
// installed native entry is worth keeping. It is pure: it observes counts and
// durations the caller reports and answers a retire/keep verdict, holding no
// interpreter state itself. interp drives the actual tier-up mechanism -
// hot-event sampling, tracing, compiling, installing, and retiring - and
// consults a Watchdog for that verdict; this package never imports interp.
package tier

import (
	"math"
	"time"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/prof"
)

// Watchdog decides whether one installed native anchor is still worth
// keeping, from two independent signals it accumulates call by call: a
// give-up/bridge failure rate over a fixed count of entries, and a
// wall-clock throughput probe that times native dispatch against the
// shadowed threaded handler it would otherwise fall back to. Retire combines
// both signals, so the caller never has to know there are two.
type Watchdog struct {
	// phase is the throughput probe's own state machine: entries are sampled
	// to time a native window, then a shadow window running the threaded
	// handler instead, and the two are compared round over round until the
	// comparison is confident enough to decide, or the round cap decides it.
	phase   phase
	size    uint32
	count   uint32
	round   uint8
	start   time.Time
	native  time.Duration
	mean    float64
	m2      float64
	retire  bool
	pending bool

	// entries, giveups, and bridges count native invocations and their
	// failures since the count last reached entryLimit (see Retire).
	entries uint32
	giveups uint32
	bridges uint32

	// gaveup classifies each of the anchor's exit descriptors once at
	// construction, so Exit's hot path only ever indexes a []bool.
	gaveup []bool
}

// phase is the throughput probe's state machine. It starts at phaseWarm,
// cycles between phaseNative and phaseShadow one round at a time, and ends
// at phaseDecided once a verdict is reached.
type phase uint8

const (
	phaseWarm phase = iota
	phaseNative
	phaseShadow
	phaseDecided
)

// sizeMin and sizeMax bound how many entries or shadow reaches one probe
// round samples: a round starts at the minimum and doubles, capped at the
// maximum, whenever its confidence bound is too wide to trust.
//
// roundsMin is the fewest paired native/shadow samples the probe collects
// before it will consider a verdict; roundsMax is the most it collects
// before it decides regardless, so a signal that never converges still
// resolves in bounded time.
//
// gain is the smallest mean relative-speedup the probe accepts as decisive
// once its confidence bound (z standard errors, a 95% interval) clears it;
// margin is the widest confidence bound the probe tolerates before it
// doubles the window instead of trusting the mean.
const (
	sizeMin   = 32
	sizeMax   = 256
	roundsMin = 3
	roundsMax = 6
	gain      = 0.01
	z         = 1.96
	margin    = 0.05
)

// entryLimit is the number of native entries a Watchdog observes before
// judging whether an installed anchor is paying for itself.
const entryLimit = 1024

// badLimit is the minimum count of give-up exits (see givesUp) or bridge
// cycles within one entryLimit that marks the anchor as a net loss rather
// than a healthy kernel's normal loop-exit traffic.
const badLimit = entryLimit / 4

// New precomputes, for each of entry's exit descriptors, whether taking it
// means the entry gave up, so the hot path only ever indexes a []bool keyed
// by descriptor ID. Only a function-entry anchor runs the throughput probe;
// every other kind starts already decided, since the probe compares a call's
// native cost against its shadowed threaded cost and only a function entry
// has a single well-defined call boundary to time.
func New(entry jit.Entry) *Watchdog {
	gaveup := make([]bool, len(entry.Exits))
	for id, exit := range entry.Exits {
		gaveup[id] = givesUp(exit.Reason)
	}
	w := &Watchdog{size: sizeMin, gaveup: gaveup}
	if entry.Kind != jit.EntryFunction {
		w.phase = phaseDecided
	}
	return w
}

// Enter counts one invocation of the installed native entry and advances the
// throughput probe's warmup and native-timing windows.
func (w *Watchdog) Enter() {
	w.entries++
	if w.phase == phaseWarm {
		w.count++
		if w.count == sizeMin {
			w.phase = phaseNative
			w.count = 0
		}
		return
	}
	if w.phase != phaseNative {
		return
	}
	if w.count == 0 {
		w.start = time.Now()
	}
	w.count++
	if w.count == w.size {
		w.native = time.Since(w.start)
		w.phase = phaseShadow
		w.count = 0
		w.pending = true
	}
}

// Reach reports whether the throughput probe is currently timing the
// shadowed threaded handler and, if so, records this reach against it. The
// caller must dispatch through the shadowed handler instead of native code
// whenever shadow is true; done reports whether this was the round's final
// reach, at which point Retire reports the round's verdict. Calling it
// outside the shadow phase is a safe no-op, so a caller may call it
// unconditionally on every dispatch to learn which path to take.
func (w *Watchdog) Reach() (shadow, done bool) {
	if w.phase != phaseShadow {
		return false, false
	}
	if w.count == 0 {
		w.start = time.Now()
	}
	w.count++
	if w.count != w.size {
		return true, false
	}
	shadowElapsed := time.Since(w.start)
	native := w.native
	w.round++
	if w.round == 1 {
		w.phase = phaseNative
		w.count = 0
		return true, true
	}
	samples := w.round - 1
	diff := 1 - float64(shadowElapsed)/float64(native)
	delta := diff - w.mean
	w.mean += delta / float64(samples)
	w.m2 += delta * (diff - w.mean)
	if samples >= roundsMin {
		variance := w.m2 / float64(samples-1)
		bound := z * math.Sqrt(variance/float64(samples))
		if w.mean-bound > gain {
			w.retire = true
			w.phase = phaseDecided
		} else if w.mean+bound < -gain {
			w.phase = phaseDecided
		} else if samples == roundsMax {
			w.phase = phaseDecided
		} else if bound > margin {
			w.size = min(w.size*2, uint32(sizeMax))
			w.phase = phaseNative
		} else {
			w.phase = phaseNative
		}
	} else {
		w.phase = phaseNative
	}
	w.count = 0
	return true, true
}

// Retire reports whether this anchor should retire right now, from either
// signal it tracks: the throughput probe decided native measures slower
// than the shadowed threaded handler, or the give-up/bridge count it just
// reached entryLimit with lost. Fewer than entryLimit entries has decided
// nothing yet; reaching entryLimit always resets every counter, so the next
// count starts clean whichever way it went. Once the throughput probe has
// already decided to retire, the entry count is not consulted: the caller
// is about to discard this Watchdog either way.
func (w *Watchdog) Retire() bool {
	if w.retire {
		return true
	}
	if w.entries < entryLimit {
		return false
	}
	bad := w.giveups >= badLimit || w.bridges >= badLimit
	w.entries, w.giveups, w.bridges = 0, 0, 0
	return bad
}

// TakePending reports whether the native call-fast-path slot for this
// anchor still needs to be cleared - the throughput probe finished timing
// the native window and is about to start shadow timing, so callers must
// stop entering native code directly until the probe is decided - and
// clears the flag so a later call reports false until it is set again.
func (w *Watchdog) TakePending() bool {
	pending := w.pending
	w.pending = false
	return pending
}

// Reset discards an incomplete throughput sample. Warmup and completed
// verdicts remain intact; an interrupted timed pair is restarted from a
// fresh native window so elapsed time outside the measured span is never
// included.
func (w *Watchdog) Reset() {
	if w.phase == phaseShadow {
		w.phase = phaseNative
	}
	w.count = 0
	w.start = time.Time{}
	w.native = 0
	w.pending = false
}

// Exit counts one give-up fallback exit. encoded is the exit descriptor ID
// plus one, as the caller's journal cell stores it, with zero meaning no
// descriptor.
func (w *Watchdog) Exit(encoded uint64) {
	if encoded == 0 {
		return
	}
	id := int(encoded - 1)
	if id >= 0 && id < len(w.gaveup) && w.gaveup[id] {
		w.giveups++
	}
}

// Bridge counts one bridge cycle. It is tracked separately from Exit so a
// bridge never counts toward the give-up rate.
func (w *Watchdog) Bridge() {
	w.bridges++
}

// givesUp reports whether taking this exit means the native entry abandoned
// the work it was compiled for. A guard failure and a cold branch both say
// the recording predicted the program wrong, and a trace cut says the code
// knowingly stopped mid-function; each pays full bailout and re-entry for
// nothing. A loop exit is how a loop normally ends and a terminal op is a
// deopt the plan intended, so neither counts.
func givesUp(reason prof.ExitReason) bool {
	switch reason {
	case prof.ExitTraceCut, prof.ExitColdBranch,
		prof.ExitGuardKind, prof.ExitGuardShape, prof.ExitGuardBounds, prof.ExitGuardValue:
		return true
	default:
		return false
	}
}
