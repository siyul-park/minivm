# minivm Benchmarks

Comparisons here are tier-matched: minivm `threaded` is a bytecode interpreter and is
compared against interpreters, while `default` and `jit` promote hot code to native
and are compared against Wazero's compiler backend. Native Go is a reference bound,
not a peer.

**Benchmark summary.** The threaded interpreter remains competitive with other interpreters, while native execution provides the largest gains on tight arithmetic, loops, and indexed memory access. The native tier still trails Wazero on most shared kernels.

| Kernel | `default` | `threaded` | Wazero |
|---|---:|---:|---:|
| `RecursiveFib(35)` | 59.59 ms | 415.3 ms | 44.4 ms |
| `RecursiveFib(20)` | 44.39 µs | 318.03 µs | 33.2 µs |

`default` is 7.2x faster than `threaded` at depth 20. Wazero remains faster on this workload.

> **Environment**: Apple M4 Pro - darwin/arm64 - Go 1.26.2.
> **Statistics**: canonical rows use `-benchtime=300ms -count=3` and report the median.

## 1. Controls

| Tier | Control | Meaning |
|---|---|---|
| Interpreter | minivm `threaded` | `WithThreshold(-1)`. Generated threaded execution, JIT disabled. |
| Interpreter | CPython, Tengo, GopherLua, Goja, gpython, Yaegi | Bytecode or AST interpreters with no native code generation. |
| Native | minivm `default` | Default adaptive execution; hot entries promote to the native tier. |
| Native | minivm `jit` | `WithThreshold(0)`. Compile on the first hot event. |
| Native | Wazero | WebAssembly runtime using its optimizing compiler backend on arm64. |
| Reference | Native Go | The same kernel written directly in Go. A lower bound, not a peer. |

A cell reads `—` when that runtime has no fixture for the operation. Bold marks the
fastest runtime **within its own tier**.

## 2. Reading these results

Read the tables row by row rather than through an aggregate. Each row is an
independent statement with its own tier controls and memory behavior.

Compare within a tier. An interpreter losing to a compiler is not a finding, and
`threaded` beating `default` on a kernel is a statement about promotion policy, not
about interpreters. `ClosureCounter`, `AllocationGraph`, `PermutationFlips`, and
`Fannkuch` currently run fastest under `threaded`.

`NQueens`, `Fannkuch`, `SortStress`, `StringBuild`, `StructTreeWalk`, and
`BinaryTrees` moved because their fixtures were corrected to declare concrete
slot types instead of `any`, which is what lets a container reach the fused
`array.get` / `array.set` / `struct.get` paths. The two tree kernels also
dropped a `ref.cast` that existed only to work around the `any` declaration.
The interpreter did not change; the kernels simply started exercising a path a
well-typed program always reaches.

Allocation behavior is a first-class result. `IterativeFib`, `TypedArraySum`,
`BranchTree`, `Mandelbrot`, `RecursiveFib`, and `IndirectRecursiveFib` hold 0 B/op and
0 allocs/op. CPython's `B/op` and `allocs/op` measure only the Go side of the
comparison harness, not CPython's own allocator, so compare CPython on `ns/op` alone.

For optimization work, reproduce the row, inspect the owning hot path, make the
smallest change, rerun the same command, and compare medians for all three metrics
before making a performance claim.

## 3. Canonical VM operations

### Control

#### `IterativeFib(30)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 506.7 ns | 0 | 0 |
|  | Tengo | 9.27 µs | 90,592 | 61 |
|  | GopherLua | 651.0 ns | 160 | 0 |
|  | Goja | 2.21 µs | 368 | 20 |
|  | gpython | 2.57 µs | 2,448 | 88 |
|  | Yaegi | 2.84 µs | 2,036 | 101 |
| Native | minivm `default` | 36.0 ns | 0 | 0 |
|  | minivm `jit` | 35.8 ns | 0 | 0 |
|  | Wazero | 52.8 ns | 8 | 1 |
| Reference | Native Go | 9.6 ns | 0 | 0 |
#### `Sieve(256)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 7.77 µs | 1,048 | 2 |
|  | Tengo | 57.17 µs | 122,504 | 1,611 |
|  | GopherLua | 22.85 µs | 18,416 | 44 |
|  | Goja | 43.29 µs | 1,872 | 25 |
|  | gpython | 35.62 µs | 5,704 | 30 |
|  | Yaegi | 18.92 µs | 1,800 | 37 |
| Native | minivm `default` | 1.34 µs | 1,048 | 2 |
|  | minivm `jit` | 1.38 µs | 1,048 | 2 |
|  | Wazero | **677.0 ns** | 8 | 1 |
| Reference | Native Go | 268.6 ns | 0 | 0 |

