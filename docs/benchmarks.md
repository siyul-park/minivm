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

All numbers in this document were measured on July 16, 2026 (the `Sieve(256)` minivm rows were re-measured on July 18, 2026 after issue #155):

- Apple M4 Pro, 12 cores
- `darwin/arm64`
- macOS 26.4.1
- Go 1.26.2

Every table reports the median of three sequential samples. Lower `ns/op`, `B/op`, and `allocs/op` are better. The runs were executed serially so concurrent benchmark processes did not compete for CPU time.

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

- `RecursiveFib(35)` places `minivm/default` at **46.30 ms**, within about **2.6%** of wazero's **45.11 ms**, while remaining allocation-free after warmup.
- Adaptive native traces reduce `IterativeFib(30)` from **738.6 ns** threaded to **76.14 ns**, `TypedArraySum(256)` from **6.299 us** to **669.0 ns**, and `BranchTree(96)` from **981.8 ns** to **235.1 ns**.
- Primitive array mutation stays on the native loop path in `Sieve(256)`: deferred-ownership elision drops the per-element retain/release pair, so a runtime-allocated array reaches the same cheap native path a typed-array constant already used. All three modes allocate `1,048 B` in `2` allocations.
- Loop-invariant container hoisting (issue #153) removes the per-access heap-cell derivation, itab guard, and slice-header reload from hoisted loop bodies. It shrinks the loop callables but leaves wall time unchanged: the removed loads sat off the out-of-order critical path.
- Branch-leg folding (issue #155) records native loop exits as branches and folds hot legs that rejoin the header back into the native loop as real back-edges. On `Sieve(256)` this removes the per-prime entry/exit round trips (scan-loop native entries drop from ~55 to ~1 per run) and cuts `default` from **4.72 us** to **1.61 us** and eager `jit` to **1.57 us**, versus **15.9 us** threaded (interleaved A/B on M4 Pro). The remaining gap to wazero (~0.66 us) is dominated by the per-iteration operand flush.
- Threshold-zero `jit` is not a warmed-JIT guarantee. It matches `default` on Sieve and BranchTree, but is slower on IterativeFib, TypedArraySum, and recursive Fibonacci because it can compile before representative traces are learned.
- Allocation-heavy workloads remain interpreter-bound. `AllocationGraph(128)` is fastest in minivm's threaded mode at **7.617 us**; adaptive and eager modes add profiling cost without native coverage.
- Indirect recursion reaches the native self-call path in adaptive mode: `IndirectRecursiveFib(20)` is **54.94 us** in `default`, versus **568 us** threaded and **42.3 us** in wazero. Eager `jit` stays at **589 us**, consistent with the threshold-zero note above.

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
| IterativeFib(30) | minivm/default | 76.14 | 0 | 0 |
| IterativeFib(30) | minivm/threaded | 738.6 | 0 | 0 |
| IterativeFib(30) | minivm/jit | 109.1 | 0 | 0 |
| IterativeFib(30) | native Go | 8.757 | 0 | 0 |
| IterativeFib(30) | wazero | 50.26 | 8 | 1 |
| IterativeFib(30) | Tengo | 9,966 | 90,592 | 61 |
| IterativeFib(30) | gopher-lua | 511.6 | 160 | 0 |
| IterativeFib(30) | Goja | 2,218 | 368 | 20 |
| IterativeFib(30) | gpython | 2,590 | 2,448 | 88 |
| IterativeFib(30) | Yaegi | 2,801 | 2,036 | 101 |
| Sieve(256) | minivm/default | 1,610 | 1,048 | 2 |
| Sieve(256) | minivm/threaded | 15,933 | 1,048 | 2 |
| Sieve(256) | minivm/jit | 1,572 | 1,048 | 2 |
| Sieve(256) | native Go | 238.3 | 0 | 0 |
| Sieve(256) | wazero | 662.8 | 8 | 1 |
| Sieve(256) | Tengo | 53,437 | 122,504 | 1,611 |
| Sieve(256) | gopher-lua | 23,269 | 18,416 | 44 |
| Sieve(256) | Goja | 43,554 | 1,872 | 25 |
| Sieve(256) | gpython | 36,598 | 5,704 | 30 |
| Sieve(256) | Yaegi | 18,655 | 1,800 | 37 |
| RecursiveFib(20) | minivm/default | 37,417 | 0 | 0 |
| RecursiveFib(20) | minivm/threaded | 351,637 | 0 | 0 |
| RecursiveFib(20) | minivm/jit | 362,772 | 0 | 0 |
| RecursiveFib(20) | native Go | 13,553 | 0 | 0 |
| RecursiveFib(20) | wazero | 31,262 | 8 | 1 |
| RecursiveFib(20) | Tengo | 841,631 | 319,345 | 28,655 |
| RecursiveFib(20) | gopher-lua | 1,032,737 | 704 | 2 |
| RecursiveFib(20) | Goja | 1,476,390 | 4,680 | 39 |
| RecursiveFib(20) | gpython | 3,764,356 | 9,807,927 | 109,494 |
| RecursiveFib(20) | Yaegi | 3,840,787 | 8,302,122 | 192,840 |
| RecursiveFib(35) | minivm/default | 46,303,288 | 0 | 0 |
| RecursiveFib(35) | minivm/threaded | 490,321,941 | 0 | 0 |
| RecursiveFib(35) | minivm/jit | 508,171,481 | 0 | 0 |
| RecursiveFib(35) | native Go | 19,463,741 | 0 | 0 |
| RecursiveFib(35) | wazero | 45,112,434 | 9 | 1 |
| RecursiveFib(35) | Tengo | 1,180,099,833 | 312,797,184 | 39,088,173 |
| RecursiveFib(35) | gopher-lua | 1,453,945,708 | 971,008 | 3,793 |
| RecursiveFib(35) | Goja | 2,058,714,041 | 375,360 | 46,373 |
| RecursiveFib(35) | gpython | 5,364,887,708 | 13,378,034,960 | 149,350,308 |
| RecursiveFib(35) | Yaegi | 5,537,717,750 | 11,324,340,872 | 263,043,707 |
| IndirectRecursiveFib(20) | minivm/default | 54,937 | 0 | 0 |
| IndirectRecursiveFib(20) | minivm/threaded | 567,896 | 0 | 0 |
| IndirectRecursiveFib(20) | minivm/jit | 589,191 | 0 | 0 |
| IndirectRecursiveFib(20) | native Go | 16,040 | 0 | 0 |
| IndirectRecursiveFib(20) | wazero | 42,331 | 8 | 1 |
| IndirectRecursiveFib(20) | Tengo | 958,917 | 319,345 | 28,655 |
| IndirectRecursiveFib(20) | gopher-lua | 944,516 | 704 | 2 |
| IndirectRecursiveFib(20) | Goja | 1,366,603 | 4,680 | 39 |
| IndirectRecursiveFib(20) | gpython | 3,961,904 | 10,158,200 | 109,494 |
| IndirectRecursiveFib(20) | Yaegi | 11,013,641 | 13,059,857 | 394,041 |
| ClosureCounter(128) | minivm/default | 3,159 | 64 | 2 |
| ClosureCounter(128) | minivm/threaded | 3,021 | 64 | 2 |
| ClosureCounter(128) | minivm/jit | 3,159 | 64 | 2 |
| ClosureCounter(128) | native Go | 34.99 | 0 | 0 |
| ClosureCounter(128) | wazero | N/A | N/A | N/A |
| ClosureCounter(128) | Tengo | 13,287 | 92,272 | 261 |
| ClosureCounter(128) | gopher-lua | 5,842 | 151 | 3 |
| ClosureCounter(128) | Goja | 10,144 | 1,264 | 13 |
| ClosureCounter(128) | gpython | 27,037 | 58,312 | 659 |
| ClosureCounter(128) | Yaegi | 33,173 | 34,784 | 786 |
| TypedArraySum(256) | minivm/default | 669.0 | 0 | 0 |
| TypedArraySum(256) | minivm/threaded | 6,299 | 0 | 0 |
| TypedArraySum(256) | minivm/jit | 3,537 | 0 | 0 |
| TypedArraySum(256) | native Go | 66.26 | 0 | 0 |
| TypedArraySum(256) | wazero | 153.5 | 8 | 1 |
| TypedArraySum(256) | Tengo | 15,676 | 94,208 | 513 |
| TypedArraySum(256) | gopher-lua | 3,415 | 4,000 | 15 |
| TypedArraySum(256) | Goja | 13,303 | 2,080 | 238 |
| TypedArraySum(256) | gpython | 7,776 | 2,496 | 246 |
| TypedArraySum(256) | Yaegi | 4,002 | 296 | 8 |
| AllocationGraph(128) | minivm/default | 9,226 | 5,120 | 256 |
| AllocationGraph(128) | minivm/threaded | 7,617 | 5,120 | 256 |
| AllocationGraph(128) | minivm/jit | 9,199 | 5,120 | 256 |
| AllocationGraph(128) | native Go | 935.5 | 1,024 | 128 |
| AllocationGraph(128) | wazero | N/A | N/A | N/A |
| AllocationGraph(128) | Tengo | 13,988 | 96,288 | 388 |
| AllocationGraph(128) | gopher-lua | 6,273 | 14,376 | 256 |
| AllocationGraph(128) | Goja | 25,412 | 78,016 | 770 |
| AllocationGraph(128) | gpython | 5,941 | 5,712 | 266 |
| AllocationGraph(128) | Yaegi | 12,222 | 1,492 | 142 |
| BranchTree(96) | minivm/default | 235.1 | 0 | 0 |
| BranchTree(96) | minivm/threaded | 981.8 | 0 | 0 |
| BranchTree(96) | minivm/jit | 234.0 | 0 | 0 |
| BranchTree(96) | native Go | 79.10 | 0 | 0 |
| BranchTree(96) | wazero | 164.2 | 16 | 1 |
| BranchTree(96) | Tengo | 17,274 | 95,384 | 660 |
| BranchTree(96) | gopher-lua | 8,775 | 2,464 | 9 |
| BranchTree(96) | Goja | 13,901 | 1,992 | 196 |
| BranchTree(96) | gpython | 12,295 | 2,168 | 203 |
| BranchTree(96) | Yaegi | 11,062 | 1,832 | 308 |

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
