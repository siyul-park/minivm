# Unified JIT Region Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove duplicated CFG/trace JIT machinery while preserving semantics, ABI, deoptimization, ownership, spill safety, and performance.

**Architecture:** Keep trace scheduling and CFG graph traversal as thin distinct drivers, but make both consume one compiler shell, one non-control opcode emitter, and one side-exit pipeline. Preserve CFG provenance in frontend analysis so ARM64 handlers no longer rediscover it.

**Tech Stack:** Go, minivm bytecode/JIT, custom ARM64 assembler.

## Global Constraints

- Preserve bytecode semantics, metrics, tier selection, exact fallback IPs, spill policy, and ARM64 ABI.
- Keep call-free module CFG restriction.
- No public API or general-purpose IR package.
- Zero steady-state native allocations.
- No performance regression above 5% on recorded tl2g medians.

---

### Task 1: Common compile shell

**Files:** `interp/jit.go`, `interp/cfg.go`

**Produces:** one helper that owns assembler creation, lowering callback, Build, Link, rejection classification, accounting, and native publication.

- [ ] Add focused compiler tests for clean rejection and module accounting.
- [ ] Extract common context/setup and compile/link helper without changing lowerers.
- [ ] Run `go test ./interp -run 'Test.*JIT|Test.*CFG'`.
- [ ] Commit `refactor(interp): unify jit compile shell`.

### Task 2: Shared side exits

**Files:** `interp/jit.go`, `interp/jit_arm64.go`, `interp/jit_arm64_cfg.go`

**Produces:** one `queueExit` API supporting current state or supplied pre-state plus optional cold retain; one exit materializer.

- [ ] Add/retain exact-IP and typed-array ownership regression tests.
- [ ] Replace `sideExit`, `cfgExit`, and direct exit appends with shared helpers.
- [ ] Run focused deopt/array/JIT tests.
- [ ] Commit `refactor(interp): unify jit side exits`.

### Task 3: Preserve CFG facts

**Files:** `interp/cfgflow.go`, `interp/cfg_test.go` or existing JIT tests

**Produces:** CFG block entry facts retaining kind, function signature, and constant-ref provenance with explicit unknown state.

- [ ] Add merge tests for equal and conflicting provenance.
- [ ] Change `blockKinds` into full fact analysis and remove ambiguous zero sentinel semantics.
- [ ] Run CFG analysis and interpreter tests.
- [ ] Commit `refactor(interp): preserve cfg value facts`.

### Task 4: One non-control opcode emitter

**Files:** `interp/jit_arm64.go`, `interp/jit_arm64_cfg.go`

**Produces:** one `emitStep` switch used by trace and CFG; CFG-specific constant/array wrappers removed.

- [ ] Add a source invariant test preventing a second ARM64 non-control opcode switch.
- [ ] Move CFG metadata preparation before lowering.
- [ ] Route both drivers through `emitStep` and delete `cfgOp`, `cfgConstGet`, `cfgArrayGet`.
- [ ] Run all focused JIT tests.
- [ ] Commit `refactor(interp): share jit opcode lowering`.

### Task 5: Thin region drivers and cleanup

**Files:** `interp/jit.go`, `interp/cfg.go`, `interp/cfgflow.go`, `interp/jit_arm64.go`, `interp/jit_arm64_cfg.go`, `docs/jit-internals.md`

**Produces:** minimal private region/block input, reduced CFG driver, no duplicated compiler orchestration, fewer symbols and lines.

- [ ] Wrap CFG blocks and trace roots in minimal private region descriptors only where it removes parameters/state.
- [ ] Merge identical entry/exit/control helpers; retain genuinely distinct trace continuation and CFG edge logic.
- [ ] Delete obsolete files/symbols and update docs.
- [ ] Compare JIT LOC/symbol counts against baseline and ensure net reduction.
- [ ] Commit `refactor(interp): converge cfg and trace jit pipelines`.

### Task 6: Completion gate

**Files:** all modified files, `docs/benchmarks.md` if measurements change.

- [ ] Run `gofmt` and `go vet ./...`.
- [ ] Run `go test -race ./...`.
- [ ] Run `GOARCH=amd64 go test ./...`.
- [ ] Run tl2g correctness and benchmark commands using the local minivm replacement.
- [ ] Verify no temporary instrumentation, adapters, or dirty generated files remain.
- [ ] Commit any final docs/test cleanup and push `feat/cfg-jit`.
