# Pass System

How the analysis/transform/optimization pipeline works and how to write new passes.

## pass.Manager Internals

`pass.Manager` is a reflection-based pipeline dispatcher. It maps result types to the passes that produce them.

```go
type Manager struct {
    passes map[reflect.Type][]reflect.Value  // type → []Pass
    cache  map[reflect.Type]reflect.Value    // type → cached result
    parent *Manager
}
```

**`Register(pass)`**: inspects the pass's `Run(*Manager) (T, error)` method signature via reflection and stores it under `reflect.TypeOf(T)`.

**`Run(value)`**: seeds the cache with `value` as the starting input (keyed by `reflect.TypeOf(value)`). Running a pass that transforms `T → T` overwrites the cached `T`.

**`Load(&result)`**: finds all passes registered for `typeof(*result)`, runs them in registration order, caches the output, and sets `*result`. Subsequent calls to `Load` for the same type return the cached value without re-running passes.

**`Convert(src, dst)`**: creates a child manager (shares registered passes, has its own cache), calls `child.Run(src)`, then `child.Load(dst)`. Used when a pass needs to run a sub-pipeline on a different input type.

**Caching semantics**: each pass runs **at most once per `Manager.Run` call**. Passes that read a cached value via `Load` see the output of the most recently executed pass for that type.

## Writing a Pass

A pass is any type with a `Run(*Manager) (T, error)` method. The type parameter `T` is the output type.

```go
type MyPass struct{}

// Declare interface compliance immediately after the type declaration.
var _ pass.Pass[*program.Program] = (*MyPass)(nil)

func NewMyPass() *MyPass { return &MyPass{} }

func (p *MyPass) Run(m *pass.Manager) (*program.Program, error) {
    // 1. Read the current program from the cache.
    var prog *program.Program
    if err := m.Load(&prog); err != nil {
        return nil, err
    }

    // 2. Run a sub-pipeline to get BasicBlocks for the main code.
    //    Wrap prog.Code in a Function so BasicBlocksPass can consume it.
    fn := &types.Function{Typ: &types.FunctionType{}, Code: prog.Code}
    var blocks []*analysis.BasicBlock
    if err := m.Convert(fn, &blocks); err != nil {
        return nil, err
    }

    // 3. Transform prog in-place (mutate Code bytes or Constants).
    // ...

    // 4. Return the (possibly same) transformed program.
    return prog, nil
}
```

**Rules:**
- Always `m.Load` before modifying — you may not be the first pass for this type.
- Construct `*types.Function{Typ: &types.FunctionType{}, Code: slice}` when you need `BasicBlocksPass` to analyze a code slice.
- Return `nil, err` on any failure — the manager propagates it and stops the pipeline.
- Return the input value unchanged if you make no modifications.
- Do not hold references to the manager after `Run` returns.

## Using pass.New for One-Off Passes

For simple cases, use the generic constructor instead of a named type:

```go
customPass := pass.New(func(m *pass.Manager) (*program.Program, error) {
    var prog *program.Program
    if err := m.Load(&prog); err != nil { return nil, err }
    // ... transform ...
    return prog, nil
})

opt := optimize.NewOptimizer(optimize.O0)
_ = opt.Register(customPass)
prog, _ = opt.Optimize(prog)
```

## Optimizer Pipeline (O1)

`optimize.NewOptimizer(O1)` registers passes in this order:

```
BasicBlocksPass          analysis  →  []*BasicBlock
ConstantFoldingPass      transform →  *program.Program
ConstantDeduplicationPass transform → *program.Program
DeadCodeEliminationPass  transform →  *program.Program
```

All transform passes receive the latest `*program.Program` from the cache and return a (possibly mutated) `*program.Program`. Because caching is by type, each pass sees the output of the previous one.

## BasicBlocksPass

Used by both the optimizer and `jitCompiler` — the same pass instance type, the same block boundary rules.

Input: `*types.Function` (seeded via `m.Run(fn)` or `m.Convert(fn, &blocks)`).

Output: `[]*analysis.BasicBlock`, where each block has:
- `Start int` — byte offset of first instruction (inclusive)
- `End int` — byte offset past last instruction (exclusive)
- `Succs []int` — indices of successor blocks in the blocks slice
- `Preds []int` — indices of predecessor blocks

Block boundaries are placed at:
- Offset 0 (always)
- The byte immediately following any `BR`, `BR_IF`, `BR_TABLE`, `UNREACHABLE`, or `RETURN` instruction
- Every branch target offset (the destination of a `BR`, `BR_IF`, or `BR_TABLE` operand)

## ConstantFoldingPass

Folds 2- and 3-instruction windows: `CONST CONST OP` → `CONST result`.

The folded sequence is **right-aligned** in the original byte range, with NOPs filling the left:

```
Before:  [I32_CONST 3][I32_CONST 4][I32_ADD]   (11 bytes)
After:   [NOP][NOP][NOP][NOP][NOP][NOP][I32_CONST 7]  (11 bytes)
```

The threaded NOP handler fast-forwards past all consecutive NOPs at compile time, making the padding free at runtime.

Supported folds:
- `I32_CONST × I32_CONST × op` for all arithmetic/bitwise/comparison ops
- `I64_CONST × I64_CONST × op` same
- `F32_CONST × F32_CONST × op` same
- `F64_CONST × F64_CONST × op` same
- `I32_CONST × I32_EQZ` (unary)
- Various type conversion folds (`I32_CONST × I32_TO_F32_S`, etc.)
- `CONST_GET (String) × CONST_GET (String) × STRING_CONCAT/EQ/...`

## ConstantDeduplicationPass

Scans all `CONST_GET` operands across all functions. If two constant indices point to equal values (`types.Value`), rewrites all references to use the lower index. Reduces constant table size and improves cache locality.

## DeadCodeEliminationPass

1. Identifies basic blocks with no predecessors (`len(blk.Preds) == 0`) and marks all their bytes as `UNREACHABLE`.
2. Compacts the bytecode: removes NOP runs and unreachable sequences by rewriting all branch offsets to account for the new byte positions.

The compaction step rewrites `BR`, `BR_IF`, `BR_TABLE`, `LOCAL_GET/SET/TEE`, `GLOBAL_GET/SET/TEE`, `CONST_GET`, `ARRAY_NEW`, `ARRAY_NEW_DEFAULT`, `STRUCT_NEW`, `STRUCT_NEW_DEFAULT`, `REF_TEST`, `REF_CAST` offsets.
