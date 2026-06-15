# Pass System

How the analysis/transform/optimization pipeline works and how to write passes.
The design follows LLVM's new pass manager: a lazy, caching **analysis manager**
(`pass.Manager`) is kept separate from an ordered **transform pipeline**
(`pass.Pipeline`).

## Agent Checklist

Before editing:

- decide whether you are writing an **analysis** (computes a cached result for an
  IR unit) or a **transform** (mutates `*program.Program` in place)
- request cached analyses with `pass.GetResult[R](m, unit)`; never recompute by hand
- preserve in-place mutation — transforms mutate the unit and report `pass.Preserved`

After editing:

- add package-local tests in `analysis/`, `transform/`, `optimize/`, or `pass/`
- for bytecode rewrites, verify branch offsets and constant/type indexes separately
- run `go test ./analysis ./transform ./optimize ./pass`

## `pass.Manager` (analysis cache)

`pass.Manager` lazily runs analyses and caches their results, keyed by result
type and unit identity. Generics carry the types; the only reflection is
`reflect.TypeFor[R]()` used as a map key (no dynamic dispatch).

```go
type Manager struct {
    analyses map[reflect.Type]func(any, *Manager) (any, error) // result type → runner
    cache    map[cacheKey]any                                  // (result type, unit) → result
}
```

- `Register[U, R](m, analysis)`: registers an `Analysis[U, R]` under `R`.
- `GetResult[R](m, unit)`: returns the cached `R` for `unit`, computing and caching
  on a miss; errors with `ErrUnregisteredAnalysis` if no analysis produces `R`.
- `Invalidate(preserved)`: drops cached results unless `preserved` is `PreserveAll()`.

## `pass.Pipeline` (transform sequence)

`pass.Pipeline[U]` runs an ordered sequence of transforms over an IR unit of type
`U`, invalidating stale analyses between passes (LLVM's `PassManager`).

```go
pl := pass.NewPipeline[*program.Program]()
pl.AddPass(transform.NewConstantFoldingPass())
prog, err := pl.Run(m, prog) // m is a *pass.Manager
```

## Writing an Analysis

An analysis implements `Analysis[U, R]` — `Run(*Manager, U) (R, error)`. It receives
its IR unit directly and may request other analyses through the manager.

```go
type BasicBlocksAnalysis struct{}

var _ pass.Analysis[*types.Function, []*BasicBlock] = (*BasicBlocksAnalysis)(nil)

func NewBasicBlocksAnalysis() *BasicBlocksAnalysis { return &BasicBlocksAnalysis{} }

func (a *BasicBlocksAnalysis) Run(m *pass.Manager, fn *types.Function) ([]*BasicBlock, error) {
    // compute control-flow blocks for fn
}
```

## Writing a Transform

A transform implements `Pass[U]` — `Run(*Manager, U) (Preserved, error)`. It mutates
the unit in place and reports which analyses it preserved. Transforms that touch
code return `pass.PreserveNone()`.

```go
type MyPass struct{}

var _ pass.Pass[*program.Program] = (*MyPass)(nil)

func NewMyPass() *MyPass { return &MyPass{} }

func (p *MyPass) Run(m *pass.Manager, prog *program.Program) (pass.Preserved, error) {
    for _, fn := range functions(prog) {
        blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
        if err != nil {
            return pass.PreserveNone(), err
        }
        // mutate fn.Code / prog.Constants in place using blocks
        _ = blocks
    }
    return pass.PreserveNone(), nil
}
```

Rules:

- request analyses with `pass.GetResult`; the manager runs `BasicBlocksAnalysis`
  on demand and caches it per function
- mutate the program in place; return `pass.PreserveNone()` after code changes so
  cached analyses are invalidated, or `pass.PreserveAll()` if nothing changed
- return `pass.PreserveNone(), err` on failure; the pipeline stops and propagates it
- do not retain the manager after `Run` returns

## Optimizer Levels

`optimize.NewOptimizer(level)` registers `BasicBlocksAnalysis` and builds the
transform pipeline for the level. Levels are cumulative:

```text
O0  no transforms
O1  ConstantFoldingPass, ConstantDeduplicationPass                          (cheap, local)
O2  + AlgebraicSimplificationPass, DeadCodeEliminationPass                  (CFG / peephole)
```

`Optimize(prog)` runs the pipeline; `AddPass(p)` appends a custom transform.
Because analyses are invalidated between passes, each transform sees fresh blocks.

## `BasicBlocksAnalysis`

Shared by the optimizer and the JIT `compiler`; same boundary rules.

Input: `*types.Function`. Output: `[]*analysis.BasicBlock`:

- `Start`: first instruction byte offset, inclusive
- `End`: byte offset past last instruction, exclusive
- `Succs`: successor block indices
- `Preds`: predecessor block indices

Block boundaries:

- offset `0`
- byte after `BR`, `BR_IF`, `BR_TABLE`, `UNREACHABLE`, or `RETURN`
- every branch target offset from `BR`, `BR_IF`, or `BR_TABLE`

## `ConstantFoldingPass`

Folds 2- and 3-instruction windows: `CONST CONST OP` → `CONST result`.

Folded output is right-aligned in the original byte range; the left side is padded
with NOPs.

```text
Before: [I32_CONST 3][I32_CONST 4][I32_ADD]             11 bytes
After:  [NOP][NOP][NOP][NOP][NOP][NOP][I32_CONST 7]    11 bytes
```

Supported folds: `I32`/`I64`/`F32`/`F64` constant pairs (arithmetic, bitwise,
comparisons), `I32_CONST × I32_EQZ`, type conversions such as
`I32_CONST × I32_TO_F32_S`, and string `CONST_GET` ops.

## `AlgebraicSimplificationPass`

Integer peepholes whose right operand is a constant, on `I32`/`I64`:

- identities dropped to NOPs, leaving the left operand: `x+0`, `x-0`, `x*1`, `x/1`,
  `x|0`, `x^0`, `x&-1`, `x<<0`, `x>>0`
- strength reduction: `x*2ⁿ` → `x << n`; unsigned `x/2ⁿ` → `x >> n`

Float identities are intentionally skipped (unsound under IEEE-754: NaN, signed
zero), as are annihilators such as `x*0` and `x&0` (they would need to drop the
live left operand).

## `ConstantDeduplicationPass`

Scans all `CONST_GET` and type operands across functions; collapses equal
constants/types to the lowest index, rewrites references, and shrinks the tables.

## `DeadCodeEliminationPass`

1. Mark bytes in basic blocks with no predecessors as `UNREACHABLE`.
2. Compact bytecode by removing NOP runs and unreachable sequences, rewriting
   branch offsets for the new positions.

Compaction rewrites only branch operands (`BR`, `BR_IF`, `BR_TABLE`). Other
operands keep meaning because compaction changes instruction positions, not
constant/type/global/local indexes.
