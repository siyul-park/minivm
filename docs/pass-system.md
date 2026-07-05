# Pass System

How minivm analyses, transforms, and optimization pipelines work.

The pass system follows LLVM’s new pass manager model:

* `pass.Manager` lazily computes and caches analyses.
* `pass.Pipeline` runs transforms in order.
* Analyses and transforms stay separate.

## Summary

Use an **analysis** when code needs to compute reusable information about an IR unit.

Use a **transform** when code mutates `*program.Program`.

Default design rule:

* keep analyses pure
* keep transforms in-place
* request analyses through `pass.Manager`
* report invalidation with `pass.Preserved`
* prefer simple local passes before complex global rewrites
* if two pass designs produce the same result, choose the simpler one
* use short, standard names such as `blocks`, `rewrite`, `fold`, `dedup`, `value`, and `pass`

## Agent Fast Path

Before editing:

| Task                    | Rule                                 |
| ----------------------- | ------------------------------------ |
| Add analysis            | implement `Analysis[U, R]`           |
| Add transform           | implement `Pass[U]`                  |
| Need analysis data      | use `pass.GetResult[R](m, unit)`     |
| Mutate bytecode         | return `pass.PreserveNone()`         |
| No mutation             | return `pass.PreserveAll()`          |
| Rewrite bytecode length | repair branches and exception tables |
| Rewrite constants/types | verify indexes separately            |

After editing:

```bash
go test ./analysis ./transform ./optimize ./pass
```

For bytecode rewrites, test branch offsets, exception handlers, and constant/type indexes separately.

## Core Model

```text
analysis:  IR unit → cached result
transform: IR unit → mutate in place → preserved analyses
pipeline:  ordered transforms + invalidation
```

Analyses should compute facts. They should not mutate the program.

Transforms may mutate the program. They must report which cached analyses are still valid.

## `pass.Manager`

`pass.Manager` owns analysis registration and caching.

It caches results by:

* result type
* IR unit identity

```go
type Manager struct {
    analyses map[reflect.Type]func(*Manager, any) (any, error)
    cache    map[cacheKey]any
}
```

Main APIs:

| API                           | Purpose                                             |
| ----------------------------- | --------------------------------------------------- |
| `Register[U, R](m, analysis)` | register analysis result type `R` for unit type `U` |
| `GetResult[R](m, unit)`       | get cached result or compute it on demand           |
| `Invalidate(preserved)`       | drop stale cached results                           |

Rules:

* never recompute registered analyses by hand
* always request analyses with `pass.GetResult`
* do not retain the manager after `Run` returns
* keep analysis result types specific and meaningful

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
2. receives the pass’s `Preserved` result
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

Example:

```go
type BasicBlocksAnalysis struct{}

var _ pass.Analysis[*types.Function, []*BasicBlock] = (*BasicBlocksAnalysis)(nil)

func NewBasicBlocksAnalysis() *BasicBlocksAnalysis {
    return &BasicBlocksAnalysis{}
}

func (a *BasicBlocksAnalysis) Run(m *pass.Manager, fn *types.Function) ([]*BasicBlock, error) {
    // compute blocks
}
```

Rules:

* keep analysis read-only
* request dependent analyses through the manager
* avoid hidden global state
* return deterministic results for the same unit state

## Writing a Transform

A transform implements:

```go
type Pass[U] interface {
    Run(*Manager, U) (Preserved, error)
}
```

Example:

```go
type MyPass struct{}

var _ pass.Pass[*program.Program] = (*MyPass)(nil)

func NewMyPass() *MyPass {
    return &MyPass{}
}

func (p *MyPass) Run(m *pass.Manager, prog *program.Program) (pass.Preserved, error) {
    for _, fn := range functions(prog) {
        blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
        if err != nil {
            return pass.PreserveNone(), err
        }

        _ = blocks
        // mutate fn.Code or prog.Constants in place
    }

    return pass.PreserveNone(), nil
}
```

Rules:

* mutate the unit in place
* use `pass.GetResult` for analysis data
* return `pass.PreserveNone()` after code changes
* return `pass.PreserveAll()` only when nothing changed
* return `pass.PreserveNone(), err` on failure
* do not keep references to stale analysis results after mutation

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

`Optimize(prog)` runs the configured pipeline.

`AddPass(p)` appends a custom transform.

Because analyses are invalidated between transforms, each pass receives fresh analysis data.

## Basic Blocks

`BasicBlocksAnalysis` is shared by the optimizer and JIT.

Input:

```text
*types.Function
```

Output:

```text
[]*analysis.BasicBlock
```

Each block contains:

| Field   | Meaning                                  |
| ------- | ---------------------------------------- |
| `Start` | first instruction byte offset, inclusive |
| `End`   | byte offset after the last instruction   |
| `Succs` | successor block indexes                  |
| `Preds` | predecessor block indexes                |

Block boundaries are:

* offset `0`
* every branch target
* the byte after `BR`, `BR_IF`, `BR_TABLE`, `UNREACHABLE`, or `RETURN`

Keep these boundary rules consistent with the JIT.

## Global Value Numbering

