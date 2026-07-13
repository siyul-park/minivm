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
# Full benchmark suite
make benchmark

# Fusion migration and ref-ownership cases (ten samples for benchstat)
make benchmark-fusion

# Pure threaded interpreter only
go test -run="-" -bench="BenchmarkInterpreter_Run/threaded" -benchmem ./interp/...

# Cross-runtime comparison
cd benchmarks && go test -run="-" -bench="BenchmarkFib35" -benchmem -benchtime=2s ./...

# ARM64 JIT coverage workloads
cd benchmarks && go test -run="^$" -bench="BenchmarkJITIssue60" -benchmem -benchtime=2s ./...
```

## Summary

Main findings:

- minivm's threaded interpreter is allocation-light and faster than the compared script VMs on recursive numeric workloads
- the ARM64 JIT significantly improves hot recursive and loop-heavy code
- on `fib(35)`, the JIT improves minivm by about **13x** over the threaded interpreter
- the JIT brings minivm close to native-code runtimes such as wazero while preserving boxed values and deoptimization support
- most scalar instructions in the pure threaded interpreter dispatch in roughly **11-13 ns/op** with zero allocations

## Cross-Runtime Comparison: `fib(35)`

This benchmark computes recursive `fib(35)` without memoization.

- Result: `9,227,465`
- Recursive calls: about `29.8M`
- Source: `benchmarks/fib_test.go`
- Timing excludes runtime setup where applicable

| Runtime | ns/op | B/op | allocs/op | vs native Go | Execution model |
|---|---:|---:|---:|---:|---|
| native Go | 19,324,275 | 0 | 0 | 1x | compiled |
| wazero | 44,409,757 | 16 | 2 | 2.3x | WASM to native JIT |
| **minivm JIT** | **51,911,961** | **4,918** | **45** | **2.7x** | threaded interpreter + tracing ARM64 JIT |
| minivm interp | 669,343,195 | 288 | 2 | 35x | threaded interpreter |
| tengo | 1,138,199,604 | 312,799,988 | 39,088,179 | 59x | bytecode VM |
| gopher-lua | 1,462,044,917 | 971,008 | 3,793 | 76x | register VM |
| goja | 2,052,722,000 | 383,488 | 46,384 | 106x | bytecode VM |

On this workload, the ARM64 JIT reduces minivm execution time from about **669 ms** to **52 ms**. This makes the JIT path about **13x faster** than the threaded interpreter.

Compared with other script VMs, minivm JIT is about **22-40x faster** on this benchmark. The pure threaded interpreter also remains competitive while staying allocation-light.

minivm JIT is still slower than wazero by about **1.2x**. This is expected: minivm keeps NaN-boxed values and deoptimization state, while wazero compiles WASM to unboxed native code without the same fallback requirements.

### Warmup vs. Steady-State Allocations

The `minivm interp` and `minivm JIT` rows above run `-benchtime=2s`, which repeats `Run`/`Reset` enough times to amortize one-time setup: the interpreter's entry-trace capture, and (for JIT) native trace compilation and installation. At a small `-benchtime`/`-benchtime=Nx` (few `b.N` iterations), that one-time cost dominates and inflates the reported `allocs/op`, making the runtime look far less allocation-light than it is in steady state.

`benchmarks/alloc_test.go` isolates the two phases explicitly by running one untimed `Run`/`Reset` cycle before `b.ResetTimer()`:

| Benchmark | Isolates | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| `BenchmarkFib35AllocInterpSteady` | threaded interpreter, warm | 516,040,250 | 0 | 0 |
| `BenchmarkFib20AllocJITWarmup` | one fresh compile + `fib(20)` run | 171,915 | 290,147 | 2,264 |
| `BenchmarkFib35AllocJITSteady` | JIT, trace already installed | 48,199,078 | 0 | 0 |

Both the threaded interpreter and the JIT are zero-allocation per `Run` once warm. The JIT's ~2,264 allocations and ~290 KB are a one-shot cost from `interp.New`, tracer capture, and native compilation/installation on the first hot entry; they do not recur on subsequent `Run` calls against the same interpreter.

Large bytecode programs also used to make profiler warmup quadratic because each newly observed instruction offset reallocated the collector's sample slice to its exact length. Geometric growth reduced a 10,000-row tl2g batch benchmark (`-benchtime=20x`) from about 50.9 MB/op to 551 KB/op; the remaining figure includes the workload's output and other setup allocations.

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
| `fib(20)` — i32 recursive | 570,765 | 0 | 0 |
| `factorial(10)` — i64 with early exit via `br_if` | 310 | 0 | 0 |

For the deep-recursion `fib(35)` result with JIT enabled, see the cross-runtime comparison above.

## ARM64 JIT

On ARM64, minivm compiles hot recorded traces to native code. Supported paths include numeric operations, direct calls, selected indirect function dispatch, ref-counted slots, selected read-only heap operations, and loops with safepoint polling.

Unsupported paths either deoptimize or continue through the threaded interpreter. These include allocation, mutation, host calls, heap-promoted i64 values, and unsupported heap shapes.

The default threshold is `4096` executed instructions, which is about 32 samples at the default tick interval of 128.

### JIT Coverage Workloads

`BenchmarkJITIssue60` tracks key JIT coverage workloads.

| Workload | interp ns/op | JIT ns/op | Effect |
|---|---:|---:|---:|
| `indirect_call_fib_via_local` | 810,879 | 57,000 | 14x |
| `closure_counter_loop` | 17,983 | 1,059 | 17x |
| `typed_array_sum` | 34,059 | 2,780 | 12x |

Loop-anchored trace compilation lets hot loop bodies run in native code between safepoints instead of deoptimizing on every iteration. Recursive function references through locals can also remain native when guards succeed.

### JIT Coordination Workloads

`BenchmarkJITIssue101` tracks a LightGBM-style branchy batch path with many tiny tree-score functions called over one mutable `f64` feature row. It is sensitive to per-tick coordination overhead after a trace is already installed.

| Workload | Mode | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| `branchy_batch_tree_evaluation` | threaded | 1,568 | 0 | 0 |
| `branchy_batch_tree_evaluation` | baseline CFG tier | 1,386 | 0 | 0 |

The whole-CFG tier is about 12% faster on this workload while preserving zero steady-state allocations.

A tl2g regression model with 30 trees exposed a separate baseline gap: its evaluator is a call-free top-level loop over primitive typed-array constants, so the earlier CFG gate never attempted it and trace execution took about 103,800 ns for one prediction. Allowing call-free module CFGs and fusing constant typed-array reads reduced `BenchmarkPredictSingle/regression` to 2,901–2,914 ns/op. With adaptive batch workers, `BenchmarkPredictBatch/regression/200` measured 131,290–138,331 ns/op (about 656–692 ns/row), 2,960 B/op, and 19 allocs/op on Apple M4 Pro. These numbers include tl2g's output allocation, pool coordination, row copying, and objective dispatch.

On x86-64, JIT is not implemented yet. The runtime falls back to threaded execution.

## Methodology

- Threaded interpreter, lifecycle, marshal, and traversal benchmarks use `-benchtime=1s`.
- Cross-runtime and JIT coverage benchmarks use `-benchtime=2s`.
- Fusion comparisons use `-benchmem -count=10`; compare before and after output with `benchstat`.
- `BenchmarkInterpreter_Run/threaded` always runs with `WithThreshold(-1)`, so it measures the pure threaded interpreter.
- The `minivm JIT` rows use default `interp.New`, which enables ARM64 trace compilation.
- `Interpreter.Reset()` is called between iterations.
- `interp.New()` is called once outside the timed loop.
- Cross-runtime benchmarks live in the separate `benchmarks/` Go module.
- wazero uses its default compiler runtime, with module instantiation excluded from timing.

Cross-runtime library versions:

- wazero v1.12.0
- gopher-lua v1.1.2
- tengo v2.17.0
- goja v0.0.0-20260311135729

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
