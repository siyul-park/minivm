# Benchmarks

Benchmark results, execution characteristics, and measurement methodology for minivm.

## When to Read

Use this document when making or reviewing performance claims, changing benchmark workloads, changing runtime thresholds, or comparing minivm with other runtimes.

For implementation details, see `docs/jit-internals.md`. For profiling counters, see `docs/profile.md`.

## Source of Truth

| Concern | File or command |
|---|---|
| full benchmark suite | `make benchmark` |
| interpreter benchmarks | `interp` package benchmarks |
| cross-runtime benchmarks | `benchmarks/` module |
| profiling counters | `docs/profile.md` |
| platform support | `docs/compatibility.md` |

## Environment

Unless stated otherwise, benchmarks were run on:

- `darwin/arm64`
- Apple M4 Pro, 12 cores
- Go 1.26.2

minivm currently provides ARM64 JIT support only. On ARM64, the default `interp.New` can compile hot recorded traces, including function entries and loop headers, into native code. Pure interpreter benchmarks disable the JIT with `WithThreshold(-1)`.

## Running Benchmarks

```bash
# Full canonical package and VM-kernel suite
make benchmark-core

# Pull-request-sized benchmark suite
make benchmark-pr

# Cross-runtime comparison, matching the table below
cd benchmarks
go test -tags=compare -run='^$' -bench='.' -benchmem -benchtime=300ms -count=3 ./...
```

The cross-runtime command runs each benchmark three times. The tables below report the median `ns/op`, `B/op`, and `allocs/op` for every workload/runtime combination.

The comparison is informational rather than a strict end-to-end latency ranking. minivm reports execution-only `Interpreter.Run` time and excludes result extraction and reset, while other runtimes may include host-call result materialization and conversion. This boundary difference is most significant for short workloads, so small ratios should not be treated as precise runtime speedups.

## Summary

The canonical cross-runtime suite shows both the strengths and the current limits of minivm:

- `RecursiveFib(35)` places `minivm/default` near wazero and ahead of the script runtimes measured here, while remaining zero-allocation after warmup.
- `IterativeFib(30)` and `BranchTree(96)` show the adaptive default tier in the same order of magnitude as wazero while remaining allocation-free.
- `TypedArraySum(256)` is allocation-free in all minivm modes, but wazero remains materially faster in this fixture.
- `Sieve(256)` now favors native loop traces: `default` is **5.165 us** and eager `jit` is **5.172 us**, versus **15.831 us** for the threaded interpreter. All three modes allocate only the typed array itself (`1,048 B`, `2 allocs`).
- `IndirectRecursiveFib(20)` is a clear weak point: `minivm/default` is substantially slower than wazero despite remaining allocation-free.
- `AllocationGraph(128)` exposes object-management cost. minivm is faster than Tengo, Goja, and Yaegi, but slower than gpython and gopher-lua in this fixture.

These results are workload measurements, not general language rankings. The runtimes use different value models, safety boundaries, and compilation strategies.

## Cross-Runtime Comparison

### minivm Modes

| Mode | Construction | Meaning |
|---|---|---|
| `default` | `interp.New(prog)` | standard adaptive policy; may install and enter ARM64 traces after profiling |
| `threaded` | `interp.New(prog, interp.WithThreshold(-1))` | JIT disabled; pure threaded execution |
| `jit` | `interp.New(prog, interp.WithThreshold(0))` | eager profiling/compilation policy; not a precompiled or guaranteed-native steady state |

The `jit` label therefore means **threshold zero**, not “fully warmed native code.” Function and module entries become eligible on the first sample, while loop roots reached through an unconditional backward branch wait for eight exact back-edges so the first iteration does not bias the recorded branch path. It can still be slower than `default` when early entry compilation produces incomplete traces or when the workload is dominated by unsupported allocation paths.

Each runtime measures an already prepared callable through result recovery. Compilation, module construction, fixture injection, warmup, and minivm `Reset` are excluded. Runtime-specific host-call and result-conversion costs remain part of the measured invocation, so small differences should not be interpreted as VM-core instruction throughput alone.

### Complete Results

Environment: Apple M4 Pro, `darwin/arm64`, Go 1.26.2. Command: `go test -tags=compare -run='^$' -bench='.' -benchmem -benchtime=300ms -count=3 ./...`. Each row is the median of three samples.

