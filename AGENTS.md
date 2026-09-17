# Agent Instructions

`minivm` is a Go-native bytecode VM. Threaded execution is the semantic baseline; ARM64 JIT is an optimization with threaded fallback.

| Owner | Scope |
|---|---|
| `docs/coding-patterns.md` | Go design, naming, API patterns |
| `docs/testing.md` | test methodology and validation |
| `docs/refactoring.md` | structural review and simplification |
| topic docs | architecture facts |

More-specific `AGENTS.md` narrows this file. Direct user/system/developer instructions override repository rules.

## Rules

1. Inspect current code, tests, owner docs, and nested instructions before designing.
2. Run `git status --short`; preserve unrelated changes; do not stage or commit unless requested.
3. Make the smallest complete change. Reject speculative abstractions, wrappers, aliases, shims, duplicate policy, and future-only extension points.
4. Every changed symbol needs a clear owner, boundary, purpose, and name.
5. One behavior has one implementation. Keep JIT policy in `internal/jit`; target mechanics in target packages.
6. Change generated files only through their generator; run `make generate` and `make check-generated`.
7. Canonical topic docs describe supported/current state; guides describe procedures; plans and audits preserve history or future work. Keep one canonical owner per topic.
8. Non-trivial structural changes require `docs/refactoring.md` to reach simplification fixed point.

## Delegation

Split work into independently verifiable units. Every handoff states:

- **Scope** — paths, symbols, behavior, exclusions.
- **Contract** — required result and invariants.
- **Proof** — narrowest falsifying check plus broader checks.
- **Output** — changes, decisions, evidence, risks.

Do not delegate vague objectives. Parallel work requires disjoint write ownership or an explicit integration boundary. Delegated output is evidence; verify it against the tree and owner docs.

| Work | Model | Use |
|---|---|---|
| Exploration | Haiku / Sonnet | bounded search, tracing, evidence |
| Implementation | Sonnet | TDD implementation, focused refactor |
| Design / escalation | Opus | ownership, representation, architecture, high-risk sequencing |

## Workflow

1. Check status and repository instructions.
2. Read affected code, tests, and owner docs; define contract and proof.
3. Follow `docs/testing.md` for test design and TDD.
4. Implement the smallest owning change.
5. Review ownership, boundaries, naming, declaration order, and semantic parity.
6. Run `docs/refactoring.md` for structural changes.
7. Run focused checks, then repository gates.
8. Re-read every changed file against repository rules.

## Task Router

| Task | Read | Owners | Focused check |
|---|---|---|---|
| Opcode | `instruction-set.md`, `guides/add-opcode.md` | `instr/`, `internal/codegen/` | `go test ./instr ./internal/codegen ./interp` |
| Runtime / memory | `architecture.md`, `memory-model.md` | `interp/`, `types/` | `go test ./interp ./types` |
| JIT / ARM64 | `jit-internals.md`, `value-representation.md` | `internal/jit/`, `internal/journal/`, `internal/asm/` | `go test ./internal/... ./interp` |
| Optimization | `pass-system.md` | `analysis/`, `transform/`, `optimize/`, `pass/`, `internal/ssa/transform/` | package tests |
| Verification | `verification.md` | `program/verify.go`, `instr/type.go` | `go test ./program ./interp` |
| Debug / profile | `debugging.md`, `profile.md` | `debug/`, `interp/`, `prof/` | package tests |

## Completion

Completion requires: clear ownership; necessary symbols only; contract-constraining tests; applicable structural review at fixed point; current generated output; canonical docs without stale duplicates; and applicable checks. Report skipped validation.

Repository baseline:

```bash
make check-generated check-tidy check-fmt vet
go test ./...
go test -race ./...
GOOS=linux GOARCH=arm64 go build ./...
GOOS=linux GOARCH=arm64 go test -exec=true ./...
git diff --check
```bash
