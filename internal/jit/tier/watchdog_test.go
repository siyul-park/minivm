package tier_test

import (
	"testing"
	"time"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/tier"
	"github.com/siyul-park/minivm/prof"
	"github.com/stretchr/testify/require"
)

// maxProbeIterations bounds the drive helpers below so a probe that never
// reaches the phase a test expects fails fast instead of hanging. It is well
// above any window size the probe reaches on its own (see probeWindowMax).
const maxProbeIterations = 4000

// functionEntry builds a minimal jit.Entry that runs the throughput probe,
// with the given exit descriptors in order.
func functionEntry(exits ...jit.Exit) jit.Entry {
	return jit.Entry{Kind: jit.EntryFunction, Exits: exits}
}

// driveToShadow calls Enter, attempting Reach after each call, until the
// probe reports it is in the shadow phase, and returns how many Enter calls
// that took. The Reach call that detects the transition is itself a real
// recorded reach - exactly what a caller does on its very next dispatch -
// so the round it starts already has one reach spent by the time this
// returns. A delay applied before Enter during the warmup portion of the
// drive costs only wall-clock time: Enter only starts a timed window once
// the probe actually transitions to native measurement, so sleeping
// uniformly across the whole drive still produces a controlled
// native-window duration.
func driveToShadow(t *testing.T, w *tier.Watchdog, delay time.Duration) int {
	t.Helper()
	for i := 1; i <= maxProbeIterations; i++ {
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Enter()
		if shadow, _ := w.Reach(); shadow {
			return i
		}
	}
	t.Fatal("watchdog never reached the shadow phase")
	return 0
}

// driveShadowRound calls Reach until it reports the round complete, sleeping
// delay before each call, and returns how many calls that took.
func driveShadowRound(t *testing.T, w *tier.Watchdog, delay time.Duration) int {
	t.Helper()
	for i := 1; i <= maxProbeIterations; i++ {
		if delay > 0 {
			time.Sleep(delay)
		}
		if _, done := w.Reach(); done {
			return i
		}
	}
	t.Fatal("shadow round never completed")
	return 0
}

// driveToShadowMaybe is the non-fatal counterpart to driveToShadow, for a
// watchdog that may already be decided: once decided, Enter never reaches
// the shadow phase again, so this reports that instead of failing the test.
func driveToShadowMaybe(w *tier.Watchdog, delay time.Duration) (calls int, reached bool) {
	for i := 1; i <= maxProbeIterations; i++ {
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Enter()
		if shadow, _ := w.Reach(); shadow {
			return i, true
		}
	}
	return maxProbeIterations, false
}

func TestNew(t *testing.T) {
	t.Run("starts already decided for an entry kind that never probes", func(t *testing.T) {
		w := tier.New(jit.Entry{Kind: jit.EntryLoop})
		shadow, _ := w.Reach()
		require.False(t, shadow)
		require.False(t, w.Retire())
		require.False(t, w.TakePending())
		for i := 0; i < 1000; i++ {
			w.Enter()
		}
		shadow, _ = w.Reach()
		require.False(t, shadow, "a decided probe must never start timing")
		require.False(t, w.TakePending())
	})

	t.Run("starts probing for a function entry", func(t *testing.T) {
		w := tier.New(functionEntry())
		shadow, _ := w.Reach()
		require.False(t, shadow)
		driveToShadow(t, w, 0)
	})
}

func TestWatchdog_Enter(t *testing.T) {
	t.Run("excludes the warmup window from the throughput probe", func(t *testing.T) {
		w := tier.New(functionEntry())
		w.Enter()
		shadow, _ := w.Reach()
		require.False(t, shadow, "a single entry must not start timing the throughput probe")
		driveToShadow(t, w, 0)
	})

	t.Run("does nothing for an entry kind that never probes", func(t *testing.T) {
		w := tier.New(jit.Entry{Kind: jit.EntryModule})
		for i := 0; i < 500; i++ {
			w.Enter()
		}
		shadow, _ := w.Reach()
		require.False(t, shadow)
		require.False(t, w.TakePending())
	})
}

