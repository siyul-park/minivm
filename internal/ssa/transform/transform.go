// Package transform holds internal/ssa's transformation policies, one pass
// per concern - FoldPass, ForwardPass, CSEPass, GuardPass, HoistPass, and
// DCEPass - and is the only implementation of each: an ahead-of-time
// optimizer reaches them over bytecode through transform.SSAPass, and a
// compile reaches them over the same IR it lowers. It knows only ssa, graph,
// pass, instr, and types - never the JIT itself - so every pass here is
// correct and useful whether fn came from a bytecode frontend with no guard
// or deopt state at all, or from a trace frontend whose speculation this
// package's own GuardPass exists to clean up after.
//
// This package composes nothing: a caller builds its own
// pass.Pipeline[*ssa.Function] from the passes it needs, in the order it
// needs them. FoldPass must run before CSEPass sees a folded constant;
// ForwardPass must run before CSEPass, because a computation over a slot read
// twice is two computations until the second read is the first read's own
// value; CSEPass must run before GuardPass, because a guard's operand is only
// recognizably equal to an earlier guard's once CSEPass has unified the
// values they read; DCEPass runs last, because every earlier pass can leave
// behind an operation - a folded computation's now-unused operands, a
// deduplicated guard's now-unread OpState - that only liveness can tell is
// safe to drop. See docs/pass-system.md for the full ordering rationale and
// docs/architecture.md for this package's dependency boundary. optimize owns
// the leveled, user-facing composition of these passes, so nothing here
// should grow back into an Optimizer-shaped composer.
package transform
