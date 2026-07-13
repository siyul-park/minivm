# Test and Benchmark Deep Correction Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` task-by-task. Track every step with checkboxes.

**Goal:** Correct Phase 9-10 benchmark work, fix discovered runtime bug, and make benchmark/CI gates prove the behavior they claim.

**Architecture:** Preserve completed Phase 0-8 commits. Treat current uncommitted Phase 9-10 files as task-owned input. Split correction into production correctness, kernel/JIT semantics, command/CI gates, then final review.

**Tech Stack:** Go 1.26.2, `testing`, `testify/require`, Make, GitHub Actions, generated threaded interpreter.

## Global Constraints

- Follow `AGENTS.md` and `docs/coding-patterns.md`.
- Use TDD for production behavior changes.
- Never edit generated `interp/threaded.go` without changing `internal/cmd/geninterp` source and regenerating.
- Preserve unrelated user changes; stage explicit files only.
- Keep external runtime comparisons behind `compare` build tag.
- Call a mode `jit_warm` only after proving native code emission.
- One review and one dedicated commit per task.

---

## Progress

| Task | Scope | Status | Commit | Review |
|---|---|---|---|---|
| 1 | Ref-array defaults and broken kernel fixtures | In progress | - | - |
| 2 | Canonical kernels, JIT proof, compare isolation | Pending | - | - |
| 3 | Make targets, CI tiers, selection gates | Pending | - | - |
| 4 | Documentation, full verification, final review | Pending | - | - |
## Task 1: Ref-array Defaults and Broken Fixtures

**Files:**
- Modify: `interp/interp_test.go`
- Modify: `internal/cmd/geninterp/lower.go`
- Regenerate: `interp/threaded.go`
- Modify: `benchmarks/control.go`
- Modify: `benchmarks/memory_test.go`

- [ ] Add runtime specification: `ARRAY_NEW_DEFAULT` for ref arrays yields `BoxedNull` elements.
- [ ] Run focused test and confirm failure shows raw zero instead of `BoxedNull`.
- [ ] Fix generator source to initialize ref elements with `types.BoxedNull`; regenerate output.
- [ ] Add missing array type to sieve program.
- [ ] Make allocation graph checksum compare semantic null, not raw implementation zero.
- [ ] Run `go test -race ./interp` and `(cd benchmarks && go test -race ./...)`.
- [ ] Run generator check, formatting, diff review, then commit production fix separately from fixture corrections when practical.

## Task 2: Canonical Kernels, JIT Proof, Compare Isolation

**Files:**
- Modify: `benchmarks/*_test.go`
- Modify: `benchmarks/compare_fib.go`
- Modify: `benchmarks/compare_fib_test.go`
- Modify: `benchmarks/README.md`

- [ ] Make all tagged and untagged files use one package name.
- [ ] Remove duplicated cold-JIT kernel measurement; `interp` owns cold/exit/deopt lifecycle costs.
- [ ] Build `jit_warm` fixtures with shared cache and profiler-backed warmup.
- [ ] Assert native emission before starting timer; use profiler-free timed interpreter sharing compiled cache.
- [ ] Keep `threaded` for every kernel and `pool` only where embedding lifecycle is measured.
- [ ] Run correctness, benchmark-name smoke, tagged comparison compile, timer-region audit, race test, and review.
- [ ] Commit canonical kernel correction.
## Task 3: Make Targets, CI Tiers, Selection Gates

**Files:**
- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/benchmark.yml`
- Modify: `AGENTS.md`
- Modify: `.claude/CLAUDE.md`

- [ ] Replace invalid full-path benchmark regexes with Go slash-component-compatible filters.
- [ ] Add shell audit that fails when required benchmark owners/modes are not selected.
- [ ] Keep one quick PR report, one canonical target, one repeated nightly target, and one optional comparison target.
- [ ] Remove overlapping legacy comparison job or adapt it to canonical stable names; no duplicate push job.
- [ ] Make CI dependencies explicit and cache both root and nested benchmark modules.
- [ ] Smoke each target with `-benchtime=1x`; parse output to prove required rows ran.
- [ ] Parse workflow YAML, review diff, and commit gate correction.

## Task 4: Documentation, Full Verification, Final Review

**Files:**
- Modify: `docs/plans/test-benchmark-rework.md`
- Modify: `docs/plans/test-benchmark-deep-correction.md`
- Modify: `docs/testing.md`
- Modify: `docs/benchmarks.md`
- Modify: `docs/coding-patterns.md` only if final rules changed

- [ ] Reconcile Phase 9-10 status with actual commits and reproducible commands.
- [ ] Record every discovered defect, root cause, fix, and verification result.
- [ ] Run `make check`, `make coverage-check`, `make fuzz`, generator check, race tests, and benchmark smoke targets.
- [ ] Run `gofmt`, `goimports`, `go vet`, Linux/ARM64 compile checks, and final timer/benchmark selection audits.
- [ ] Review all changed symbols for removal, naming, ownership, declaration order, and simpler control flow.
- [ ] Mark checkboxes complete only after command output passes and working tree state is understood.
- [ ] Commit final docs/gates, then verify clean status and recent commit sequence.
