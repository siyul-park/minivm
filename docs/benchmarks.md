# Benchmarks

Current performance results, execution characteristics, and measurement methodology for minivm.

## When to Read

Use this document when making or reviewing performance claims, changing benchmark workloads, changing runtime thresholds, or comparing minivm with other runtimes.

For implementation details, see `docs/jit-internals.md`. For profiling counters, see `docs/profile.md`.

## Source of Truth

| Concern | File or command |
|---|---|
| package and public API benchmarks | `interp/*_test.go`, `types/*_test.go` |
| runtime-neutral VM kernels | `benchmarks/` module |
| full canonical suite | `make benchmark-core` |
| pull-request smoke suite | `make benchmark-pr` |
| external runtime comparison | `make benchmark-compare` |
| profiling counters | `docs/profile.md` |

## Measurement Environment

Current tables were re-measured on **August 10, 2026** on the same host:

- Apple M4 Pro, 12 cores
- `darwin/arm64`
- Go 1.26.2
- CPython 3.13.x where available

Focused interpreter/API, reference, and CPython-comparison tables use `-benchtime=300ms -count=3` and report the median of three sequential samples. The complete runtime-neutral kernel snapshot uses `-benchtime=100ms -count=1`. The compare run exits non-zero because the existing Fannkuch default/JIT correctness assertions fail; its threaded result and the other selected comparison rows complete.

Lower `ns/op`, `B/op`, and `allocs/op` are better. Result extraction, reset, fixture construction, and verification remain outside the timed kernel where the benchmark defines them that way.

## Reproduction

```bash
# Full external comparison used by the complete matrix
cd benchmarks
go test -tags=compare -run='^$' -bench='^(BenchmarkNumeric_(NBody|SpectralNorm|Mandelbrot|MatMul)|BenchmarkCall_(NQueens|Fannkuch)|BenchmarkMemory_(BinaryTrees|SortStress|StringBuild))$' -benchmem -benchtime=300ms -count=3 ./...

# Issue #164 A/B: alternate this command between commit 10d6adf and the
# current checkout five times, then take each side's median.
go test -run='^$' \
  -bench='^(BenchmarkControl_IterativeFib|BenchmarkControl_Sieve|BenchmarkCall_RecursiveFib|BenchmarkCall_IndirectRecursiveFib|BenchmarkCall_ClosureCounter|BenchmarkMemory_TypedArraySum|BenchmarkMemory_AllocationGraph|BenchmarkMemory_PermutationFlips|BenchmarkMemory_StructTreeWalk|BenchmarkNumeric_BranchTree)$' \
  -benchmem -benchtime=300ms -count=1 .

# Public interpreter and pool API costs
go test -run='^$' \
  -bench='^(BenchmarkNew|BenchmarkInterpreter_(Reset|Push|Pop|PopBoxed|Peek|Alloc|Retain|Release)|BenchmarkPool_(Get|Put))$' \
  -benchmem -benchtime=300ms -count=3 ./interp

# Exact threaded execution samples
go test -run='^$' -bench='^BenchmarkInterpreter_Run/.*/Threaded$' \
  -benchmem -benchtime=300ms -count=3 ./interp

# Cold unconditional-backedge overhead
go test -run='^$' -bench='^BenchmarkInterpreter_Run/ColdBackedge$' \
  -benchmem -benchtime=300ms -count=3 ./interp

# Reference traversal
go test -run='^$' -bench='^Benchmark(Array|Struct|TypedMap|Map)_Refs$' \
  -benchmem -benchtime=300ms -count=3 ./types
```

## Summary

- The current runtime-neutral kernel snapshot was re-measured on **August 10, 2026** on Apple M4 Pro / `darwin/arm64` with `-benchtime=100ms -count=1`.
- `BinaryTrees(4,6)` is **1.225 ms / 768 B / 8 allocs** in threaded mode, versus the previous **2.63 ms / 556,192 B / 8,888 allocs**: about **53% faster**, **99.86% fewer bytes**, and **99.91% fewer allocations**.
- `StructTreeWalk(9)` is **158.5 us / 768 B / 8 allocs** in threaded mode, down from the previous **65,712 B / 1,027 allocs** footprint.
- `AllocationGraph(128)` remains allocation-heavy at **5.06 us / 1,024 B / 128 allocs** in threaded mode because it intentionally exercises fresh array/header allocation.
- The focused CPython comparison uses three samples on the same host. BinaryTrees is currently **1.20x CPython**, Fannkuch **1.25x**, SortStress **0.95x**, StringBuild **1.97x**, NBody **1.34x**, and NQueens **1.42x**.
- Fannkuch `default` and `jit` still fail their existing correctness assertion; the threaded row remains measurable.

