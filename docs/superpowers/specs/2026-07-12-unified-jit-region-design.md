# Unified JIT Region Design

## Goal

Make trace and whole-CFG compilation two frontends of one native lowering pipeline. Remove duplicate opcode dispatch, side-exit construction, module linking, and backend-specific control-flow code while preserving current behavior and performance.

## Constraints

- Preserve bytecode semantics, profiler metrics, tier selection, deoptimization IPs, spill policy, and ARM64 ABI.
- Keep trace observation and CFG static analysis separate; only their normalized native representation and backend are shared.
- Do not add a public API or a general-purpose IR package.
- Keep unsupported operations resumable through exact-IP threaded fallback.
- Keep call-free module CFG restriction until module/native-call ABI has an independent proof and regression test.
- Preserve zero steady-state allocations in native execution.
- Do not broaden supported opcodes or change tier-selection behavior during this refactor.

## Revised Scope

The first design overstated how much control flow can be normalized immediately. Trace lowering is not merely block lowering: it owns learned branch continuations, tail stitching, loop roots, inlined frames, and mutation-sensitive spill policy. Converting all of that into a new graph IR in one step would add abstraction before removing complexity.

The implementation therefore uses a staged convergence:

1. unify compilation setup, build/link/rejection, side exits, and ordinary opcode emission;
2. retain trace continuation scheduling and CFG edge scheduling as small frontend-owned control-flow drivers;
3. introduce a shared region/block representation only for canonical block input and entry state;
4. merge terminator lowering only after both drivers express the same semantics without adapters.

## Shared Representation

Existing `step`, `value`, `activation`, and `lowering` remain the value model. `step` is the sole ordinary native operation descriptor. CFG analysis and trace recording may populate different metadata, but backend handlers consume the same fields.

The private region layer stays intentionally small:

```go
type region struct {
    entry  anchor
    blocks []regionBlock
    loop   bool
    cfg    bool
}

type regionBlock struct {
    ip    int
    kinds []types.Kind
    ops   []step
    term  regionTerm
}
```

Labels are assembler output state and must not live in frontend IR. They are allocated by the backend. Entry stack kinds remain frontend data because CFG blocks require canonical reload state, while trace blocks may omit them.

`regionTerm` is introduced incrementally. It initially represents only CFG direct edges, conditional edges, tables, return, completion, and fallback. Trace continuation scheduling remains in its existing driver until its `pending`, `tail`, `queued`, and loop semantics can be represented without duplicated state.

## Frontends

### CFG frontend

`compileCFG` becomes a pure `buildCFGRegion` step plus the common compiler shell. It runs basic-block and stack-state analysis, converts instructions into annotated `step` values, and emits explicit CFG terminators. Unsupported instructions become exact-IP fallback terminators.

`blockKinds` must return full slot facts rather than immediately erasing them to `types.Kind`. Constant-ref provenance and direct-call signatures belong to frontend analysis, not ARM64-specific handlers. The merge operation must explicitly handle unknown provenance; zero cannot ambiguously mean both “unknown” and a valid value.

### Trace frontend

Trace roots remain produced from the published trace tree. The first migration wraps each root as a one-entry region while preserving `walk` as the control-flow driver. Ordinary opcode dispatch moves out of `walk`; branch continuation discovery and tail stitching do not.

Only after shared emission is stable may trace branches become region terminators. This prevents a large semantic rewrite from being mixed with mechanical deduplication.

## Shared Backend

One `emitStep` switch handles non-control opcodes for both tiers. It contains the existing trace handlers and consumes metadata prepared by each frontend. CFG wrappers such as `cfgConstGet` and `cfgArrayGet` disappear only when their behavior is represented by shared `step` metadata and handlers.

Control flow initially has two thin drivers:

- `lowerTraceRegion` owns observed branch continuation scheduling, loop roots, and tail stitching.
- `lowerCFGRegion` owns canonical block reload and static graph edges.

