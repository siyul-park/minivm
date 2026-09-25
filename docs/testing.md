# Testing

Owns test contracts, structure, methodology, reachability, completeness, and validation.

`coding-patterns.md` owns general code design and style; topic docs own behavior; `AGENTS.md` owns repository gates.

## Contract

Tests are the executable specification of a feature. A test `MUST` show public usage and promised behavior; structure serves that contract, not implementation shape or coverage.

### Public Boundary

Feature contract tests `MUST` use only target-package public symbols and `MUST` live in `package <target>_test`. Wrappers that hide target API usage `MUST NOT` be added merely for reuse; setup helpers `MAY` exist only when they keep the specified behavior visible.

### Readability

Tests `MUST` stay clear, concise, and directly understandable. Each case `MUST` show one meaningful, representative usage of the symbol under test, with input, operation, and expected result visible. A case represents behavior, not a branch or implementation path.

### Organization

Each public symbol `MUST` have one top-level test owner; its cases sit directly beneath it at one depth. A test function `MUST` use either direct cases or table-driven cases, never both, and `MUST NOT` mix abstraction levels.

`require.Eventually` callbacks `MUST` contain no assertions. They `MUST` capture results and errors, return only readiness conditions, and assert the captured state after polling.

When multiple inputs and outputs express one usage pattern, an anonymous test-case struct slice `MAY` be used. The data and generation code `MUST` remain simple enough to read as specification.

Tests `MUST` use only the code under test's public interface. Direct private-symbol reference indicates a design problem and `MUST` be resolved by changing the design so the behavior is specified through its proper boundary.

### F.I.R.S.T.

Tests `MUST` be **Fast, Independent, Repeatable, Self-validating, and Timely**. They `MUST NOT` depend on other tests, uncontrolled mutable state, manual inspection, or unnecessary setup.

## TDD

For each behavior change, the agent `MUST`:

1. state the contract and invariants;
2. write the narrowest falsifying test;
3. observe the expected failure when practical;
4. implement the smallest owning change;
5. run focused checks, then applicable structural and repository gates.

Tests `MUST` cover applicable success, failure, boundaries, ownership/lifecycle, compatibility, parity, and architecture contracts. The agent `MUST` use the lowest test layer that proves the contract.

## Evidence

Coverage proves reachability, not quality. When no red phase is available, `MUST` use coverage to prove execution and `MUST NOT` alter production behavior to manufacture failure.

| Layer | Proves |
|---|---|
| Public | exported behavior, errors, lifecycle |
| Runtime | opcode behavior, traps, ownership |
| Parity | threaded vs optimized/fused/native behavior where present |
| Frontend | acceptance, plan/SSA shape, `ssa.Verify` |
| Backend | layout, bindings, moves, metadata, bridge/deopt points |
| Golden | exact native instruction stream for a specified input shape |
| Async | publication, shutdown, race behavior |
| Fuzz | bounded trust-boundary or differential properties |
| Integration | public end-to-end behavior |

## Native / JIT

Frontend tests `MUST` prove frontend contracts. Backend tests `MUST` prove machine layout, bindings, moves, metadata, and bridge/deopt points when a native backend exists. Interpreter tests `MUST` prove threaded parity through public results, errors, ownership, and execution; native parity applies wherever the native tier can enter.

Current proof layers for the native tier:

| Layer | Owner |
|---|---|
| Backend goldens | `internal/jit/arm64` |
| Compile maps | `internal/jit/compile` |
| Runtime e2e | `internal/jit` |
| Interpreter parity | `interp` `TestWithThreshold`, `benchmarks` `TestKernels/*/jit` |

ARM64 goldens are the native instruction specification. The expected stream `MUST` be authored independently, with explicit input shape, complete instruction output, and required metadata checked.

## Validation

The agent `MUST` run the smallest falsifying command first, then applicable package, race, coverage, architecture, and repository checks. It `MUST` skip only inapplicable checks and `MUST` report each skip.

Typical package checks:

```bash
go test ./...
go test -race ./...
go test -coverprofile=coverage.out ./<affected-package>
```

## Completeness

A complete change `MUST` provide:

- metadata and runtime coverage for every opcode;
- verifier policy coverage for every opcode;
- backend status matching `instruction-set.md`;
- owner tests for exported contracts;
- architecture-specific evidence for architecture-specific contracts.

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
