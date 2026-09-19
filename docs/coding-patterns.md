# Coding Patterns

Normative code-design rules for `minivm`.

`AGENTS.md` owns workflow; `testing.md` owns test contracts and structure; `refactoring.md` owns structural review; topic docs own architecture facts.

## Design

The agent `MUST` implement required behavior with the fewest necessary symbols and least necessary code without weakening responsibility boundaries. Conceptual surface is minimized, not line count.

Every symbol `MUST` earn its existence through a distinct responsibility, invariant, ownership boundary, or reusable abstraction. Every behavior `MUST` have one implementation and every semantic rule `MUST` have one owner; other code `MUST` call, compose, or encode that owner.

Symbols `MUST` have one coherent responsibility, a narrow contract, and a scope broad enough to serve every caller needing that responsibility. The agent `MUST NOT` combine unrelated roles to force reuse.

1. **Abstract semantic duplication.** When multiple sites implement the same behavior or rule, the agent `MUST` move it into one owner. Similar syntax alone `MUST NOT` trigger abstraction.
2. **Merge overlapping symbols.** When symbols have substantially the same responsibility at the same abstraction level, the agent `MUST` consolidate them. It `SHOULD` prefer one general symbol over parallel variants, wrappers, aliases, or coordinators.
3. **Split real boundaries.** The agent `MUST` separate symbols only when responsibility, invariant, ownership, lifecycle, or abstraction level differs. It `MUST NOT` split to shorten code or create symmetry.
4. **Reuse before extension.** The agent `SHOULD` prefer existing symbols and composition before adding layers, extension points, policy knobs, or parallel mechanisms.

The agent `MUST` keep related state and behavior together, keep dependency direction explicit, and keep public contracts no larger than required.

## Functions

The agent `MUST` extract a helper only when it removes semantic duplication, names reusable behavior or policy, isolates an abstraction level, or is required as a function value. A private helper `SHOULD` have at least two callers; the agent `MUST` inline a single-use helper unless its name expresses a real policy or mechanic.

The agent `SHOULD` use methods for receiver-owned behavior and package functions for construction or behavior with no natural receiver. One function `MUST` stay at one abstraction level.

The agent `MUST` order declarations for reading: callers before callees, related symbols adjacent, type and methods together.

## Naming

A name `MUST` describe role, contract, or ownership. One word is the rule, not a preference: the agent `SHOULD` use one word by default. A multi-word name `MUST` be used only when one word cannot express the required distinction, and then it `MUST` add only the minimum necessary qualifier.

- The agent `MUST` use one term for one concept across packages.
- The agent `MUST NOT` repeat package, receiver, phase, or representation without meaning.
- The agent `MUST` keep standard abbreviations: `ID`, `IP`, `ABI`, `JIT`, `VM`, `SSA`, `CFG`, `GVN`, `DCE`.
- The agent `MUST` use one-letter names only for conventional receivers, indexes, and tiny scopes.

| Form | Meaning |
|---|---|
| `HasX` | contains or registers X |
| `IsX` | state predicate |
| `MatchX` | comparison or validation against X |
| `X` | direct boolean value |

The agent `MUST` use `At` for position/time predicates. It `MUST` reserve `Build`, `Compile`, `Publish`, `Capture`, and `Use` for actions or transitions. Capability names `MUST` be singular; collections and stores `MUST` be plural.

## Types and APIs

Every exported symbol is a maintenance commitment.

- The agent `MUST` accept interfaces only when callers supply behavior; it `MUST` define them where behavior is consumed.
- Constructors `MUST` return concrete types.
- Exported structs `SHOULD` stay small; writable state `MUST` stay behind its owner.
- The agent `SHOULD` prefer immutable values and defensive copies at ownership boundaries.
- The agent `MUST NOT` add parameter-group structs named `Request`, `Response`, `Result`, `Data`, `Info`, or `Context` without a contract.
- The agent `MUST NOT` expose speculative options, algorithms, extension points, or policy knobs.
- The agent `MUST NOT` add aliases or pass-through wrappers without a distinct contract.

Constructors `MUST` require inputs with no safe default. Functional options `MUST` be used only for optional behavior that improves the API. The agent `MUST` validate required dependencies and shape at construction, and `MUST` validate complete builders at `Build`.

Types `MUST` own invariants and transitions. Compile, publish, install, reset, retain, release, and close `MUST` be implemented as behavior, not as external field assignments.

## Errors

- Semantic errors `MUST` use stable `ErrXxx` sentinels.
- The agent `MUST` preserve dependency identity when callers depend on it; it `MUST` use `%w` when adding context.
- The agent `MUST` translate error categories only at their owning boundary.
- The agent `MUST NOT` create semantic categories with `fmt.Errorf` alone.
- Errors `MUST NOT` expose private or sensitive process state.

Panic `MUST` be used only for impossible programmer errors, `Must*` APIs, or documented hot-path invariants with one recovery boundary. Normal runtime failures `MUST` return errors.

## Ownership and Concurrency

Ownership `MUST` be explicit. Mutable storage `MUST` stay within its owner; borrowed values `MUST NOT` cross ownership boundaries. Retain/release transitions `MUST` belong to the owner of the transition.

Contexts `MUST` be first parameters for blocking, I/O, or process-boundary operations; the agent `MUST NOT` store request contexts in long-lived objects.

Shared mutable state `MUST` have one owner and one synchronization strategy. Long-lived goroutines `MUST` have explicit shutdown. Resources `MUST` be released exactly once.

## File Order

Within a Go file, the agent `MUST` declare in this order:

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

Comments SHOULD be omitted unless they express facts the code cannot express.

Comments MAY be added only when they are clearly necessary to preserve:

- invariants and consequences;
- external or cross-package constraints;
- rejected alternatives with evidence;
- external contracts or specifications.

The agent MUST NOT use comments to narrate code, restate names, label `arrange/act/assert`, or explain obvious control flow. It MUST improve names, types, or structure instead.

Exported symbols MUST have normal Go doc comments.

## Generated and Platform Code

Generated files `MUST` change only through their generator. Platform mechanics `MUST` stay behind matching build constraints and owner packages.

## Related

- `AGENTS.md` — workflow and delegation
- `refactoring.md` — structural review and simplification
- `architecture.md` — package/runtime architecture
- `testing.md` — test contract, structure, methodology, and validation
