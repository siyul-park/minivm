# Unified JIT Region Design

## Goal

Make trace and whole-CFG compilation two frontends of one native lowering pipeline. Remove duplicate opcode dispatch, side-exit construction, module linking, and backend-specific control-flow code while preserving current behavior and performance.

## Constraints

- Preserve bytecode semantics, profiler metrics, tier selection, deoptimization IPs, and ARM64 ABI.
- Keep trace observation and CFG static analysis separate; only their native representation and backend are shared.
- Do not add a public API or a second general-purpose IR package.
- Keep unsupported operations resumable through exact-IP threaded fallback.
- Keep call-free module CFG restriction until module/native-call ABI has an independent proof and regression test.
- Preserve zero steady-state allocations in native execution.

## Architecture

Both frontends produce an internal `region` made of ordered `block` values. A block owns ordinary bytecode-derived operations and one explicit terminator. The backend lowers regions only; it does not know whether a block came from a trace tree or static CFG.

Existing `step`, `value`, `activation`, and `lowering` remain the value model. `step` becomes the sole native operation descriptor. CFG analysis annotates steps with static kinds, constant references, direct callees, and known array shapes. Trace recording annotates the same fields from observations.

The region types stay private in `interp`:

```go
type region struct {
    entry  int
    blocks []regionBlock
}

type regionBlock struct {
    label asm.Label
    ops   []step
    term  regionTerm
}

type regionTerm struct {
    kind   termKind
    ip     int
    target int
    other  int
}
```

Only fields required by current behavior are added. Branch-table targets and trace side exits use compact slices owned by the terminator.

## Frontends

### CFG frontend

`compileCFG` becomes `buildCFGRegion`. It runs basic-block and stack-state analysis, converts each instruction to an annotated `step`, and emits explicit branch, conditional branch, table branch, return, or fallback terminators. Unsupported instructions terminate the block with exact-IP fallback rather than entering a separate lowering path.

### Trace frontend

The trace tree adapter becomes `buildTraceRegions`. Each published anchor produces one region. Existing branch inlining decisions stay in the adapter. Observed outcomes become explicit terminators instead of being interpreted inside `walk`.

## Shared Backend

One `lowerRegion` function owns entry setup, block binding, canonical state restoration, operation emission, terminator emission, deferred exits, assembler build, and linking.

One `emitStep` switch handles all non-control opcodes. It contains the current trace handlers and accepts statically or dynamically supplied metadata. CFG-specific wrappers such as `cfgOp`, `cfgConstGet`, and `cfgArrayGet` disappear after their metadata preparation moves into the CFG frontend or shared handlers.

One `emitTerm` handles direct edges, conditional edges, branch tables, return, fallback, loop budget, and native calls. Trace-only continuation and CFG edges are represented by terminator data rather than separate backend functions.

## Compilation Pipeline

`compiler.Compile` becomes the only compilation entry. It accepts a frontend-produced region set, lowers each region, and returns one `module`. CFG-first tier selection remains in `Interpreter.build`, but both attempts call the same compile/link machinery.

Assembler errors classified as clean rejection remain centralized. Module byte and emit accounting is performed once. Native entry publication remains unchanged.

## Side Exits and Ownership

All guard exits use one `queueExit` helper. It snapshots canonical state, records the resume IP, and optionally records references that must be retained only on the cold path. The backend materializes queued exits after hot blocks.

Constant typed-array markers remain ownership-neutral on the hot path. If a guard deoptimizes, the queued exit retains the referenced value before returning to threaded execution.

## File Structure

- `interp/jit.go`: common region/value/compiler types and shared compilation pipeline.
- `interp/jit_region.go`: private region representation and frontend-neutral validation helpers.
- `interp/jit_trace.go`: trace-tree to region conversion, replacing trace-specific lowering orchestration.
- `interp/jit_cfg.go`: CFG analysis to region conversion, absorbing `cfg.go` and `cfgflow.go` where practical.
- `interp/jit_arm64.go`: shared ARM64 step and terminator lowering.
- Remove `interp/jit_arm64_cfg.go` after migration.

Files are split by responsibility, not by tier. No file may contain a second opcode-dispatch switch.

## Testing

- Differential tests run identical programs through threaded, trace-region, and CFG-region paths.
- Region construction tests assert terminators and metadata, not ARM64 instruction shape.
- Existing deopt, ownership, loop-budget, native-call, and pool tests remain behavior tests.
- Add a source-level invariant test or lightweight grep check ensuring only one native opcode switch remains.
- Run `go test -race ./...`, `GOARCH=amd64 go test ./...`, focused ARM64 JIT tests, and tl2g correctness/benchmarks.
- Performance must not regress by more than 5% from the current tl2g single and 200-row batch medians.

## Migration

1. Introduce region types and shared compile/link shell without behavior change.
2. Adapt trace roots to regions while retaining existing opcode handlers.
3. Adapt CFG blocks to regions.
4. Merge opcode dispatch into `emitStep`.
5. Merge control flow into `emitTerm`.
6. Delete obsolete CFG-specific backend symbols and files.
7. Simplify tests and documentation, then run the completion gate.

Each stage must pass focused tests and be committed independently. Temporary adapters are removed in the same series; no compatibility layer remains at completion.
