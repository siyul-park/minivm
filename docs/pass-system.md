# Pass System

How analysis/transform/optimization pipeline works and how to write passes.

## Agent Checklist

Before editing:

- identify whether pass consumes `*program.Program`, `*types.Function`, or another cached type
- use `m.Load` for current state and `m.Convert` for sub-pipelines
- preserve in-place mutation unless existing pass returns a replacement

After editing:

- add package-local tests in `analysis/`, `transform/`, `optimize/`, or `pass/`
- for bytecode rewrites, verify branch offsets and constant/type indexes separately
- run `go test ./analysis ./transform ./optimize ./pass`

## `pass.Manager` Internals

`pass.Manager`: reflection-based pipeline dispatcher mapping result types to producing passes.

```go
type Manager struct {
    passes map[reflect.Type][]reflect.Value // type → []Pass
    cache  map[reflect.Type]reflect.Value   // type → cached result
    parent *Manager
}
```

- `Register(pass)`: inspects `Run(*Manager) (T, error)` and registers pass under `reflect.TypeOf(T)`.
- `Run(value)`: seeds cache with `value` by `reflect.TypeOf(value)`. A `T → T` pass overwrites cached `T`.
- `Load(&result)`: runs all passes producing `typeof(*result)` in registration order, caches output, sets `*result`; later loads return cache.
- `Convert(src,dst)`: creates child manager sharing passes but own cache, runs `src`, then loads `dst`.
- Caching: each pass runs at most once per `Manager.Run`; passes reading via `Load` see latest cached value for that type.

## Writing a Pass

A pass is any type with `Run(*Manager) (T, error)`.

```go
type MyPass struct{}

var _ pass.Pass[*program.Program] = (*MyPass)(nil)

func NewMyPass() *MyPass { return &MyPass{} }

func (p *MyPass) Run(m *pass.Manager) (*program.Program, error) {
    var prog *program.Program
    if err := m.Load(&prog); err != nil {
        return nil, err
    }

    fn := &types.Function{Typ: &types.FunctionType{}, Code: prog.Code}

    var blocks []*analysis.BasicBlock
    if err := m.Convert(fn, &blocks); err != nil {
        return nil, err
    }

    // mutate prog.Code or prog.Constants in-place
    _ = blocks

    return prog, nil
}
```

Rules:

- always `m.Load` before modifying; another pass may have already transformed the value
- wrap code slices as `*types.Function{Typ: &types.FunctionType{}, Code: slice}` for `BasicBlocksPass`
- return `nil, err` on failure; manager stops and propagates it
- return input unchanged if no modifications made
- do not retain manager after `Run` returns

## One-Off Passes

Use `pass.New` for simple inline passes.

```go
customPass := pass.New(func(m *pass.Manager) (*program.Program, error) {
    var prog *program.Program
    if err := m.Load(&prog); err != nil {
        return nil, err
    }
    // transform
    return prog, nil
})

opt := optimize.NewOptimizer(optimize.O0)
_ = opt.Register(customPass)
prog, _ = opt.Optimize(prog)
```

## Optimizer Pipeline `O1`

`optimize.NewOptimizer(O1)` registers:

```text
BasicBlocksPass            analysis  → []*BasicBlock
ConstantFoldingPass        transform → *program.Program
ConstantDeduplicationPass  transform → *program.Program
DeadCodeEliminationPass    transform → *program.Program
```

Transform passes load latest cached `*program.Program` and return possibly mutated one. Because caching is by type, each pass sees previous pass output.

## `BasicBlocksPass`

Shared by optimizer and `compiler`; same pass type and boundary rules.

Input: `*types.Function` via `m.Run(fn)` or `m.Convert(fn, &blocks)`.

Output: `[]*analysis.BasicBlock`:

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

Folded output is right-aligned in original byte range; left side padded with NOPs.

```text
Before: [I32_CONST 3][I32_CONST 4][I32_ADD]             11 bytes
After:  [NOP][NOP][NOP][NOP][NOP][NOP][I32_CONST 7]    11 bytes
```

Normal threaded NOP fast-forwards consecutive NOPs as one dispatch. `WithTick(1)` preserves exact per-instruction boundaries for hooks/debugging.

Supported folds:

- `I32_CONST × I32_CONST × op`: arithmetic, bitwise, comparisons
- `I64_CONST × I64_CONST × op`: same
- `F32_CONST × F32_CONST × op`: same
- `F64_CONST × F64_CONST × op`: same
- `I32_CONST × I32_EQZ`
- type conversions such as `I32_CONST × I32_TO_F32_S`
- `CONST_GET(String) × CONST_GET(String) × STRING_CONCAT/EQ/...`

## `ConstantDeduplicationPass`

Scans all `CONST_GET` operands in all functions. If multiple constant indices point to equal `types.Value`s, rewrites references to lowest index. Shrinks constant table and improves cache locality.

## `DeadCodeEliminationPass`

1. Mark bytes in basic blocks with no predecessors as `UNREACHABLE`.
2. Compact bytecode by removing NOP runs and unreachable sequences, rewriting branch offsets for new positions.

Compaction rewrites only branch operands: `BR`, `BR_IF`, `BR_TABLE`. Other operands keep meaning because compaction changes instruction positions, not constant/type/global/local indexes.
