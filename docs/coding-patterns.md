# Coding Patterns

Normative code-design rules for `minivm`. `AGENTS.md` owns workflow; `testing.md` owns test contracts and structure; `refactoring.md` owns structural review; topic docs own architecture facts.

## Design

Implement required behavior with the fewest necessary symbols and least necessary code without weakening responsibility boundaries. Minimize **conceptual surface**, not line count.

Every symbol must earn its existence through a distinct responsibility, invariant, ownership boundary, or reusable abstraction. Every behavior has one implementation and every semantic rule has one owner; other code calls, composes, or encodes that owner.

Design symbols for **high cohesion and legitimate reuse**: one coherent responsibility, narrow contract, broad enough to serve every caller that needs that responsibility. Reuse does not justify combining unrelated roles.

1. **Abstract semantic duplication.** When multiple sites implement the same behavior or rule, move it into one owner. Similar syntax is not enough.
2. **Merge overlapping symbols.** When symbols have substantially the same responsibility at the same abstraction level, consolidate them. Prefer one general symbol over parallel variants, wrappers, aliases, or coordinators.
3. **Split real boundaries.** Separate symbols only when responsibility, invariant, ownership, lifecycle, or abstraction level differs. Do not split to shorten code or create symmetry.
4. **Reuse before extension.** Prefer existing symbols and composition before adding layers, extension points, policy knobs, or parallel mechanisms.

Keep related state and behavior together, dependency direction explicit, and public contracts no larger than required.

## Functions

Extract a helper only when it removes semantic duplication, names reusable behavior or policy, isolates an abstraction level, or is required as a function value. A private helper normally has at least two callers; inline single-use helpers unless the name expresses a real policy or mechanic.

Use methods for receiver-owned behavior and package functions for construction or behavior with no natural receiver. Keep one function at one abstraction level.

Order declarations for reading: callers before callees, related symbols adjacent, type and methods together.

## Naming

Names describe role, contract, or ownership. Use one word by default. A multi-word name is an exception and is allowed only when one word cannot express the required distinction; add only the minimum necessary qualifier.

- One word is the rule, not a preference.
- Use one term for one concept across packages.
- Do not repeat package, receiver, phase, or representation without meaning.
- Keep standard abbreviations: `ID`, `IP`, `ABI`, `JIT`, `VM`, `SSA`, `CFG`, `GVN`, `DCE`.
- Use one-letter names only for conventional receivers, indexes, and tiny scopes.

| Form | Meaning |
|---|---|
| `HasX` | contains or registers X |
| `IsX` | state predicate |
| `MatchX` | comparison or validation against X |
| `X` | direct boolean value |

Use `At` for position/time predicates. Reserve `Build`, `Compile`, `Publish`, `Capture`, and `Use` for actions or transitions. Capability names are singular; collections and stores are plural.

## Types and APIs

Every exported symbol is a maintenance commitment.

- Accept interfaces only when callers supply behavior; define them where behavior is consumed.
- Return concrete constructor types.
- Keep exported structs small and writable state behind its owner.
- Prefer immutable values and defensive copies at ownership boundaries.
- Do not add parameter-group structs named `Request`, `Response`, `Result`, `Data`, `Info`, or `Context` without a contract.
- Do not expose speculative options, algorithms, extension points, or policy knobs.
- Do not add aliases or pass-through wrappers without a distinct contract.

Constructors require inputs with no safe default. Functional options are for optional behavior only when they improve the API. Validate required dependencies/shape at construction; validate complete builders at `Build`.

Types own invariants and transitions. Compile, publish, install, reset, retain, release, and close are behavior, not field assignments.

## Errors

- Use stable `ErrXxx` sentinels for semantic errors.
- Preserve dependency identity when callers depend on it; use `%w` when adding context.
- Translate error categories only at their owning boundary.
- Do not create semantic categories with `fmt.Errorf` alone.
- Do not expose private or sensitive process state in errors.

Panic is for impossible programmer errors, `Must*` APIs, or documented hot-path invariants with one recovery boundary. Normal runtime failures return errors.

## Ownership and Concurrency

Ownership is explicit. Keep mutable storage within its owner; borrowed values do not cross ownership boundaries. Retain/release transitions belong to the owner of the transition.

Contexts are first parameters for blocking, I/O, or process-boundary operations; never store request contexts in long-lived objects.

Shared mutable state has one owner and synchronization strategy. Long-lived goroutines have explicit shutdown. Resources are released exactly once.

## File Order

Within a Go file:

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

Struct fields read from ownership/policy toward runtime state; synchronization fields are last.

## Comments

Comments state facts the code cannot express:

- invariants and consequences;
- external or cross-package constraints;
- rejected alternatives with evidence;
- external contracts/specifications.

Do not narrate code, restate names, label `arrange/act/assert`, or explain obvious control flow. Improve names, types, or structure instead. Exported symbols need normal Go doc comments.

## Generated and Platform Code

Generated files change only through their generator. Platform mechanics stay behind matching build constraints and owner packages.

## Related

- `AGENTS.md` — workflow and delegation
- `refactoring.md` — structural review and simplification
- `architecture.md` — package/runtime architecture
- `testing.md` — test contract, structure, methodology, and validation
