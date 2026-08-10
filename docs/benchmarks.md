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

All current tables were re-measured on **August 10, 2026** on the same host:

- Apple M4 Pro, 12 cores
- `darwin/arm64`
- Go 1.26.2
- CPython 3.13.x where available

The package/API and runtime-neutral kernel suites use `-benchtime=300ms -count=3` and sequential runs. Values in this document are the median of the three samples. The `compare` suite uses the same host and command; it exits non-zero because the existing Fannkuch default/JIT correctness assertions fail, although its threaded result and the other comparison rows complete.

Lower `ns/op`, `B/op`, and `allocs/op` are better. Result extraction, reset, fixture construction, and verification remain outside the timed kernel where the benchmark defines them that way.

## Reproduction

```bash
# Full external comparison used by the complete matrix
cd benchmarks
go test -tags=compare -run='^$' -bench='.' -benchmem -benchtime=300ms -count=3 ./...

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

- The current canonical run was re-measured on **August 10, 2026** on Apple M4 Pro / `darwin/arm64`, using `-benchtime=300ms -count=3`.
- Small-struct pooling cuts `BinaryTrees(4,6)` from the previous **2.63 ms / 556,192 B / 8,888 allocs** to **~1.25 ms / 768 B / 8 allocs** in threaded mode: about **52% faster**, **99.86% fewer bytes**, and **99.91% fewer allocations**.
- `StructTreeWalk(9)` is now **~164 us / 768 B / 8 allocs** in threaded mode; its allocation footprint is down from the previous **65,712 B / 1,027 allocs** baseline.
- `AllocationGraph(128)` remains **~5.10 us / 1,024 B / 128 allocs** in threaded mode; its workload still exercises array/header allocation rather than struct pooling.
- The current threaded canonical results are now the authority for this branch. Adaptive `default` and threshold-zero `jit` results are reported separately because threshold-zero is not equivalent to a warmed native trace.
- The `-tags=compare` matrix completed all workloads but exits non-zero because `BenchmarkCall_Fannkuch/default` and `/jit` fail their existing correctness assertion. A control run from the parent of the struct-pooling commit reproduces the same Fannkuch failures, so this is not caused by the latest `Reset()` ordering change. The threaded Fannkuch row remains measurable.
- CPython comparison is now measured on the same `darwin/arm64` host. Threaded minivm is about **1.10x CPython on BinaryTrees**, **1.88x on StringBuild**, and **1.29x on Fannkuch**; it remains faster on SpectralNorm, Mandelbrot, MatMul, and SortStress.

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

| Workload | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| IterativeFib(30) | minivm/default | 39.33 | 0 | 0 |
| IterativeFib(30) | minivm/threaded | 523 | 0 | 0 |
| IterativeFib(30) | minivm/jit | 50.40 | 0 | 0 |
| IterativeFib(30) | native Go | 9.26 | 0 | 0 |
| IterativeFib(30) | wazero | 52.22 | 8 | 1 |
| IterativeFib(30) | Tengo | 9,317 | 90,592 | 61 |
| IterativeFib(30) | gopher-lua | 505.4 | 160 | 0 |
| IterativeFib(30) | Goja | 2,218 | 368 | 20 |
| IterativeFib(30) | gpython | 2,572 | 2,448 | 88 |
| IterativeFib(30) | Yaegi | 2,830 | 2,036 | 101 |
| Sieve(256) | minivm/default | 1,600 | 1,048 | 2 |
| Sieve(256) | minivm/threaded | 11,166 | 1,048 | 2 |
| Sieve(256) | minivm/jit | 1,638 | 1,048 | 2 |
| Sieve(256) | native Go | 235.9 | 0 | 0 |
| Sieve(256) | wazero | 682 | 8 | 1 |
| Sieve(256) | Tengo | 54,604 | 122,504 | 1,611 |
| Sieve(256) | gopher-lua | 23,209 | 18,416 | 44 |
| Sieve(256) | Goja | 43,413 | 1,872 | 25 |
| Sieve(256) | gpython | 35,333 | 5,704 | 30 |
| Sieve(256) | Yaegi | 18,551 | 1,800 | 37 |
| RecursiveFib(20) | minivm/default | 39,848 | 0 | 0 |
| RecursiveFib(20) | minivm/threaded | 326,457 | 0 | 0 |
| RecursiveFib(20) | minivm/jit | 397,492 | 0 | 0 |
| RecursiveFib(20) | native Go | 14,733 | 0 | 0 |
| RecursiveFib(20) | wazero | 34,053 | 8 | 1 |
| RecursiveFib(20) | Tengo | 881,570 | 319,345 | 28,655 |
| RecursiveFib(20) | gopher-lua | 1,100,545 | 704 | 2 |
| RecursiveFib(20) | Goja | 1,526,767 | 4,680 | 39 |
| RecursiveFib(20) | gpython | 3,988,691 | 9,807,927 | 109,494 |
| RecursiveFib(20) | Yaegi | 4,343,709 | 8,302,129 | 192,840 |
| RecursiveFib(35) | minivm/default | 49,835,633 | 0 | 0 |
| RecursiveFib(35) | minivm/threaded | 451,642,872 | 0 | 0 |
| RecursiveFib(35) | minivm/jit | 537,925,620 | 0 | 0 |
| RecursiveFib(35) | native Go | 19,964,486 | 0 | 0 |
| RecursiveFib(35) | wazero | 46,364,970 | 9 | 1 |
| RecursiveFib(35) | Tengo | 1,170,923,708 | 312,798,368 | 39,088,180 |
| RecursiveFib(35) | gopher-lua | 1,493,115,459 | 971,008 | 3,793 |
| RecursiveFib(35) | Goja | 2,196,104,875 | 375,360 | 46,373 |
| RecursiveFib(35) | gpython | 5,519,133,833 | 13,378,034,000 | 149,350,302 |
| RecursiveFib(35) | Yaegi | 5,779,152,708 | 11,324,344,136 | 263,043,710 |
| IndirectRecursiveFib(20) | minivm/default | 487,171 | 0 | 0 |
| IndirectRecursiveFib(20) | minivm/threaded | 594,075 | 0 | 0 |
| IndirectRecursiveFib(20) | minivm/jit | 709,284 | 0 | 0 |
| IndirectRecursiveFib(20) | native Go | 15,929 | 0 | 0 |
| IndirectRecursiveFib(20) | wazero | 42,839 | 8 | 1 |
| IndirectRecursiveFib(20) | Tengo | 944,911 | 319,346 | 28,655 |
| IndirectRecursiveFib(20) | gopher-lua | 944,551 | 704 | 2 |
| IndirectRecursiveFib(20) | Goja | 1,354,928 | 4,680 | 39 |
| IndirectRecursiveFib(20) | gpython | 4,031,035 | 10,158,213 | 109,494 |
| IndirectRecursiveFib(20) | Yaegi | 11,226,460 | 13,059,875 | 394,041 |
| Fannkuch(6) | minivm/default | **FAIL** | — | — |
| Fannkuch(6) | minivm/threaded | 551,796 | 34,608 | 1,442 |
| Fannkuch(6) | minivm/jit | **FAIL** | — | — |
| Fannkuch(6) | native Go | 17,784 | 17,280 | 720 |
| Fannkuch(6) | gpython | 1,511,250 | 1,367,678 | 16,944 |
| Fannkuch(6) | CPython 3.13 | 429,294 | 9 | 0 |
| ClosureCounter(128) | minivm/default | 3,423 | 64 | 2 |
| ClosureCounter(128) | minivm/threaded | 2,663 | 64 | 2 |
| ClosureCounter(128) | minivm/jit | 3,424 | 64 | 2 |
| ClosureCounter(128) | native Go | 38.20 | 0 | 0 |
| ClosureCounter(128) | Tengo | 12,919 | 92,272 | 261 |
| ClosureCounter(128) | gopher-lua | 5,846 | 151 | 3 |
| ClosureCounter(128) | Goja | 10,069 | 1,264 | 13 |
| ClosureCounter(128) | gpython | 27,748 | 58,312 | 659 |
| ClosureCounter(128) | Yaegi | 33,509 | 34,784 | 786 |
| TypedArraySum(256) | minivm/default | 304.8 | 0 | 0 |
| TypedArraySum(256) | minivm/threaded | 2,949 | 0 | 0 |
| TypedArraySum(256) | minivm/jit | 503 | 0 | 0 |
| TypedArraySum(256) | native Go | 72.56 | 0 | 0 |
| TypedArraySum(256) | wazero | 155.6 | 8 | 1 |
| TypedArraySum(256) | Tengo | 15,254 | 94,208 | 513 |
| TypedArraySum(256) | gopher-lua | 3,395 | 4,000 | 15 |
| TypedArraySum(256) | Goja | 13,107 | 2,080 | 238 |
| TypedArraySum(256) | gpython | 7,458 | 2,496 | 246 |
| TypedArraySum(256) | Yaegi | 3,954 | 296 | 8 |
| AllocationGraph(128) | minivm/default | 6,955 | 1,024 | 128 |
| AllocationGraph(128) | minivm/threaded | 5,098 | 1,024 | 128 |
| AllocationGraph(128) | minivm/jit | 6,949 | 1,024 | 128 |
| AllocationGraph(128) | native Go | 942.6 | 1,024 | 128 |
| AllocationGraph(128) | Tengo | 13,347 | 96,288 | 388 |
| AllocationGraph(128) | gopher-lua | 6,370 | 14,376 | 256 |
| AllocationGraph(128) | Goja | 25,759 | 78,016 | 770 |
| AllocationGraph(128) | gpython | 5,686 | 5,712 | 266 |
| AllocationGraph(128) | Yaegi | 12,195 | 1,492 | 142 |
| BranchTree(96) | minivm/default | 270.4 | 0 | 0 |
| BranchTree(96) | minivm/threaded | 674.8 | 0 | 0 |
| BranchTree(96) | minivm/jit | 262.7 | 0 | 0 |
| BranchTree(96) | native Go | 79.50 | 0 | 0 |
| BranchTree(96) | wazero | 172.5 | 16 | 1 |
| BranchTree(96) | Tengo | 17,027 | 95,384 | 660 |
| BranchTree(96) | gopher-lua | 8,872 | 2,464 | 9 |
| BranchTree(96) | Goja | 13,976 | 1,992 | 196 |
| BranchTree(96) | gpython | 12,311 | 2,168 | 203 |
| BranchTree(96) | Yaegi | 11,207 | 1,832 | 308 |

Wazero has no equivalent canonical fixture for `ClosureCounter(128)` or `AllocationGraph(128)`, so those rows are `N/A`.

### CPython Comparison

Ten kernels ported from `siyul-park/minipy`'s benchmark corpus answer a
narrower question than the table above: how minivm's threaded interpreter
compares with CPython's, on the same algorithm. minipy compiles Python to
minivm bytecode, so its own published ratios conflate its code generation with
minivm's execution cost; hand-written kernels isolate the second.

**Measured on the same `darwin/arm64` host as the canonical suite.** The threaded mode is reported because it is the closest direct comparison to CPython's interpreter execution; `default` and `jit` may install native traces. Median of three samples at `-benchtime=300ms`; `B/op` and `allocs/op` are minivm's.

| Workload | minivm/threaded ns/op | CPython 3.13 ns/op | ratio | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|
| SpectralNorm(24) | 287,442 | 424,866 | 0.68 | 648 | 6 |
| Mandelbrot(16x16) | 151,451 | 182,652 | 0.83 | 0 | 0 |
| MatMul(16) | 190,776 | 211,240 | 0.90 | 6,216 | 6 |
| SortStress(128) | 356,580 | 336,827 | 1.06 | 5,136 | 512 |
| NBody(100) | 338,833 | 233,030 | 1.45 | 504 | 14 |
| Fannkuch(6) | 551,796 | 429,294 | 1.29 | 34,608 | 1,442 |
| NQueens(7) | 339,113 | 223,114 | 1.52 | 120 | 6 |
| BinaryTrees(4,6) | 1,249,671 | 1,131,697 | 1.10 | 768 | 8 |
| StringBuild(512) | 697,653 | 370,399 | 1.88 | 1,724,176 | 5,118 |

Ratios below 1.00 mean minivm is faster. Reading them:

- **Scalar work remains competitive.** Spectral norm, Mandelbrot, and matrix multiply are 0.68-0.90x CPython; SortStress is now 1.06x on the same host.
- **Struct pooling materially narrows BinaryTrees.** The threaded result is 1.10x CPython with 768 B/op and 8 allocs/op, down from the previous 1.35x / 556,192 B/op / 8,888 allocs/op.
- **StringBuild remains the widest current gap** at 1.88x CPython, with 1.72 MB/op and 5,118 allocs/op.
- **NBody and NQueens are currently slower than CPython** on this host at 1.45x and 1.52x respectively; these rows should not be compared with the previous Linux-host ratios.

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
| Construction | `empty program` | 2,976 | 35,338 | 29 |
| Construction | `program, JIT disabled` | 3,072 | 35,416 | 32 |
| Construction | `program, JIT enabled` | 2,786 | 35,416 | 32 |
| Reset | `scalar state` | 24.73 | 0 | 0 |
| Reset | `heap state` | 34.25 | 8 | 1 |
| Reset | `installed JIT state` | 37.85 | 0 | 0 |
| Stack | `Push scalar` | 16.06 | 0 | 0 |
| Stack | `Push reference` | 72.97 | 16 | 1 |
| Stack | `Pop` | 6.79 | 0 | 0 |
| Stack | `PopBoxed` | 6.65 | 0 | 0 |
| Stack | `Peek` | 1.79 | 0 | 0 |
| Heap | `Alloc` | 37.54 | 16 | 1 |
| Heap | `Retain` | 19.23 | 0 | 0 |
| Heap | `Release` | 17.38 | 0 | 0 |
| Pool | `Get, uncontended` | 30.51 | 0 | 0 |
| Pool | `Get, miss` | 2,275 | 35,192 | 26 |
| Pool | `Get, shared-JIT miss` | 11,904 | 44,808 | 285 |
| Pool | `parallel round trip` | 259.5 | 0 | 0 |
| Pool | `Put, uncontended` | 124.2 | 0 | 0 |

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
| nop | `i32.const_nop_returns_i32` | 18.17 | 0 | 0 |
| constant | `i32.const_returns_i32` | 17.15 | 0 | 0 |
| i32 arithmetic | `i32.add` | 20.80 | 0 | 0 |
| i64 arithmetic | `i64.add` | 21.92 | 0 | 0 |
| f32 arithmetic | `f32.add` | 20.96 | 0 | 0 |
| f64 arithmetic | `f64.add` | 20.93 | 0 | 0 |
| conversion | `i32.to_i64_s` | 19.73 | 0 | 0 |
| branch | `br` | 17.97 | 0 | 0 |
| branch | `br_if` | 20.45 | 0 | 0 |
| branch | `br_table` | 20.28 | 0 | 0 |
| call | `direct bytecode call` | 31.30 | 0 | 0 |
| array | `constant array get` | 21.08 | 0 | 0 |
| struct | `new` | 20.50 | 0 | 0 |
| struct | `set + get` | 60.13 | 0 | 0 |

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
| array | no child refs | 2.87 | 0 | 0 |
| array | child refs | 2.52 | 0 | 0 |
| struct | no child refs | 1.77 | 0 | 0 |
| struct | child refs | 1.94 | 0 | 0 |
| typed map | child refs | 24.38 | 0 | 0 |
| dynamic map | no child refs | 1.81 | 0 | 0 |
| dynamic map | child refs | 26.58 | 0 | 0 |

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
- Output was grouped by exact benchmark name. Current rows use the median of three sequential samples.

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