### Calls
#### `RecursiveFib(20)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 318.03 µs | 0 | 0 |
|  | CPython | 562.79 µs | 26 | 0 |
|  | Tengo | 930.21 µs | 319,347 | 28,655 |
|  | GopherLua | 1.07 ms | 704 | 2 |
|  | Goja | 1.52 ms | 4,680 | 39 |
|  | gpython | 3.89 ms | 9,807,919 | 109,494 |
|  | Yaegi | 4.50 ms | 8,302,177 | 192,840 |
| Native | minivm `default` | **44.39 µs** | 0 | 0 |
|  | minivm `jit` | 44.31 µs | 0 | 0 |
|  | Wazero | **33.20 µs** | 8 | 1 |
| Reference | Native Go | 14.61 µs | 0 | 0 |
#### `IndirectRecursiveFib`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 580.9 µs | 0 | 0 |
|  | Tengo | 944.71 µs | 319,359 | 28,655 |
|  | GopherLua | 941.72 µs | 704 | 2 |
|  | Goja | 1.37 ms | 4,680 | 39 |
|  | gpython | 3.90 ms | 10,158,202 | 109,494 |
|  | Yaegi | 10.98 ms | 13,059,853 | 394,041 |
| Native | minivm `default` | 70.09 µs | 0 | 0 |
|  | minivm `jit` | 69.36 µs | 0 | 0 |
|  | Wazero | **42.34 µs** | 8 | 1 |
| Reference | Native Go | 15.72 µs | 0 | 0 |
#### `ClosureCounter(128)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 2.60 µs | 64 | 2 |
|  | Tengo | 13.59 µs | 92,272 | 261 |
|  | GopherLua | 5.88 µs | 151 | 3 |
|  | Goja | 10.11 µs | 1,264 | 13 |
|  | gpython | 27.29 µs | 58,312 | 659 |
|  | Yaegi | 33.69 µs | 34,784 | 786 |
| Native | minivm `default` | 3.34 µs | 64 | 2 |
|  | minivm `jit` | 3.33 µs | 64 | 2 |
| Reference | Native Go | 34.9 ns | 0 | 0 |
#### `NQueens(7)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 196.49 µs | 120 | 6 |
|  | CPython | 226.15 µs | 11 | 0 |
|  | gpython | 782.30 µs | 363,441 | 4,156 |
| Native | minivm `default` | 104.60 µs | 120 | 6 |
|  | minivm `jit` | 104.69 µs | 120 | 6 |
| Reference | Native Go | 4.21 µs | 0 | 0 |
#### `Fannkuch(6)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 399.53 µs | 34,608 | 1,442 |
|  | CPython | 430.81 µs | 20 | 0 |
|  | gpython | 1.57 ms | 1,367,678 | 16,944 |
| Native | minivm `default` | 402.25 µs | 34,608 | 1,442 |
|  | minivm `jit` | 404.40 µs | 34,608 | 1,442 |
| Reference | Native Go | 17.54 µs | 17,280 | 720 |

