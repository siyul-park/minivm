# Pass System

How minivm analyses, transforms, and optimization pipelines work.

## When to Read

Use this document when changing `analysis/`, `transform/`, `optimize/`, `pass/`, or `internal/ssa/transform/`.

For bytecode semantics, see `docs/instruction-set.md`. For branch and handler validity, see `docs/verification.md`. For the SSA IR itself and JIT contracts, see `docs/jit-internals.md`.

## Summary

The pass system follows LLVM's new pass manager model:

- `pass.Manager` lazily computes and caches analyses
- `pass.Pipeline` runs transforms in order
- analyses and transforms stay separate

Use an analysis when code needs reusable information about an IR unit. Use a transform when code mutates `*program.Program`.

Design rules:

- keep analyses pure
- keep transforms in-place
- request analyses through `pass.Manager`
- report invalidation with `pass.Preserved`
- prefer simple local passes before complex global rewrites

## Source of Truth

| Concern | File or package |
|---|---|
| pass manager and pipeline | `pass/` |
| analyses | `analysis/` |
| transforms | `transform/` |
| optimizer levels | `optimize/` |
| branch and handler repair | `transform/rewrite.go` |
| SSA transformation policies (one per pass, no composer) | `internal/ssa/transform/` |

## Core Model

```text
analysis:  IR unit -> cached result
transform: IR unit -> mutate in place -> preserved analyses
pipeline:  ordered transforms + invalidation
```

Analyses compute facts. They should not mutate the program.

Transforms may mutate the program. They must report which cached analyses remain valid.

## `pass.Manager`

`pass.Manager` owns analysis registration and caching.

It caches results by result type and IR unit identity.

Main APIs:

| API | Purpose |
|---|---|
| `Register[U, R](m, analysis)` | register analysis result type `R` for unit type `U` |
| `GetResult[R](m, unit)` | get cached result or compute it on demand |
| `Invalidate(preserved)` | drop stale cached results |

Rules:

- never recompute registered analyses by hand
- always request analyses with `pass.GetResult`
- do not retain the manager after `Run` returns
- keep analysis result types specific and meaningful

If no registered analysis produces `R`, `GetResult` returns `ErrUnregisteredAnalysis`.

## `pass.Pipeline`

`pass.Pipeline[U]` runs transforms over an IR unit.

```go
pl := pass.NewPipeline[*program.Program]()
pl.Add(transform.NewFoldPass())

prog, err := pl.Run(m, prog)
```

The pipeline:

1. runs each pass in order
2. receives the pass's `Preserved` result
3. invalidates stale analyses
4. stops on the first error

Transforms see fresh analysis results after earlier transforms invalidate stale ones.

## Writing an Analysis

An analysis implements:

```go
type Analysis[U, R] interface {
    Run(*Manager, U) (R, error)
}
```

Rules:

- keep analysis read-only
- request dependent analyses through the manager
- avoid hidden global state
- return deterministic results for the same unit state

## Writing a Transform

A transform implements:

```go
type Pass[U] interface {
    Run(*Manager, U) (Preserved, error)
}
```

Rules:

- mutate the unit in place
- use `pass.GetResult` for analysis data
- return `pass.PreserveNone()` after code changes
- return `pass.PreserveAll()` only when nothing changed
- return `pass.PreserveNone(), err` on failure
- do not keep references to stale analysis results after mutation

## Optimizer Levels

`optimize.New(level)` registers required analyses and builds a cumulative pipeline.

```text
O0  no transforms

O1  FoldPass
    DedupPass

O2  FoldPass
    AlgebraicPass
    DedupPass
    DCEPass

O3  FoldPass
    AlgebraicPass
    GVNPass
    DedupPass
    DCEPass
```

`Optimize(prog)` runs the configured pipeline. `Add(p)` appends a custom transform.

Because analyses are invalidated between transforms, each pass receives fresh analysis data.

## Basic Blocks

`BlocksAnalysis` is shared by the optimizer and JIT.

Each block contains:

| Field | Meaning |
|---|---|
| `Start` | first instruction byte offset, inclusive |
| `End` | byte offset after the last instruction |
| `Succs` | successor block indexes |
| `Preds` | predecessor block indexes |

Block boundaries are:

- offset `0`
- every branch target
- the byte after `BR`, `BR_IF`, `BR_TABLE`, `UNREACHABLE`, `RETURN`, or `RETURN_CALL`

