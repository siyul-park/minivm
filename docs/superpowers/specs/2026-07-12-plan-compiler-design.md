# Plan-Based JIT Compiler Design

## Goal

Hide static and trace compilation behind one compiler entry point. The interpreter requests compilation for an address; the compiler selects the best available plan, lowers it through one backend contract, and returns an installable module.

## Constraints

- Preserve bytecode semantics, deoptimization IPs, ownership, spill safety, loop safepoints, native-call ABI, metrics meaning, and current performance.
- Keep `Interpreter` unaware of CFG, trace trees, planner order, and strategy rejection.
- Keep public APIs unchanged and all new compiler types private.

## Architecture

The compiler owns an ordered planner chain. Each planner inspects the same compile input and returns zero or more complete plans. The compiler lowers plans in order and stops at the first successful module.

```go
type planner interface {
    plan(*compileInput) ([]plan, error)
}

type lowerer interface {
    lower(*lowering, plan) bool
}
```

The interpreter calls only:

```go
module, err := compiler.Compile(interpreter, address)
```

The interpreter does not branch on plan source or rejection reason.

## Compiler Input

`compileInput` is a normalized, read-only view of one compilation request:

```go
type compileInput struct {
    interpreter *Interpreter
    address     int
    function    *types.Function
    globals     []types.Kind
    functions   map[int]*types.Function
    installed   bool
}
```

The input owns only normalized data reused by planners and lowering. Constants, heap values, and tracer snapshots remain reachable through `interpreter` unless immutability requires copying. Avoid duplicating fields without a clear consumer.

## Plan Model

A plan describes one installable native entry and all blocks needed to execute it:

```go
type plan struct {
    entry  entry
    blocks []block
    spill  spillPolicy
}

type entry struct {
    anchor anchor
    kind   entryKind
}

type entryKind uint8

const (
    entryFunction entryKind = iota
    entryLoop
    entryModule
)
```

`entryKind` expresses runtime calling behavior. It replaces `native.cfg`; strategy identity must never affect installation.

```go
type block struct {
    offset int
    state  state
    steps  []step
    term   terminator
}

type state struct {
    slots []slot
}

type slot struct {
    kind        types.Kind
    ref         int
    refKnown    bool
    callee      int
    calleeKnown bool
}
```

`slot` records only facts consumed by lowering. Function signatures are resolved from `compileInput.functions` using `callee`; no duplicate signature pointer is stored.

```go
type terminator struct {
    kind    terminatorKind
    ip      int
    targets []int
}

type terminatorKind uint8

const (
    terminateFallthrough terminatorKind = iota
    terminateBranch
    terminateBranchIf
    terminateBranchTable
    terminateReturn
    terminateComplete
    terminateFallback
)
```

Targets are block offsets, never assembler labels. Labels are backend state allocated during lowering.

## Planners

`staticPlanner` builds plans from verified bytecode, basic blocks, and stack/dataflow facts. It owns static call-target and constant-ref analysis. A structurally unsupported function yields no plan; an unsupported opcode becomes an exact-IP fallback terminator when the surrounding graph remains valid.

`tracePlanner` converts immutable trace snapshots into the same plan model. It owns observed guards, learned continuations, loop roots, caller-tail stitching, and spill-policy derivation. Pending continuations become ordinary plan blocks before lowering.

Planner order is compiler policy:

1. static planner when no native entry is installed;
2. trace planner;
3. threaded execution when no plan lowers successfully.

No score or generic cost model is introduced until measured use cases require one.

## Compiler Flow

`compiler.Compile` is the only strategy-selection entry point:

```go
func (c *compiler) Compile(i *Interpreter, addr int) (*module, error) {
    input, ok := newCompileInput(i, addr)
    if !ok {
        return nil, nil
    }

    module := newModule()
    for _, planner := range c.planners {
        plans, err := planner.plan(input)
        if err != nil {
            return nil, err
        }
        for _, plan := range plans {
            ok, err := c.compile(input, plan, module)
            if err != nil {
                return nil, err
            }
            if ok {
                return module, nil
            }
        }
    }
    return module, nil
}
```

The exact loop may accumulate compatible entry and loop plans into one module, but the interpreter still receives one opaque module. Build, link, clean rejection, accounting, and publication remain centralized.

## Lowering

The backend receives only `compileInput`, `plan`, and `lowering` state. It cannot inspect planner identity.

Trace-specific source objects are removed from `lowering`: `tree`, `branches`, and `loop`. Lowering may retain a planner-neutral block worklist because learned continuations need the symbolic state produced at the branch point. The worklist stores only plan blocks, labels, snapshots, and ordering metadata; it must not contain trace trees, CFG nodes, or planner-specific types.

Shared lowering responsibilities:

- allocate labels for plan blocks;
- restore each block's entry state;
- emit ordinary steps through one opcode dispatcher;
- emit terminators through one control-flow dispatcher;
- queue and materialize side exits;
- preserve frame journal and native-call ABI.

