# Plan-Based JIT Compiler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Replace separate CFG and trace compilation paths with one plan-based compiler selected entirely inside `compiler.Compile`.

**Architecture:** `staticPlanner` and `tracePlanner` convert their source data into the same private `plan` model. The ARM64 lowerer accepts only a plan, and the interpreter installs opaque entries classified by ABI rather than compilation strategy.

**Tech Stack:** Go, minivm bytecode analyses, ARM64 assembler, testify/require.

## Global Constraints

- Preserve bytecode semantics, exact deoptimization IPs, ref ownership, spill safety, loop safepoints, and native-call ABI.
- Keep every new type private and avoid public API changes.
- Keep `Interpreter` unaware of CFG, trace trees, planner order, and strategy rejection.
- Use `plan`, `planner`, `block`, `state`, `slot`, `terminator`, and `entry` consistently; do not introduce synonyms.
- Keep unsupported operations resumable through threaded fallback.
- Keep top-level module native calls restricted until the module-entry ABI is proven independently.
- Maintain tl2g single-row and 200-row medians within 5% of the current baseline.

---

### Task 1: Add the Common Plan Model

**Files:**
- Create: `interp/jit_plan.go`
- Create: `interp/jit_plan_test.go`
- Modify: `docs/superpowers/specs/2026-07-12-plan-compiler-design.md`

**Interfaces:**
- Produces: `plan`, `entry`, `entryKind`, `block`, `state`, `slot`, `terminator`, `terminatorKind`, `spillPolicy`, `planner`, and `compileInput`.
- Produces: `func newCompileInput(*Interpreter, int) (*compileInput, bool)` and `func (p plan) valid() bool`.

- [x] Write table tests for plan validation: unique block offsets, valid entry target, valid terminator targets, and ABI-compatible entry kinds.
- [x] Run `go test ./interp -run TestPlan -count=1`; expect failure because the model does not exist.
- [x] Implement the minimal private model and validation in declaration order.
- [x] Run `go test ./interp -run TestPlan -count=1`; expect pass.
- [x] Commit with `refactor(interp): add jit plan model`.

### Task 2: Convert Static Compilation into `staticPlanner`

**Files:**
- Modify: `interp/jit_plan.go`
- Modify: `interp/jit_plan_test.go`
- Delete after migration: `interp/cfg.go`
- Delete after migration: `interp/cfgflow.go`

**Interfaces:**
- Consumes: `compileInput`, `plan`, `block`, `state`, `slot`, and `terminator`.
- Produces: `type staticPlanner struct{}` and `func (staticPlanner) plan(*compileInput) ([]plan, error)`.

- [x] Add behavior tests for direct branches, conditional branches, branch tables, exact-IP fallbacks, ref provenance merge, direct-call facts, and module-call rejection.
- [x] Run `go test ./interp -run 'TestStaticPlan|TestPlan' -count=1`; expect failures.
- [x] Move basic-block and dataflow logic from `cfg.go`/`cfgflow.go` into `staticPlanner`, producing backend-neutral blocks and terminators with no assembler labels.
- [x] Delete `compileCFG`, `blockFacts`, and all `cfg*` analysis symbols that have no remaining caller.
- [x] Run `go test ./interp -run 'TestStaticPlan|TestPlan|TestInterpreter_JIT' -count=1`; expect pass.
- [x] Commit with `refactor(interp): plan static jit compilation`.

### Task 3: Convert Trace Trees into `tracePlanner`

**Files:**
- Modify: `interp/jit_plan.go`
- Modify: `interp/jit_plan_test.go`
- Modify: `interp/trace.go`
- Modify: `interp/trace_test.go`

**Interfaces:**
- Consumes: immutable `tree` snapshots and the common plan model.
- Produces: `type tracePlanner struct{}` and `func (tracePlanner) plan(*compileInput) ([]plan, error)`.
- Produces generic deferred `work` blocks carrying only plan blocks, symbolic snapshots, labels, and hit ordering.

- [x] Add tests for entry traces, loop roots, partial cuts, aborted branches, learned continuations, caller-tail suffixes, and mutation-derived spill policy.
- [x] Run `go test ./interp -run 'TestTracePlan|TestTracer' -count=1`; expect failures.
- [x] Convert trace roots and eligible continuations into plan blocks; retain runtime-produced snapshots through planner-neutral deferred work rather than tree-specific lowering fields.
- [x] Ensure loop roots and entry-at-zero loop exclusions match existing behavior.
- [x] Run focused trace and JIT tests; expect pass.
- [x] Commit with `refactor(interp): plan trace jit compilation`.