### Memory and data structures
#### `TypedArraySum(256)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 3.07 µs | 0 | 0 |
|  | Tengo | 15.83 µs | 94,208 | 513 |
|  | GopherLua | 3.42 µs | 4,000 | 15 |
|  | Goja | 13.29 µs | 2,080 | 238 |
|  | gpython | 7.63 µs | 2,496 | 246 |
|  | Yaegi | 4.23 µs | 296 | 8 |
| Native | minivm `default` | 311.0 ns | 0 | 0 |
|  | minivm `jit` | 306.8 ns | 0 | 0 |
|  | Wazero | **161.0 ns** | 8 | 1 |
| Reference | Native Go | 70.8 ns | 0 | 0 |
#### `AllocationGraph(128)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 4.58 µs | 1,024 | 128 |
|  | Tengo | 13.60 µs | 96,288 | 388 |
|  | GopherLua | 6.45 µs | 14,376 | 256 |
|  | Goja | 25.84 µs | 78,016 | 770 |
|  | gpython | 5.84 µs | 5,712 | 266 |
|  | Yaegi | 13.81 µs | 1,492 | 142 |
| Native | minivm `default` | 4.58 µs | 1,024 | 128 |
|  | minivm `jit` | 4.59 µs | 1,024 | 128 |
| Reference | Native Go | 944.1 ns | 1,024 | 128 |
#### `PermutationFlips(24,64)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 77.92 µs | 14,336 | 128 |
|  | Tengo | 321.12 µs | 292,858 | 9,856 |
|  | GopherLua | **101.03 µs** | 78,808 | 451 |
|  | Goja | 262.24 µs | 122,504 | 765 |
|  | gpython | 228.35 µs | 115,560 | 2,496 |
|  | Yaegi | 174.84 µs | 112,600 | 5,591 |
| Native | minivm `default` | 77.66 µs | 14,336 | 128 |
|  | minivm `jit` | 77.82 µs | 14,336 | 128 |
| Reference | Native Go | 1.08 µs | 0 | 0 |
#### `StructTreeWalk(9)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 137.5 µs | 768 | 8 |
|  | Tengo | 280.54 µs | 458,379 | 5,114 |
|  | GopherLua | 545.66 µs | 818,512 | 11,253 |
|  | Goja | 448.41 µs | 558,961 | 6,149 |
|  | gpython | 1.26 ms | 2,570,669 | 34,797 |
|  | Yaegi | 846.99 µs | 1,422,624 | 35,306 |
| Native | minivm `default` | 133.5 µs | 768 | 8 |
|  | minivm `jit` | 135.6 µs | 768 | 8 |
| Reference | Native Go | 12.97 µs | 16,368 | 1,023 |
#### `BinaryTrees(4..6)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 951.4 µs | 768 | 8 |
|  | CPython | 991.16 µs | 45 | 0 |
|  | gpython | 9.87 ms | 19,457,623 | 280,714 |
| Native | minivm `default` | 749.5 µs | 768 | 8 |
|  | minivm `jit` | 752.7 µs | 768 | 8 |
| Reference | Native Go | 118.71 µs | 201,936 | 8,414 |
#### `SortStress(128,2)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 186.7 µs | 5,136 | 512 |
|  | CPython | 339.27 µs | 16 | 0 |
|  | gpython | 870.26 µs | 23,448 | 2,034 |
| Native | minivm `default` | 51.07 µs | 5,136 | 512 |
|  | minivm `jit` | 66.49 µs | 5,136 | 512 |
| Reference | Native Go | 4.38 µs | 1,024 | 2 |
#### `StringBuild(512)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 335.8 µs | 85,408 | 4,107 |
|  | CPython | 368.74 µs | 17 | 0 |
|  | gpython | 1.25 ms | 2,104,720 | 21,456 |
| Native | minivm `default` | 261.7 µs | 85,408 | 4,107 |
|  | minivm `jit` | 260.6 µs | 85,408 | 4,107 |
| Reference | Native Go | 138.19 µs | 855,892 | 5,001 |

### Numeric
#### `BranchTree(96)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 524.4 ns | 0 | 0 |
|  | Tengo | 16.98 µs | 95,384 | 660 |
|  | GopherLua | 8.56 µs | 2,464 | 9 |
|  | Goja | 13.91 µs | 1,992 | 196 |
|  | gpython | 12.75 µs | 2,168 | 203 |
|  | Yaegi | 10.56 µs | 1,832 | 308 |
| Native | minivm `default` | 266.5 ns | 0 | 0 |
|  | minivm `jit` | 261.6 ns | 0 | 0 |
|  | Wazero | **167.0 ns** | 16 | 1 |
| Reference | Native Go | 78.4 ns | 0 | 0 |
#### `NBody(5,100)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 314.0 µs | 504 | 14 |
|  | CPython | **230.89 µs** | 11 | 0 |
|  | gpython | 1.17 ms | 382,762 | 34,975 |
| Native | minivm `default` | 78.97 µs | 504 | 14 |
|  | minivm `jit` | 82.71 µs | 504 | 14 |
| Reference | Native Go | 3.39 µs | 0 | 0 |
#### `SpectralNorm(24,2)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 281.1 µs | 648 | 6 |
|  | CPython | 415.89 µs | 20 | 0 |
|  | gpython | 1.94 ms | 2,457,137 | 52,718 |
| Native | minivm `default` | 36.51 µs | 648 | 6 |
|  | minivm `jit` | 36.36 µs | 648 | 6 |
| Reference | Native Go | 2.79 µs | 576 | 3 |
#### `Mandelbrot(16x16)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 140.4 µs | 0 | 0 |
|  | CPython | 181.52 µs | 9 | 0 |
|  | gpython | 684.61 µs | 324,977 | 23,643 |
| Native | minivm `default` | 44.01 µs | 0 | 0 |
|  | minivm `jit` | 44.02 µs | 0 | 0 |
| Reference | Native Go | 2.99 µs | 0 | 0 |
#### `MatMul(16)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 176.4 µs | 6,216 | 6 |
|  | CPython | 210.72 µs | 10 | 0 |
|  | gpython | 701.09 µs | 90,712 | 9,350 |
| Native | minivm `default` | 16.61 µs | 6,216 | 6 |
|  | minivm `jit` | 16.58 µs | 6,216 | 6 |
| Reference | Native Go | 2.66 µs | 6,144 | 3 |

