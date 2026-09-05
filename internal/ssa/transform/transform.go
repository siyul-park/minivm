// Package transform holds internal/ssa's transformation policies, one pass
// per concern - FoldPass, CSEPass, GuardPass, and DCEPass - exactly as the
// top-level transform package holds one transformation policy per pass for
// bytecode. It knows only ssa, graph, pass, instr, and types - never the JIT
// itself - so every pass here is correct and useful whether fn came from a
// bytecode frontend with no guard or deopt state at all, or from a trace
// frontend whose speculation this package's own GuardPass exists to clean up
// after.
//
// This package composes nothing: a caller builds its own
// pass.Pipeline[*ssa.Function] from the passes it needs, in the order it
// needs them. FoldPass must run before CSEPass sees a folded constant;
// CSEPass must run before GuardPass, because a guard's operand is only
// recognizably equal to an earlier guard's once CSEPass has unified the
// values they read; DCEPass runs last, because every earlier pass can leave
// behind an operation - a folded computation's now-unused operands, a
// deduplicated guard's now-unread OpState - that only liveness can tell is
// safe to drop. See docs/pass-system.md for the full ordering rationale and
// docs/architecture.md for this package's dependency boundary. optimize will
// own the leveled, user-facing composition of these passes once a
// bytecode-to-SSA-to-bytecode route exists to run them over
// *program.Program; until then, nothing in this package should grow back
// into an Optimizer-shaped composer.
package transform
