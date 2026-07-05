# Coding Patterns

Default coding style for minivm contributors and agents.

## When to Read

Use this document before writing or changing code, especially when local style is unclear or a new pattern is needed.

Match nearby code first. Use these rules to resolve ambiguity, not to override a clear local pattern.

## Source of Truth

| Concern | Source |
|---|---|
| formatting | `gofmt` |
| package-specific style | nearby code in the same package |
| public API shape | existing public APIs and tests |
| documentation shape | `docs/README.md` |
| architectural ownership | `docs/architecture.md` |

## Fast Path

Read only the sections relevant to the change.

| Change | Read |
|---|---|
| function shape, helper extraction, naming | principles, functions |
| types, interfaces, fields, constructors | types |
| public APIs, options, builders, parsers | APIs |
| errors, panic, recover | errors |
| architecture-specific files | build tags |
| tests | tests |
| commits and PRs | git and PRs |
| documentation changes | docs |

## Principles

### Simpler is Better

If two designs provide the same behavior, choose the simpler one.

Prefer fewer files, fewer types, fewer functions, fewer methods, fewer arguments, fewer names, less indirection, and more local code.

Do not add abstraction only because code can be split. Add abstraction when it clearly improves readability, removes real duplication, isolates real complexity, or names a meaningful domain step.

### Keep Public Surfaces Small

Push complexity down. Public APIs should stay simple even when the implementation is difficult.

Prefer simple APIs over exposed mechanisms, explicit behavior over hidden behavior, local complexity over distributed state, and predictable structure over clever abstraction.

### Read Top-Down

Important behavior comes first. Details follow later.

Readers should understand the flow by reading downward:

```text
Run
  parse
  exec
  commit
```

Avoid forcing readers to jump around to reconstruct the main path.

### Be Obvious

Prefer mechanically obvious code over clever code.

A slightly longer implementation is better when it avoids hidden state, improves debugging, preserves interpreter/JIT symmetry, makes control flow explicit, or keeps behavior easy to verify.

### Preserve Symmetry

Interpreter and JIT paths should stay structurally similar when possible.

Symmetry matters more than small local optimizations because it keeps behavior easier to compare, test, and maintain.

### Keep Related Code Close

Keep state, validation, mutation, and cleanup for one behavior near each other.

Avoid splitting logic across files or helpers unless the split has a clear ownership or readability benefit.

## Functions

Each function should operate at one conceptual level.

Prefer this shape:

```go
func (i *Interpreter) run(ctx context.Context) error {
    for i.active() {
        if err := i.step(ctx); err != nil {
            return err
        }
    }
    return nil
}
```

Avoid mixing high-level orchestration with low-level details in the same function unless the local code is clearer that way.

Extract a helper when it removes real duplication, gives a meaningful name to a domain step, or isolates complexity. Do not extract a helper only to shorten a function.

Keep helper names short and direct. Prefer names that describe the operation, not the implementation trick.

## Types

Add a type when it owns data with behavior, names a real domain concept, or prevents repeated error-prone structure.

Do not add a type only to group two values temporarily, hide a simple tuple, or create an abstraction before it is needed.

Interfaces should be small and consumer-owned. Prefer concrete types until there is a real boundary.

Constructors should establish invariants. If a value has no invariants, a struct literal may be clearer.

## APIs

Public APIs should make the common path obvious and keep advanced behavior explicit.

Prefer options for rare configuration and direct arguments for required behavior.

Functional options may be declared immediately before the constructor they
configure. This keeps read order aligned with call sites such as
`interp.New(prog, interp.WithTick(1))`.

Keep builders focused. A builder should construct one thing, validate inputs near construction, and avoid becoming a general mutable configuration store.

Do not expose internal representation unless callers have a stable reason to depend on it.

## Errors

Return errors for expected failure. Panic only for internal invariants that indicate a bug.

Keep error values stable when callers can reasonably branch on them.

Use structured error types only when callers need more than a message.

Do not recover broadly. Recovery should be local, documented, and tied to a specific boundary.

## Build Tags

Keep architecture-specific files isolated behind build tags.

Portable behavior belongs in the default implementation. Architecture-specific files should provide the narrow part that must differ.

When adding a new architecture path, update `docs/compatibility.md` and the relevant implementation guide.

## Tests

Tests should cover behavior, not internal shape, unless the internal shape is the contract being protected.

Prefer table tests for repeated behavior and focused tests for subtle control flow.

When a change touches interpreter and JIT paths, test both paths or explain why one path is not applicable.

Keep fixtures small. A test should make the important bytecode or runtime behavior visible near the assertion.

## Git and PRs

Keep commits focused. A commit should have one reason to exist.

Use commit messages that name the area and the behavior, for example:

```text
interp: preserve refs during array slice
```

PR descriptions should include what changed, why it changed, how it was validated, and any intentionally deferred follow-up.

## Docs

Documentation should have one owner for each topic. Other documents should summarize and link to that owner instead of repeating the full explanation.

Use the standard document shape from `docs/README.md`:

1. title and short purpose
2. `When to Read`
3. `Source of Truth` when relevant
4. main content
5. `Maintenance Notes`
6. `Related Docs`

Keep wording direct and standard. Prefer `minivm`, `threaded interpreter`, `JIT`, `trace`, `opcode`, `value`, `ref`, and `heap` consistently.

## Maintenance Notes

When changing coding patterns:

- prefer rules that can be checked by reading nearby code
- avoid adding process that does not prevent real mistakes
- keep this document shorter than the code it governs
- update `docs/README.md` if the documentation shape changes
- keep terminology aligned with the rest of `docs/`

## Related Docs

- `docs/README.md` — documentation ownership and format
- `docs/architecture.md` — package boundaries and ownership
- `docs/guides/add-opcode.md` — opcode implementation workflow
- `docs/guides/add-architecture.md` — architecture backend workflow
