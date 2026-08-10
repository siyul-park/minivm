# minivm Benchmarks

> **A performance map for the VM — not a leaderboard.**
>
> Current results: **August 10, 2026 · Apple M4 Pro · darwin/arm64 · Go 1.26.2**

This document records the current performance characteristics of minivm, the workloads that define them, and the methodology behind every comparison.

## Performance at a glance

The most useful baseline for minivm is not a foreign language runtime. It is the Go ecosystem around it: native Go shows the cost of interpretation itself, while Go-based interpreters show how minivm compares with other implementations written in the same language and running on the same host.

### Go ecosystem comparison

`threaded` is the appropriate minivm baseline for this comparison because it measures the VM without native JIT specialization. Results below are medians from the same three-sample run on this machine.

| Workload | minivm threaded | Native Go | wazero | Tengo | gopher-lua | Goja |
|---|---:|---:|---:|---:|---:|---:|
| RecursiveFib(20) | **323 µs** | 14.6 µs | 33.9 µs | 889 µs | 1.11 ms | 1.52 ms |
| StructTreeWalk | **158 µs** | 12.8 µs | — | 281 µs | 549 µs | 447 µs |
| BinaryTrees | **1.20 ms** | 118 µs | — | — | — | — |

These are not a leaderboard: each runtime has a different value model and execution architecture. The useful signal is the relative cost of executing the same deterministic workload. minivm's threaded interpreter is substantially closer to native Go and other Go-based runtimes on these kernels than the slower general-purpose interpreters in the comparison suite.

### Current runtime profile

| Signal | Current result | What it tells us |
|---|---:|---|
| Allocation-free execution | **0 B / 0 allocs** on several hot kernels | the VM can execute tight scalar loops without Go heap pressure |
| StructTreeWalk | **159 µs / 768 B / 8 allocs** | reusable struct storage keeps recursive object workloads bounded |
| BinaryTrees | **1.23 ms / 768 B / 8 allocs** | object-heavy execution stays allocation-light |
| SpectralNorm | **42 µs / 648 B / 6 allocs** | numeric kernels benefit strongly from the adaptive tier |
| StringBuild | **536–616 µs / 1.72 MB / 5,118 allocs** | strings remain the clearest allocation-heavy workload |

The strongest signal is the combination: minivm keeps many VM operations allocation-free, reuses short-lived objects, and can move compute-heavy kernels toward native execution when the adaptive tier is effective. String construction remains a distinct workload with materially different allocation behavior.

> **Current caveat:** Fannkuch `default` and `jit` still fail a correctness assertion in the `compare` suite. The issue is tracked separately in **#184**.

## How to reproduce

### External reference appendix

```bash
cd benchmarks
go test -tags=compare -run='^$' \
  -bench='^(BenchmarkNumeric_(NBody|SpectralNorm|Mandelbrot|MatMul)|BenchmarkCall_(NQueens|Fannkuch)|BenchmarkMemory_(BinaryTrees|SortStress|StringBuild))$' \
  -benchmem -benchtime=300ms -count=3 ./...
```

This appendix is supplementary. External runtimes are useful for context, but they are not the primary performance baseline for minivm; the Go ecosystem comparison above is.

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

## Go mode comparison

The canonical kernel snapshot below is the main comparison surface for minivm. It shows how `default`, `threaded`, and `jit` behave on the same workloads.

- **Numeric kernels are strongest in the adaptive tier.** SpectralNorm, Mandelbrot, and MatMul are best in `default` or `jit` mode.
- **Threaded mode is the interpreter baseline.** It is the cleanest way to measure dispatch and ownership costs without native specialization.
- **Object reuse keeps allocation-heavy kernels bounded.** BinaryTrees and StructTreeWalk use 768 B/op and 8 allocs/op in threaded mode.
- **StringBuild is still the clearest allocation-heavy hotspot.** It remains a useful target for future work.

The comparison is intentionally narrow. Different execution modes change how quickly a workload reaches native code or stays on threaded dispatch, but they still share the same value model, safety boundaries, and benchmark fixtures.

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

1. **StringBuild** — the highest allocation volume and the clearest remaining hotspot.
2. **NBody / NQueens** — threaded mode is slower than the adaptive tier, suggesting execution-path rather than allocation work.
3. **BinaryTrees** — threaded and adaptive modes are already close, so remaining cost should be profiled before changing RC or struct construction.
4. **Fannkuch correctness** — `default` and `jit` need investigation independently of performance work (#184).

Do not optimize from a single ratio. First reproduce the row, then profile the dominant path, then compare a focused before/after run.

## Methodology

- Canonical workloads have correctness checks with fixed results or checksums.
- The compare suite performs correctness validation before timing and uses untimed warmup/allocation preparation where the fixture requires it. It is supplementary to the main Go mode comparisons.
- Focused comparison tables use `-benchtime=300ms -count=3` and report the median of three sequential samples.
- The complete kernel snapshot uses `-benchtime=100ms -count=1`; it is a snapshot, not a regression gate.
- minivm `default`, `threaded`, and `jit` differ through execution thresholds.
- External parsing, compilation, module creation, and lookup stay outside timers where the runtime API permits.
- Optional external runtime comparisons launch the foreign runtime once per benchmark process, run the workload inside the foreign timer, and verify the expected result before timing.
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
| `benchmarks/` | runtime-neutral kernels and optional external comparisons |

A benchmark should have a correctness test owned by the same public behavior or canonical fixture. Service-specific workloads do not define VM performance baselines.

## Running the suite

| Command | Purpose |
|---|---|
| `make benchmark-pr` | fast pull-request smoke suite |
| `make benchmark-core` | local canonical benchmark run |
| `make benchmark-nightly` | repeated scheduled suite |
| `make benchmark-compare` | optional external runtime comparison appendix |

For performance claims, keep benchmark processes sequential and record the host, Go version, command, and sample count. Use repeated measurements and `benchstat` before declaring a regression or improvement.

## Benchmark reading guide

**Start with “Performance at a glance.”** It shows minivm against the most relevant Go implementations and then summarizes its current runtime profile.

**Use the external reference appendix for broader context.** Those runtimes are supplementary comparisons, not the primary performance baseline.

**Use “Canonical kernel snapshot” for execution-tier behavior.** It shows where `default`, `threaded`, and `jit` differ.

**Use “Interpreter and API costs” for micro-optimization.** These rows identify the cost of individual runtime operations.

**Use “Methodology” before publishing numbers.** Absolute timings only make sense with their machine, command, sample count, and benchmark semantics.

## Related documentation

- `docs/profile.md` — runtime counters and profiling
- `docs/jit-internals.md` — ARM64 trace JIT behavior
- `docs/instruction-set.md` — opcode semantics and JIT support
- `docs/compatibility.md` — supported platforms
- GitHub issue **#184** — Fannkuch `default`/`jit` correctness failure