## 4. Direct interpreter operations

These measure the cost of a public operation itself. Unlike the workload tables there is no `default`/`threaded`/`jit` axis for an API that has none, so each operation lists its own contrast cases instead.

| Operation | Case | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| `New` | Empty | 4,133 | 35,290 | 28 |
| `New` | Program | 4,376 | 35,368 | 31 |
| `New` | JITEnabled | 4,584 | 35,368 | 31 |
| `Reset` | Scalar | 47.19 | 0 | 0 |
| `Reset` | Heap | 84.72 | 8 | 1 |
| `Reset` | JITState | **38.87** | 0 | 0 |
| `Push` | Scalar | 21.87 | 0 | 0 |
| `Push` | Reference | 100.5 | 16 | 1 |
| `Pop` | — | 18.70 | 0 | 0 |
| `PopBoxed` | — | 19.52 | 0 | 0 |
| `Peek` | — | 2.032 | 0 | 0 |
| `Alloc` | — | 21.10 | 0 | 0 |
| `Retain` | — | 20.48 | 0 | 0 |
| `Release` | — | 20.38 | 0 | 0 |
| `Pool.Get` | Uncontended | 30.64 | 0 | 0 |
| `Pool.Get` | Miss | 3.270 µs | 35,144 | 25 |
| `Pool.Get` | SharedJITMiss | 15.464 µs | 44,760 | 284 |
| `Pool.Get` | ParallelRoundTrip | 327.0 ns | 1 | 0 |
| `Pool.Put` | Uncontended | 136.3 ns | 0 | 0 |
| `StructGetLocalFusion` | — | 227.112 ms | 221,640 | 33 |
| `ArrayGetContainerFusion` | global | 3.619 ms | 16 | 2 |
| `ArrayGetContainerFusion` | upvalue | 3.719 ms | 72 | 4 |

## 5. Reference traversal operations

The `Traceable.Refs` benchmarks append into a caller-owned destination slice. Every traversal case is allocation-free in the current measurement.

| Operation | Case | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| `Array.Refs` | no refs | 3.148 | 0 | 0 |
| `Array.Refs` | child refs | 2.678 | 0 | 0 |
| `TypedMap.Refs` | no refs | 27.66 | 0 | 0 |
| `Map.Refs` | no refs | 2.137 | 0 | 0 |
| `Map.Refs` | child refs | 31.73 | 0 | 0 |
| `Struct.Refs` | no refs | 2.149 | 0 | 0 |
| `Struct.Refs` | child refs | 2.231 | 0 | 0 |

## 6. Interpreter execution primitives

Each `BenchmarkInterpreter_Run` row is the time to execute a whole bytecode program, not the latency of a single opcode. Setup, reset, and result validation stay outside the timer.

