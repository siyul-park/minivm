# minivm Benchmarks

> **A performance map for the VM — not a leaderboard.**
>
> Current results: **August 10, 2026 · Apple M4 Pro · darwin/arm64 · Go 1.26.2**

This document records the current performance characteristics of minivm, the workloads that define them, and the methodology behind every comparison.

## At a glance

| Signal | Current result | What it tells us |
|---|---:|---|
| BinaryTrees | **1.19× CPython** | Struct-heavy execution is now close to CPython |
| BinaryTrees allocation | **768 B / 8 allocs** | Struct pooling removed almost all per-run allocation |
| StructTreeWalk | **158.5 µs** | Pooled struct traversal remains allocation-light |
| SpectralNorm | **0.67× CPython** | Numeric loops are a strong point |
| StringBuild | **1.97× CPython** | Allocation-heavy string workloads remain a gap |

**Lower is better.** A ratio below `1.00×` means minivm is faster than the comparison runtime.

### The #175 result

`BinaryTrees(4,6)` moved from **2.63 ms / 556,192 B / 8,888 allocs** to **1.225 ms / 768 B / 8 allocs** in threaded execution.

That is approximately **53% less time**, **99.86% fewer bytes**, and **99.91% fewer allocations**. The remaining gap is now primarily execution cost rather than object allocation.

> **Current caveat:** Fannkuch `default` and `jit` fail a correctness assertion in the `compare` suite. This predates the current struct-pool work and is tracked separately in **#184**.

## How to reproduce

### Focused CPython comparison

```bash
cd benchmarks
go test -tags=compare -run='^$' \
  -bench='^(BenchmarkNumeric_(NBody|SpectralNorm|Mandelbrot|MatMul)|BenchmarkCall_(NQueens|Fannkuch)|BenchmarkMemory_(BinaryTrees|SortStress|StringBuild))$' \
  -benchmem -benchtime=300ms -count=3 ./...
```

### Canonical VM kernels

```bash
cd benchmarks
go test -run='^$' -bench='.' -benchmem -benchtime=100ms -count=1 ./...
```

### Interpreter/API benchmarks

```bash
go test -run='^$' \
  -bench='^(BenchmarkNew|BenchmarkInterpreter_(Reset|Push|Pop|PopBoxed|Peek|Alloc|Retain|Release)|BenchmarkPool_(Get|Put))$' \
  -benchmem -benchtime=300ms -count=3 ./interp
```

### Reference traversal

```bash
go test -run='^$' -bench='^Benchmark(Array|Struct|TypedMap|Map)_Refs$' \
  -benchmem -benchtime=300ms -count=3 ./types
```

## Comparison with CPython

These nine hand-written kernels isolate VM execution more closely than minipy's generated corpus. The threaded interpreter is used because it is the closest direct comparison to CPython's interpreter execution.

| Workload | minivm / CPython | minivm ns/op | CPython ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|
| SpectralNorm(24) | **0.67×** | 284,793 | 424,750 | 648 | 6 |
| Mandelbrot(16×16) | **0.77×** | 146,340 | 191,414 | 0 | 0 |
| MatMul(16) | **0.84×** | 175,880 | 209,459 | 6,216 | 6 |
| SortStress(128) | **0.95×** | 331,264 | 349,302 | 5,136 | 512 |
| BinaryTrees(4,6) | **1.19×** | 1,214,215 | 1,018,784 | 768 | 8 |
| NBody(100) | **1.34×** | 319,231 | 238,152 | 504 | 14 |
| NQueens(7) | **1.42×** | 318,854 | 224,831 | 120 | 6 |
| StringBuild(512) | **1.97×** | 741,999 | 376,976 | 1,724,160 | 5,118 |
| Fannkuch(6) | **1.25×** | 546,448 | 438,148 | 34,608 | 1,442 |

### Reading the table

- **Numeric kernels are strong:** SpectralNorm, Mandelbrot, and MatMul all beat CPython.
- **BinaryTrees is no longer allocation-bound:** it is only 1.19× CPython with 768 B/op and 8 allocs/op.
- **StringBuild is the clearest remaining allocation-heavy gap:** 1.72 MB/op and 5,118 allocations.
- **NBody and NQueens remain slower:** these are useful candidates for future execution-path optimization.

The comparison is intentionally narrow. Different runtimes have different value representations, safety boundaries, compilation strategies, and host-call costs. These ratios should not be interpreted as a general language-performance ranking.

## VM execution modes