These results are workload measurements, not general language rankings. The runtimes use different value models, safety boundaries, host-call conventions, and compilation strategies.

## Cross-Runtime Comparison

### minivm Modes

| Mode | Construction | Meaning |
|---|---|---|
| `default` | `interp.New(prog)` | adaptive policy; hot ARM64 entries and loop roots may become native |
| `threaded` | `interp.New(prog, interp.WithThreshold(-1))` | JIT disabled; generated threaded execution only |
| `jit` | `interp.New(prog, interp.WithThreshold(0))` | eager profiling and compilation policy; native entry is not guaranteed |

Function and module entries in threshold-zero mode become eligible on the first sample. Loop roots reached through unconditional backward branches wait for eight exact hits to avoid specializing on the first iteration.

Each minivm kernel times `Interpreter.Run` only. Result extraction, reset, fixture construction, verification, and warmup remain outside the measured duration. External runtimes time repeated invocation of an already prepared program or function where their APIs permit; result materialization and conversion boundaries still differ, so small ratios are not precise VM-core comparisons.

### Complete Results

The current kernel sweep was re-measured on the same host with `-benchtime=100ms -count=1`. It is a current snapshot rather than a statistical regression gate; focused comparisons below use three samples. Fannkuch `default` and `jit` fail their existing correctness assertion and are therefore omitted from the measured rows.

| Workload | Mode | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Call_RecursiveFib/20 | default | 39723 | 0 | 0 |
| Call_RecursiveFib/20 | threaded | 325991 | 0 | 0 |
| Call_RecursiveFib/20 | jit | 390390 | 0 | 0 |
| Call_RecursiveFib/35 | default | 49649192 | 0 | 0 |
| Call_RecursiveFib/35 | threaded | 424763741 | 0 | 0 |
| Call_RecursiveFib/35 | jit | 532586074 | 0 | 0 |
| Call_IndirectRecursiveFib | default | 491271 | 0 | 0 |
| Call_IndirectRecursiveFib | threaded | 577162 | 0 | 0 |
| Call_IndirectRecursiveFib | jit | 692954 | 0 | 0 |
| Call_ClosureCounter | default | 3259 | 64 | 2 |
| Call_ClosureCounter | threaded | 2510 | 64 | 2 |
| Call_ClosureCounter | jit | 3240 | 64 | 2 |
| Call_NQueens | default | 153706 | 120 | 6 |
| Call_NQueens | threaded | 320605 | 120 | 6 |
| Call_NQueens | jit | 153757 | 120 | 6 |
| Call_Fannkuch | threaded | 535461 | 34608 | 1442 |
| Control_IterativeFib | default | 27.57 | 0 | 0 |
| Control_IterativeFib | threaded | 508.3 | 0 | 0 |
| Control_IterativeFib | jit | 44.60 | 0 | 0 |
| Control_Sieve | default | 1543 | 1048 | 2 |
| Control_Sieve | threaded | 11108 | 1048 | 2 |
| Control_Sieve | jit | 1541 | 1048 | 2 |
| Memory_TypedArraySum | default | 293.7 | 0 | 0 |
| Memory_TypedArraySum | threaded | 3593 | 0 | 0 |
| Memory_TypedArraySum | jit | 496.7 | 0 | 0 |
| Memory_AllocationGraph | default | 6850 | 1024 | 128 |
| Memory_AllocationGraph | threaded | 5062 | 1024 | 128 |
| Memory_AllocationGraph | jit | 6862 | 1024 | 128 |
| Memory_PermutationFlips | default | 123939 | 14336 | 128 |
| Memory_PermutationFlips | threaded | 77681 | 14336 | 128 |
| Memory_PermutationFlips | jit | 113346 | 14336 | 128 |
| Memory_StructTreeWalk | default | 177712 | 768 | 8 |
| Memory_StructTreeWalk | threaded | 158541 | 768 | 8 |
| Memory_StructTreeWalk | jit | 177437 | 768 | 8 |
| Memory_BinaryTrees | default | 1340006 | 768 | 8 |
| Memory_BinaryTrees | threaded | 1225020 | 768 | 8 |
| Memory_BinaryTrees | jit | 1335787 | 768 | 8 |
| Memory_SortStress | default | 73550 | 5136 | 512 |
| Memory_SortStress | threaded | 319602 | 5136 | 512 |
| Memory_SortStress | jit | 73064 | 5136 | 512 |
| Memory_StringBuild | default | 627992 | 1724160 | 5118 |
| Memory_StringBuild | threaded | 615925 | 1724160 | 5118 |
| Memory_StringBuild | jit | 536041 | 1724160 | 5118 |
| Numeric_BranchTree | default | 252.9 | 0 | 0 |
| Numeric_BranchTree | threaded | 587.8 | 0 | 0 |
| Numeric_BranchTree | jit | 252.9 | 0 | 0 |
| Numeric_NBody | default | 99203 | 504 | 14 |
| Numeric_NBody | threaded | 313339 | 504 | 14 |
| Numeric_NBody | jit | 99615 | 504 | 14 |
| Numeric_SpectralNorm | default | 41898 | 648 | 6 |
| Numeric_SpectralNorm | threaded | 277639 | 648 | 6 |
| Numeric_SpectralNorm | jit | 42331 | 648 | 6 |
| Numeric_Mandelbrot | default | 49233 | 0 | 0 |
| Numeric_Mandelbrot | threaded | 139513 | 0 | 0 |
| Numeric_Mandelbrot | jit | 49551 | 0 | 0 |
| Numeric_MatMul | default | 38191 | 6216 | 6 |
| Numeric_MatMul | threaded | 174987 | 6216 | 6 |
| Numeric_MatMul | jit | 37966 | 6216 | 6 |

