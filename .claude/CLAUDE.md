# CLAUDE.md

Claude-specific workflow gate. Read `AGENTS.md` first; it is the common contract for Claude Code and Codex.

## Required Flow

Before changing code or tests:

1. Read task-relevant docs from `AGENTS.md`.
2. Read `docs/coding-patterns.md` through its Fast Path.
3. Always apply §0, then apply the task-specific sections from the document's When to Read table.

Before reporting done:

1. Complete the Agent Completion Gate in `AGENTS.md`.
2. Re-read touched files against the checklist below.
3. Fix every convention violation found.
4. Record any convention-driven refactor intentionally left unapplied with the reason.

Do not summarize a change as complete until both the common gate and this Claude-specific gate pass.

## How to Use `docs/coding-patterns.md`

`docs/coding-patterns.md` is the authority; this file is only a Claude-specific enforcement checklist.

1. Start with §0 for simplicity, symbol review, algorithm review, and repeat-until-stable rules.
2. Add the task sections from the document's When to Read table.
3. Before finishing, re-run §0.7-§0.9 and §7.4 on every touched file.
4. For workflow or convention changes, update both `AGENTS.md` and `.claude/CLAUDE.md` as required by §8.

## Coding Style Summary

Use this as a section map, not a replacement for `docs/coding-patterns.md`.

- **Simplify first** — every symbol needs a reason to exist; remove, inline, merge, narrow, or make private before adding structure (§0.7).
- **Recheck algorithms** — prefer one direct pass, local state, exact ownership, and obvious runtime data flow; benchmark performance claims (§0.8).
- **Repeat until stable** — run another simplification pass until no safe symbol, ownership, control-flow, or algorithm improvement remains (§0.9).
- **Layout** — declarations follow the fixed 11-slot order; callers above callees; `With*` option functions may sit immediately above the constructor they configure (§1.3, §2.4).
- **Methods over functions** — a private function used by one type becomes a method on it, even with an unused receiver; single-use helpers stay inline (§1.4, §1.5).
- **One abstraction level** — entry points read as a narrative; push mechanics into intent-named helpers only when extraction is useful (§1.1, §1.4).
- **Struct fields** — lifecycle/policy -> infrastructure -> program data -> runtime state -> counters -> read-only config -> sync primitives (§2.5).
- **Errors** — explicit, wrapped with `%w`, sentinels preserved; panic only for invariants recovered at the execution boundary (§4).
- **Tests** — one test per public symbol; inline setup/run/scan; use `require`; avoid helpers by default (§6.1-§6.8).
- **Docs** — convention changes update `docs/coding-patterns.md`, `AGENTS.md`, and `.claude/CLAUDE.md`; invariant/pitfall changes update `docs/architecture.md` (§8).

## Pre-finish Self-Review

Re-read every new or modified file against this checklist before reporting a change as done. Each item maps to `docs/coding-patterns.md`.

- **Symbol and algorithm pass (§0.7-§0.9)** — review every touched file/type/interface/field/function/method/parameter/result/constant/variable. Remove or narrow anything without a current reason. Look for a simpler direct algorithm, then repeat until another pass finds no safe improvement.
- **File layout (§2.4)** — declarations follow the fixed 11-slot order: public type -> private type -> public const -> private const -> public value -> private value -> constructor function (`NewFoo`/`newFoo`) -> public function -> public method -> private method -> private function. Sentinel errors and interface assertions are values, placed after types. Private constructors stay above methods even though they are private functions. When converting a package function into a method (§1.5), move its declaration from private-function territory into method territory.
- **Method order and option exception (§1.3)** — within a slot, every caller is declared above its callees. Convenience wrappers (`Run`, `Close`, ...) appear above the lower-level methods they orchestrate. `With*` option functions may appear immediately above the constructor they configure; this is an allowed exception, not a declaration-order violation.
- **Single abstraction level (§1.1)** — if a public method directly mixes locks, channels, atomics, and select-blocks, extract intent-named helpers (`take`, `wait`, `drop`, ...) so the entry point reads as a short narrative at one level. Prefer short, intent-revealing names (`take`, not `tryRecvFromIdleChannel`).
- **Methods vs package functions (§1.5)** — every private package function is suspect. If only one type uses it, convert to a method on that type even when the receiver is unused. Strategy callbacks are no exception: define the helper as a method and pass `t.fn` (method value) at the call site. Package functions survive only when two or more types use them, the function is genuinely reusable public utility, or it is a constructor.
- **Single-use helpers stay inline (§1.4 + §1.5 counter-rule)** — do not extract a tiny helper used by exactly one caller just to make it a method. The rule is: two or more call sites in one type -> method; one call site -> inline.
- **Struct field layering (§2.5)** — fields group with blank lines as lifecycle/policy -> infrastructure -> program data -> runtime state -> mutable counters -> read-only config -> sync primitives. Bridge/cache state sits with the infrastructure it serves, not with runtime state. Plain `int`/`bool` config is read-only config near the bottom, not policy.
- **Cross-package boundary stays value-shaped (§0.5, §3.5)** — when package B integrates package A's output, do not expose B internals through a callback interface for A. Package A defines plain-value input/output structs; B fills input, calls one A entry point, and applies output itself.
- **One test per public symbol (§6.2, §6.3)** — behavior for an existing public method belongs in that method's `Test<Type>_<Method>` as a `t.Run`. Do not add a parallel top-level test for the same symbol.
- **Tests assert behavior (§6.1)** — never mutate unexported fields to fabricate state unreachable from the public API. White-box reads may be acceptable; white-box writes are not.
- **No test helpers by default (§6.1, §6.8)** — inline program construction, the run sequence, and tracer-state scans into each `t.Run`. Do not add production API purely to shorten a test (§3.5).
- **One helper owns cleanup or fallback (§1.4)** — repeated cleanup pairs or repeated callee preconditions at each call site signal one owner helper. JIT branch lowering routes through one `branchOrExit` rather than duplicating continuation eligibility checks per target.
- **No skipped phases without recording why (§0.9)** — when a planned simplification is not safe, record the reason in the final summary. Do not silently drop it.
