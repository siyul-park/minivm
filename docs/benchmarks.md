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

All numbers in this document were measured on July 15, 2026:

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

- `RecursiveFib(35)` places `minivm/default` at **48.43 ms**, within about **3.5%** of wazero's **46.79 ms**, while remaining allocation-free after warmup.
- Adaptive native traces reduce `IterativeFib(30)` from **730.9 ns** threaded to **71.83 ns**, `TypedArraySum(256)` from **6.239 us** to **655.2 ns**, and `BranchTree(96)` from **986.4 ns** to **228.0 ns**.
- Primitive array mutation stays on the native loop path in `Sieve(256)`: `default` is **5.052 us** and eager `jit` is **5.017 us**, versus **15.385 us** threaded. All three modes allocate `1,048 B` in `2` allocations.
- Threshold-zero `jit` is not a warmed-JIT guarantee. It matches `default` on Sieve and BranchTree, but is slower on IterativeFib, TypedArraySum, and recursive Fibonacci because it can compile before representative traces are learned.
- Allocation-heavy workloads remain interpreter-bound. `AllocationGraph(128)` is fastest in minivm's threaded mode at **7.513 us**; adaptive and eager modes add profiling cost without native coverage.
- Indirect recursion remains a major gap: `IndirectRecursiveFib(20)` takes about **598 us** in adaptive/eager modes, versus **41.8 us** in wazero.

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
| IterativeFib(30) | minivm/default | 71.83 | 0 | 0 |
| IterativeFib(30) | minivm/threaded | 730.9 | 0 | 0 |
| IterativeFib(30) | minivm/jit | 108.6 | 0 | 0 |
| IterativeFib(30) | native Go | 8.337 | 0 | 0 |
| IterativeFib(30) | wazero | 49.84 | 8 | 1 |
| IterativeFib(30) | Tengo | 9,722 | 90,592 | 61 |
| IterativeFib(30) | gopher-lua | 494.4 | 160 | 0 |
| IterativeFib(30) | Goja | 2,166 | 368 | 20 |
| IterativeFib(30) | gpython | 2,427 | 2,448 | 88 |
| IterativeFib(30) | Yaegi | 2,709 | 2,036 | 101 |
| Sieve(256) | minivm/default | 5,052 | 1,048 | 2 |
| Sieve(256) | minivm/threaded | 15,385 | 1,048 | 2 |
| Sieve(256) | minivm/jit | 5,017 | 1,048 | 2 |
| Sieve(256) | native Go | 247.8 | 0 | 0 |
| Sieve(256) | wazero | 645.4 | 8 | 1 |
| Sieve(256) | Tengo | 51,918 | 122,504 | 1,611 |
| Sieve(256) | gopher-lua | 22,102 | 18,416 | 44 |
| Sieve(256) | Goja | 41,769 | 1,872 | 25 |
| Sieve(256) | gpython | 34,104 | 5,704 | 30 |
| Sieve(256) | Yaegi | 18,673 | 1,800 | 37 |
| RecursiveFib(20) | minivm/default | 41,053 | 0 | 0 |
| RecursiveFib(20) | minivm/threaded | 377,151 | 0 | 0 |
| RecursiveFib(20) | minivm/jit | 395,583 | 0 | 0 |
| RecursiveFib(20) | native Go | 15,333 | 0 | 0 |
| RecursiveFib(20) | wazero | 34,224 | 8 | 1 |
| RecursiveFib(20) | Tengo | 915,188 | 319,346 | 28,655 |
| RecursiveFib(20) | gopher-lua | 1,130,909 | 704 | 2 |
| RecursiveFib(20) | Goja | 1,529,105 | 4,680 | 39 |
| RecursiveFib(20) | gpython | 4,124,278 | 9,807,929 | 109,494 |
| RecursiveFib(20) | Yaegi | 4,258,056 | 8,302,133 | 192,840 |
| RecursiveFib(35) | minivm/default | 48,426,669 | 0 | 0 |
| RecursiveFib(35) | minivm/threaded | 512,675,498 | 0 | 0 |
| RecursiveFib(35) | minivm/jit | 539,464,080 | 0 | 0 |
| RecursiveFib(35) | native Go | 20,957,448 | 0 | 0 |
| RecursiveFib(35) | wazero | 46,785,131 | 9 | 1 |
| RecursiveFib(35) | Tengo | 1,171,601,625 | 312,797,728 | 39,088,178 |
| RecursiveFib(35) | gopher-lua | 1,475,545,292 | 971,008 | 3,793 |
| RecursiveFib(35) | Goja | 2,033,197,667 | 375,360 | 46,373 |
| RecursiveFib(35) | gpython | 5,238,414,292 | 13,378,035,520 | 149,350,319 |
| RecursiveFib(35) | Yaegi | 5,439,306,583 | 11,324,340,056 | 263,043,718 |
| IndirectRecursiveFib(20) | minivm/default | 598,302 | 0 | 0 |
| IndirectRecursiveFib(20) | minivm/threaded | 564,863 | 0 | 0 |
| IndirectRecursiveFib(20) | minivm/jit | 598,147 | 0 | 0 |
| IndirectRecursiveFib(20) | native Go | 15,610 | 0 | 0 |
| IndirectRecursiveFib(20) | wazero | 41,827 | 8 | 1 |
| IndirectRecursiveFib(20) | Tengo | 919,605 | 319,345 | 28,655 |
| IndirectRecursiveFib(20) | gopher-lua | 935,191 | 704 | 2 |
| IndirectRecursiveFib(20) | Goja | 1,335,157 | 4,680 | 39 |
| IndirectRecursiveFib(20) | gpython | 3,757,153 | 10,158,202 | 109,494 |
| IndirectRecursiveFib(20) | Yaegi | 10,576,088 | 13,059,851 | 394,041 |
| ClosureCounter(128) | minivm/default | 3,089 | 64 | 2 |
| ClosureCounter(128) | minivm/threaded | 2,877 | 64 | 2 |
| ClosureCounter(128) | minivm/jit | 3,119 | 64 | 2 |
| ClosureCounter(128) | native Go | 33.77 | 0 | 0 |
| ClosureCounter(128) | wazero | N/A | N/A | N/A |
| ClosureCounter(128) | Tengo | 12,936 | 92,272 | 261 |
| ClosureCounter(128) | gopher-lua | 5,720 | 152 | 3 |
| ClosureCounter(128) | Goja | 9,752 | 1,264 | 13 |
| ClosureCounter(128) | gpython | 26,111 | 58,312 | 659 |
| ClosureCounter(128) | Yaegi | 32,116 | 34,784 | 786 |
| TypedArraySum(256) | minivm/default | 655.2 | 0 | 0 |
| TypedArraySum(256) | minivm/threaded | 6,239 | 0 | 0 |
| TypedArraySum(256) | minivm/jit | 3,484 | 0 | 0 |
| TypedArraySum(256) | native Go | 64.34 | 0 | 0 |
| TypedArraySum(256) | wazero | 151.9 | 8 | 1 |
| TypedArraySum(256) | Tengo | 15,603 | 94,208 | 513 |
| TypedArraySum(256) | gopher-lua | 3,291 | 4,000 | 15 |
| TypedArraySum(256) | Goja | 12,877 | 2,080 | 238 |
| TypedArraySum(256) | gpython | 7,210 | 2,496 | 246 |
| TypedArraySum(256) | Yaegi | 3,897 | 296 | 8 |
| AllocationGraph(128) | minivm/default | 9,044 | 5,120 | 256 |
| AllocationGraph(128) | minivm/threaded | 7,513 | 5,120 | 256 |
| AllocationGraph(128) | minivm/jit | 9,099 | 5,120 | 256 |
| AllocationGraph(128) | native Go | 906.1 | 1,024 | 128 |
| AllocationGraph(128) | wazero | N/A | N/A | N/A |
| AllocationGraph(128) | Tengo | 13,677 | 96,288 | 388 |
| AllocationGraph(128) | gopher-lua | 6,030 | 14,376 | 256 |
| AllocationGraph(128) | Goja | 24,285 | 78,016 | 770 |
| AllocationGraph(128) | gpython | 5,431 | 5,712 | 266 |
| AllocationGraph(128) | Yaegi | 11,804 | 1,492 | 142 |
| BranchTree(96) | minivm/default | 228 | 0 | 0 |
| BranchTree(96) | minivm/threaded | 986.4 | 0 | 0 |
| BranchTree(96) | minivm/jit | 230 | 0 | 0 |
| BranchTree(96) | native Go | 77.39 | 0 | 0 |
| BranchTree(96) | wazero | 156.9 | 16 | 1 |
| BranchTree(96) | Tengo | 16,719 | 95,384 | 660 |
| BranchTree(96) | gopher-lua | 8,215 | 2,464 | 9 |
| BranchTree(96) | Goja | 13,476 | 1,992 | 196 |
| BranchTree(96) | gpython | 11,504 | 2,168 | 203 |
| BranchTree(96) | Yaegi | 10,476 | 1,832 | 308 |

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
