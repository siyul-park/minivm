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

The cross-runtime comparison table was measured on July 30, 2026. The public
API cost tables below it were measured on July 16, 2026:

- Apple M4 Pro, 12 cores
- `darwin/arm64`
- macOS 26.4.1
- Go 1.26.2

Every table reports the median of three sequential samples. Lower `ns/op`, `B/op`, and `allocs/op` are better. The runs were executed serially so concurrent benchmark processes did not compete for CPU time. The comparison numbers come from one `-benchtime=300ms` pass over the whole suite, so a short-warmup adaptive mode reads slightly slower there than in a focused single-kernel run.

## Reproduction

```bash
# Full external comparison used by the complete matrix
cd benchmarks
go test -tags=compare -run='^$' -bench='.' -benchmem -benchtime=300ms -count=3 ./...

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

- `RecursiveFib(35)` places `minivm/default` at **46.18 ms**, within about **3.9%** of wazero's **44.45 ms**, while remaining allocation-free after warmup.
- Adaptive native traces reduce `IterativeFib(30)` from **746.3 ns** threaded to **92.15 ns**, `TypedArraySum(256)` from **6.313 us** to **683.2 ns**, and `BranchTree(96)` from **952.0 ns** to **267.1 ns**.
- Fused threaded handlers check stack room once for their own net push instead of once per folded source. That removes about 5,400 generated lines and cuts threaded time on every fusion-heavy kernel: `BranchTree(96)` **-4.3%**, `TypedArraySum(256)` **-4.1%**, `IterativeFib(30)` **-3.6%**, and `Sieve(256)` **-2.8%** (interleaved A/B, median of five). Native and adaptive modes are unchanged within noise.
- Primitive array mutation stays on the native loop path in `Sieve(256)`: deferred-ownership elision drops the per-element retain/release pair, so a runtime-allocated array reaches the same cheap native path a typed-array constant already used. All three modes allocate `1,048 B` in `2` allocations.
- Loop-invariant container hoisting (issue #153) removes the per-access heap-cell derivation, itab guard, and slice-header reload from hoisted loop bodies. It shrinks the loop callables but leaves wall time unchanged: the removed loads sat off the out-of-order critical path.
- Branch-leg folding (issue #155) records native loop exits as branches and folds hot legs that rejoin the header back into the native loop as real back-edges. On `Sieve(256)` this removes the per-prime entry/exit round trips (scan-loop native entries drop from ~55 to ~1 per run) and cuts `default` from **4.72 us** to **2.68 us**, versus **16.2 us** threaded. The remaining gap to wazero (**687.3 ns**) is dominated by the per-iteration operand flush.
- Threshold-zero `jit` is not a warmed-JIT guarantee. It matches `default` on Sieve and BranchTree, but is slower on IterativeFib, TypedArraySum, and recursive Fibonacci because it can compile before representative traces are learned.
- Allocation-heavy workloads remain interpreter-bound. `AllocationGraph(128)` is fastest in minivm's threaded mode at **7.774 us**; adaptive and eager modes add profiling cost without native coverage.
- Indirect recursion reaches the native self-call path in adaptive mode: `IndirectRecursiveFib(20)` is **54.85 us** in `default`, versus **576 us** threaded and **43.3 us** in wazero. Eager `jit` stays at **594 us**, consistent with the threshold-zero note above.

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
| IterativeFib(30) | minivm/default | 92.15 | 0 | 0 |
| IterativeFib(30) | minivm/threaded | 746.3 | 0 | 0 |
| IterativeFib(30) | minivm/jit | 95.07 | 0 | 0 |
| IterativeFib(30) | native Go | 9.256 | 0 | 0 |
| IterativeFib(30) | wazero | 52.63 | 8 | 1 |
| IterativeFib(30) | Tengo | 9,406 | 90,592 | 61 |
| IterativeFib(30) | gopher-lua | 513.9 | 160 | 0 |
| IterativeFib(30) | Goja | 2,226 | 368 | 20 |
| IterativeFib(30) | gpython | 2,599 | 2,448 | 88 |
| IterativeFib(30) | Yaegi | 2,856 | 2,036 | 101 |
| Sieve(256) | minivm/default | 2,682 | 1,048 | 2 |
| Sieve(256) | minivm/threaded | 16,239 | 1,048 | 2 |
| Sieve(256) | minivm/jit | 2,696 | 1,048 | 2 |
| Sieve(256) | native Go | 237.2 | 0 | 0 |
| Sieve(256) | wazero | 687.3 | 8 | 1 |
| Sieve(256) | Tengo | 53,630 | 122,504 | 1,611 |
| Sieve(256) | gopher-lua | 23,289 | 18,416 | 44 |
| Sieve(256) | Goja | 43,140 | 1,872 | 25 |
| Sieve(256) | gpython | 35,636 | 5,704 | 30 |
| Sieve(256) | Yaegi | 18,762 | 1,800 | 37 |
| RecursiveFib(20) | minivm/default | 38,266 | 0 | 0 |
| RecursiveFib(20) | minivm/threaded | 357,450 | 0 | 0 |
| RecursiveFib(20) | minivm/jit | 368,423 | 0 | 0 |
| RecursiveFib(20) | native Go | 14,455 | 0 | 0 |
| RecursiveFib(20) | wazero | 31,077 | 8 | 1 |
| RecursiveFib(20) | Tengo | 809,410 | 319,346 | 28,655 |
| RecursiveFib(20) | gopher-lua | 1,055,661 | 704 | 2 |
| RecursiveFib(20) | Goja | 1,456,275 | 4,680 | 39 |
| RecursiveFib(20) | gpython | 3,701,017 | 9,807,924 | 109,494 |
| RecursiveFib(20) | Yaegi | 3,791,815 | 8,302,126 | 192,840 |
| RecursiveFib(35) | minivm/default | 46,175,150 | 0 | 0 |
| RecursiveFib(35) | minivm/threaded | 499,079,076 | 0 | 0 |
| RecursiveFib(35) | minivm/jit | 522,672,192 | 0 | 0 |
| RecursiveFib(35) | native Go | 20,107,461 | 0 | 0 |
| RecursiveFib(35) | wazero | 44,453,238 | 9 | 1 |
| RecursiveFib(35) | Tengo | 1,168,612,000 | 312,797,584 | 39,088,176 |
| RecursiveFib(35) | gopher-lua | 1,477,085,041 | 971,008 | 3,793 |
| RecursiveFib(35) | Goja | 2,099,479,958 | 375,360 | 46,373 |
| RecursiveFib(35) | gpython | 5,578,092,292 | 13,378,028,656 | 149,350,236 |
| RecursiveFib(35) | Yaegi | 5,903,047,625 | 11,324,344,728 | 263,043,676 |
| IndirectRecursiveFib(20) | minivm/default | 54,848 | 0 | 0 |
| IndirectRecursiveFib(20) | minivm/threaded | 576,182 | 0 | 0 |
| IndirectRecursiveFib(20) | minivm/jit | 594,117 | 0 | 0 |
| IndirectRecursiveFib(20) | native Go | 15,981 | 0 | 0 |
| IndirectRecursiveFib(20) | wazero | 43,260 | 8 | 1 |
| IndirectRecursiveFib(20) | Tengo | 953,537 | 319,346 | 28,655 |
| IndirectRecursiveFib(20) | gopher-lua | 954,423 | 704 | 2 |
| IndirectRecursiveFib(20) | Goja | 1,377,150 | 4,680 | 39 |
| IndirectRecursiveFib(20) | gpython | 4,018,147 | 10,158,201 | 109,494 |
| IndirectRecursiveFib(20) | Yaegi | 11,430,601 | 13,059,854 | 394,041 |
| ClosureCounter(128) | minivm/default | 3,362 | 64 | 2 |
| ClosureCounter(128) | minivm/threaded | 2,978 | 64 | 2 |
| ClosureCounter(128) | minivm/jit | 3,372 | 64 | 2 |
| ClosureCounter(128) | native Go | 38.22 | 0 | 0 |
| ClosureCounter(128) | wazero | N/A | N/A | N/A |
| ClosureCounter(128) | Tengo | 13,503 | 92,272 | 261 |
| ClosureCounter(128) | gopher-lua | 5,910 | 151 | 3 |
| ClosureCounter(128) | Goja | 10,173 | 1,264 | 13 |
| ClosureCounter(128) | gpython | 28,235 | 58,312 | 659 |
| ClosureCounter(128) | Yaegi | 34,704 | 34,784 | 786 |
| TypedArraySum(256) | minivm/default | 683.2 | 0 | 0 |
| TypedArraySum(256) | minivm/threaded | 6,313 | 0 | 0 |
| TypedArraySum(256) | minivm/jit | 621.5 | 0 | 0 |
| TypedArraySum(256) | native Go | 73.2 | 0 | 0 |
| TypedArraySum(256) | wazero | 157 | 8 | 1 |
| TypedArraySum(256) | Tengo | 15,507 | 94,208 | 513 |
| TypedArraySum(256) | gopher-lua | 3,413 | 4,000 | 15 |
| TypedArraySum(256) | Goja | 13,399 | 2,080 | 238 |
| TypedArraySum(256) | gpython | 7,652 | 2,496 | 246 |
| TypedArraySum(256) | Yaegi | 4,246 | 296 | 8 |
| AllocationGraph(128) | minivm/default | 9,370 | 5,120 | 256 |
| AllocationGraph(128) | minivm/threaded | 7,774 | 5,120 | 256 |
| AllocationGraph(128) | minivm/jit | 9,362 | 5,120 | 256 |
| AllocationGraph(128) | native Go | 943.8 | 1,024 | 128 |
| AllocationGraph(128) | wazero | N/A | N/A | N/A |
| AllocationGraph(128) | Tengo | 14,516 | 96,288 | 388 |
| AllocationGraph(128) | gopher-lua | 6,543 | 14,376 | 256 |
| AllocationGraph(128) | Goja | 26,551 | 78,016 | 770 |
| AllocationGraph(128) | gpython | 5,715 | 5,712 | 266 |
| AllocationGraph(128) | Yaegi | 12,190 | 1,492 | 142 |
| BranchTree(96) | minivm/default | 267.1 | 0 | 0 |
| BranchTree(96) | minivm/threaded | 952 | 0 | 0 |
| BranchTree(96) | minivm/jit | 264.7 | 0 | 0 |
| BranchTree(96) | native Go | 79.98 | 0 | 0 |
| BranchTree(96) | wazero | 172.1 | 16 | 1 |
| BranchTree(96) | Tengo | 18,088 | 95,384 | 660 |
| BranchTree(96) | gopher-lua | 8,716 | 2,464 | 9 |
| BranchTree(96) | Goja | 13,926 | 1,992 | 196 |
| BranchTree(96) | gpython | 12,187 | 2,168 | 203 |
| BranchTree(96) | Yaegi | 10,769 | 1,832 | 308 |

Wazero has no equivalent canonical fixture for `ClosureCounter(128)` or `AllocationGraph(128)`, so those rows are `N/A`.

## Public API Costs

These benchmarks measure the named public operation. Setup, validation, cleanup, and paired operations stay outside the manually reported `ns/op` interval; allocation metrics still describe the complete benchmark iteration.

| Area | Operation | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Construction | `empty program` | 2,545 | 34,985 | 26 |
| Construction | `program, JIT disabled` | 2,624 | 35,064 | 29 |
| Construction | `program, JIT enabled` | 2,606 | 35,064 | 29 |
| Reset | `scalar state` | 24.43 | 0 | 0 |
| Reset | `heap state` | 28.99 | 0 | 0 |
| Reset | `installed JIT state` | 28.63 | 0 | 0 |
| Stack | `Push scalar` | 15.25 | 0 | 0 |
| Stack | `Push reference` | 62.18 | 16 | 1 |
| Stack | `Pop` | 5.414 | 0 | 0 |
| Stack | `PopBoxed` | 5.436 | 0 | 0 |
| Stack | `Peek` | 1.607 | 0 | 0 |
| Heap | `Alloc` | 32.77 | 16 | 1 |
| Heap | `Retain` | 19.98 | 0 | 0 |
| Heap | `Release` | 19.31 | 0 | 0 |
| Pool | `Get, uncontended` | 22.97 | 0 | 0 |
| Pool | `Get, miss` | 2,094 | 34,840 | 23 |
| Pool | `Get, shared-JIT miss` | 10,133 | 44,456 | 282 |
| Pool | `parallel round trip` | 280.3 | 0 | 0 |
| Pool | `Put, uncontended` | 125.5 | 0 | 0 |

Scalar stack access, reset, retain/release, and uncontended pool reuse are allocation-free. Construction and pool misses are dominated by interpreter state allocation. `SharedJITMiss` includes a new pooled interpreter synchronizing against shared JIT state.

## JIT Activation Overhead

`BenchmarkInterpreter_Run/ColdBackedge` runs a 256-iteration counting loop with `WithTick(1<<20)` and `WithThreshold(1<<30)`, measuring `Run` only; result extraction and reset remain outside the reported duration. The function remains below the sample threshold, so it keeps the ordinary generated `BR` handler.

| Case | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| cold unconditional backedge | 2,324 | 0 | 0 |

This benchmark guards the cold-path boundary: exact backedge observation is installed only after periodic sampling marks a function hot, rather than adding a callback or header scan to every cold loop iteration.

## Threaded Execution Samples

The following rows use `BenchmarkInterpreter_Run/.../Threaded`: `WithTick(1)` and `WithThreshold(-1)`. Each result times one complete `Run`; setup opcodes are intentionally included, while `Reset` and result validation stay outside the measured interval.

| Area | Case | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| `nop` | `i32.const_nop_returns_i32` | 94.36 | 0 | 0 |
| constant | `i32.const_returns_i32` | 95.39 | 0 | 0 |
| i32 arithmetic | `i32.add` | 100.9 | 0 | 0 |
| i64 arithmetic | `i64.add` | 109.8 | 0 | 0 |
| f32 arithmetic | `f32.add` | 118.2 | 0 | 0 |
| f64 arithmetic | `f64.add` | 103.8 | 0 | 0 |
| conversion | `i32.to_i64_s` | 104.3 | 0 | 0 |
| branch | `br` | 99.49 | 0 | 0 |
| branch | `br_if` | 116.6 | 0 | 0 |
| branch | `br_table` | 104.1 | 0 | 0 |
| call | `direct bytecode call` | 148.4 | 0 | 0 |
| array | `constant array get` | 105.8 | 0 | 0 |
| array | `new + get` | 179.3 | 40 | 2 |
| array | `set + get` | 218 | 40 | 2 |
| struct | `constant struct get` | 113.9 | 0 | 0 |
| struct | `new` | 124.1 | 64 | 1 |
| struct | `set + get` | 188.4 | 64 | 1 |
| map | `default + len` | 132.7 | 72 | 2 |
| map | `new + get` | 202.1 | 216 | 3 |
| map | `set + len` | 208.3 | 216 | 3 |

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
| array | no child refs | 2.463 | 0 | 0 |
| array | child refs | 2.13 | 0 | 0 |
| struct | no child refs | 1.713 | 0 | 0 |
| struct | child refs | 1.8 | 0 | 0 |
| typed map | child refs | 22.92 | 0 | 0 |
| dynamic map | no child refs | 1.757 | 0 | 0 |
| dynamic map | child refs | 24.94 | 0 | 0 |

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
- Output was grouped by exact benchmark name, and each documented value is the median of exactly three samples.

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