| Workload | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| IterativeFib(30) | minivm/default | 69.9 | 0 | 0 |
| IterativeFib(30) | minivm/threaded | 718.5 | 0 | 0 |
| IterativeFib(30) | minivm/jit | 73.59 | 0 | 0 |
| IterativeFib(30) | native | 8.444 | 0 | 0 |
| IterativeFib(30) | wazero | 47.98 | 8 | 1 |
| IterativeFib(30) | tengo | 9,670 | 90,592 | 61 |
| IterativeFib(30) | gopher-lua | 496.9 | 160 | 0 |
| IterativeFib(30) | goja | 2,137 | 368 | 20 |
| IterativeFib(30) | gpython | 2,440 | 2,448 | 88 |
| IterativeFib(30) | yaegi | 2,695 | 2,036 | 101 |
| Sieve(256) | minivm/default | 5,165 | 1,048 | 2 |
| Sieve(256) | minivm/threaded | 15,831 | 1,048 | 2 |
| Sieve(256) | minivm/jit | 5,172 | 1,048 | 2 |
| Sieve(256) | native | 229.9 | 0 | 0 |
| Sieve(256) | wazero | 642.4 | 8 | 1 |
| Sieve(256) | tengo | 51,497 | 122,504 | 1,611 |
| Sieve(256) | gopher-lua | 22,080 | 18,416 | 44 |
| Sieve(256) | goja | 42,387 | 1,872 | 25 |
| Sieve(256) | gpython | 34,919 | 5,704 | 30 |
| Sieve(256) | yaegi | 18,998 | 1,800 | 37 |
| RecursiveFib(20) | minivm/default | 37,686 | 0 | 0 |
| RecursiveFib(20) | minivm/threaded | 353,404 | 0 | 0 |
| RecursiveFib(20) | minivm/jit | 359,322 | 0 | 0 |
| RecursiveFib(20) | native | 13,699 | 0 | 0 |
| RecursiveFib(20) | wazero | 30,896 | 8 | 1 |
| RecursiveFib(20) | tengo | 811,030 | 319,346 | 28,655 |
| RecursiveFib(20) | gopher-lua | 1,040,265 | 704 | 2 |
| RecursiveFib(20) | goja | 1,461,585 | 4,680 | 39 |
| RecursiveFib(20) | gpython | 3,656,195 | 9,807,935 | 109,494 |
| RecursiveFib(20) | yaegi | 3,752,830 | 8,302,126 | 192,840 |
| RecursiveFib(35) | minivm/default | 47,048,123 | 0 | 0 |
| RecursiveFib(35) | minivm/threaded | 487,293,996 | 0 | 0 |
| RecursiveFib(35) | minivm/jit | 496,864,164 | 0 | 0 |
| RecursiveFib(35) | native | 19,129,096 | 0 | 0 |
| RecursiveFib(35) | wazero | 44,150,405 | 9 | 1 |
| RecursiveFib(35) | tengo | 1,139,802,250 | 312,798,144 | 39,088,182 |
| RecursiveFib(35) | gopher-lua | 1,448,413,000 | 971,008 | 3,793 |
| RecursiveFib(35) | goja | 2,033,437,791 | 375,360 | 46,373 |
| RecursiveFib(35) | gpython | 5,148,001,292 | 13,378,035,136 | 149,350,297 |
| RecursiveFib(35) | yaegi | 5,357,106,709 | 11,324,346,072 | 263,043,770 |
| IndirectRecursiveFib(20) | minivm/default | 569,521 | 0 | 0 |
| IndirectRecursiveFib(20) | minivm/threaded | 555,324 | 0 | 0 |
| IndirectRecursiveFib(20) | minivm/jit | 569,709 | 0 | 0 |
| IndirectRecursiveFib(20) | native | 15,576 | 0 | 0 |
| IndirectRecursiveFib(20) | wazero | 42,267 | 8 | 1 |
| IndirectRecursiveFib(20) | tengo | 922,213 | 319,346 | 28,655 |
| IndirectRecursiveFib(20) | gopher-lua | 932,204 | 704 | 2 |
| IndirectRecursiveFib(20) | goja | 1,337,977 | 4,680 | 39 |
| IndirectRecursiveFib(20) | gpython | 3,712,726 | 10,158,210 | 109,494 |
| IndirectRecursiveFib(20) | yaegi | 10,443,107 | 13,059,874 | 394,041 |
| ClosureCounter(128) | minivm/default | 3,032 | 64 | 2 |
| ClosureCounter(128) | minivm/threaded | 2,841 | 64 | 2 |
| ClosureCounter(128) | minivm/jit | 3,047 | 64 | 2 |
| ClosureCounter(128) | native | 33.74 | 0 | 0 |
| ClosureCounter(128) | wazero | N/A | N/A | N/A |
| ClosureCounter(128) | tengo | 13,045 | 92,272 | 261 |
| ClosureCounter(128) | gopher-lua | 5,748 | 151 | 3 |
| ClosureCounter(128) | goja | 9,827 | 1,264 | 13 |
| ClosureCounter(128) | gpython | 25,897 | 58,312 | 659 |
| ClosureCounter(128) | yaegi | 31,750 | 34,784 | 786 |
| TypedArraySum(256) | minivm/default | 635.6 | 0 | 0 |
| TypedArraySum(256) | minivm/threaded | 6,309 | 0 | 0 |
| TypedArraySum(256) | minivm/jit | 579.3 | 0 | 0 |
| TypedArraySum(256) | native | 64.21 | 0 | 0 |
| TypedArraySum(256) | wazero | 150.1 | 8 | 1 |
| TypedArraySum(256) | tengo | 15,340 | 94,208 | 513 |
| TypedArraySum(256) | gopher-lua | 3,263 | 4,000 | 15 |
| TypedArraySum(256) | goja | 12,695 | 2,080 | 238 |
| TypedArraySum(256) | gpython | 7,251 | 2,496 | 246 |
| TypedArraySum(256) | yaegi | 4,274 | 296 | 8 |
| AllocationGraph(128) | minivm/default | 7,542 | 5,120 | 256 |
| AllocationGraph(128) | minivm/threaded | 7,390 | 5,120 | 256 |
| AllocationGraph(128) | minivm/jit | 7,502 | 5,120 | 256 |
| AllocationGraph(128) | native | 900.4 | 1,024 | 128 |
| AllocationGraph(128) | wazero | N/A | N/A | N/A |
| AllocationGraph(128) | tengo | 13,697 | 96,288 | 388 |
| AllocationGraph(128) | gopher-lua | 5,958 | 14,376 | 256 |
| AllocationGraph(128) | goja | 24,155 | 78,016 | 770 |
| AllocationGraph(128) | gpython | 5,401 | 5,712 | 266 |
| AllocationGraph(128) | yaegi | 11,502 | 1,492 | 142 |
| BranchTree(96) | minivm/default | 222.4 | 0 | 0 |
| BranchTree(96) | minivm/threaded | 949.4 | 0 | 0 |
| BranchTree(96) | minivm/jit | 224.7 | 0 | 0 |
| BranchTree(96) | native | 77.55 | 0 | 0 |
| BranchTree(96) | wazero | 156.3 | 16 | 1 |
| BranchTree(96) | tengo | 16,906 | 95,384 | 660 |
| BranchTree(96) | gopher-lua | 8,225 | 2,464 | 9 |
| BranchTree(96) | goja | 13,464 | 1,992 | 196 |
| BranchTree(96) | gpython | 11,627 | 2,168 | 203 |
| BranchTree(96) | yaegi | 10,412 | 1,832 | 308 |