## Interpreter Boundary

`Interpreter.build` is removed. The hot path calls `compiler.Compile(i, addr)` and handles only the opaque result. Strategy-specific metrics and rejection accounting move into the compiler.

`native` stores calling behavior, not compilation history:

```go
type native struct {
    callable asm.Callable
    entry    entryKind
}
```

Installation publishes function entries based on `entryKind`, not a CFG flag. Loop, function, and module entry wrappers remain runtime concerns.

## Naming Rules

- Use role nouns: `plan`, `planner`, `block`, `state`, `slot`, `terminator`, `entry`.
- Use verbs for transformations: `plan`, `compile`, `lower`, `emit`, `publish`.
- Do not retain historical prefixes such as `cfg*` in shared code.
- Keep `trace` only for recorder and `tracePlanner` internals.
- Avoid synonyms such as candidate, region, graph, fragment, and program for the same concept.
- Keep private types and methods unexported.
- Prefer one-word names when the role remains unambiguous; use compound names only to distinguish real concepts.

## File Structure

Target production files:

- `interp/jit.go`: compiler, input, module, native entry, common lowering state, build/link/publication.
- `interp/jit_plan.go`: plan model, validation, static planner, trace planner.
- `interp/jit_arm64.go`: ARM64 lowering, opcode emission, terminators, ABI, deoptimization.
- `interp/trace.go`: runtime observation and immutable snapshots only.
- `interp/jit_stub.go`: unsupported architecture stub.

Remove `cfg.go`, `cfgflow.go`, `jit_arm64_cfg.go`, `jit_arm64_step.go`, and `jit_structure_test.go`. Tests consolidate into `jit_plan_test.go`, `jit_arm64_test.go`, and `trace_test.go`.

## Metrics

Interpreter-level metrics remain strategy-neutral:

- `vm_jit_attempts_total`
- `vm_jit_errors_total`
- `vm_jit_emits_total`
- `vm_jit_bytes_total`

Existing CFG-specific metrics are removed or replaced by compiler-internal diagnostics only if still useful. Tests assert observable behavior and selected entry kind, not planner names or metric side effects.

## Testing

- Plan construction tests cover static branches, trace continuations, loop roots, exact fallback IPs, slot merging, and direct-call facts.
- Differential tests execute representative programs through threaded execution and compiler-selected native execution.
- Existing ownership, heap-replacement, deoptimization, spill, loop-budget, native-call, tail-call, and pool tests remain behavior gates.
- Structural tests assert one compiler entry point, one lowerer method, no `native.cfg`, and no CFG/trace selection in `Interpreter`.
- Completion gate: `go vet ./...`, `go test -race ./...`, `GOARCH=amd64 go test ./...`, focused ARM64 JIT tests, and tl2g correctness and benchmarks.
- Single-row and 200-row tl2g medians must remain within 5% of the current baseline.

## Migration

1. Add the plan model and validation without changing execution.
2. Convert static analysis into `staticPlanner` and test plan output.
3. Convert immutable trace trees into `tracePlanner` plans, including continuations and loop entries.
4. Change the lowerer contract to `lower(*lowering, plan)` and add unified block and terminator emission.
5. Move strategy selection into `compiler.Compile(i, addr)` and remove `Interpreter.build`.
6. Replace `native.cfg` with `entryKind` and simplify installation.
7. Remove trace scheduling fields from `lowering` after plan conversion owns them.
8. Consolidate production and test files, remove obsolete symbols, and update JIT/profile documentation.
9. Run the completion gate and compare symbol count, production LOC, and tl2g performance.

Each migration step must preserve a passing focused test set and end without compatibility adapters.

## Completion Criteria

- `Interpreter` contains no CFG/trace strategy selection or strategy-specific native flags.
- `compiler.Compile(i, addr)` is the sole compilation entry point.
- `lowerer` exposes one `lower(*lowering, plan)` method.
- Static and trace planners produce the same plan type.
- ARM64 lowering has one block path, one opcode path, and one terminator path.
- `lowering` contains no trace tree, CFG node, or planner-specific scheduling state; any deferred work uses generic plan blocks and symbolic snapshots.
- Obsolete CFG-specific production files and symbols are deleted.
- Production JIT file count, symbol count, and LOC decrease from the current branch.
- All correctness, race, cross-architecture, and performance gates pass.
## Entry ABI Invariants

`entryKind` is an ABI classification, not a planner tag:

- `entryFunction` starts at a function frame boundary, performs normal function return teardown, and may be published in the native-call slot.
- `entryLoop` resumes an existing frame at a loop header and must never be published as a callable function entry.
- `entryModule` starts the top-level module frame and must not be used as a cross-function native-call target.

A planner may emit `entryFunction` only when the produced plan satisfies the existing framed callable ABI. Installation and native-call publication depend solely on this invariant.