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

## CodeGraph

`AGENTS.md` owns the code-navigation rule. Claude-specific use:

- Structural questions (definition, callers, callees, impact, flow) go to `codegraph_*` tools before `Grep`/`Read` sweeps.
- `codegraph_context` first for task or area orientation; one `codegraph_explore` for the source it surfaces.
- `codegraph_trace` for "how does X reach Y"; `codegraph_impact` before changing a shared symbol.
- A response that starts with the staleness banner names files to `Read` directly; other files stay authoritative.
- Raw `Read` of every file to be edited remains required.