### CPython Comparison

Ten kernels ported from `siyul-park/minipy`'s benchmark corpus answer a
narrower question than the table above: how minivm's threaded interpreter
compares with CPython's, on the same algorithm. minipy compiles Python to
minivm bytecode, so its own published ratios conflate its code generation with
minivm's execution cost; hand-written kernels isolate the second.

**Measured on the same `darwin/arm64` host as the canonical suite.** The threaded mode is reported because it is the closest direct comparison to CPython's interpreter execution; `default` and `jit` may install native traces. Median of three samples at `-benchtime=300ms`; `B/op` and `allocs/op` are minivm's.

| Workload | minivm/threaded ns/op | CPython 3.13 ns/op | ratio | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|
| SpectralNorm(24) | 284,793 | 424,750 | 0.67 | 648 | 6 |
| Mandelbrot(16x16) | 146,340 | 191,414 | 0.77 | 0 | 0 |
| MatMul(16) | 175,880 | 209,459 | 0.84 | 6,216 | 6 |
| SortStress(128) | 331,264 | 349,302 | 0.95 | 5,136 | 512 |
| NBody(100) | 319,231 | 238,152 | 1.34 | 504 | 14 |
| Fannkuch(6) | 546,448 | 438,148 | 1.25 | 34,608 | 1,442 |
| NQueens(7) | 318,854 | 224,831 | 1.42 | 120 | 6 |
| BinaryTrees(4,6) | 1,214,215 | 1,018,784 | 1.19 | 768 | 8 |
| StringBuild(512) | 741,999 | 376,976 | 1.97 | 1,724,160 | 5,118 |

Ratios below 1.00 mean minivm is faster. Reading them:

- **Scalar work remains competitive.** Spectral norm, Mandelbrot, and matrix multiply are 0.67-0.84x CPython; SortStress is now 0.95x on the same host.
- **Struct pooling materially narrows BinaryTrees.** The threaded result is 1.19x CPython with 768 B/op and 8 allocs/op, down from the previous 1.35x / 556,192 B/op / 8,888 allocs/op.
- **StringBuild remains the widest current gap** at 1.97x CPython, with 1.72 MB/op and 5,118 allocs/op.
- **NBody and NQueens are currently slower than CPython** on this host at 1.34x and 1.42x respectively.

