# Agent Instructions

`minivm` is a Go-native bytecode VM. Threaded execution is the semantic baseline; native compilation is a planned optimization rebuild.

## Precedence

- Direct user, system, or developer instructions `MUST` override repository rules on conflict. The agent `MUST` follow the higher instruction and `SHOULD` note the conflict in its output.
- A more-specific `AGENTS.md` `MUST` be treated as narrowing this file. On conflict, the more-specific file wins.
- `AGENTS.md` owns workflow. Topic docs own architecture facts. On a workflow-versus-fact question, the agent `MUST` follow `AGENTS.md` for process and the topic owner doc for behavior.

| Owner | Scope |
|---|---|
| `docs/coding-patterns.md` | Go design, naming, API patterns |
| `docs/testing.md` | test methodology and validation |
| `docs/refactoring.md` | structural review and simplification |
| topic docs | architecture facts |

## Rules

1. The agent `MUST` inspect current code, tests, owner docs, and nested instructions before designing a change.
2. The agent `MUST` run `git status --short` before editing; it `MUST NOT` modify files unrelated to the task, and it `MUST NOT` stage, commit, amend, push, or create a PR unless the user explicitly requested it.
3. The agent `MUST` implement the smallest complete change that satisfies the contract. It `MUST NOT` add speculative abstractions, wrappers, aliases, shims, duplicate policy, or future-only extension points.
4. Every changed symbol `MUST` have one clear owner, one boundary, one purpose, and one name that expresses its role or contract.
5. One behavior `MUST` have one implementation. Native compilation policy, when present, `MUST` stay in its compiler owner; target mechanics `MUST` stay in target packages.
6. Generated files `MUST NOT` be edited directly; the agent `MUST` change them only through their generator and `MUST` run `make generate` and `make check-generated`.
7. The agent `MUST` keep one canonical owner per topic. Canonical topic docs `MUST` describe supported/current state; guides `MUST` describe procedures; plans and audits `MAY` preserve history or future work. The agent `MUST NOT` duplicate a contract owned elsewhere; it `MUST` link to the owner instead.
8. A non-trivial structural change (package/type boundary, ownership, lifecycle, control flow, abstraction, public contract, or performance-sensitive structure) `MUST` apply `docs/refactoring.md` until the simplification fixed point is reached.

## Delegation

The agent `MUST` split delegated work into independently verifiable units. Every handoff `MUST` state:

- **Scope** — paths, symbols, behavior, exclusions.
- **Contract** — required result and invariants.
- **Proof** — narrowest falsifying check plus broader checks.
- **Output** — changes, decisions, evidence, risks.

The agent `MUST NOT` delegate a vague objective. Parallel work `MUST` have disjoint write ownership or an explicit integration boundary. Delegated output `MUST` be treated as evidence only; the agent `MUST` verify it against the tree and owner docs before accepting it.

| Work | Model | Use |
|---|---|---|
| Exploration | Haiku / Sonnet | bounded search, tracing, evidence |
| Implementation | Sonnet | TDD implementation, focused refactor |
| Design / escalation | Opus | ownership, representation, architecture, high-risk sequencing |

If a problem exceeds the agent's current capability to resolve reliably, the agent `MUST` consult an adviser for help from a higher-capability model.

## Workflow

The agent `MUST` perform the following steps in order and `MUST NOT` report completion while any applicable step is skipped without a recorded reason:

1. Check status and repository instructions.
2. Read affected code, tests, and owner docs; define contract and proof.
3. Follow `docs/testing.md` for test design and TDD.
4. Implement the smallest owning change.
5. Review ownership, boundaries, naming, declaration order, and semantic parity.
6. Apply `docs/refactoring.md` for structural changes.
7. Run focused checks, then repository gates.
8. Re-read every changed file against repository rules.

## Task Router

For each task type, the agent `MUST` read the listed owners before implementing and `MUST` run the listed focused check. Additional checks `MAY` be added when the change touches further packages.

| Task | Read | Owners | Focused check |
|---|---|---|---|
| Opcode | `instruction-set.md`, `guides/add-opcode.md` | `instr/`, `internal/codegen/` | `go test ./instr ./internal/codegen ./interp` |
| Runtime / memory | `architecture.md`, `memory-model.md` | `interp/`, `types/` | `go test ./interp ./types` |
| Native / ARM64 | `jit-internals.md`, `jit-lessons.md`, `value-representation.md` | `internal/asm/`, `internal/jit/`, `transform/`, `interp/native.go` | `go test ./internal/... ./transform ./interp` |
| Optimization | `pass-system.md` | `analysis/`, `transform/`, `optimize/`, `pass/` | package tests |
| Verification | `verification.md` | `program/verify.go`, `instr/type.go` | `go test ./program ./interp` |
| Debug / profile | `debugging.md`, `profile.md` | `debug/`, `interp/`, `prof/` | package tests |

## Completion

The agent `MUST` satisfy all of the following before reporting completion; any inapplicable item `MUST` be reported as skipped with a reason:

- clear ownership for every changed symbol;
- necessary symbols only;
- contract-constraining tests;
- applicable structural review at fixed point;
- current generated output;
- canonical docs without stale duplicates;
- applicable checks passed.

Repository baseline:

```bash
make check-generated check-tidy check-fmt vet
go test ./...
go test -race ./...
GOOS=linux GOARCH=arm64 go build ./...
GOOS=linux GOARCH=arm64 go test -exec=true ./...
git diff --check
```
