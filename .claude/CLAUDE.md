# CLAUDE.md

@../AGENTS.md

Claude-specific additions to the shared contract. `AGENTS.md` owns repository workflow and shared rules.

## Execution

- For multi-file, uncertain, or risky work: explore → plan → edit.
- Choose the falsifying verification target before editing.
- Re-read every changed file against `AGENTS.md` and its owner docs before completion.
- Run the Completion Gate before reporting done, staging, committing, or opening a PR.
- Final reports must state evidence, changed docs, and every skipped validation or benchmark with its reason.

## Benchmarks

- `make benchmark-pr` — quick report.
- `make benchmark-core` — canonical suite.
- `make benchmark-compare` — tagged external comparisons only.
- Do not substitute benchmark results for correctness tests.

## RepoWise

Use RepoWise as indexed evidence, never as the source of truth. `AGENTS.md`, owner docs, and the live tree govern.

- `get_overview()` — once when entering an unfamiliar repository.
- `get_context(...)` — triage files, modules, symbols, callers, or history before editing.
- `get_answer(...)` / `get_why(...)` — repository questions and design rationale.
- `get_risk(...)` / `get_change_risk(...)` — impact and diff review.
- `get_health(...)` — post-change structural/code-health check.
- `repowise update` — refresh stale index evidence.
- Use `repowise distill <cmd>` for noisy commands; omitted output is recoverable with `repowise expand`.

Raw `Read` of every file to be edited remains required even when RepoWise has already indexed it.