| Mode | Construction | Meaning |
|---|---|---|
| `default` | `interp.New(prog)` | adaptive execution; hot entries may become native |
| `threaded` | `interp.New(prog, interp.WithThreshold(-1))` | generated threaded execution with JIT disabled |
| `jit` | `interp.New(prog, interp.WithThreshold(0))` | eager profiling/compilation policy |

A `jit` benchmark name does **not** by itself prove native execution. Native entry must be confirmed by the runtime or profiler. On platforms without a native backend, execution remains threaded.

## Canonical kernel snapshot

The complete kernel sweep below is a current snapshot, not a statistical regression gate. It was measured with `-benchtime=100ms -count=1`.

| Workload | default | threaded | jit |
|---|---:|---:|---:|
| RecursiveFib(20) | 39.7 µs | 326.0 µs | 390.4 µs |
| RecursiveFib(35) | 49.6 ms | 424.8 ms | 532.6 ms |
| IndirectRecursiveFib | 491 µs | 577 µs | 693 µs |
| ClosureCounter | 3.26 µs | 2.51 µs | 3.24 µs |
| NQueens | 154 µs | 321 µs | 154 µs |
| IterativeFib | 27.6 ns | 508 ns | 44.6 ns |
| Sieve | 1.54 µs | 11.1 µs | 1.54 µs |
| TypedArraySum | 294 ns | 3.59 µs | 497 ns |
| AllocationGraph | 6.85 µs | 5.06 µs | 6.86 µs |
| PermutationFlips | 124 µs | 77.7 µs | 113 µs |
| StructTreeWalk | 178 µs | **159 µs** | 177 µs |
| BinaryTrees | 1.34 ms | **1.23 ms** | 1.34 ms |
| SortStress | 73.6 µs | 320 µs | 73.1 µs |
| StringBuild | 628 µs | 616 µs | 536 µs |
| BranchTree | 253 ns | 588 ns | 253 ns |
| NBody | 99.2 µs | 313 µs | 99.6 µs |
| SpectralNorm | 41.9 µs | 278 µs | 42.3 µs |
| Mandelbrot | 49.2 µs | 140 µs | 49.6 µs |
| MatMul | 38.2 µs | 175 µs | 38.0 µs |

Fannkuch `default` and `jit` are omitted from this table because their correctness assertion currently fails; see #184.

## Interpreter and API costs

These benchmarks measure named public operations. Setup and paired operations stay outside the reported operation interval where the benchmark defines them that way.

| Area | Operation | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Construction | empty program | 3,409 | 35,338 | 29 |
| Construction | program, JIT disabled | 3,430 | 35,416 | 32 |
| Construction | program, JIT enabled | 3,445 | 35,416 | 32 |
| Reset | scalar state | 28.3 | 0 | 0 |
| Reset | heap state | 38.5 | 8 | 1 |
| Stack | Push scalar | 20.2 | 0 | 0 |
| Stack | Push reference | 43.7 | 16 | 1 |
| Stack | Pop | 8.75 | 0 | 0 |
| Stack | PopBoxed | 7.74 | 0 | 0 |
| Stack | Peek | 1.83 | 0 | 0 |
| Heap | Alloc | 37.6 | 16 | 1 |
| Heap | Retain | 19.9 | 0 | 0 |
| Heap | Release | 18.3 | 0 | 0 |
| Pool | Get, hit | 34.0 | 0 | 0 |
| Pool | Get, miss | 3,251 | 35,192 | 26 |
| Pool | Put | 116.5 | 0 | 0 |

The important signal here is not that every operation is tiny; it is that the hot reuse path is allocation-free. Pool misses and interpreter construction naturally pay for fresh runtime state.

## Threaded execution

Representative complete `Run` cases use `WithTick(1)` and `WithThreshold(-1)`. Setup opcodes are included in the timed run; reset and result validation are not.

| Case | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| i32.const_nop_returns_i32 | 17.55 | 0 | 0 |
| i32.const_returns_i32 | 17.55 | 0 | 0 |
| i32.add | 19.86 | 0 | 0 |
| i64.add | 19.86 | 0 | 0 |
| f32.add | 19.86 | 0 | 0 |
| f64.add | 19.86 | 0 | 0 |
| i32.to_i64_s | 19.86 | 0 | 0 |
| br | 18.15 | 0 | 0 |
| br_if | 20.54 | 0 | 0 |
| br_table | 20.37 | 0 | 0 |
| direct bytecode call | 30.97 | 0 | 0 |

These are whole-program cases, not isolated opcode latency measurements.

## JIT activation boundary

