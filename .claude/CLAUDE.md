# CLAUDE.md

Claude-specific workflows only. Read `AGENTS.md` first.

## Code Changes

Before changing code, read `docs/coding-patterns.md` and any task-relevant docs listed in `AGENTS.md`.

Follow `docs/coding-patterns.md` unless the task or repository context requires a narrower rule.

## Pre-finish Self-Review

Re-read every new or modified file against this checklist before reporting a change as done. Each item maps to a `docs/coding-patterns.md` rule that has been missed on a first pass.

- **File layout (§2.4)** — declarations follow the fixed 10-slot order: public type → private type → public const → private const → public value → private value → public function → public method → private method → private function. Sentinel errors and interface assertions are values, placed after types — not before. When converting a package function into a method (§1.5), move the declaration out of slot 10 into slot 9.
- **Method order (§1.3)** — within a slot, every caller is declared above its callees. Convenience wrappers (`Run`, `Close`, …) appear above the lower-level methods they orchestrate. Constructors (`New`) appear above the `With*` option functions they consume.
- **Single abstraction level (§1.1)** — if a public method directly mixes locks, channels, atomics, and select-blocks, extract intent-named helpers (`take`, `wait`, `drop`, …) so the entry point reads as a short narrative at one level. Prefer short, intent-revealing names (`take`, not `tryRecvFromIdleChannel`).
- **Methods vs package functions (§1.5)** — every private package function is suspect. If only one type uses it, convert to a method on that type even when the receiver is unused. Strategy callbacks are no exception: define the helper as a method and pass `t.fn` (method value) at the call site. Package functions survive only when ≥2 types use them, the function is genuinely reusable public utility, or it is a constructor.
- **Single-use helpers stay inline (§1.4 + §1.5 counter-rule)** — do not extract a tiny helper used by exactly one caller just to make it a method. If the body is ≤~15 straight-line lines, inline it; the method conversion rule does not justify ceremony around one-shot logic. The two rules together: ≥2 call sites in one type → method; 1 call site → inline. Never re-extract just to move into slot 9.
- **Struct field layering (§2.5)** — fields grouped with blank lines as policy/lifecycle → infrastructure → program data → runtime state → mutable counters → read-only config → sync primitives. Bridge/cache state (e.g. a "what's been jitted" map) sits with the infrastructure it serves, not with the runtime state that mutates beside it. Plain `int`/`bool` config (thresholds, cutoff, tick) is read-only config, near the bottom — not policy.
- **Cross-package boundary uses value-type structs (§0.5 + §1.5)** — when package B integrates package A's output, do NOT expose package B's internals through a `View`-style interface for A to call back into. Package A defines plain-value input/output structs (e.g. `Call`/`Outcome`); B fills the input, hands them to a single A entry point, applies the output itself. The contract is two structs, never an interface shaped around B's storage.
- **One test per public symbol (§6.3)** — a behavior that exercises an existing public method belongs in that method's `Test<Type>_<Method>` as a `t.Run`. Do not add a parallel top-level test for the same symbol.
- **Tests assert behavior (§6.1)** — never mutate unexported fields (`p.live.Add(1)`, etc.) to fabricate a state unreachable from the public API. That is testing defensive dead code, not behavior. White-box reads of unexported fields are fine; white-box writes are not.
- **No duplicated cleanup (§1.4)** — the same `_ = x.Close(); counter.Add(-1)` pair in multiple branches signals an extractable private helper.
- **No skipped phases without recording why** — when a refactor plan lists a step you cannot apply (e.g. "extract shared tail from `whole`/`blocks`" but the middles diverge enough that the param list explodes), record the reason in the final summary. Do not silently drop the step; future passes will reach the same conclusion and waste the same effort.