Wazero has no corresponding canonical implementation for `ClosureCounter(128)` or `AllocationGraph(128)`, so those rows are marked `N/A`.

## Threaded Interpreter Throughput

The following results measure the pure threaded interpreter with JIT disabled.

Each row represents a full `Interpreter.Run` + `Reset` cycle. Setup instructions are included, so the numbers reflect practical dispatch overhead rather than isolated opcode cost.

| Area | Typical result |
|---|---:|
| `nop` | 8 ns/op |
| constants | 9 ns/op |
| i32 arithmetic, bitwise, comparison | about 11 ns/op |
| i64 arithmetic and comparison | about 12-13 ns/op |
| f32/f64 arithmetic and comparison | about 11 ns/op |
| numeric conversions | about 11-12 ns/op |
| branches | about 10-14 ns/op |
| bytecode calls | about 15-16 ns/op |
| host function calls | about 18 ns/op |
| array operations | about 30-44 ns/op |
| struct operations | about 25-39 ns/op |
| map operations | about 55-139 ns/op |

Detailed opcode semantics belong in `docs/instruction-set.md`; this document keeps benchmark interpretation only.

## Heap Lifecycle and Traversal

Lifecycle benchmarks use public heap APIs and include forced cyclic GC.

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `Alloc/free_slot_reuse` | 10.1 | 0 | 0 |
| `Alloc/small_heap_cyclic_gc` | 48.1 | 40 | 2 |
| `Release/primitive_struct` | 28.6 | 64 | 1 |
| `Release/ref_array` | 52.7 | 48 | 4 |
| `Release/ref_struct` | 54.4 | 72 | 3 |
| `Release/ref_valued_map` | 155.0 | 224 | 5 |