func TestWatchdog_Reach(t *testing.T) {
	t.Run("returns false outside the shadow phase", func(t *testing.T) {
		w := tier.New(functionEntry())
		shadow, done := w.Reach()
		require.False(t, shadow)
		require.False(t, done)
		w.Enter()
		shadow, done = w.Reach()
		require.False(t, shadow)
		require.False(t, done)
	})

	t.Run("the first round never decides", func(t *testing.T) {
		w := tier.New(functionEntry())
		driveToShadow(t, w, 0)
		driveShadowRound(t, w, 0)
		shadow, _ := w.Reach()
		require.False(t, shadow, "a completed round returns to native timing")
		require.False(t, w.Retire())
		// Not decided: the probe must be able to reach the shadow phase again.
		driveToShadow(t, w, 0)
	})

	t.Run("decides to retire when native measures slower than the shadowed handler", func(t *testing.T) {
		w := tier.New(functionEntry())
		const slow = 300 * time.Microsecond
		for round := 0; round < 4; round++ {
			driveToShadow(t, w, slow)
			driveShadowRound(t, w, 0)
		}
		shadow, _ := w.Reach()
		require.False(t, shadow)
		require.True(t, w.Retire())
	})

	t.Run("decides to keep when native measures faster than the shadowed handler", func(t *testing.T) {
		w := tier.New(functionEntry())
		const slow = 300 * time.Microsecond
		for round := 0; round < 4; round++ {
			driveToShadow(t, w, 0)
			driveShadowRound(t, w, slow)
		}
		shadow, _ := w.Reach()
		require.False(t, shadow)
		require.False(t, w.Retire())
	})

	t.Run("grows the window when one round's timing sharply diverges from the rest", func(t *testing.T) {
		w := tier.New(functionEntry())
		// Round 1 is always discarded (see "the first round never decides"),
		// so drive a quiet round 2 and round 3 first to establish a baseline
		// window size, recording it from round 3's native window. Round 4 is
		// the first one the probe actually evaluates (it needs 3 samples);
		// giving it a native window far slower than anything before it spikes
		// the sample variance enough to cross probeError regardless of sign,
		// which the probe answers by widening the window rather than deciding.
		const quiet = 50 * time.Microsecond
		var baseline int
		for round := 0; round < 3; round++ {
			baseline = driveToShadow(t, w, quiet)
			driveShadowRound(t, w, quiet)
		}
		const outlier = 5 * time.Millisecond
		_, reached := driveToShadowMaybe(w, outlier)
		require.True(t, reached, "the probe must still be probing before the outlier round")
		driveShadowRound(t, w, quiet)

		grown, reached := driveToShadowMaybe(w, quiet)
		require.True(t, reached, "one outlier round must not decide the verdict on its own")
		require.Greater(t, grown, baseline, "a sharp timing outlier must widen the probe window")
	})

	t.Run("settles within the round cap when the signal stays unclear", func(t *testing.T) {
		w := tier.New(functionEntry())
		// Both sides run under the same small, consistent delay every round,
		// so neither reliably reads faster than the other: the confidence
		// bound has nothing decisive to converge on, and the round cap is
		// what eventually ends the probe.
		const delay = 500 * time.Microsecond
		rounds := 0
		for ; rounds < 7; rounds++ {
			if _, reached := driveToShadowMaybe(w, delay); !reached {
				break
			}
			driveShadowRound(t, w, delay)
		}
		shadow, _ := w.Reach()
		require.False(t, shadow, "the probe must have reached a verdict")
		require.LessOrEqual(t, rounds, 7, "the round cap must force a decision by the 7th round")
	})
}

func TestWatchdog_Retire(t *testing.T) {
	giveUp := jit.Exit{Reason: prof.ExitGuardKind}

	t.Run("false before a verdict is reached", func(t *testing.T) {
		w := tier.New(functionEntry())
		require.False(t, w.Retire())
		driveToShadow(t, w, 0)
		require.False(t, w.Retire())
	})

	t.Run("true once the throughput probe decides native is the slower path", func(t *testing.T) {
		w := tier.New(functionEntry())
		const slow = 300 * time.Microsecond
		for round := 0; round < 4; round++ {
			driveToShadow(t, w, slow)
			driveShadowRound(t, w, 0)
		}
		require.True(t, w.Retire())
	})

	t.Run("false before the failure window fills", func(t *testing.T) {
		w := tier.New(functionEntry(giveUp))
		for i := 0; i < 5; i++ {
			w.Enter()
			w.Exit(1)
		}
		require.False(t, w.Retire())
	})

	t.Run("true when give-ups cross the threshold, and resets the window whether or not it failed", func(t *testing.T) {
		w := tier.New(functionEntry(giveUp))
		for i := 0; i < 2000; i++ {
			w.Enter()
			w.Exit(1)
		}
		require.True(t, w.Retire())
		require.False(t, w.Retire(), "a completed window must reset for the next one")
	})
}

