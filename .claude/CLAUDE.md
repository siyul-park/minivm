# CLAUDE.md

@../AGENTS.md

## Claude Code Overlay

This file imports the common agent contract from `AGENTS.md`. Keep shared rules there so Claude Code and Codex stay aligned.

Use this file only for Claude-specific execution reminders.

## Required Claude Flow

1. Start from the imported `AGENTS.md` workflow.
2. For multi-file, uncertain, or risky work, explore first, then plan, then edit.
3. Give yourself a runnable verification target before editing whenever possible.
4. Before reporting done, complete the `AGENTS.md` Completion Gate and the Claude checklist below.
5. Show evidence in the final summary: tests run, docs updated, and any intentionally skipped simplification.

## Claude Checklist

Before reporting done, re-read every changed code and test file and verify:

- `docs/coding-patterns.md` §2 and §16 were applied.
- Top-down package/API/flow review and bottom-up review of every affected symbol are complete.
- Every symbol has a current owner and reason; duplicate, wrapper, dead, mergeable, or private candidates were resolved.
- Functions keep one abstraction level, single-use helpers remain inline unless they isolate real mechanics, and receiver-owned behavior is a method.
- Declarations form a caller-before-callee staircase; fields and groups follow specification §9.
- Public APIs remain minimal and compatible unless the user explicitly authorized a change.
- Unit tests use the production package, tests use public observable behavior except for specification §12's narrow internal-invariant allowance, arrange is isolated per subtest, files match production owners, and assertions use `require`.
- Performance work has a baseline, profile-guided owner, correctness checks, and reproducible before/after evidence (specification §14).
- Documentation, workflow, and conventions were updated in their specification §15 owner documents.

Record any intentionally skipped simplification, validation, or benchmark target and its reason in the final summary.

## Verification Commands

Use `make benchmark-pr` for the quick benchmark report, `make benchmark-core` for the canonical suite, `make benchmark-compare` only for tagged external comparisons, `make fuzz` for bounded fuzz smoke, and `make coverage-check` for the recorded coverage floor.
