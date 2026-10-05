# Testing

Tests are the executable specification of a feature. They define the contracts, structure, evidence, and validation required to establish correct behavior.

`coding-patterns.md` owns general code design and style; topic docs own behavior; `AGENTS.md` owns repository workflow and validation gates.

## Contract

Tests are the executable specification of a feature: they should show what the public contract promises, not how the implementation happens to work.

- A test MUST show public usage and promised behavior.
- Test structure MUST serve the contract rather than implementation shape or coverage.
- A test MUST expose the target, input, operation, and expected result in the case.
- A case MUST represent behavior, not a branch or implementation path.

### Public Boundary

Contract tests should prove behavior through the same public boundary available to callers.

- Feature contract tests MUST use only target-package public symbols and MUST live in `package <target>_test`.
- Wrappers that hide target API usage MUST NOT be added merely for reuse.
- Setup helpers MAY exist only when the specified behavior remains visible.
- Private-symbol testing MUST be resolved at the public boundary rather than by exposing internals solely for tests.

### Readability

A readable test is a small, direct specification whose behavior can be understood without following test infrastructure.

- The target and its behavior MUST remain visible.
- Wrappers, builders, or helpers MUST NOT hide the call or result being specified.
- Tests MUST NOT mix different writing styles or abstraction levels at one level.
- Table data and generation code MUST remain simple enough to read as specification.
- A case MUST contain the behavior it claims to specify.

### Organization

Test structure should provide one obvious owner for each public contract and keep case structure shallow.

| Rule | Requirement |
| --- | --- |
| Owner | Each public symbol SHOULD have one top-level test function; a missing owner test is a warning, and multiple semantic owner tests are an error. |
| Depth | At most two levels: the test function and its direct cases. |
| Same behavior, many inputs | Use an inline anonymous struct table of named inputs and expected outputs; each entry is one case. |
| Different scenarios | Use one case per scenario, named for the behavior it states. |
| Similar cases | Merge cases that specify the same behavior into one case or one table. |
| Style | A test function uses either table cases or scenario cases, never both. |

- A test SHOULD use one case style per level.
- Assertions outside cases SHOULD describe setup, preconditions, or test-wide invariants, not case behavior.
- Readiness polling MUST NOT outlive teardown or retain resources after the case closes them.
- Polling conditions MUST return readiness and observed results/errors only; assertions belong after readiness is established.

### F.I.R.S.T.

F.I.R.S.T. keeps tests reliable evidence rather than intermittent diagnostics.

- Tests MUST be **Fast, Independent, Repeatable, Self-validating, and Timely**.
- Tests MUST NOT depend on other tests, uncontrolled mutable state, manual inspection, or unnecessary setup.

## TDD

TDD separates the required behavior from its implementation by making the smallest missing contract fail before the implementation is generalized.

For each behavior change, the agent MUST:

1. state the contract and invariants;
2. write the narrowest falsifying test;
3. observe the expected failure when practical;
4. implement the smallest owning change;
5. run focused checks, then applicable structural and repository gates.

- Tests MUST cover applicable success, failure, boundaries, ownership/lifecycle, compatibility, parity, and architecture contracts.
- The lowest test layer that proves the contract MUST be used.
- Structure-only tests MUST NOT be added merely to exercise implementation details.

## Evidence

Different test layers prove different facts; evidence is complete only when each required fact has an appropriate proof.

- Coverage proves reachability, not quality or completeness.
- When no red phase is available, coverage SHOULD prove execution without changing production behavior to manufacture failure.

| Layer | Proves |
| --- | --- |
| Public | exported behavior, errors, lifecycle |
| Runtime | opcode behavior, traps, ownership |
| Parity | equivalent public behavior across distinct execution paths |
| Frontend | acceptance and intermediate-representation contracts |
| Backend | machine layout, bindings, moves, metadata, and bridge/deopt contracts |
| Golden | exact low-level output for a specified input shape |
| Async | publication, shutdown, and race behavior |
| Fuzz | bounded trust-boundary or differential properties |
| Integration | public end-to-end behavior |

## Native / JIT

Native execution adds low-level contracts, but tests should still separate local backend evidence from final public behavior.

- Frontend tests MUST prove frontend contracts.
- Backend tests MUST prove machine layout, bindings, moves, metadata, and bridge/deopt contracts where applicable.
- Interpreter tests MUST prove public results, errors, ownership, and execution behavior.
- Parity MUST be tested wherever multiple execution paths are required to implement the same public contract.
- Golden tests MUST define expected low-level output independently of the implementation that produced it, with the required complete output and metadata checks.

## Validation

Validation should start with the smallest useful falsification and expand only as far as the affected contract requires.

- Run the smallest falsifying check first.
- Apply package, race, coverage, architecture, and repository checks when relevant.
- Skip only genuinely inapplicable checks, and state the reason.

## Completeness

A complete change has evidence for every contract that the change affects, including cross-layer and architecture-specific obligations.

- Repeated execution units MUST have the required metadata and runtime coverage.
- Verification policy MUST be covered at its defined boundaries.
- Backend support status MUST match the implemented contract.
- Exported contracts MUST have an owning test.
- Architecture-specific contracts MUST have architecture-specific evidence.
- Generated output and documentation freshness are repository gates, not test-completeness rules.

## Ownership

Test contracts and structure have one owner and should remain independent of incidental implementation details.

| Concern | Owner |
| --- | --- |
| Test contracts and structure | `testing.md` |
| General test code style | `coding-patterns.md` |
| Opcode metadata | opcode definitions and their contract tests |
| Verification | verifier and verifier tests |
| Runtime opcode behavior | interpreter/runtime contract tests |
| Backend status | backend instruction contract |
| JIT contracts | JIT contract documentation and tests |
| Performance evidence | benchmark documentation and benchmarks |

## Related

- `coding-patterns.md` — general code design and style
- `refactoring.md` — structural review
- `instruction-set.md` — opcode/backend contracts
- `verification.md` — bytecode validation
- `jit-internals.md` — JIT contracts
- `benchmarks.md` — performance evidence