`GlobalValueNumberingAnalysis` finds redundant pure computations within and across basic blocks.

It abstractly interprets the operand stack and assigns value numbers to computed values.

Output:

| Field       | Meaning                                            |
| ----------- | -------------------------------------------------- |
| `Redundant` | recomputation offset → replacement metadata        |
| `Defs`      | value group → definition offsets that need capture |

Candidate operations are side-effect-free and non-allocating numeric operations, plus reference comparisons.

The analysis is conservative across blocks:

* constants are stable
* constant-pool values are stable
* null refs are stable
* locals never reassigned are stable
* heap loads, globals, upvalues, and reassigned locals are opaque across blocks

Opaque values do not match across blocks, but may still match within their own block.

Availability uses forward dataflow:

```text
AVAIL_in  = intersection of predecessor AVAIL_out
AVAIL_out = AVAIL_in union generated values
```

Entry and catch blocks start with no available values.

## Constant Folding

`ConstantFoldingPass` folds small constant windows.

Main pattern:

```text
CONST CONST OP → CONST result
```

The folded instruction is right-aligned in the original byte range. The unused left side is padded with `NOP`.

```text
Before: [I32_CONST 3][I32_CONST 4][I32_ADD]
After:  [NOP...][I32_CONST 7]
```

Supported folds include:

* `I32`, `I64`, `F32`, and `F64` arithmetic
* bitwise operations
* comparisons
* `I32_EQZ`
* numeric conversions
* string `CONST_GET` operations

Comparison folds produce `i1`, matching runtime comparison results. Because there is no `i1` immediate, folded booleans are interned in the constant pool and emitted with `CONST_GET`.

## Algebraic Simplification

`AlgebraicSimplificationPass` performs integer peepholes where the right operand is a constant.

Supported identities:

```text
x + 0  → x
x - 0  → x
x * 1  → x
x / 1  → x
x | 0  → x
x ^ 0  → x
x & -1 → x
x << 0 → x
x >> 0 → x
```

Supported strength reductions:

```text
x * 2^n → x << n
x / 2^n → x >> n   // unsigned division only
```

Skipped intentionally:

* float identities, because IEEE-754 makes them unsafe
* annihilators such as `x * 0` or `x & 0`, because they would need to drop a live left operand

## Global Value Numbering Pass

`GlobalValueNumberingPass` consumes `GlobalValueNumberingAnalysis`.

It rewrites redundant recomputations to local reloads.

Within a block, it reloads from an existing or freshly captured local home.

Across blocks, it:

1. allocates one fresh local per value
2. inserts `LOCAL_TEE` at each required definition
3. replaces recomputations with `LOCAL_GET`

This pass can change code length. It uses `transform/rewrite.go` to splice edits and repair:

* `BR`
* `BR_IF`
* `BR_TABLE`
* exception-table boundaries

If a repaired branch no longer fits in signed 16 bits, the function is left unchanged.

Limits and rules:

* new local indexes must fit in the 1-byte `LOCAL_*` operand
* handler depths grow by the number of inserted locals
* top-level code may read declared `Program.Locals`
* top-level code cannot allocate fresh locals because `prog.Locals` is not rewritten by this pass
* speculative code motion and edge splitting are out of scope

## Constant Deduplication

`ConstantDeduplicationPass` collapses duplicate constants and types.

It scans all functions for:

* `CONST_GET`
* type operands

Then it:

1. maps equal constants/types to the lowest index
2. rewrites references
3. shrinks the constant and type tables

This pass changes indexes, not instruction layout.

## Dead Code Elimination

`DeadCodeEliminationPass` removes unreachable code and compacts bytecode.

Steps:

1. Mark basic blocks with no predecessors as `UNREACHABLE`.
2. Treat handler catch blocks as live roots.
3. Remove `NOP` runs and unreachable sequences.
4. Rewrite branch offsets for the new layout.
5. Remap exception-table boundaries to the first surviving instruction at or after the old offset.

Only branch operands are rewritten during compaction.

Other operands keep their meaning because compaction changes instruction positions, not constant, type, global, or local indexes.

For top-level code, the repaired code and handlers are written back to:

* `prog.Code`
* `prog.Handlers`

## Rewrite Rules

Any pass that changes bytecode length must repair all position-sensitive data.

Repair at least:

* branch offsets
* branch table targets
* exception handler starts
* exception handler ends
* exception handler catch targets

Check separately:

* constant indexes
* type indexes
* local indexes
* handler depths
* signed 16-bit branch reachability

If repair cannot preserve behavior, leave the function unchanged.

Prefer a safe no-op over a risky rewrite.

## Agent Notes

When changing the pass system:

* decide first: analysis or transform
* keep analyses cached and read-only
* keep transforms in-place and explicit
* use the manager instead of recomputing analysis facts
* invalidate aggressively when unsure
* prefer one simple pass over several tightly coupled passes
* avoid new abstractions unless they remove real duplication
* keep bytecode rewrites conservative
* never silently produce invalid branch offsets
* test indexes and offsets independently
* preserve optimizer/JIT agreement on CFG boundaries

The best pass is small, local, deterministic, and explicit about what it preserves.