Reference traversal avoids allocation when no child references are present.

| Workload | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `no_refs` | 1.0 | 0 | 0 |
| `inline_i64` | 25.1 | 0 | 0 |
| `child_refs` | 32.4 | 8 | 1 |

## Marshal

`BenchmarkInterpreter_Marshal` measures conversion from ordinary Go values into VM values.

| Value | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `i32` | 39.2 | 80 | 2 |
| `string` | 49.9 | 96 | 3 |
| `function` | 64.4 | 144 | 4 |
| `slice_i32` | 81.5 | 136 | 4 |
| `host_object` | 139.6 | 324 | 7 |
| `struct_plain` | 141.8 | 200 | 6 |
| `nested_slice_struct` | 479.2 | 426 | 13 |
| `map_string_i32` | 547.3 | 712 | 18 |

Simple scalar and string values are inexpensive to marshal. Nested structures and maps are more expensive because they require additional heap objects and reflection work.

## Recursive Workloads

These results use the threaded interpreter with JIT disabled.

| Program | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `fib(20)` — i32 recursive | 353,404 | 0 | 0 |
| `factorial(10)` — i64 with early exit via `br_if` | 310 | 0 | 0 |

For the deep-recursion `fib(35)` result with JIT enabled, see the cross-runtime comparison above.

## ARM64 JIT

On ARM64, minivm compiles hot recorded traces to native code. Supported paths include numeric operations, direct calls, selected indirect function dispatch, ref-counted slots, selected read-only heap operations, and loops with safepoint polling.

Unsupported paths either deoptimize or continue through the threaded interpreter. These include allocation, ref-bearing or complex mutation, host calls, heap-promoted i64 values, and unsupported heap shapes. Guarded primitive typed-array stores can remain inside native loop traces.

The default threshold is `4096` executed instructions, which is about 32 samples at the default tick interval of 128.

### Canonical Kernel Observations

The current canonical kernels show that native trace coverage is workload-dependent:

- adaptive `default` is effective for direct recursive calls, iterative numeric loops, typed-array reads, guarded primitive typed-array writes, and branch-heavy scalar code
- threshold-zero `jit` is not equivalent to a warmed native cache, but exact back-edge warmup keeps loop capture from over-specializing the first iteration
- `Sieve(256)` now runs about 3.1 times faster in `default` and eager `jit` than in the threaded interpreter, while preserving the same allocation count
- allocation-heavy object kernels such as `AllocationGraph` remain limited by heap management rather than scalar trace throughput
- indirect recursive calls remain substantially slower than wazero and are a priority for call-target and trace-continuation optimization

Use the complete cross-runtime table above for current numbers. Do not infer JIT entry solely from the `jit` sub-benchmark name; profiler metrics are required when a benchmark specifically claims native entry.

On x86-64, JIT is not implemented yet. The runtime falls back to threaded execution.

## Methodology

- Cross-runtime results use `-benchtime=300ms -count=3`; every table value is the median of the three samples.
- Each minivm fixture is verified before measurement. The benchmark performs one correctness run, four untimed warmup runs, and 32 untimed allocation samples before timing.
- minivm reports execution-only time for `Interpreter.Run`. Result transfer through `PopBoxed` and `Reset` remains outside the measured duration.
- `interp.New()` is called once outside the timed loop. `default` leaves all options unchanged, `threaded` changes only the threshold to `-1`, and `jit` changes only the threshold to `0`.
- External runtime parsing, compilation, module creation, and function lookup remain outside the timer where the runtime API permits. The timed loop repeatedly invokes the prepared function or compiled program.
- wazero uses its default compiler runtime, with module compilation and instantiation excluded from timing.
- Cross-runtime benchmarks live in the separate `benchmarks/` Go module and are enabled only with the `compare` build tag.
- The workload sources preserve the same input and expected result, but runtime-specific object representation and call conventions differ. Treat the results as end-to-end kernel comparisons rather than isolated opcode comparisons.

