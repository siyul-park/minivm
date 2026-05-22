# CLAUDE.md

Claude-specific workflows only. Read `AGENTS.md` first.

## Code Changes

Before changing code, read `docs/coding-patterns.md` and any task-relevant docs listed in `AGENTS.md`.

Follow `docs/coding-patterns.md` unless the task or repository context requires a narrower rule.

## Pre-finish Self-Review

Re-read every new or modified file against this checklist before reporting a change as done. Each item maps to a `docs/coding-patterns.md` rule that has been missed on a first pass.

- **File layout (§2.4)** — declarations follow the fixed 10-slot order: public type → private type → public const → private const → public value → private value → public function → public method → private method → private function. Sentinel errors and interface assertions are values, placed after types — not before.
- **Method order (§1.3)** — within a slot, every caller is declared above its callees. Convenience wrappers (`Run`, `Close`, …) appear above the lower-level methods they orchestrate.
- **Single abstraction level (§1.1)** — if a public method directly mixes locks, channels, atomics, and select-blocks, extract intent-named helpers (`take`, `wait`, `drop`, …) so the entry point reads as a short narrative at one level. Prefer short, intent-revealing names (`take`, not `tryRecvFromIdleChannel`).
- **Struct field layering (§2.5)** — fields grouped with blank lines as config → infrastructure → program data → runtime state → raw counters. No policy or counter field interleaved between two state fields.
- **One test per public symbol (§6.3)** — a behavior that exercises an existing public method belongs in that method's `Test<Type>_<Method>` as a `t.Run`. Do not add a parallel top-level test for the same symbol.
- **Tests assert behavior (§6.1)** — never mutate unexported fields (`p.live.Add(1)`, etc.) to fabricate a state unreachable from the public API. That is testing defensive dead code, not behavior. White-box reads of unexported fields are fine; white-box writes are not.
- **No duplicated cleanup (§1.4)** — the same `_ = x.Close(); counter.Add(-1)` pair in multiple branches signals an extractable private helper.
