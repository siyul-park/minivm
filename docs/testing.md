# Testing

Executable contracts and test ownership.

## When to Read

Read when changing public APIs, opcodes, verification, runtime behavior, optimization, JIT lowering, or benchmark fixtures.

## Source of Truth

| Concern | Owner |
|---|---|
| Test shape and naming | `coding-patterns.md` §12 |
| Opcode metadata | `instr/type.go` and `TestValid` |
| Verification policy | `program/verify.go` and its completeness test |
| Runtime opcode corpus | `interp/interp_test.go` |
| Backend support | `instruction-set.md` |

## Rules

Tests are specifications, not implementation snapshots.

- Tests live beside their production owner.
- External tests use `package <name>_test`.
- One top-level test owns each independent exported contract; cases are subtests.
- Test names state behavior, not implementation steps.
- Write the test first and observe the expected failure.
- Tests written after code must be mutation-validated.
- Do not add production APIs or proxies solely for tests.
- Test internal invariants through public behavior, generated output, or executable artifacts.

## Test Layers

| Layer | Contract |
|---|---|
| Public | exported API behavior, errors, lifecycle |
| Runtime | one visible fixture per opcode behavior, including traps and ownership |
| Parity | threaded vs optimized/fused/JIT observable behavior |
| Backend | layout, register bindings, moves, entry kind, bridge points, deopt metadata |
| Golden | exact machine instruction stream for a specified shape |
| Frontend | static/trace acceptance, `ssa.Verify`, plan/SSA graph equivalence |
| Async | queue publication, adoption, shutdown, race safety |
| Fuzz | bounded trust-boundary and differential properties |
| Integration | real public parse-to-close flows |

## White-Box Exceptions

A test may use the production package name only when the required contract cannot be expressed through a public boundary. Each exception must document why it exists and its removal condition. Do not create new white-box tests when an observable contract already exists.

## Golden Machine Code

ARM64 golden tests are the native-code specification.

A golden test must:

1. construct the input shape explicitly;
2. define the expected instruction stream before comparing output;
3. assert `asm.Assembler.Instructions()` exactly;
4. assert relevant entry/layout metadata;
5. build the assembler successfully.

Do not compare only selected opcodes or offsets when the emitted stream itself is the contract.

## Mutation

A new test is considered effective only after a covered production line is deliberately broken and the test fails. Restore the mutation before final validation.

Useful mutations for JIT work include removing a guard, changing a representation width, changing bridge classification, skipping a journal field, or bypassing native execution.

## Completeness

Repository completeness gates enforce:

- every opcode has valid metadata and runtime coverage;
- verifier policy covers every opcode;
- native opcode status matches the current backend;
- exported contracts have an owner test;
- generated code and documentation indexes stay synchronized.

## Required Validation

```text
go test ./...
go test -race ./...
make check-generated check-tidy check-fmt vet
GOOS=linux GOARCH=arm64 go build ./...
GOOS=linux GOARCH=arm64 go test -exec=true ./...
git diff --check
```

## Related Docs

- `coding-patterns.md`
- `instruction-set.md`
- `verification.md`
- `jit-internals.md`
- `benchmarks.md`
