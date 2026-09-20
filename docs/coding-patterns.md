# Coding Patterns

Normative code-design rules for `minivm`.

`AGENTS.md` owns workflow; `testing.md` owns test contracts and structure; `refactoring.md` owns structural review; topic docs own architecture facts.

## Design

Required behavior `MUST` be implemented with the fewest necessary symbols and least necessary code without weakening responsibility boundaries. Conceptual surface is minimized, not line count.

Every symbol `MUST` earn its existence through a distinct responsibility, invariant, ownership boundary, or reusable abstraction.

Every behavior `MUST` have one implementation and every semantic rule `MUST` have one owner. Other code `MUST` call, compose, or encode that owner.

Symbols `MUST` have one coherent responsibility, a narrow contract, and a scope broad enough to serve every caller needing that responsibility. Unrelated roles `MUST NOT` be combined to force reuse.

### Dependency Direction

A symbol `MUST NOT` depend on a symbol that is more specific, more context-dependent, or less stable than itself. Dependencies `MUST` flow from specific, context-dependent, and less stable code toward more general, context-independent, and stable code.

Lower-level or reusable symbols `MUST` be refined to the most general form that fully expresses their responsibility. They `MUST NOT` depend on caller-specific policy, types, lifecycle, or semantics.

Higher-level symbols `MAY` depend on lower-level symbols when those symbols provide behavior in a more general and context-independent form. Lower-level symbols `MUST NOT` depend on the higher-level code that uses them.

When extracting shared functionality, code `MUST NOT` merely be moved into a lower layer. The responsibility and its dependencies `MUST` be generalized so that the resulting symbol can serve a broader context and has fewer reasons to change than its dependents.

### Physical Cohesion

Logical cohesion `MUST` be reflected in physical layout.

Symbols owned by the same owner and forming one cohesive responsibility `MUST` reside in the same file. Symbols sharing an ownership boundary `MUST NOT` be scattered across files without a real responsibility or abstraction boundary.

Strongly related symbols `MUST` be placed physically close together. Symbols that directly compose one behavior or have a direct caller-callee relationship `SHOULD` be adjacent.

Files and declaration order `MUST` make ownership, responsibility, and relationships easy to read. Physical separation `MUST` represent a real responsibility, ownership, or abstraction boundary.

### Structural Rules

1. **Abstract semantic duplication.*- When multiple sites implement the same behavior or rule, it `MUST` move into one owner. Similar syntax alone `MUST NOT` trigger abstraction.

2. **Merge overlapping symbols.*- When symbols have substantially the same responsibility at the same abstraction level, they `MUST` be consolidated. One general symbol `SHOULD` be preferred over parallel variants, wrappers, aliases, or coordinators.

3. **Split real boundaries.*- Symbols `MUST` be separated only when responsibility, invariant, ownership, lifecycle, or abstraction level differs. They `MUST NOT` be split to shorten code or create symmetry.

4. **Reuse before extension.*- Existing symbols and composition `SHOULD` be preferred before adding layers, extension points, policy knobs, or parallel mechanisms.

## Functions

A helper `MUST` be extracted only when it removes semantic duplication, names reusable behavior or policy, isolates an abstraction level, or is required as a function value.

A private helper `SHOULD` have at least two callers. A single-use helper `MUST` be inlined unless its name expresses a real policy or mechanic.

Methods `SHOULD` be used for receiver-owned behavior and package functions for construction or behavior with no natural receiver.

One function `MUST` stay at one abstraction level.

Declarations `MUST` be ordered for reading: callers before callees, related symbols adjacent, type and methods together.

## Naming

A name `MUST` describe role, contract, or ownership.

One word is the rule, not a preference: one word `SHOULD` be used by default. A multi-word name `MUST` be used only when one word cannot express the required distinction, and then it `MUST` add only the minimum necessary qualifier.

- One term `MUST` be used for one concept across packages.
- Package, receiver, phase, or representation `MUST NOT` be repeated without meaning.
- Standard abbreviations `MUST` be kept: `ID`, `IP`, `ABI`, `JIT`, `VM`, `SSA`, `CFG`, `GVN`, `DCE`.
- One-letter names `MUST` be used only for conventional receivers, indexes, and tiny scopes.

