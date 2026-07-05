# CLAUDE.md

Claude-specific workflows only. Read `AGENTS.md` first.

## Code Changes

Before changing code or tests, read `docs/coding-patterns.md` and any task-relevant docs listed in `AGENTS.md`. Treat that read as mandatory for each editing task, not optional background context.

Follow `docs/coding-patterns.md` unless the task or repository context requires a narrower rule. Before reporting done, re-read touched files against the checklist below and the relevant `docs/coding-patterns.md` sections. This review is a strict completion gate: fix every convention violation you find, and record any convention-driven refactor you intentionally leave unapplied with the reason.

## Coding Style Summary

`docs/coding-patterns.md` is the authority; this is the short form.

- **Always consult style docs before editing and finishing** — read `docs/coding-patterns.md` before any code/test modification, then strictly review every touched code/test file against the task-relevant sections before reporting done.
- **Layout** — declarations follow the fixed 11-slot order (§2.4); callers above callees; `With*` may sit immediately above the constructor they configure (§1.3).
- **Methods over functions** — a private function used by one type becomes a method on it, even with an unused receiver (§1.5); but a single-use ≤~15-line helper stays inline (§1.4).
- **One abstraction level** — entry points read as a narrative; push mechanics into intent-named helpers (§1.1).
- **Struct fields** — layered policy → infrastructure → program data → runtime state → counters → read-only config → mutex (§2.5).
- **Errors** — explicit, wrapped with `%w`, sentinels preserved; panic only in interpreter-threaded paths recovered by `Run` (§4).
- **Tests** — one test per public symbol (§6.3); inline setup/run/scan with no test helpers (§6.1, §6.8); assert behavior, never write unexported fields (§6.1).
- **No duplication** — collapse repeated cleanup or a repeated branch-or-fallback decision into one helper (§1.4).
- **Docs** — a new convention is incomplete without the matching `docs/` + `AGENTS.md`/`CLAUDE.md` update (§8).

## Pre-finish Self-Review

Re-read every new or modified file against this checklist before reporting a change as done. Each item maps to a `docs/coding-patterns.md` rule that has been missed on a first pass.

- **File layout (§2.4)** — declarations follow the fixed 11-slot order: public type → private type → public const → private const → public value → private value → constructor function (`NewFoo`/`newFoo`) → public function → public method → private method → private function. Sentinel errors and interface assertions are values, placed after types — not before. Private constructors stay above methods even though they are private functions. When converting a package function into a method (§1.5), move its declaration from private-function territory into method territory.
- **Method order (§1.3)** — within a slot, every caller is declared above its callees. Convenience wrappers (`Run`, `Close`, …) appear above the lower-level methods they orchestrate. Functional options may appear immediately above the constructor they configure.
- **Single abstraction level (§1.1)** — if a public method directly mixes locks, channels, atomics, and select-blocks, extract intent-named helpers (`take`, `wait`, `drop`, …) so the entry point reads as a short narrative at one level. Prefer short, intent-revealing names (`take`, not `tryRecvFromIdleChannel`).
- **Methods vs package functions (§1.5)** — every private package function is suspect. If only one type uses it, convert to a method on that type even when the receiver is unused. Strategy callbacks are no exception: define the helper as a method and pass `t.fn` (method value) at the call site. Package functions survive only when ≥2 types use them, the function is genuinely reusable public utility, or it is a constructor.
- **Single-use helpers stay inline (§1.4 + §1.5 counter-rule)** — do not extract a tiny helper used by exactly one caller just to make it a method. If the body is ≤~15 straight-line lines, inline it; the method conversion rule does not justify ceremony around one-shot logic. The two rules together: ≥2 call sites in one type → method; 1 call site → inline. Never re-extract just to move into slot 9.
- **Struct field layering (§2.5)** — fields grouped with blank lines as policy/lifecycle → infrastructure → program data → runtime state → mutable counters → read-only config → sync primitives. Bridge/cache state (e.g. a "what's been tried" map) sits with the infrastructure it serves, not with the runtime state that mutates beside it. Plain `int`/`bool` config (thresholds, cutoff, tick) is read-only config, near the bottom — not policy.
- **Cross-package boundary uses value-type structs (§0.5 + §1.5)** — when package B integrates package A's output, do NOT expose package B's internals through a `View`-style interface for A to call back into. Package A defines plain-value input/output structs (e.g. `Call`/`Outcome`); B fills the input, hands them to a single A entry point, applies the output itself. The contract is two structs, never an interface shaped around B's storage.
- **One test per public symbol (§6.3)** — a behavior that exercises an existing public method belongs in that method's `Test<Type>_<Method>` as a `t.Run`. Do not add a parallel top-level test for the same symbol.
- **Tests assert behavior (§6.1)** — never mutate unexported fields (`p.live.Add(1)`, etc.) to fabricate a state unreachable from the public API. That is testing defensive dead code, not behavior. White-box reads of unexported fields are fine; white-box writes are not.
- **No test helpers (§6.1 + §6.8)** — inline program construction, the run sequence, and any tracer-state scan into each `t.Run`. Do not extract a `branchTreeProgram()`, a `runEvalI32()` run wrapper, or a `traceReturnsConst()` scan, even for JIT white-box introspection that nests loops or repeats across subtests. Do not add production API to a type purely to shorten a test (§0.5).
- **No duplicated cleanup or fallback decision (§1.4)** — the same `_ = x.Close(); counter.Add(-1)` pair across branches, or re-stating a callee's precondition at every call site to pick between it and a fallback, signals an extractable helper. JIT branch lowering routes through one `branchOrExit` rather than guarding each target with `continuation`'s own eligibility condition.
- **No skipped phases without recording why** — when a refactor plan lists a step you cannot apply (e.g. "extract shared tail from `whole`/`blocks`" but the middles diverge enough that the param list explodes), record the reason in the final summary. Do not silently drop the step; future passes will reach the same conclusion and waste the same effort.
