# Coding Patterns

Code design minimizes conceptual surface while keeping responsibility and ownership boundaries explicit.

`AGENTS.md` owns workflow; `testing.md` owns test contracts and structure; `refactoring.md` owns structural review; topic docs own architecture facts and domain contracts.

## Design

Design minimizes conceptual surface while keeping responsibility and ownership boundaries explicit.

- Required behavior MUST use the fewest necessary symbols and least necessary code without weakening boundaries.
- Every symbol MUST earn its existence through a distinct responsibility, invariant, ownership boundary, or reusable abstraction.
- Each behavior MUST have one implementation and each semantic rule MUST have one owner.
- Each symbol MUST have one coherent responsibility, a narrow contract, and sufficient scope for every legitimate caller.
- Unrelated roles MUST NOT be combined merely to force reuse.

### Dependency Direction

Dependency direction keeps stable, general code independent from specific policy and context.

- A symbol MUST NOT depend on code that is more specific, more context-dependent, or less stable than itself.
- Lower-level or reusable symbols MUST express their responsibility in the most general form that fully fits it and MUST NOT depend on caller-specific policy, types, lifecycle, or semantics.
- Higher-level code MAY depend on lower-level code when that code is general and context-independent; lower-level code MUST NOT depend on its callers.
- Shared functionality MUST be generalized when extracted; it MUST NOT be moved downward merely to relocate complexity.

### Physical Cohesion

Physical layout should make ownership and collaboration visible while preserving real structural boundaries.

- Symbols with one owner and cohesive responsibility MUST share a file.
- Strongly related symbols MUST be physically close; direct collaborators SHOULD be adjacent.
- Files and declaration order MUST make ownership, responsibility, and relationships easy to read.
- Physical separation MUST represent a real responsibility, ownership, or abstraction boundary.

### Structural Rules

These rules reduce accidental duplication and unnecessary boundaries without hiding genuine differences.

1. **Abstract semantic duplication.** Multiple implementations of the same behavior or rule MUST converge on one owner; syntax similarity alone MUST NOT trigger abstraction.
2. **Merge overlapping symbols.** Symbols with substantially the same responsibility at the same abstraction level MUST be consolidated; prefer one general symbol over parallel variants, wrappers, aliases, or coordinators.
3. **Split real boundaries.** Separate symbols only when responsibility, invariant, ownership, lifecycle, or abstraction level differs.
4. **Reuse before extension.** Prefer existing symbols and composition before adding layers, extension points, policy knobs, or parallel mechanisms.

### Structural Signals

Structural metrics are review signals, not proof of a design violation.

- Consider cyclomatic complexity with statement count and nesting depth.
- Consider coupling with direct fan-in, fan-out, and dependency depth.
- Near-clone analysis SHOULD require meaningful body size and a semantic or naming relation; syntax similarity alone MUST NOT require abstraction.
- Similar or symmetric siblings SHOULD be adjacent when they share an owner and implementation shape.
- Metrics MUST NOT define universal declaration order, justify automatic reordering, or prove single responsibility.
- Prefer mechanical checks for directly expressible rules; heuristic signals SHOULD remain advisory and use deliberately high thresholds.

## Functions

Function structure should make meaningful behavior reusable and readable, not merely make functions smaller.

- A helper MUST be extracted only to remove semantic duplication, name reusable behavior or policy, isolate an abstraction level, or serve as a function value.
- A private helper SHOULD have at least two callers.
- A simple single-use wrapper SHOULD be inlined unless its name expresses a real policy or mechanic; complex one-use mechanics require review rather than mechanical removal.
- Methods SHOULD express receiver-owned behavior; package functions SHOULD express construction or behavior with no natural receiver.
- One function MUST stay at one abstraction level.
- Declarations MUST be ordered for reading: callers before callees, related symbols adjacent, and cohesive implementations together.

## Naming

Names should expose role, contract, or ownership with the smallest vocabulary that preserves the required distinction.

- One term MUST represent one concept across packages.
- One word SHOULD be the default; a multi-word name MUST add only the minimum qualifier needed to express a distinction.
- Package, receiver, phase, or representation MUST NOT be repeated without meaning.
- Standard abbreviations MUST be kept: `ID`, `IP`, `ABI`, `JIT`, `VM`, `SSA`, `CFG`, `GVN`, `DCE`.
- One-letter names MUST be limited to conventional receivers, indexes, and tiny scopes.

| Form | Meaning |
| --- | --- |
| `HasX` | contains or registers X |
| `IsX` | state predicate |
| `MatchX` | comparison or validation against X |
| `X` | direct boolean value |

