# Testing

Owns test contracts, structure, methodology, reachability, completeness, and validation. `coding-patterns.md` owns general code design and style; topic docs own behavior; `AGENTS.md` owns repository gates.

## Contract

Tests are the executable specification of a feature. A test must show how the feature is used and what behavior it promises; test structure exists to preserve that specification, not to mirror implementation or maximize coverage.

### Public Boundary

Feature contract tests use only target-package public symbols and live in an external test package (`package <target>_test`). This makes the user-visible boundary explicit and prevents unexported implementation dependencies.

Do not wrap or re-abstract the target package merely to remove duplication. A wrapper that hides target API usage cannot serve as the feature specification. Setup helpers are allowed only when they do not hide the specified behavior.

### Readability

Keep tests clear, concise, and directly understandable. Each case shows one meaningful, representative usage of the symbol under test, with input, operation, and expected result visible. A case represents behavior, not a branch or implementation path.

### Organization

Each public symbol under test has one top-level test function as its test owner. Its detailed cases belong directly under that function; test hierarchy is limited to one level and nested cases are prohibited.

Use one case representation per test function: direct cases or table-driven cases, never both. Do not mix cases with different abstraction levels or depths.

When multiple inputs and outputs express one usage pattern, an anonymous test-case struct slice is allowed. The data and generation code must remain simple enough to read as specification.

### F.I.R.S.T.

Tests must be **Fast, Independent, Repeatable, Self-validating, and Timely**. Do not depend on other tests, uncontrolled mutable state, manual inspection, or unnecessary setup.

## Internal Contracts

Internal tests are allowed only when an internal boundary is itself a contract, such as verifier policy, frontend acceptance or SSA shape, backend lowering, or native instruction output. Use the smallest owning boundary and apply the same clarity, organization, and F.I.R.S.T. rules.

## TDD

For each behavior change: state the contract and invariants; write the narrowest falsifying test; observe the expected failure when practical; implement the smallest owning change; run focused checks, then applicable structural and repository gates.

Cover applicable success, failure, boundaries, ownership/lifecycle, compatibility, parity, and architecture contracts. Use the lowest test layer that proves the contract.

## Evidence

Coverage measures reachability, not quality. When behavior already exists and no red phase is available, use coverage to prove reachability without changing production behavior to manufacture a failure.

| Layer | Proves |
|---|---|
| Public | exported behavior, errors, lifecycle |
| Runtime | opcode behavior, traps, ownership |
| Parity | threaded vs optimized/fused/JIT behavior |
| Frontend | acceptance, plan/SSA shape, `ssa.Verify` |
| Backend | layout, bindings, moves, metadata, bridge/deopt points |
| Golden | exact native instruction stream for a specified input shape |
| Async | publication, shutdown, race behavior |
| Fuzz | bounded trust-boundary or differential properties |
| Integration | public end-to-end behavior |

## Native / JIT

Frontend tests prove frontend contracts. Backend tests prove machine layout, bindings, moves, metadata, and bridge/deopt points. Interpreter tests prove threaded/native parity through public results, errors, ownership, and execution.

ARM64 goldens are the native instruction specification: define expected instructions independently of the emitter, build the input shape explicitly, fix the expected stream first, then build the assembler, and assert the complete stream and relevant metadata.

## Validation

Run the smallest falsifying command first, then applicable package, race, coverage, architecture, and repository checks. Skip only inapplicable checks and report the skip.

Typical package checks:

```bash
go test ./...
go test -race ./...
go test -coverprofile=coverage.out ./<affected-package>
```

## Completeness

- every opcode has metadata and runtime coverage;
- verifier policy covers every opcode;
- backend status matches `instruction-set.md`;
- exported contracts have owner tests;
- architecture-specific contracts have architecture-specific evidence.

Generated output and documentation index freshness are repository gates, not test-completeness rules.

## Ownership

| Concern | Owner |
|---|---|
| Test contracts and structure | `testing.md` |
| General test code style | `coding-patterns.md` |
| Opcode metadata | `instr/type.go`, `TestValid` |
| Verification | `program/verify.go`, verifier tests |
| Runtime opcode corpus | `interp/interp_test.go` |
| Backend status | `instruction-set.md` |
| JIT contracts | `jit-internals.md` |
| Performance evidence | `benchmarks.md` |

## Related

- `coding-patterns.md` — general code design and style
- `refactoring.md` — structural review
- `instruction-set.md` — opcode/backend contracts
- `verification.md` — bytecode validation
- `jit-internals.md` — JIT contracts
- `benchmarks.md` — performance evidence
