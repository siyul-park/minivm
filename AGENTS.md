# Agent Instructions

`minivm` is a Go-native bytecode VM embedded in Go services. The threaded interpreter is the semantic baseline; ARM64 JIT execution is an optimization with threaded fallback.

`docs/coding-patterns.md` is the normative coding specification. It is binding for every production and test change. Do not treat repeated nearby code as permission to violate it.

## Rules

1. Read `AGENTS.md` and `docs/coding-patterns.md` before editing.
2. Read the task-router document for the affected area before changing code.
3. Inspect the current implementation before designing a change. Do not infer behavior from names alone.
4. Obey the repository instructions that cover every touched path. A more-specific nested `AGENTS.md` overrides this file; direct user/system/developer instructions override repository instructions.
5. Prefer the smallest clear change that preserves ownership, semantics, and compatibility. Do not add speculative abstractions, wrappers, aliases, or compatibility shims.
6. Work test-first. For native code, write the intended instruction stream before implementing the lowering. Tests written after implementation require mutation validation.
7. Review every non-trivial change top-down and bottom-up, then run another simplification pass. A removable symbol, duplicated rule, unnecessary abstraction, or avoidable comment is a defect.
8. Comments are exceptional. Keep only facts the code cannot express: non-obvious invariants, external constraints, rejected alternatives with evidence, or external contracts. Tests must read as specifications without explanatory comments.
9. One behavior has one implementation. Keep architecture-neutral policy in `internal/jit` and architecture mechanics in `internal/jit/<arch>`.
10. Generated files are never edited directly. Change the generator and run `make generate`.
11. Documentation describes the final supported state only. Do not put implementation history, migration narratives, superseded designs, progress logs, or old decisions into long-lived topic docs. Historical material belongs only in explicitly dated plans or audits.
12. Keep documentation owned by one canonical document. Summaries link to the owner instead of duplicating its details.
13. Do not overwrite unrelated user changes. Do not commit or stage unless explicitly requested.

## Workflow

1. `git status --short`.
2. Read the relevant code, tests, and owner documents.
3. State the contract and choose the narrowest test that proves it.
4. Write the test; observe the expected failure when practical.
5. Implement the smallest owning change.
6. Run the simplification and adversarial review passes.
7. Run focused tests first, then the repository completion checks.
8. Re-read every changed file against the coding specification before reporting completion.

## Task Router

| Task | Read first | Main owners | Verify |
|---|---|---|---|
| Opcode | `instruction-set.md`, `guides/add-opcode.md` | `instr/`, `internal/codegen/` | `go test ./instr ./internal/codegen ./interp` |
| Runtime / memory | `architecture.md`, `memory-model.md` | `interp/`, `types/` | `go test ./interp ./types` |
| JIT / ARM64 | `jit-internals.md`, `value-representation.md` | `internal/jit/`, `internal/journal/`, `internal/asm/` | `go test ./internal/... ./interp` |
| Optimization | `pass-system.md` | `analysis/`, `transform/`, `optimize/`, `pass/`, `internal/ssa/transform/` | relevant package tests |
| Verification | `verification.md` | `program/verify.go`, `instr/type.go` | `go test ./program ./interp` |
| Debugger / profile | `debugging.md`, `profile.md` | `debug/`, `interp/`, `prof/` | relevant package tests |

## Architecture Invariants

- Heap index `0` is permanent `Null`; reference cleanup is iterative.
- A frame keeps function address and callable heap reference distinct.
- Strings compare by content and published strings are immutable.
- Threaded execution is the semantic baseline.
- A JIT lowering that declines or fails leaves threaded execution available.
- A failed speculative lowering must not partially mutate IR, stack facts, ownership, labels, or published state.
- Native fallback must materialize exactly the interpreter state required at the resume point.
- `program.Verify` owns untrusted bytecode validation and is independent of runtime state and optimization policy.

## Documentation Ownership

| Topic | Owner |
|---|---|
| repository workflow and coding rules | `AGENTS.md`, `docs/coding-patterns.md` |
| package boundaries and runtime invariants | `docs/architecture.md` |
| opcode semantics and JIT status | `docs/instruction-set.md` |
| values and representation | `docs/value-representation.md` |
| heap ownership and lifecycle | `docs/memory-model.md` |
| JIT contracts and assembler boundaries | `docs/jit-internals.md` |
| testing contracts | `docs/testing.md` |
| benchmark methodology and results | `docs/benchmarks.md` |
| platform support | `docs/compatibility.md` |

Update the owner, then remove stale copies elsewhere.

## Completion Gate

Do not report completion until:

- ownership and boundaries are clear;
- every touched symbol still has a reason to exist;
- a further safe simplification pass finds nothing to remove, merge, inline, narrow, privatize, or rename;
- tests constrain the intended behavior, with mutation evidence when written after implementation;
- every test reaches the code it names, confirmed by a coverage profile rather than assumed from a pass;
- comments contain only non-obvious facts;
- no behavior has a second implementation;
- generated output is current;
- canonical docs describe the final state without duplicated or obsolete text;
- relevant tests, race checks, static checks, generated checks, and architecture checks pass;
- any intentionally skipped validation or simplification is reported with its reason.

Required baseline checks for a completed change:

```bash
make check-generated check-tidy check-fmt vet
go test ./...
go test -race ./...
GOOS=linux GOARCH=arm64 go build ./...
GOOS=linux GOARCH=arm64 go test -exec=true ./...
git diff --check
```