`At` MUST be used for position/time predicates.

`Build`, `Compile`, `Publish`, `Capture`, and `Use` MUST be reserved for actions or transitions.

Capability names MUST be singular; collections and stores MUST be plural.

## Types and APIs

Public APIs are stable contracts: expose the minimum caller-facing abstraction and keep implementation state behind its owner.

- Interfaces MUST be used only when callers supply behavior and MUST be defined where that behavior is consumed.
- Public inputs SHOULD use the narrowest reusable abstraction that fully expresses the contract; public outputs SHOULD use the most concrete public type unless polymorphism is itself the contract.
- Public APIs MUST be usable without callers naming private types.
- Exported types SHOULD expose contract rather than mutable implementation state; data-only values and ABI bridge types MAY expose their fields.
- Constructors MUST require inputs with no safe default and MUST validate required dependencies and shape; `Build` MUST validate a complete builder.
- Required constructor values MUST be passed as arguments; optional values MUST be injected through functional options.
- Generic parameter-group types, speculative options, extension points, aliases, and pass-through wrappers MUST NOT be exposed without a distinct contract.

## Ownership

Ownership makes it explicit who creates, changes, and ends mutable state or resources.

- One symbol MUST own each mutable state and its transitions.
- Private state belongs to its owner.
- An unexported implementation component MAY access its exported owner's private state when it implements that owner's responsibility; unrelated same-package code MUST use the owner's contract.
- Borrowed values MUST NOT cross or outlive their ownership boundary.
- Retain/release and resource transitions MUST have one owner, and each resource MUST be released exactly once.
- Layout-only declarations MAY name private members but MUST NOT access runtime state.

## Concurrency

Concurrency rules prevent races and leaks by making shared state and shutdown ownership explicit.

- Shared mutable state MUST have one owner and one synchronization strategy.
- Long-lived goroutines MUST have an explicit shutdown path.
- Blocking, I/O, and process-boundary operations MUST take context first.
- Request contexts MUST NOT be stored in long-lived objects.

## Errors

Errors are contracts: callers should be able to classify failures, preserve causes, and avoid leaking implementation state.

- Semantic errors MUST use stable `ErrXxx` sentinels.
- Dependency identity MUST be preserved when callers depend on it; use `%w` when adding context.
- Error categories MUST be translated only at their owning boundary.
- Semantic categories MUST NOT be created with `fmt.Errorf` alone.
- Errors MUST NOT expose private or sensitive process state.
- Panic MUST be limited to impossible programmer errors, `Must*` APIs, or documented hot-path invariants with one recovery boundary.
- Normal runtime failures MUST return errors.

## File Order

Declaration order should let a reader follow ownership and behavior from public concepts to implementation mechanics.

Within a Go file, package-level declarations MUST appear in this order:

1. public types
2. private types
3. public constants
4. private constants
5. variables
6. all `init` functions
7. public options/functions
8. public constructors
9. public methods
10. clone/conversion and interface hooks
11. private functions/methods

- `init` functions MUST stay together and MUST precede non-`init` functions and methods.
- A cohesive implementation MUST read from higher-level behavior toward shared machinery.
- Callers MUST precede exclusive helpers; symmetric counterparts SHOULD be adjacent; shared leaves MUST follow the code that uses them.
- Within one type, constructors MUST precede other behavior.
- These ordering rules MUST NOT justify artificial file splits, wrappers, or duplicated helpers.

## Comments

Comments preserve facts that code cannot express; they should not narrate code that is already visible.

Comments MAY be added only when clearly necessary to preserve:

- invariants and consequences;
- external or cross-package constraints;
- rejected alternatives with evidence;
- external contracts or specifications.

Necessary comments MUST state the smallest sufficient fact, constraint, invariant, or consequence.

Comments MUST NOT narrate code, restate names, label control-flow phases, or explain obvious behavior. Prefer a better name, type, or structure.

Exported symbols SHOULD have normal Go doc comments. Public API changes SHOULD document user-visible behavior, constraints, ownership, or other facts the code cannot express.

## Generated and Platform Code

Generated and platform-specific code should remain behind the mechanism and boundary that own it.

- Generated files MUST change only through their generator.
- Platform mechanics MUST stay behind matching build constraints and owner packages.

## Related

- `AGENTS.md` — workflow and delegation
- `refactoring.md` — structural review and simplification
- `architecture.md` — package/runtime architecture
- `testing.md` — test contract, structure, methodology, and validation