### Task 4: Lower One Plan Through One ARM64 Path

**Files:**
- Modify: `interp/jit.go`
- Modify: `interp/jit_arm64.go`
- Modify: `interp/jit_arm64_test.go`
- Delete: `interp/jit_arm64_cfg.go`
- Delete: `interp/jit_arm64_step.go`
- Delete or merge: `interp/jit_arm64_cfg_test.go`
- Delete: `interp/jit_structure_test.go`

**Interfaces:**
- Changes: `lowerer` to `lower(*lowering, plan) bool`.
- Produces: one block emitter, one opcode dispatcher, and one terminator dispatcher.
- Removes: `lowerCFG`, `cfgEntry`, `cfgBlock`, `cfgEdge`, `cfgIf`, `cfgTable`, `cfgCall`, `emitStep(..., static bool)`, and planner-specific lowering fields.

- [x] Add focused tests covering static and observed plans through the same lowerer contract, including exact fallback IP, native calls, loop budgets, and cold retain exits.
- [x] Run ARM64 JIT tests; expect failure until the contract changes.
- [x] Implement plan block scheduling, entry-state restoration, ordinary opcode emission, and terminator emission in `jit_arm64.go`.
- [x] Replace the `static bool` opcode branch with slot facts already encoded in the plan.
- [x] Remove trace tree/CFG nodes from `lowering`; keep only generic deferred work where branch-time symbolic snapshots are required.
- [x] Delete obsolete ARM64 files and merge their tests into `jit_arm64_test.go`.
- [x] Run `go test ./asm/... ./interp -count=1`; expect pass.
- [x] Commit with `refactor(interp): lower unified jit plans`.

### Task 5: Move Strategy Selection into `compiler.Compile`

**Files:**
- Modify: `interp/jit.go`
- Modify: `interp/interp.go`
- Modify: `interp/jit_test.go`
- Modify: `interp/interp_test.go`
- Modify: `interp/pool_test.go`

**Interfaces:**
- Changes: `func (c *compiler) Compile(i *Interpreter, addr int) (*module, error)` becomes the only compilation entry point.
- Removes: `Interpreter.build`, `compileCFG`, strategy branching in the interpreter, and CFG-specific metrics.
- Changes: `native` to `{callable asm.Callable; entry entryKind}` and removes `loop`/`cfg` strategy flags.

- [x] Add tests proving `Interpreter` calls one compiler path and installs function, loop, and module entries solely by `entryKind`.
- [x] Run focused compiler/install tests; expect failures.
- [x] Implement the ordered planner chain inside `compiler.Compile`, trying static plans only before a function entry has been installed, then trace plans.
- [x] Move clean rejection and strategy diagnostics into compiler-local accounting; retain only strategy-neutral interpreter metrics.
- [x] Remove `Interpreter.build`, `native.cfg`, and CFG-specific metric assertions.
- [x] Run `go test -race ./interp -count=1`; expect pass.
- [x] Commit with `refactor(interp): hide jit strategy selection`.

### Task 6: Consolidate Files, Docs, and Verify

**Files:**
- Modify: `docs/jit-internals.md`
- Modify: `docs/profile.md`
- Modify: `docs/architecture.md` if the ownership map mentions CFG selection.
- Modify: `docs/superpowers/plans/2026-07-12-plan-compiler.md`
- Final production files: `interp/jit.go`, `interp/jit_plan.go`, `interp/jit_arm64.go`, `interp/trace.go`, `interp/jit_stub.go`.

**Interfaces:**
- Produces no new runtime interface; this task removes obsolete names, files, comments, and tests.

- [x] Search for `compileCFG`, `lowerCFG`, `native.cfg`, `cfgEntry`, `cfgBlock`, `cfgEdge`, `jit_arm64_cfg`, and `jit_arm64_step`; expect no production matches.
- [x] Re-read every touched symbol against `docs/coding-patterns.md` sections 0, 1, 2, 5, 6, 7, and 8; inline or remove single-use wrappers and inconsistent names.
- [x] Update JIT and profile documentation to describe the plan compiler and strategy-neutral metrics.
- [x] Run `gofmt`, `goimports`, `go vet ./...`, `go test -race ./...`, and `GOARCH=amd64 go test ./...`.
- [x] Run tl2g correctness and benchmarks for single-row and 200-row regression cases; require less than 5% median regression.
- [x] Compare production JIT file count, symbol count, and LOC against commit `e55d7ee`; require all three to decrease.
- [x] Mark this plan complete and commit with `docs(jit): document plan compiler`.
- [x] Push `feat/cfg-jit` and update PR #139.