A branch to the past-the-end offset has no successor block: the analysis models
it as a virtual exit for any function, top-level or not. `program.Verify` is the
sole owner of whether that virtual exit is legal, and it only accepts one for
top-level code (slot `0`); a function body that branches to its own end is
malformed. `BlocksAnalysis` stays permissive because the JIT needs the
same CFG shape regardless of scope; `DCEPass` additionally
re-checks scope before repairing a branch offset, since blind relocation
would otherwise silently propagate a target that verification would have
rejected.

`BR`, `BR_IF`, and `BR_TABLE` targets are computed once in `instr.Targets`
and reused by `program.Verify` and `BlocksAnalysis`, so the same
arithmetic is not duplicated at each call site.

Keep these boundary rules consistent with the JIT and verifier.

## Global Value Numbering

`GVNAnalysis` finds redundant pure computations within and across basic blocks.

It abstractly interprets the operand stack and assigns value numbers to computed values.

Candidate operations are side-effect-free and non-allocating numeric operations, plus reference comparisons.

The analysis is conservative across blocks:

- constants are stable
- constant-pool values are stable
- null refs are stable
- locals never reassigned are stable
- heap loads, globals, upvalues, and reassigned locals are opaque across blocks

Opaque values do not match across blocks, but may still match within their own block.

## Constant Folding

`FoldPass` folds small constant windows.

```text
CONST CONST OP -> CONST result
```

The folded instruction is right-aligned in the original byte range. The unused left side is padded with `NOP`.

Supported folds include numeric arithmetic, bitwise operations, comparisons, `I32_EQZ`, numeric conversions, and string `CONST_GET` operations.

Comparison folds produce `i1`, matching runtime comparison results. Because there is no `i1` immediate, folded booleans are interned in the constant pool and emitted with `CONST_GET`.

## Algebraic Simplification

`AlgebraicPass` performs integer peepholes where the right operand is a constant.

Supported identities:

```text
x + 0  -> x
x - 0  -> x
x * 1  -> x
x / 1  -> x
x | 0  -> x
x ^ 0  -> x
x & -1 -> x
x << 0 -> x
x >> 0 -> x
```

Supported strength reductions:

```text
x * 2^n -> x << n
x / 2^n -> x >> n   // unsigned division only
```

Skipped intentionally:

- float identities, because IEEE-754 makes them unsafe
- annihilators such as `x * 0` or `x & 0`, because they would need to drop a live left operand

## SSA Transformation Policies

