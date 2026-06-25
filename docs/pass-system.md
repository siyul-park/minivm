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
O3  + GlobalValueNumberingPass (replaces block-local CSE)                   (cross-block value numbering)
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

## `GlobalValueNumberingAnalysis`

Assigns every value a function computes a value number by abstractly
interpreting the operand stack, then finds recomputations that are redundant
both within and across basic blocks. Equal expressions hash-cons to the same
number; commutative ops canonicalize operand order.

Input: `*types.Function`. Output: `*GlobalValueNumbering`:

- `Redundant`: finalizing-op offset → `Redundancy{Start, End, Kind, Home, Def}`,
  one per recomputation of a value already produced on every path that reaches it
- `Defs`: group id → definition offsets that must receive a `LOCAL_TEE` so the
  value is captured for later reloads (`Home >= 0` uses need no entry)

Only side-effect-free, non-allocating numeric ops (and reference comparisons) are
candidates. Each value carries a block-local number (for within-block matching,
exactly the old local pass) and a function-wide global key. Cross-block identity
is conservative: only constants, the constant pool, the null reference, and
locals never reassigned carry a stable global key; values built from a mutable
load (heap, globals, upvalues) or a reassigned local are opaque across blocks
(sound — an opaque value never matches) and still match within their own block.

Availability is a forward dataflow over global ids: `AVAIL_in = ⋂ preds
AVAIL_out`, `AVAIL_out = AVAIL_in ∪ gen`, with an optimistic ⊤ initialization on
non-entry blocks so the intersection converges through loop back-edges. A pure,
contiguous recomputation of an available value (at a control-flow merge, or a
loop-invariant expression recomputed inside the loop) is reported redundant;
catch and entry blocks have no predecessors, so nothing is available there.

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

A folded **comparison** yields `i1`, not `i32`, matching the kind a dynamic
compare produces. Since there is no `i1` immediate, the boolean is interned into
the constant pool (`types.I1`, deduped to two singletons per run) and emitted as
`CONST_GET`, e.g. `[I32_CONST 1][I32_CONST 1][I32_EQ]` →
`[NOP×8][CONST_GET idx]` with `I1(true)` appended to the pool. Arithmetic and
bitwise folds still emit the inline `*_CONST` for their numeric kind.

## `AlgebraicSimplificationPass`

Integer peepholes whose right operand is a constant, on `I32`/`I64`:

- identities dropped to NOPs, leaving the left operand: `x+0`, `x-0`, `x*1`, `x/1`,
  `x|0`, `x^0`, `x&-1`, `x<<0`, `x>>0`
- strength reduction: `x*2ⁿ` → `x << n`; unsigned `x/2ⁿ` → `x >> n`

Float identities are intentionally skipped (unsound under IEEE-754: NaN, signed
zero), as are annihilators such as `x*0` and `x&0` (they would need to drop the
live left operand).

## `GlobalValueNumberingPass`

Consumes `GlobalValueNumberingAnalysis` and rewrites each redundant
recomputation to a load. Within-block recomputations load a live local home
(`Home >= 0`, with `LOCAL_GET Home`) or a freshly captured slot; cross-block
recomputations capture the value at every definition in `Defs[Def]` with an
inserted `LOCAL_TEE` into one fresh slot per value and reload it at each use.
Speculative code motion (inserting computes on edges to make a *partial*
redundancy full) is out of scope: it needs edge splitting, unsafe under the
signed-16-bit branch operands.

Unlike the peephole passes, this pass changes code length: it uses the `rewriter`
(`transform/rewrite.go`) to splice edits and then repair every `BR`/`BR_IF`/
`BR_TABLE` operand and exception-table boundary for the new layout, bailing
(leaving the function untouched) if a branch would no longer reach within its
signed 16-bit operand. Allocating a local shifts the operand-stack base, so each
handler's `Depth` grows by the number of locals added; new local indexes must
stay below 256 (the 1-byte `LOCAL` operand). The top-level body (slot 0) compiles
with no locals and cannot allocate, so only the load-from-home case applies there;
its repaired code and handlers are written back to `prog.Code` and
`prog.Handlers`.

## `ConstantDeduplicationPass`

Scans all `CONST_GET` and type operands across functions; collapses equal
constants/types to the lowest index, rewrites references, and shrinks the tables.

## `DeadCodeEliminationPass`

1. Mark bytes in basic blocks with no predecessors as `UNREACHABLE`. Handler
   `Catch` blocks are entered out of band (no CFG predecessors), so they are
   treated as live roots and never marked dead.
2. Compact bytecode by removing NOP runs and unreachable sequences, rewriting
   branch offsets for the new positions and remapping each exception-table
   boundary to the first surviving instruction at or after its old offset.

Compaction rewrites only branch operands (`BR`, `BR_IF`, `BR_TABLE`). Other
operands keep meaning because compaction changes instruction positions, not
constant/type/global/local indexes. For the top-level body (slot 0) the repaired
code and exception table are written back to `prog.Code` and `prog.Handlers`.
