# Pass System

How minivm analyses, transforms, and optimization pipelines work.

## When to Read

Use this document when changing `analysis/`, `transform/`, `optimize/`, or `pass/`.

For bytecode semantics, see `docs/instruction-set.md`. For branch and handler validity, see `docs/verification.md`.

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
pl.AddPass(transform.NewConstantFoldingPass())

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

`optimize.NewOptimizer(level)` registers required analyses and builds a cumulative pipeline.

```text
O0  no transforms

O1  ConstantFoldingPass
    ConstantDeduplicationPass

O2  ConstantFoldingPass
    AlgebraicSimplificationPass
    ConstantDeduplicationPass
    DeadCodeEliminationPass

O3  ConstantFoldingPass
    AlgebraicSimplificationPass
    GlobalValueNumberingPass
    ConstantDeduplicationPass
    DeadCodeEliminationPass
```

`Optimize(prog)` runs the configured pipeline. `AddPass(p)` appends a custom transform.

Because analyses are invalidated between transforms, each pass receives fresh analysis data.

## Basic Blocks

`BasicBlocksAnalysis` is shared by the optimizer and JIT.

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
it as a virtual exit, while `program.Verify` limits that exit to top-level code.

Keep these boundary rules consistent with the JIT and verifier.

## Global Value Numbering

`GlobalValueNumberingAnalysis` finds redundant pure computations within and across basic blocks.

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

`ConstantFoldingPass` folds small constant windows.

```text
CONST CONST OP -> CONST result
```

The folded instruction is right-aligned in the original byte range. The unused left side is padded with `NOP`.

Supported folds include numeric arithmetic, bitwise operations, comparisons, `I32_EQZ`, numeric conversions, and string `CONST_GET` operations.

Comparison folds produce `i1`, matching runtime comparison results. Because there is no `i1` immediate, folded booleans are interned in the constant pool and emitted with `CONST_GET`.

## Algebraic Simplification

`AlgebraicSimplificationPass` performs integer peepholes where the right operand is a constant.

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