`internal/ssa/transform` is a second pass system over `*ssa.Function`, reusing `pass.Pass`, `pass.Analysis`, `pass.Manager`, and `pass.Pipeline` exactly as the top-level `transform` and `optimize` do over `*program.Program` - no second pipeline abstraction exists. Its role in `docs/coding-patterns.md` §6 is the top-level `transform`'s role, not `optimize`'s: it owns one transformation policy per pass and exposes no composer. A caller builds its own `pass.Pipeline[*ssa.Function]` and adds `FoldPass`, `CSEPass`, `GuardPass`, `HoistPass`, then `DCEPass`, in that order, because CSE has to unify a repeated load's value before a guard built on each read is recognizably the same guard, DCE has to run last to sweep up whatever folding, deduplicating, guard elimination, and hoisting leave behind (an operand a fold made unused, an `OpState` a removed guard no longer resumes into), and Hoist reads best once Fold and CSE have already canonicalized the function, so it moves one instance of an invariant computation rather than a would-be duplicate. None of that is a correctness requirement for `HoistPass` specifically, though: it never unifies a value (only `CSEPass` and `GuardPass`'s shared `dedup` does that) and never moves a guard or anything else that carries deopt state (only `GuardPass`'s target), so it is sound wherever it runs in the sequence - unlike `GuardPass`, whose placement after `CSEPass` is load-bearing. `internal/ssa/transform/transform_test.go`'s `TestPassOrder` composes exactly this pipeline and asserts the ordering facts end to end. `optimize` will own the leveled, user-facing composition of these passes once a bytecode-to-SSA-to-bytecode route exists to run them over `*program.Program`; until then, composition stays the caller's, so `internal/ssa/transform` never grows an `Optimizer`-shaped composer that would only duplicate `optimize.Optimizer`'s shape.

Every pass here must stay correct and useful on a function with no JIT-specific fact at all, not only one `internal/jit/frontend` produced with guards and deopt state - `internal/ssa/transform` does not import `internal/jit`, so nothing in it may assume one is present.

| Pass | Collapses | Mechanism |
|---|---|---|
| `FoldPass` | an `OpExec` whose opcode is `IsPure()` and whose arguments are all `OpConst` | direct per-operation rewrite, no rebuild - decodes each `types.Boxed` argument into its native Go type and computes with ordinary operators |
| `CSEPass` | two `OpConst` or pure `OpExec` operations with equal value, related by dominance | `dedup`, keyed by opcode and argument identity |
| `GuardPass` | two guards of the same kind admitting the same fact over the same operand, related by dominance | `dedup`, keyed by guard kind, operand, and the admitted fact (target kind, shape, bounds, or specialized value) |
| `HoistPass` | an `OpConst`, or a pure and non-trapping `OpExec`, every one of whose arguments is defined outside a loop it sits in (or was itself just hoisted) | loop-invariant code motion: moves the operation into the loop's preheader when one already exists, never by splitting an edge to build one |
| `DCEPass` | an operation with no live result and no effect, and any block unreachable from the entry | mark-sweep liveness seeded from every operation instr's effect model says runs unconditionally, propagated backward through every value an operation reads - including an `OpState`'s own frame stacks |

`CSEPass` and `GuardPass` share one engine (`dedup` in `internal/ssa/transform/dedup.go`): a dominator-tree-scoped hash-consing table, walked in the dominator tree's own preorder so a key one block establishes stays visible to every block it dominates and is forgotten once that whole subtree is done. This is the SSA counterpart of `GVNAnalysis`, and is far smaller than it: a value here is its own definition, so identity is free, and dominance alone - no available-expression dataflow, no per-block value renumbering, no stable-versus-opaque story for mutable loads - decides what one definition may stand in for.

`GuardPass` has no bytecode counterpart; a guard is a fact only a JIT frontend's speculation invents. It is the pass `docs/jit-internals.md`'s "Heap Reads and Mutations" section describes as still needed: a repeated heap access re-emits and re-guards independently today, and running `CSEPass` first is what makes the second access's guard operand equal the first's.

`HoistPass` (`internal/ssa/transform/hoist.go`) is the general loop-invariant code motion `docs/jit-internals.md`'s "No hoist, no carry" paragraph always said belonged in an optimizer rather than in the trace frontend: it uses `internal/graph`'s dominance and `LoopHeaders` directly, over any function, not only a trace-compiled one. It is deliberately narrower than that paragraph once envisioned, though. Eligibility is `OpConst` or a pure, non-trapping `OpExec` - never a guard, an `OpLoad`, an `OpStore`, or anything else that reads or writes `Local`, `Global`, `Upval`, `Heap`, `Frame`, or `Branch`, and never an integer division or remainder, whose zero-divisor fault could otherwise fire on a loop trip count of zero that the original program never reached. Every one of the four guards always carries deopt state (`ssa.Verify`'s own `resume()` rule), and that state names a frame chain and stack valid at the guard's original position, not at a point before the loop ran - `HoistPass` refuses anything that carries state outright rather than try to reconstruct one that would be. It hoists only into a preheader that already exists (the loop header's one predecessor from outside the loop) and never splits an edge to build one. See `internal/ssa/transform/hoist.go`'s own documentation for the full argument, including which of `hoistable`'s three restrictions in `internal/jit/traceplan.go` were backend representation artifacts and which - the ban on ref-array containers - is a real hazard that belongs to retain/release pairing instead, still deferred.

The eventual plan is to delete `transform`'s `FoldPass`, `AlgebraicPass`, `DCEPass`, and `GVNPass` in favor of these five, once a bytecode-to-SSA-to-bytecode route exists to run them over `*program.Program` too. Nothing calls `internal/ssa/transform` yet: it has no consumer in `internal/jit` or elsewhere, and both that bytecode route and the JIT's own use of these passes are future work.

## Rewrite Rules

Any pass that changes bytecode length must repair all position-sensitive data.

Repair at least:

- branch offsets
- branch table targets
- exception handler starts
- exception handler ends
- exception handler catch targets

Check separately:

- constant indexes
- type indexes
- local indexes
- handler depths
- signed 16-bit branch reachability

If repair cannot preserve behavior, leave the function unchanged.

Prefer a safe no-op over a risky rewrite.

## Maintenance Notes

When changing the pass system:

- decide first whether the change is analysis or transform
- keep analyses cached and read-only
- keep transforms in-place and explicit
- use the manager instead of recomputing analysis facts
- invalidate aggressively when unsure
- prefer one simple pass over several tightly coupled passes
- keep bytecode rewrites conservative
- never silently produce invalid branch offsets
- test indexes and offsets independently
- preserve optimizer/JIT agreement on CFG boundaries

## Related Docs

- `docs/instruction-set.md` — opcode semantics
- `docs/verification.md` — valid bytecode and stack rules
- `docs/jit-internals.md` — CFG agreement with trace roots
- `docs/coding-patterns.md` — code style conventions