Cross-runtime library versions:

- wazero v1.12.0
- gopher-lua v1.1.2
- Tengo v2.17.0
- goja v0.0.0-20260311135729-065cd970411c
- gpython v0.2.0
- Yaegi v0.16.1

## Benchmark Ownership

Use two benchmark layers:

| Owner | Measures |
|---|---|
| `interp/interp_test.go` | public interpreter and pool API costs, dispatch, opcode families, lifecycle, heap operations, and JIT lifecycle states |
| `benchmarks/` | runtime-neutral VM kernels and optional cross-runtime comparisons |

Package-owned microbenchmarks may remain beside their production owner only when they measure a distinct package contract, such as `types.Traceable.Refs` or analysis construction. Service-domain workloads do not define canonical VM performance.

Every benchmark behavior has an independently owned correctness test. Interpreter execution cases map to `TestInterpreter_Run`; construction, reset, stack, heap, and pool cases map to their same-named public API tests.

## Interpreter Benchmark Methodology

Interpreter benchmark names match public API owners:

```text
BenchmarkNew
BenchmarkInterpreter_Run
BenchmarkInterpreter_Reset
BenchmarkInterpreter_Push
BenchmarkInterpreter_Pop
BenchmarkInterpreter_PopBoxed
BenchmarkInterpreter_Peek
BenchmarkInterpreter_Alloc
BenchmarkInterpreter_Retain
BenchmarkInterpreter_Release
BenchmarkPool_Get
BenchmarkPool_Put
```

Each fixture is validated once before timing. `BenchmarkInterpreter_Run` times only `Run`; `Reset`, result validation, fixture construction, and JIT warmup remain outside the timer. `BenchmarkInterpreter_Reset` times only `Reset`; re-execution remains outside the timer. Pool miss benchmarks time `Get` while pool construction and cleanup remain outside the timer.

Execution modes are explicit sub-benchmarks:

| Mode | Boundary |
|---|---|
| `Threaded` | exact threaded dispatch with fusion and JIT disabled |
| `Fused` | generated fused threaded execution with JIT disabled |
| `JITWarm` | native stub already emitted; execution only |
| `JITCold` | trace capture and compilation included; interpreter construction excluded |
| `JITExit` | first alternate branch from a warmed native trace |
| `JITDeopt` | first trapping guard failure from a warmed native trace |

Warm, exit, and deoptimization fixtures assert that a native stub exists before timing. Cold fixtures assert native emission after timing. Straight dispatch and numeric cases report fixed `opcodes/op` alongside `ns/op`.

Pool benchmarks separate uncontended reuse, capacity miss, shared-JIT miss, parallel round trips, and put cost. Parallel workers use independent interpreter instances obtained from the pool.

## Execution Tiers

| Target | Use | Contents |
|---|---|---|
| `make benchmark-pr` | pull requests | stable construction, reset, dispatch, representative numeric/control/call/array cases, and four threaded kernels |
| `make benchmark-core` | local canonical run | all package benchmarks and all runtime-neutral VM kernels |
| `make benchmark-nightly` | scheduled report | canonical suite with repeated samples, including cold JIT, exits, deoptimization, large collections, and parallel pool cases |
| `make benchmark-compare` | optional analysis | `compare`-tagged external runtimes only |

Pull-request and nightly jobs report results without comparing against golden numbers. Use repeated output with `benchstat` for manual analysis. Add an automated threshold only after a benchmark has stable variance and both statistical and practical limits are documented.

## Maintenance Notes

When changing benchmark documentation:

- keep claims tied to concrete benchmark rows
- include platform, Go version, and benchmark command
- avoid repeating opcode semantics already covered by `instruction-set.md`
- avoid repeating JIT internals already covered by `jit-internals.md`
- update README headline numbers only after this document changes

## Related Docs

- `docs/profile.md` — sampling and runtime counters
- `docs/jit-internals.md` — trace JIT behavior
- `docs/instruction-set.md` — opcode semantics
- `docs/compatibility.md` — platform support
