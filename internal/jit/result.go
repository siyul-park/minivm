package jit

import "github.com/siyul-park/minivm/prof"

// Result is the outcome of one Compiler.Compile call: which anchor and
// frontend it ran, what happened, and — when compilation emitted native
// code — the Code it produced.
type Result struct {
	Code     *Code
	Anchor   Anchor
	Frontend prof.Frontend
	Outcome  prof.CompileOutcome
	Reason   prof.CompileReason
	Err      error
}

// prefer returns whichever of current and candidate reports the
// higher-priority rejection reason, breaking a tie toward the later
// frontend. Compile calls it to keep the most informative outcome across
// frontends that both failed to emit anything.
func (current Result) prefer(candidate Result) Result {
	if reasonPriority(candidate.Reason) > reasonPriority(current.Reason) ||
		reasonPriority(candidate.Reason) == reasonPriority(current.Reason) && candidate.Frontend > current.Frontend {
		return candidate
	}
	return current
}

// reasonPriority orders compile-reason preference so prefer keeps the most
// diagnostic rejection reason when multiple frontends fail.
func reasonPriority(reason prof.CompileReason) int {
	switch reason {
	case prof.CompileReasonInvalidPlan:
		return 1
	case prof.CompileReasonLoweringRejected, prof.CompileReasonBackendUnavailable:
		return 2
	case prof.CompileReasonRegisterPressure:
		return 3
	case prof.CompileReasonBranchRange:
		return 4
	case prof.CompileReasonError:
		return 5
	default:
		return 0
	}
}
