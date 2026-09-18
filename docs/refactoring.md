# Refactoring

Owns structural review for non-trivial changes.

Keywords `MUST`, `MUST NOT`, `SHOULD`, `SHOULD NOT`, and `MAY` follow `AGENTS.md`. `coding-patterns.md` defines the design target; `testing.md` defines test evidence; `benchmarks.md` defines performance evidence.

## Scope

The agent `MUST` apply this review when a change alters package/type boundaries, ownership, lifecycle, control flow, abstractions, public contracts, or performance-sensitive structure. It `MUST NOT` apply it to formatting, isolated mechanical edits, and trivial renames.

The agent `MUST NOT` widen the task merely to refactor unrelated code. It `MUST` simplify the affected design until the scope reaches a fixed point.

## Invariants

A simplification is valid only when it preserves:

- required behavior, outputs, and failure semantics;
- ownership, lifecycle, concurrency, and ordering;
- public compatibility unless explicitly changed;
- relevant performance constraints.

Smaller code is not automatically simpler. The agent `MUST` optimize conceptual surface and responsibility boundaries, not line count.

## Review

### Scope

The agent `MUST` start from the requirement, not the diff. It `MUST` identify affected files, entry points, owners, constraints, and the responsibility changed by the work, and `MUST` state that responsibility in one sentence.

The agent `MUST` review successively within a file, across files in a package, and across packages in a module. At each level, it `MUST` inspect cohesion, ownership, dependency direction, and the necessity of each boundary. For repository-wide work, it `MUST` process lower-dependency packages before their consumers. A local fixed point does not establish a package or module fixed point.

### Top Down

"Top" is the owning scope under review: a file, package, or module, not necessarily a function. The agent `MUST` review from that scope's contract down to its implementation:

1. file, package, or module responsibility and dependency direction;
2. public contract and ownership boundary;
3. behavior and control flow;
4. state and lifecycle;
5. implementation mechanics.

The agent `MUST` ask: Is each responsibility in its narrowest correct owner? Is dependency direction valid? Is ownership explicit? Did the change add a layer without a contract?

### Bottom Up

The agent `MUST` review every changed symbol and every nearby symbol whose ownership, visibility, call relationship, or contract changed. For each retained symbol it `MUST` ask: **Why does this symbol exist now?**

The agent `MUST` remove, inline, merge, narrow, privatize, rename, or replace a symbol when an existing symbol or simpler structure provides the same contract. It `MUST` check for dead fields, stale parameters, wrappers, aliases, shims, one-call indirections, duplicate state, and duplicate ownership.

### Simplify

The agent `MUST` apply passes in this order:

1. **Remove / merge** — dead symbols, duplicate state/helpers, redundant owners.
2. **Narrow** — visibility, fields, operations, lifetime, ownership.
3. **Control flow** — branches, state transitions, temporaries, delegation.
4. **Mechanics** — redundant work, allocation, conversion, traversal, or lookup when behavior and required performance remain intact.
5. **Tests / docs** — preserve contract tests; remove structure-only tests; describe final state.

The agent `MUST NOT` create an abstraction merely to move complexity elsewhere. It `SHOULD` prefer one cohesive symbol serving all legitimate callers over parallel variants.

### Fixed Point

Any structural change to ownership, control flow, interfaces, abstractions, or package boundaries `MUST` restart review from **Top Down**.

```text
Top Down → Bottom Up → Simplify
    ↑                     │
    └── structural change ┘
          │
          └── no change → Validate
```

The agent `MUST` stop only when a complete pass finds no further **safe structural improvement within scope**. This is the simplification fixed point. For repository-wide work, this requires file, package, and module review; the agent `MUST` record the contract or evidence blocking each meaningful remaining candidate.

### Validate

The agent `MUST` confirm the originating contract, inspect the final diff as a reviewer, search for obsolete symbols/docs, and run validation from `testing.md` and `AGENTS.md`. It `MUST` use `benchmarks.md` when structure can affect measured cost.

Tests prove behavior; they do not prove structural completeness. Benchmark evidence proves measured cost; it does not justify unrelated structural changes.

## Rejected Simplifications

The agent `MUST` record only meaningful candidates that were considered and rejected. Each entry `MUST` state the candidate and the evidence blocking it: invariant, compatibility, ownership, or measured cost.

```text
Rejected: inline X.
Blocked by: Y owns a shared invariant.

Rejected: remove interface Z.
Blocked by: public compatibility.

Rejected: replace traversal with lookup.
Blocked by: measured hot-path regression.
```

The agent `MUST NOT` record speculative or trivial alternatives.

## Review Output

The agent `MUST` report:

- **Top Down** — findings or PASS
- **Bottom Up** — findings or PASS
- **Simplification** — remaining safe improvements or FIXED POINT
- **Validation** — findings or PASS
- **Changes** — meaningful structural simplifications
- **Rejected** — evidence-backed rejected candidates only
- **Risk** — unresolved correctness, compatibility, lifecycle, concurrency, or performance concerns

The agent `MUST NOT` report completion while a known safe structural improvement or unresolved review finding remains.

## Related

- `coding-patterns.md` — code-design rules
- `testing.md` — test evidence
- `benchmarks.md` — performance evidence
