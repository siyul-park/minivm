# Test and Benchmark Rework Plan

Issues [#144](https://github.com/siyul-park/minivm/issues/144) and [#131](https://github.com/siyul-park/minivm/issues/131).

## Goal

Make tests readable executable specifications of public behavior, then rebuild benchmarks around explicit VM costs and runtime-neutral kernels without changing bytecode semantics.

## Execution Rule

Every phase uses this fixed gate:

1. Write or reorganize specification tests first.
2. Run focused tests and observe the expected failure when behavior or a gate is missing.
3. Make the smallest change that satisfies the specification.
4. Run focused package tests, race tests where applicable, formatting, and generated-code checks.
5. Review the complete phase diff against `AGENTS.md` and `docs/coding-patterns.md`.
6. Record review findings and verification below.
7. Mark the phase complete only after a dedicated commit.

No later phase starts before the previous phase passes its review gate.

## Progress

| Phase | Scope | Status | Commit | Review |
|---|---|---|---|---|
| 0 | Baseline, inventory, and plan | Complete | `test: record test and benchmark baseline` | Passed |
| 1 | Test ownership matrices and completeness gates | Complete | `test: add test ownership gates` | Passed |
| 2 | `instr` executable specifications and fuzzing | Complete | `test(instr): reorganize executable specifications` | Passed |
| 3 | `types` executable specifications and fuzzing | Complete | `test(types): reorganize executable specifications` | Passed |
| 4 | `program` construction, parser, verifier, and fuzzing | Complete | `test(program): reorganize executable specifications` | Passed |
| 5 | Interpreter public API, marshal, host, pool, and lifecycle | Complete | `test(interp): align public API specifications` | Passed |
| 6 | Opcode corpus and threaded/optimized/JIT semantic parity | Pending | - | - |
| 7 | Optimizer, backend, generator, and support packages | Pending | - | - |
| 8 | Interpreter benchmark consolidation | Pending | - | - |
| 9 | Runtime-neutral kernel and comparison benchmark rebuild | Pending | - | - |
| 10 | Coverage gates, CI, documentation, and final review | Pending | - | - |

## Global Constraints

- Follow `AGENTS.md` and `docs/coding-patterns.md`.
- Preserve unrelated user changes.
- Use one top-level test per exported behavior owner and shallow behavior-named subtests.
- Prefer exported APIs; retain white-box tests only for documented safety invariants.
- Keep fixtures, execution, timer boundaries, and expectations visible.
- Use `require`, not `assert`; avoid generic test or benchmark frameworks.
- Keep bytecode semantics unchanged. Production fixes require a failing specification first and a separate commit.
- Keep benchmarks deterministic and validate each fixture outside the timed loop.
- Keep external runtime comparisons optional and outside regression gates.

## Phase 0: Baseline, Inventory, and Plan

### Deliverables

- [x] Create branch `feature/test-benchmark-specs` from clean `main`.
- [x] Read repository instructions and test, benchmark, and documentation conventions.
- [x] Inventory all test, fuzz, example, and benchmark functions.
- [x] Inventory exported functions and methods against top-level owner tests.
- [x] Record `make check`, `make coverage`, package coverage, and total coverage.
- [x] Record current benchmark ownership and classify each case as interpreter microbenchmark, VM kernel, comparison, or obsolete duplicate.
- [x] Review inventory for false positives from private receiver types and type aliases.
- [x] Commit baseline plan and inventory.

### Verification

```bash
make check
make coverage
go tool cover -func=coverage.out
```

### Review

- Confirm no production or test behavior changed.
- Confirm inventory distinguishes exported public contracts from exported methods on private types.
- Confirm every later acceptance criterion has a phase owner.

### Baseline

`make check` passed before changes. Coverage was measured with `GOENV_VERSION=1.26.2` because `.go-version` selects Go 1.25.0 while `go.mod` requires Go 1.26.2; goenv's automatic toolchain path otherwise mixes compiler caches.

| Package | Coverage | Public owners | Missing exact owner tests |
|---|---:|---:|---:|
| `analysis` | 78.0% | 5 | 3 |
| `asm` | 82.8% | 44 | 15 |
| `asm/amd64` | 100.0% | 1 | 1 |
| `asm/arm64` | 50.4% | 155 | 152 |
| `cli` | 92.5% | 6 | 2 |
| `debug` | 93.6% | 12 | 11 |
| `instr` | 78.1% | 44 | 12 |
| `interp` | 20.6% | 66 | 15 |
| `optimize` | 100.0% | 4 | 1 |
| `pass` | 100.0% | 9 | 5 |
| `prof` | 76.8% | 22 | 10 |
| `program` | 76.9% | 25 | 12 |
| `transform` | 87.4% | 10 | 5 |
| `types` | 85.8% | 172 | 71 |
| **total** | **72.8%** | **575** | **315** |

Exact owner-test counts are structural migration indicators, not claims that the listed behavior is currently untested. Existing umbrella tests often cover several owners and will be split in later phases.

Current benchmark classification:

| Current owner | Classification | Phase |
|---|---|---:|
| `benchmarks/alloc_test.go` | interpreter construction/warm/reset microbenchmarks; move to `interp` | 8 |
| `benchmarks/fusion_test.go` | direct interpreter dispatch/fusion microbenchmarks; move to `interp` | 8 |
| `benchmarks/jit_issue60_test.go` | indirect call, closure, and typed-array VM kernels; rename and split | 9 |
| `benchmarks/jit_issue101_test.go` | branch-tree VM kernel plus misplaced correctness test; rename and split | 6, 9 |
| `benchmarks/fib_test.go` | external runtime comparison; isolate behind `compare` tag | 9 |
| `analysis/blocks_test.go` | package-owned analysis microbenchmark; retain with owner | 7 |
| `types/*_test.go` benchmarks | package-owned value/tracing microbenchmarks; retain only distinct signals | 3 |

## Phase 1: Test Ownership Matrices and Completeness Gates

### Files

- Create `docs/testing.md` as permanent testing ownership document.
- Modify `docs/README.md`, `AGENTS.md`, and `.claude/CLAUDE.md` only where routing is missing.
- Add narrow completeness tests in owning packages; do not add a cross-package reflection framework.

### Tasks

- [x] Build a reviewed public symbol-to-owner-test matrix grouped by package and production file.
- [x] Build an opcode matrix covering metadata, parser/formatter, verifier, runtime corpus, and applicable JIT parity.
- [x] Document intentional exclusions: constants, aliases, marker methods, architecture stubs, and private invariants.
- [x] Add an opcode registry completeness specification in `instr`.
- [x] Add verifier-policy completeness specification in `program`.
- [x] Add runtime-corpus completeness specification in `interp`.
- [x] Run focused tests, review diff, update progress, and commit.

### Verification

```bash
go test -race ./instr ./program ./interp
make check-generated
```

### Review

- Gates fail for a deliberately omitted opcode entry.
- Gates derive from authoritative registries rather than duplicated hard-coded opcode lists.
- Documentation is descriptive; tests remain enforcement source of truth.

## Phase 2: `instr` Executable Specifications and Fuzzing

### Files

`instr/builder_test.go`, `instr/code_test.go`, `instr/handler_test.go`, `instr/instr_test.go`, `instr/kind_test.go`, `instr/opcode_test.go`, `instr/parse_test.go`, `instr/type_test.go`, `instr/fuzz_test.go`.

### Tasks

- [x] Align test files with production ownership and remove umbrella ownership.
- [x] Specify `Kind` string, numeric, representation, and size behavior.
- [x] Specify opcode classification, metadata uniqueness, `TypeOf`, and `Valid`.
- [x] Specify instruction construction, operand access, width, mutation, target calculation, and formatting.
- [x] Specify marshal/unmarshal and symbolic branch/handler resolution.
- [x] Add bounded parser and instruction round-trip fuzz tests.
- [x] Remove duplicate or issue-history test names.
- [x] Run focused tests, review diff, update progress, and commit.

### Verification

```bash
go test -race ./instr
go test -run='^$' -fuzz=FuzzInstructionRoundTrip -fuzztime=10s ./instr
```

## Phase 3: `types` Executable Specifications and Fuzzing

### Files

All production-matched `types/*_test.go` files plus `types/fuzz_test.go`.

### Tasks

- [x] Specify value zero/null/kind and every boxing/unboxing boundary.
- [x] Consolidate primitive scalar and scalar-type behavior by public owner.
- [x] Specify array, struct, map, iterator, string, function, closure, and error contracts.
- [x] Split iterator and aggregate umbrella tests into method-owned tests.
- [x] Specify append-to-destination reference tracing, invalid indexes, ownership, map overwrite/delete, NaN, and signed zero behavior.
- [x] Add bounded type and function parse/format round-trip fuzz tests.
- [x] Keep only meaningful traversal microbenchmarks and validate their fixtures.
- [x] Run focused tests, review diff, update progress, and commit.

### Verification

```bash
go test -race ./types
go test -run='^$' -fuzz=FuzzParseType -fuzztime=10s ./types
go test -run='^$' -fuzz=FuzzParseFunction -fuzztime=10s ./types
```

## Phase 4: `program` Construction, Parser, Verifier, and Fuzzing

### Files

`program/program_test.go`, `program/builder_test.go`, `program/parse_test.go`, `program/verify_test.go`, `program/fuzz_test.go`.

### Tasks

- [x] Specify `New`, every `With*` option, and `Program.String`.
- [x] Specify builder interning and final program assembly separately from instruction encoding.
- [x] Structure verifier cases under valid, structure, bounds, control, stack, types, calls, and handlers.
- [x] Specify `VerifyError.Error` and `VerifyError.Unwrap`.
- [x] Add parser/string round-trip fuzzing.
- [x] Add arbitrary-bytecode `FuzzVerify` and prove no panic at admission boundary.
- [x] Run focused tests, review diff, update progress, and commit.

### Verification

```bash
go test -race ./program
go test -run='^$' -fuzz=FuzzVerify -fuzztime=10s ./program
```

## Phase 5: Interpreter Public API, Marshal, Host, Pool, and Lifecycle

### Files

`interp/interp_test.go`, `interp/marshal_test.go`, `interp/host_test.go`, `interp/pool_test.go`, `interp/coroutine_test.go`, `interp/cache_test.go`, `interp/trace_test.go`.

### Tasks

- [x] Keep `interp_test.go` ownership limited to `interp.go`: construction, options, accessors, stack, heap, reset, close, and run.
- [x] Move marshal/unmarshal contracts into `marshal_test.go` and cover primitives, named values, pointers, collections, custom marshalers/converters, cycles, overflow, and mismatches.
- [x] Split host function and host object tests by public method owner.
- [x] Specify `Push`, `Pop`, `PopBoxed`, and `Peek` ownership differences through observable lifecycle behavior.
- [x] Specify pool capacity, reuse, blocking, cancellation, reset, close, outstanding members, and idempotence without sleeps; review error aggregation without adding a test-only injection seam.
- [x] Keep only essential cache, trace, reference-count, and coroutine white-box invariants.
- [x] Run focused race tests, review diff, update progress, and commit.

### Verification

```bash
go test -race ./interp
go test -race -run='TestPool_|TestInterpreter_(Marshal|Unmarshal|Run|Reset|Close)' ./interp
```

## Phase 6: Opcode Corpus and Semantic Parity

### Files

`interp/interp_test.go`, relevant optimizer/JIT tests, and matrix sections in `docs/testing.md`.

### Tasks

- [ ] Convert `runTests` into behavior-named rows in instruction-family declaration order.
- [ ] Ensure each runtime opcode has an explicit success, error, or documented exclusion case.
- [ ] Keep complex call, coroutine, error, reset, and ownership lifecycles as explicit subtests.
- [ ] Remove issue-number and implementation-path test names.
- [ ] Add bounded valid-program optimizer parity.
- [ ] Add bounded verified-program threaded/JIT parity on supported architectures.
- [ ] Compare stack, errors, globals, host effects, coroutine state, and reset behavior rather than trace shape.
- [ ] Run focused and architecture tests, review diff, update progress, and commit.

### Verification

```bash
go test -race ./interp ./optimize ./transform
GOOS=linux GOARCH=arm64 go test -exec=true ./interp ./optimize ./transform
```

## Phase 7: Optimizer, Backend, Generator, and Support Packages

### Files

Tests in `analysis`, `asm`, `cli`, `debug`, `internal/cmd/geninterp`, `optimize`, `pass`, `prof`, and `transform`.

### Tasks

- [ ] Align tests to exported constructor and method owners.
- [ ] Require structural rewrite proof plus execution parity for transforms.
- [ ] Specify optimizer levels through output rather than private pipeline contents.
- [ ] Keep exact machine encoding, relocation, register allocation, W^X, and pointer stability tests where public/backend contracts require them.
- [ ] Split profiler/collector tests by production ownership and add concurrency/state boundaries.
- [ ] Test debugger stepping with real interpreter programs.
- [ ] Test CLI/REPL through injected filesystems and buffers.
- [ ] Specify generator definition completeness, duplicate rejection, deterministic rendering, and `-check` behavior.
- [ ] Run focused tests, review diff, update progress, and commit.

### Verification

```bash
go test -race ./analysis ./asm/... ./cli ./debug ./internal/cmd/geninterp ./optimize ./pass ./prof ./transform
make check-generated
```

## Phase 8: Interpreter Benchmark Consolidation

### Files

`interp/interp_test.go` and obsolete interpreter-owned benchmark files elsewhere.

### Tasks

- [ ] Move interpreter-owned microbenchmarks under matching public API benchmark owners in `interp/interp_test.go`.
- [ ] Align benchmark and correctness-test names and subcase hierarchy.
- [ ] Add construction, reset, empty run, NOP, dispatch, stack, local, global, numeric, branch, call, heap, collection, coroutine, pool, and supported JIT lifecycle cases.
- [ ] Keep setup, verification, reset, and warmup outside execution-only timers.
- [ ] Validate fixtures once before timing and final state/checksum after timing where needed.
- [ ] Use deterministic state, `b.Loop()`, `b.ReportAllocs()`, and explicit custom units only where stable.
- [ ] Run compile/smoke benchmarks, review diff, update progress, and commit.

### Verification

```bash
go test ./interp -run='^$' -bench='^Benchmark(New|Interpreter_)' -benchmem -benchtime=1x
```

## Phase 9: Runtime-Neutral Kernel and Comparison Benchmark Rebuild

### Files

`benchmarks/control.go`, `benchmarks/control_test.go`, `benchmarks/call.go`, `benchmarks/call_test.go`, `benchmarks/memory.go`, `benchmarks/memory_test.go`, `benchmarks/numeric.go`, `benchmarks/numeric_test.go`, `benchmarks/compare_fib_test.go`, and removal of obsolete files.

### Tasks

- [ ] Add deterministic iterative Fibonacci, recursive Fibonacci, indirect recursive Fibonacci, sieve, typed array sum, closure counter, branch tree, and allocation graph kernels.
- [ ] Give each kernel fixed inputs, expected result/checksum, threaded mode, and optional meaningful JIT/pool modes.
- [ ] Reframe synthetic LightGBM-shaped workload as `BranchTree`.
- [ ] Remove issue-number, fusion-only, and service-domain benchmark ownership.
- [ ] Put external runtime comparisons behind `//go:build compare` and `BenchmarkCompare_*` names.
- [ ] Keep cross-runtime results informational and separate from internal gates.
- [ ] Run kernel and comparison smoke benchmarks, review diff, update progress, and commit.

### Verification

```bash
(cd benchmarks && go test -run='^$' -bench='^(BenchmarkControl|BenchmarkCall|BenchmarkMemory|BenchmarkNumeric)' -benchmem -benchtime=1x ./...)
(cd benchmarks && go test -tags=compare -run='^$' -bench='^BenchmarkCompare' -benchmem -benchtime=1x ./...)
```

## Phase 10: Coverage Gates, CI, Documentation, and Final Review

### Files

`Makefile`, `.github/workflows/ci.yml`, `docs/testing.md`, `docs/benchmarks.md`, `docs/README.md`, this plan, and package tests requiring final cleanup.

### Tasks

- [ ] Add `benchmark-core` and `benchmark-compare` targets; keep `benchmark` as canonical core alias.
- [ ] Add deterministic PR benchmark reporting and nightly/full benchmark commands without performance failure thresholds.
- [ ] Add fuzz smoke commands for instruction, type, function, program, verifier, and parity fuzzers.
- [ ] Establish package coverage gates only from stable post-migration measurements; total coverage must not fall below baseline.
- [ ] Remove duplicate tests, unused helpers, arbitrary sleeps, issue-number names, stale benchmark files, and generated artifacts.
- [ ] Update testing and benchmark methodology docs with exact commands and ownership.
- [ ] Run all validation, inspect final diff, perform another simplification pass, and run final code review.
- [ ] Mark both issues complete in this document and commit final gates/docs.

### Verification

```bash
make check
make coverage
go tool cover -func=coverage.out
make benchmark-core
make benchmark-compare
GOOS=linux GOARCH=arm64 go build ./...
GOOS=linux GOARCH=arm64 go test -exec=true ./...
```

## Phase Review Log

### Phase 0

- Focused verification: inventory scripts reviewed against exported receiver visibility.
- Broad verification: `make check`; `GOENV_VERSION=1.26.2 make coverage`.
- Review findings fixed: excluded exported methods on private receiver types, included architecture subpackages, and separated package-owned microbenchmarks from interpreter-owned cases.
- Intentionally retained structure: no code or test files changed before migration phases.
- Coverage/benchmark effect: baseline only; total 72.8%.
- Commit: `test: record test and benchmark baseline`.

### Phase 1

- Focused verification: `GOENV_VERSION=1.26.2 go test -race ./instr ./program ./interp`.
- Broad verification: `GOENV_VERSION=1.26.2 make check-generated`; `git diff --check`.
- Review findings fixed: included architecture subpackages in the API inventory, renamed the verifier matrix by role, and replaced direct `t.Fatalf` use with `require`.
- Gate validation: missing `opcodeCount` failed `TestValid`; omitted `array.append` failed verifier policy coverage; deliberately omitted `nop` failed runtime corpus coverage.
- Intentionally retained structure: the verifier policy map remains an independent test oracle; the exact symbol and opcode matrices remain verbose because each public owner and opcode needs visible ownership.
- Coverage/benchmark effect: `instr` 78.1% -> 78.7%; `program` 76.9%; `interp` 20.6%; no benchmark change.
- Commit: `test: add test ownership gates`.

### Phase 2

- Focused verification: `GOENV_VERSION=1.26.2 go test -race ./instr`; both fuzz targets ran for 10 seconds.
- Broad verification: explicit `gofmt`, `/Users/siyulpark/go/bin/goimports`, `go vet ./instr`, generator `-check`, and `git diff --check`.
- Review findings fixed: restored production declaration order, kept `TestValid` as exhaustive registry coverage instead of a duplicated opcode list, and moved numeric readers/parsers to `parse_test.go`.
- Production bug fixed: `KindAny.Repr()` no longer aliases to `KindI32`; committed separately as `aae004a`.
- Intentionally retained structure: no `handler_test.go` because `Handler` is a behavior-free data record; handler resolution is specified through `Builder.Try` and `Builder.Handlers`.
- Coverage/benchmark effect: `instr` 78.7% -> 93.9%; no benchmark change.
- Commit: `test(instr): reorganize executable specifications`.

### Phase 3

- Focused verification: `GOENV_VERSION=1.26.2 go test -race ./types`; both parse fuzz targets ran for 10 seconds.
- Broad verification: method-owned traversal benchmarks compiled and ran with `-benchtime=1x`; explicit `gofmt`, `goimports`, `go vet ./types`, owner inventory, and `git diff --check` passed.
- Review findings fixed: split primitive and iterator umbrella tests, restored production declaration order, folded cached-field checks into constructors, removed duplicate `Kind` alias tests, and preserved destination prefixes for every `Traceable` contract.
- Benchmark review: renamed benchmarks to their public method owners, separated `Map.Refs` from `TypedMap.Refs`, moved map-get timing to the later VM-kernel phase, validated fixtures before timing, and used preallocated scratch with `b.Loop()`.
- Intentionally retained structure: `PrimitiveType_*` groups behavior of exported primitive type singleton instances whose concrete receiver types are private.
- Coverage/benchmark effect: `types` 85.8% -> 90.2%; retained traversal benchmarks report 0 allocs/op with reused destination buffers.
- Commit: `test(types): reorganize executable specifications`.

### Phase 4

- Focused verification: `GOENV_VERSION=1.26.2 go test -race ./program`; parser and verifier fuzz targets each ran for 10 seconds.
- Broad verification: explicit `gofmt`, `goimports`, `go vet ./program`, owner inventory, coverage, and `git diff --check` passed.
- Review findings fixed: aligned builder tests with program assembly ownership instead of instruction-offset encoding, added globals to full text round trips, and grouped verifier cases by shallow behavioral category.
- Production bug fixed: verifier now rejects branch targets inside instruction operands before CFG construction; committed separately as `6d2870d`.
- Intentionally retained structure: verifier policy coverage remains inside `TestVerify` because it is the public admission contract, not generator metadata.
- Coverage/benchmark effect: `program` 76.9% -> 80.9%; no benchmark change.
- Commit: `test(program): reorganize executable specifications`.

### Phase 5

- Focused verification: all interpreter public API, marshal, host, pool, error, coroutine, cache, and tracer tests passed; `GOENV_VERSION=1.26.2 go test -race ./interp` passed.
- Broad verification: explicit `gofmt`, `goimports`, `go vet ./interp`, owner inventory, coverage, and `git diff --check` passed.
- Review findings fixed: moved marshal tests to the production-matched file, split the host-object umbrella by public method, replaced timeout-based pool cancellation with channel coordination, removed duplicate opcode execution from pool tests, and named tracer white-box tests by invariant.
- Ownership specified: `Pop` releases loaded refs, `PopBoxed` transfers boxed refs, and `Peek` preserves stack ownership.
- Intentionally retained structure: cache/tracer wiring and pool live-count checks remain white-box safety invariants because no exported observation path exists. Pool close error aggregation has no deterministic public failure source; no test-only production seam was added.
- Coverage/benchmark effect: `interp` 20.6% -> 21.1%; no benchmark change.
- Commit: `test(interp): align public API specifications`.

Add one entry after each later phase:

```text
Phase N
- Focused verification: ...
- Broad verification: ...
- Review findings fixed: ...
- Intentionally retained structure: ...
- Coverage/benchmark effect: ...
- Commit: ...
```
