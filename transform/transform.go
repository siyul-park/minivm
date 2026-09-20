// Package transform holds every rewrite that runs over SSA and the two
// conversions that reach it from bytecode: Translate decodes a function into
// an ssa.Function against read-only Module evidence, emit writes an
// ssa.Function back out as bytecode, and SSAPass is the program-level pass
// that takes every function of a program the whole way round. Beside it,
// DedupPass is the one bytecode-only rewrite, collapsing a program's
// duplicate constants and types.
//
// The SSA transformation policies live here too, one pass per concern -
// FoldPass, PromotePass, ForwardPass, CSEPass, GuardPass, HoistPass, and
// DCEPass - and this package is the only implementation of each: an
// ahead-of-time optimizer reaches them over bytecode through SSAPass, and a
// native compiler reaches them over the same IR it lowers. A pass knows only
// ssa, graph, pass, instr, and types - never an interpreter or a target - so
// every one is correct and useful whether fn came from Translate with no
// guard at all or from an optimizing tier whose speculation GuardPass exists
// to clean up after.
//
// This package composes nothing: a caller builds its own
// pass.Pipeline[*ssa.Function] from the passes it needs, in the order it
// needs them. FoldPass must run before CSEPass sees a folded constant;
// PromotePass and ForwardPass must run before CSEPass, because a computation
// over a slot read twice is two computations until the second read is the
// first read's own value - PromotePass making that so across a merge, where
// ForwardPass by construction cannot; CSEPass must run before GuardPass,
// because a guard's operand is only recognizably equal to an earlier guard's
// once CSEPass has unified the values they read; DCEPass runs last, because
// every earlier pass can leave behind an operation - a folded computation's
// now-unused operands, an elided guard's now-unread OpState - that only
// liveness can tell is safe to drop. See docs/pass-system.md for the full
// ordering rationale and docs/architecture.md for this package's dependency
// boundary. optimize owns the leveled, user-facing composition of these
// passes, so nothing here should grow back into an Optimizer-shaped composer.
package transform