| Operation | Threaded | Fused | JITWarm | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|
| `i32.const_nop_returns_i32` | 16.60 ns | 16.82 ns | 23.05 ns | 0 | 0 |
| `i32.const_i32.const_drop_returns_i32` | 20.64 ns | 20.09 ns | 21.94 ns | 0 | 0 |
| `i32.const_dup_returns_i32_i32` | 19.31 ns | 19.36 ns | 21.89 ns | 0 | 0 |
| `i32.const_i32.const_swap_returns_i32_i32` | 20.36 ns | **20.23 ns** | 23.41 ns | 0 | 0 |
| `i32.const_i32.const_i32.const_select_returns_i32` | 21.55 ns | **21.51 ns** | 23.48 ns | 0 | 0 |
| `br_i32.const_i32.const_returns_i32` | 16.99 ns | **16.30 ns** | 22.91 ns | 0 | 0 |
| `i32.const_br_if_i32.const_i32.const_returns_i32` | 20.58 ns | **17.55 ns** | 21.92 ns | 0 | 0 |
| `i32.const_br_table_i32.const_i32.const_returns_i32` | 20.37 ns | **19.74 ns** | 23.68 ns | 0 | 0 |
| `const.get_call_i32.const_return_returns_i32` | 31.67 ns | **21.23 ns** | 23.89 ns | 0 | 0 |
| `const.get_call_i32.const_i32.const_return_returns_i32_i32` | 33.19 ns | **21.47 ns** | 24.69 ns | 0 | 0 |
| `i32.const_const.get_return_call_local.get_i32.const_i32.add_return_returns_i32` | 37.31 ns | **22.88 ns** | — | 0 | 0 |
| `i32.const_yield_reports_yield` | 166.0 ns | 171.8 ns | 214.8 ns | 0 | 0 |
| `const.get_call_through_yield_i32.const_i32.add_return_returns_i32` | 86.24 ns | **84.27 ns** | — | 112 | 1 |
| `const.get_call_coro.done_i32.const_yield_return_returns_i1` | 53.31 ns | **53.12 ns** | — | 112 | 1 |
| `const.get_call_coro.value_i32.const_yield_return_returns_i32` | 64.21 ns | 64.79 ns | — | 112 | 1 |
| `i32.const_global.set_global.get_returns_i32` | 21.08 ns | — | — | 0 | 0 |

## 7. Benchmark interpretation

The main result is the gap between minivm's interpreter and native tiers on
compute-heavy kernels. `IterativeFib`, `Sieve`, `BranchTree`, and similar tight
loops benefit most from native execution.

The interpreter tier remains competitive with other interpreters, while the
native tier still trails Wazero on most shared kernels. Allocation-heavy and
call-heavy workloads remain less favorable because more execution stays in
threaded code.

The tables use a fixed environment and protocol.

Allocation-heavy results are reported with `B/op` and `allocs/op` because memory
behavior is often more informative than execution time alone. CPython allocation
figures reflect only the Go benchmark harness and should not be compared directly.

## 8. Benchmark fixture inventory

| Benchmark | Fixture | Main signal |
|---|---|---|
| `IterativeFib` | n=30 | arithmetic and loops |
| `Sieve` | n=256 | typed-array access |
| `RecursiveFib` | n=20,35 | calls and recursion |
| `IndirectRecursiveFib` | fixed recursive workload | indirect calls |
| `ClosureCounter` | 128 iterations | closures and calls |
| `NQueens` | n=7 | recursive state |
| `Fannkuch` | n=6 | permutation search |
| `TypedArraySum` | 256 elements | indexed loads |
| `AllocationGraph` | depth=128 | allocation and release |
| `PermutationFlips` | size=24, depth=64 | array mutation |
| `StructTreeWalk` | depth=9 | struct access |
| `BinaryTrees` | depth=4..6 | object construction |
| `SortStress` | n=128, rounds=2 | sorting |
| `StringBuild` | 512 tokens | string operations |
| `BranchTree` | 96 nodes | branch-heavy control |
| `NBody` | 5 bodies, 100 steps | f64 arithmetic |
| `SpectralNorm` | n=24, 2 iterations | f64 loops |
| `Mandelbrot` | 16x16 | tight f64 loop |
| `MatMul` | n=16 | f64 multiply-accumulate |

## 9. Methodology

- Inputs and correctness checks are deterministic.
- Setup, verification, warmup, reset, cleanup, and result calculation stay outside the timed operation.
- Canonical comparison tables use `-benchtime=300ms -count=3` and report the median.
- Use interleaved A/B runs with `benchstat` when comparing variants.
- Compare CPython on `ns/op` only; `B/op` and `allocs/op` measure the Go harness.

## 10. Reproduction

```bash
cd benchmarks
go test -run='^$' -bench='^(BenchmarkControl|BenchmarkMemory|BenchmarkNumeric|BenchmarkCall)' \
  -benchmem -benchtime=300ms -count=3 .

# External runtimes
go test -tags=compare -run='^$' -bench='^(BenchmarkControl|BenchmarkMemory|BenchmarkNumeric|BenchmarkCall)' \
  -benchmem -benchtime=300ms -count=3 .
```

## 11. Ownership

| Location | Responsibility |
|---|---|
| `interp/*_test.go` | interpreter and JIT benchmarks |
| `types/*_test.go` | reference traversal benchmarks |
| `benchmarks/` | runtime-neutral workloads and external comparisons |