CPython runs as a subprocess, since it is not a Go library. The driver spawns
`python3.13` once, runs the workload `b.N` times inside a `time.perf_counter()`
window, and reports that elapsed time, so the interpreter's own startup is
excluded rather than amortized. It verifies the checksum before timing, runs
with `PYTHONHASHSEED=0`, and the row is skipped when `python3.13` is absent.

```bash
cd benchmarks
go test -tags=compare -run='^$' \
  -bench='^(BenchmarkNumeric_(NBody|SpectralNorm|Mandelbrot|MatMul)|BenchmarkCall_(NQueens|Fannkuch)|BenchmarkMemory_(BinaryTrees|SortStress|StringBuild))$' \
  -benchmem -benchtime=300ms -count=3 ./...
```

## Public API Costs

These benchmarks measure the named public operation. Setup, validation, cleanup, and paired operations stay outside the manually reported `ns/op` interval; allocation metrics still describe the complete benchmark iteration.

| Area | Operation | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Construction | `empty program` | 3,409 | 35,338 | 29 |
| Construction | `program, JIT disabled` | 3,430 | 35,416 | 32 |
| Construction | `program, JIT enabled` | 3,445 | 35,416 | 32 |
| Reset | `scalar state` | 28.30 | 0 | 0 |
| Reset | `heap state` | 38.50 | 8 | 1 |
| Reset | `installed JIT state` | 37.04 | 0 | 0 |
| Stack | `Push scalar` | 20.23 | 0 | 0 |
| Stack | `Push reference` | 43.74 | 16 | 1 |
| Stack | `Pop` | 8.75 | 0 | 0 |
| Stack | `PopBoxed` | 7.74 | 0 | 0 |
| Stack | `Peek` | 1.83 | 0 | 0 |
| Heap | `Alloc` | 37.57 | 16 | 1 |
| Heap | `Retain` | 19.86 | 0 | 0 |
| Heap | `Release` | 18.31 | 0 | 0 |
| Pool | `Get, uncontended` | 33.96 | 0 | 0 |
| Pool | `Get, miss` | 3,251 | 35,192 | 26 |
| Pool | `Get, shared-JIT miss` | 11,742 | 44,808 | 285 |
| Pool | `parallel round trip` | 243.9 | 0 | 0 |
| Pool | `Put, uncontended` | 116.5 | 0 | 0 |

Scalar stack access, retain/release, and uncontended pool reuse are allocation-free. Heap reset currently reports a small 8 B / 1 alloc overhead. Construction and pool misses are dominated by interpreter state allocation. `SharedJITMiss` includes a new pooled interpreter synchronizing against shared JIT state.

## JIT Activation Overhead

`BenchmarkInterpreter_Run/ColdBackedge` runs a 256-iteration counting loop with `WithTick(1<<20)` and `WithThreshold(1<<30)`, measuring `Run` only; result extraction and reset remain outside the reported duration. The function remains below the sample threshold, so it keeps the ordinary generated `BR` handler.

| Case | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| cold unconditional backedge | 2,039 | 0 | 0 |

This benchmark guards the cold-path boundary: exact backedge observation is installed only after periodic sampling marks a function hot, rather than adding a callback or header scan to every cold loop iteration.

## Threaded Execution Samples

The following rows use `BenchmarkInterpreter_Run/.../Threaded`: `WithTick(1)` and `WithThreshold(-1)`. Each result times one complete `Run`; setup opcodes are intentionally included, while `Reset` and result validation stay outside the measured interval.

| Area | Case | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| nop | `i32.const_nop_returns_i32` | 17.55 | 0 | 0 |
| constant | `i32.const_returns_i32` | 17.55 | 0 | 0 |
| i32 arithmetic | `i32.add` | 19.86 | 0 | 0 |
| i64 arithmetic | `i64.add` | 19.86 | 0 | 0 |
| f32 arithmetic | `f32.add` | 19.86 | 0 | 0 |
| f64 arithmetic | `f64.add` | 19.86 | 0 | 0 |
| conversion | `i32.to_i64_s` | 19.86 | 0 | 0 |
| branch | `br` | 18.15 | 0 | 0 |
| branch | `br_if` | 20.54 | 0 | 0 |
| branch | `br_table` | 20.37 | 0 | 0 |
| call | `direct bytecode call` | 30.97 | 0 | 0 |

These are complete program cases, not isolated opcode latency. Allocation-bearing array, struct, and map rows include object construction in the same run.