Both call the same `enter`, `emitStep`, `queueExit`, exit materialization, and compile/link shell. The final state may merge these drivers if doing so reduces code; a forced universal terminator engine is not a requirement.

## Compilation Pipeline

A single compiler helper owns assembler creation, architecture selection, context initialization, lowering invocation, `Build`, `Link`, rejection classification, accounting, and native entry publication.

The frontend must provide spill policy metadata. Trace mutation checks currently inspect the entire trace tree, including learned continuations. CFG backward edges rely on assembler spill restrictions. Consolidation must preserve both protections rather than deriving spill safety from only the current block.

`Interpreter.build` keeps CFG-first selection and existing metrics. The common compiler does not decide which frontend wins.

## Side Exits and Ownership

All guard exits use one `queueExit` helper. It snapshots canonical state, records the exact resume IP, and optionally records a reference retained only on the cold path. Exit materialization occurs after hot blocks.

The helper must support both snapshot sources:

- current symbolic state after flushing;
- a supplied pre-operation state for guards whose operation mutates symbolic values.

Constant typed-array markers remain ownership-neutral on the hot path. If a guard deoptimizes, the queued exit retains the referenced value before returning to threaded execution. This behavior requires a regression test that replaces the heap cell after compilation and verifies both current-data loading and balanced ownership.

## File Structure

- `interp/jit.go`: common compiler context, module construction, build/link/rejection, value state, and exits.
- `interp/jit_region.go`: minimal private region/block/terminator representation and validation.
- `interp/jit_cfg.go`: CFG analysis and region construction, absorbing `cfg.go` and `cfgflow.go` where this improves locality.
- `interp/jit_arm64.go`: shared ARM64 opcode handlers and trace control-flow driver.
- `interp/jit_arm64_cfg.go`: reduced to CFG control-flow emission, then removed only if no distinct driver remains.

A separate `jit_trace.go` is not required unless extracting trace region construction makes `jit.go` materially smaller. File creation is subordinate to symbol reduction.

## Testing

- Differential behavior tests run representative programs through threaded, trace, and CFG tiers.
- CFG analysis tests assert full slot facts, merge behavior, terminators, and exact fallback IPs.
- Existing trace continuation, loop-root, tail-call, mutation spill, deopt, ownership, native-call, and pool tests remain unchanged until the shared path passes them.
- Add a source-level invariant test that rejects a second non-control opcode dispatch switch in ARM64 JIT code.
- Run focused JIT tests after every migration step.
- Completion gate: `go test -race ./...`, `GOARCH=amd64 go test ./...`, focused ARM64 JIT tests, and tl2g correctness/benchmarks.
- Performance must not regress by more than 5% from the recorded tl2g single and 200-row batch medians.

## Migration

1. Extract common compiler setup and build/link/rejection without changing either lowerer.
2. Unify side-exit queuing and materialization, including pre-state and cold retain semantics.
3. Extract one shared non-control `emitStep` dispatch; keep both control-flow drivers.
4. Upgrade CFG dataflow to preserve complete slot facts and build minimal CFG regions.
5. Make CFG lowering consume the shared handlers and delete `cfgOp`, `cfgConstGet`, and `cfgArrayGet` duplicates.
6. Wrap trace roots in regions and remove compiler orchestration duplicated around `walk`.
7. Compare the two remaining control-flow drivers; merge only semantics that are truly identical.
8. Delete obsolete symbols/files, simplify tests and docs, then run the completion gate.

Each stage must pass focused tests and be committed independently. Temporary adapters are removed in the same series. The completion criterion is less code and fewer concepts than the starting point, not merely a shared type name.

## Completion Criteria

- One compiler setup/build/link/accounting implementation.
- One non-control opcode dispatch implementation.
- One side-exit representation and materialization implementation.
- CFG-specific opcode wrappers removed.
- No new public API and no general-purpose IR package.
- Net reduction in JIT symbols and lines after deleting migration adapters.
- Existing trace-specific scheduling remains only where its semantics are genuinely distinct.