`ColdBackedge` measures a 256-iteration loop with a deliberately high threshold. The function remains below the sample threshold and therefore uses the ordinary generated branch handler.

| Case | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| cold unconditional backedge | 2,039 | 0 | 0 |

The benchmark protects the cold path: backedge observation is sampled periodically instead of adding a callback or header scan to every cold loop iteration.

## Reference traversal

`types.Traceable.Refs` accepts caller-provided storage, keeping the canonical traversal cases allocation-free.

| Value | Case | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| array | no child refs | 2.69 | 0 | 0 |
| array | child refs | 2.38 | 0 | 0 |
| struct | no child refs | 1.81 | 0 | 0 |
| struct | child refs | 1.92 | 0 | 0 |
| typed map | child refs | 24.47 | 0 | 0 |
| dynamic map | no child refs | 1.84 | 0 | 0 |
| dynamic map | child refs | 26.70 | 0 | 0 |

## What the numbers mean

### Strong areas

- Stable numeric loops benefit most from the adaptive native tier.
- Threaded struct workloads now have almost no allocation overhead after pooling.
- Reference traversal is allocation-free when callers reuse storage.

### Current opportunities

1. **StringBuild** — the largest measured CPython gap and the highest allocation volume.
2. **NBody / NQueens** — slower than CPython despite low allocation counts, suggesting execution-path rather than allocation work.
3. **BinaryTrees** — only 1.19× CPython now; remaining cost should be profiled before changing RC or struct construction again.
4. **Fannkuch correctness** — `default` and `jit` need investigation independently of performance work (#184).

Do not optimize from a single ratio. First reproduce the row, then profile the dominant path, then compare a focused before/after run.

## Methodology

- Canonical workloads have correctness checks with fixed results or checksums.
- The compare suite performs correctness validation before timing and uses untimed warmup/allocation preparation where the fixture requires it.
- Focused comparison tables use `-benchtime=300ms -count=3` and report the median of three sequential samples.
- The complete kernel snapshot uses `-benchtime=100ms -count=1`; it is a snapshot, not a regression gate.
- minivm `default`, `threaded`, and `jit` differ through execution thresholds.
- External parsing, compilation, module creation, and lookup stay outside timers where the runtime API permits.
- CPython is launched once per benchmark process; the workload runs inside `time.perf_counter()`, excluding interpreter startup.
- CPython comparisons use `PYTHONHASHSEED=0` and verify the expected result before timing.
- Cross-runtime comparison is optional and requires the `compare` build tag.

### Comparison runtimes

| Runtime | Version |
|---|---|
| CPython | 3.13.x |
| wazero | v1.12.0 |
| gopher-lua | v1.1.2 |
| Tengo | v2.17.0 |
| Goja | v0.0.0-20260311135729-065cd970411c |
| gpython | v0.2.0 |
| Yaegi | v0.16.1 |

## Benchmark ownership

| Location | Responsibility |
|---|---|
| `interp/*_test.go` | interpreter construction, execution, reset, stack/heap, pool, JIT lifecycle |
| `types/*_test.go` | reference traversal contracts |
| `benchmarks/` | runtime-neutral kernels and external comparisons |

A benchmark should have a correctness test owned by the same public behavior or canonical fixture. Service-specific workloads do not define VM performance baselines.

## Running the suite

| Command | Purpose |
|---|---|
| `make benchmark-pr` | fast pull-request smoke suite |
| `make benchmark-core` | local canonical benchmark run |
| `make benchmark-nightly` | repeated scheduled suite |
| `make benchmark-compare` | optional external runtime comparison |

For performance claims, keep benchmark processes sequential and record the host, Go version, command, and sample count. Use repeated measurements and `benchstat` before declaring a regression or improvement.

## Benchmark reading guide

**Start with “At a glance.”** It answers whether the latest change moved an important workload.

**Use “Comparison with CPython” for cross-runtime questions.** Ratios are intentionally workload-specific.

**Use “Canonical kernel snapshot” for execution-tier behavior.** It shows where `default`, `threaded`, and `jit` differ.

**Use “Interpreter and API costs” for micro-optimization.** These rows identify the cost of individual runtime operations.

**Use “Methodology” before publishing numbers.** Absolute timings only make sense with their machine, command, sample count, and benchmark semantics.

## Related documentation

- `docs/profile.md` — runtime counters and profiling
- `docs/jit-internals.md` — ARM64 trace JIT behavior
- `docs/instruction-set.md` — opcode semantics and JIT support
- `docs/compatibility.md` — supported platforms
- GitHub issue **#184** — Fannkuch `default`/`jit` correctness failure
