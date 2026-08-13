# CLAUDE.md

@../AGENTS.md

Claude-specific additions to the shared contract above. Keep every shared rule in `AGENTS.md` so Claude Code and Codex stay aligned.

## Execution

- For multi-file, uncertain, or risky work: explore, then plan, then edit.
- Pick a runnable verification target before editing, not after.
- Run the `AGENTS.md` Completion Gate before reporting done, committing a stage, or opening a PR.
- Report evidence in the final summary: commands run, docs updated, and any intentionally skipped simplification, validation, or benchmark with its reason.

## Benchmarks

Use `make benchmark-pr` for a quick report and `make benchmark-core` for the canonical suite. `make benchmark-compare` is only for tagged external comparisons.