func TestWatchdog_TakePending(t *testing.T) {
	t.Run("reports pending once the native window finishes measuring", func(t *testing.T) {
		w := tier.New(functionEntry())
		require.False(t, w.TakePending())
		driveToShadow(t, w, 0)
		require.True(t, w.TakePending())
	})

	t.Run("clears the flag once taken", func(t *testing.T) {
		w := tier.New(functionEntry())
		driveToShadow(t, w, 0)
		require.True(t, w.TakePending())
		require.False(t, w.TakePending())
	})

	t.Run("never becomes pending for an entry kind that never probes", func(t *testing.T) {
		w := tier.New(jit.Entry{Kind: jit.EntryModule})
		for i := 0; i < 100; i++ {
			w.Enter()
		}
		require.False(t, w.TakePending())
	})
}

func TestWatchdog_Reset(t *testing.T) {
	t.Run("restarts an interrupted native window", func(t *testing.T) {
		w := tier.New(functionEntry())
		w.Enter()
		w.Reset()
		// Still reachable: the warmup/native measurement was not corrupted.
		driveToShadow(t, w, 0)
	})

	t.Run("restarts an interrupted shadow window and clears the pending flag", func(t *testing.T) {
		w := tier.New(functionEntry())
		driveToShadow(t, w, 0)
		require.True(t, w.TakePending())
		w.Reset()
		shadow, _ := w.Reach()
		require.False(t, shadow, "an interrupted shadow window returns to native timing")
		require.False(t, w.TakePending())
		driveToShadow(t, w, 0)
	})

	t.Run("leaves a decided probe alone", func(t *testing.T) {
		w := tier.New(functionEntry())
		const slow = 300 * time.Microsecond
		for round := 0; round < 4; round++ {
			driveToShadow(t, w, slow)
			driveShadowRound(t, w, 0)
		}
		require.True(t, w.Retire())
		w.Reset()
		require.True(t, w.Retire(), "a decided verdict must survive a boundary reset")
		shadow, _ := w.Reach()
		require.False(t, shadow)
	})
}

func TestWatchdog_Exit(t *testing.T) {
	giveUp := jit.Exit{Reason: prof.ExitGuardKind}
	notGiveUp := jit.Exit{Reason: prof.ExitLoop}

	t.Run("counts a give-up reason toward the failure window", func(t *testing.T) {
		w := tier.New(functionEntry(giveUp, notGiveUp))
		for i := 0; i < 2000; i++ {
			w.Enter()
			w.Exit(1) // descriptor 0: giveUp
		}
		require.True(t, w.Retire())
	})

	t.Run("does not count a non-give-up reason", func(t *testing.T) {
		w := tier.New(functionEntry(giveUp, notGiveUp))
		for i := 0; i < 2000; i++ {
			w.Enter()
			w.Exit(2) // descriptor 1: notGiveUp
		}
		require.False(t, w.Retire())
	})

	t.Run("ignores a zero descriptor", func(t *testing.T) {
		w := tier.New(functionEntry(giveUp))
		for i := 0; i < 2000; i++ {
			w.Enter()
			w.Exit(0)
		}
		require.False(t, w.Retire())
	})

	t.Run("ignores an out-of-range descriptor", func(t *testing.T) {
		w := tier.New(functionEntry(giveUp))
		for i := 0; i < 2000; i++ {
			w.Enter()
			w.Exit(99)
		}
		require.False(t, w.Retire())
	})
}

func TestWatchdog_Bridge(t *testing.T) {
	t.Run("counts toward the failure window independently of give-ups", func(t *testing.T) {
		w := tier.New(functionEntry())
		for i := 0; i < 2000; i++ {
			w.Enter()
			w.Bridge()
		}
		require.True(t, w.Retire())
	})
}