| Form     | Meaning                            |
| -------- | ---------------------------------- |
| `HasX`   | contains or registers X            |
| `IsX`    | state predicate                    |
| `MatchX` | comparison or validation against X |
| `X`      | direct boolean value               |

`At` `MUST` be used for position/time predicates.

`Build`, `Compile`, `Publish`, `Capture`, and `Use` `MUST` be reserved for actions or transitions.

Capability names `MUST` be singular; collections and stores `MUST` be plural.

## Types and APIs

Every exported symbol is a maintenance commitment.

- Interfaces `MUST` be accepted only when callers supply behavior; they `MUST` be defined where behavior is consumed.
- Constructors `MUST` return concrete types.
- Exported structs `SHOULD` stay small; writable state `MUST` stay behind its owner.
- Immutable values and defensive copies at ownership boundaries `SHOULD` be preferred.
- Parameter-group structs named `Request`, `Response`, `Result`, `Data`, `Info`, or `Context` `MUST NOT` be added without a contract.
- Speculative options, algorithms, extension points, or policy knobs `MUST NOT` be exposed.
- Aliases or pass-through wrappers `MUST NOT` be added without a distinct contract.

Constructors `MUST` require inputs with no safe default.

Functional options `MUST` be used only for optional behavior that improves the API.

Required dependencies and shape `MUST` be validated at construction, and complete builders `MUST` be validated at `Build`.

Types `MUST` own invariants and transitions. Compile, publish, install, reset, retain, release, and close `MUST` be implemented as behavior, not as external field assignments.

## Ownership and Concurrency

Ownership `MUST` be explicit.

Mutable storage `MUST` stay within its owner; borrowed values `MUST NOT` cross ownership boundaries.

Retain/release transitions `MUST` belong to the owner of the transition.

Contexts `MUST` be first parameters for blocking, I/O, or process-boundary operations; request contexts `MUST NOT` be stored in long-lived objects.

Shared mutable state `MUST` have one owner and one synchronization strategy.

Long-lived goroutines `MUST` have explicit shutdown.

Resources `MUST` be released exactly once.

## Errors

Semantic errors `MUST` use stable `ErrXxx` sentinels.

Dependency identity `MUST` be preserved when callers depend on it; `%w` `MUST` be used when adding context.

Error categories `MUST` be translated only at their owning boundary.

Semantic categories `MUST NOT` be created with `fmt.Errorf` alone.

Errors `MUST NOT` expose private or sensitive process state.

Panic `MUST` be used only for impossible programmer errors, `Must*` APIs, or documented hot-path invariants with one recovery boundary.

Normal runtime failures `MUST` return errors.

## File Order

Within a Go file, declarations `MUST` appear in this order:

1. public types
2. private types
3. public constants
4. private constants
5. variables
6. `init`
7. public options/functions
8. public constructors
9. public methods
10. clone/conversion and interface hooks
11. private functions/methods

Struct fields `MUST` read from ownership/policy toward runtime state; synchronization fields `MUST` be last.

## Comments

Comments `SHOULD` be omitted unless they express facts the code cannot express.

Comments `MAY` be added only when they are clearly necessary to preserve:

- invariants and consequences;
- external or cross-package constraints;
- rejected alternatives with evidence;
- external contracts or specifications.

Necessary comments `MUST` be dense and concise. Every word `MUST` justify its presence.

Comments `MUST NOT` be used to narrate code, restate names, label `arrange/act/assert`, or explain obvious control flow. Names, types, or structure `MUST` be improved instead.

Exported symbols `MUST` have normal Go doc comments.

## Generated and Platform Code

Generated files `MUST` change only through their generator.

Platform mechanics `MUST` stay behind matching build constraints and owner packages.

## Related

- `AGENTS.md` — workflow and delegation
- `refactoring.md` — structural review and simplification
- `architecture.md` — package/runtime architecture
- `testing.md` — test contract, structure, methodology, and validation