### `BenchmarkInterpreter_Run` Modes

| Mode | Options | Timed state |
|---|---|---|
| `Threaded` | `WithTick(1)`, `WithThreshold(-1)` | exact generated threaded dispatch; JIT and fusion disabled |
| `Fused` | `WithThreshold(-1)` | default generated fusion with JIT disabled |
| `JITWarm` | `WithTick(1)`, `WithThreshold(0)` | warmup runs until a native entry is installed, then `Run` only |

The representative table above shows `Threaded` cases. The complete package suite also emits applicable `Fused` and `JITWarm` sub-benchmarks; unsupported JIT entries are skipped rather than reported as native results.

## Reference Traversal

`types.Traceable.Refs` implementations reuse caller-provided storage. The current canonical cases are allocation-free.

| Value | Case | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| array | no child refs | 2.69 | 0 | 0 |
| array | child refs | 2.38 | 0 | 0 |
| struct | no child refs | 1.81 | 0 | 0 |
| struct | child refs | 1.92 | 0 | 0 |
| typed map | child refs | 24.47 | 0 | 0 |
| dynamic map | no child refs | 1.84 | 0 | 0 |
| dynamic map | child refs | 26.70 | 0 | 0 |

## ARM64 JIT Interpretation

The adaptive default tier is strongest on stable numeric loops, direct recursive calls, typed-array reads, primitive typed-array writes, and branch-heavy scalar code. Unsupported allocation, complex ref-bearing mutation, host calls, and heap-promoted `i64` paths deoptimize or remain threaded.

Do not infer native execution solely from the `jit` sub-benchmark name. A benchmark that claims a warmed native entry must prove it through a native stub or profiler metrics. On architectures without a native backend, all modes remain threaded.

## Methodology

- Every canonical kernel has a correctness test with a fixed expected result or checksum.
- The cross-runtime suite performs one correctness run, four untimed warmup runs, and 32 untimed allocation samples before timing minivm.
- minivm `default`, `threaded`, and `jit` differ only in their threshold option.
- External parsing, compilation, module creation, and function lookup remain outside the timer where the runtime API permits.
- Wazero uses its default compiler runtime; module compilation and instantiation are excluded from timing.
- Cross-runtime comparisons live in the separate `benchmarks/` module and require the `compare` build tag.
- Output was grouped by exact benchmark name. Focused rows use the median of three sequential samples; the complete kernel snapshot is a single 100ms sample per benchmark.

Cross-runtime library versions:

- wazero v1.12.0
- gopher-lua v1.1.2
- Tengo v2.17.0
- Goja v0.0.0-20260311135729-065cd970411c
- gpython v0.2.0
- Yaegi v0.16.1

## Benchmark Ownership

| Owner | Measures |
|---|---|
| `interp/interp_test.go` | interpreter construction, execution tiers, reset, stack/heap operations, pool behavior, JIT lifecycle |
| `types/*_test.go` | reference traversal contracts |
| `benchmarks/` | runtime-neutral VM kernels and optional external comparisons |

A benchmark must have a correctness test owned by the same public behavior or canonical fixture. Service-domain workloads do not define VM performance baselines.

## Execution Tiers

| Target | Use | Contents |
|---|---|---|
| `make benchmark-pr` | pull requests | stable construction, reset, dispatch, representative interpreter cases, and threaded kernels |
| `make benchmark-core` | local canonical run | all package benchmarks and all runtime-neutral VM kernels |
| `make benchmark-nightly` | scheduled report | repeated canonical suite, including JIT lifecycle and parallel pool cases |
| `make benchmark-compare` | optional analysis | external runtime comparisons enabled by the `compare` build tag |

Pull-request and nightly targets report raw results without comparing against golden numbers. Use repeated output with `benchstat` for statistical comparison before making regression claims.

## Maintenance Notes

- Run benchmark processes sequentially when publishing absolute numbers.
- Keep claims tied to concrete rows and record the platform, Go version, command, and sample count.
- Remove a metric when its benchmark no longer exists; do not preserve historical numbers as current results.
- Update the README headline only after this document is updated.

## Related Docs

- `docs/profile.md` — sampling and runtime counters
- `docs/jit-internals.md` — trace JIT behavior
- `docs/instruction-set.md` — opcode semantics and JIT support
- `docs/compatibility.md` — platform support
