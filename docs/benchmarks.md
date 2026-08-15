# minivm Benchmarks

Comparisons here are tier-matched: minivm `threaded` is a bytecode interpreter and is
compared against interpreters, while `default` and `jit` promote hot code to native
and are compared against Wazero's compiler backend. Native Go is a reference bound,
not a peer.

**Interpreter tier.** minivm `threaded` is the fastest interpreter measured on 13 of
the 19 canonical kernels. The widest margins are `BranchTree` (13.8x faster than the
next interpreter), `ClosureCounter` (2.34x), `RecursiveFib(20)` (1.81x over CPython),
`StructTreeWalk` (1.77x), `Sieve` (1.66x), and `SpectralNorm` (1.49x over CPython).
It trails on six: CPython leads `NQueens` (1.44x), `NBody` (1.38x), `Fannkuch`
(1.25x), and `BinaryTrees` (1.24x), GopherLua leads `PermutationFlips` (1.14x), and
`StringBuild` is effectively a tie with CPython (1.04x).

**Native tier.** This tier is not yet competitive with Wazero. minivm `default` leads
only `IterativeFib` (1.30x); Wazero is ahead on `Sieve`, `TypedArraySum`,
`BranchTree`, `RecursiveFib(20)`, and `IndirectRecursiveFib`. Note also that `jit`'s
eager policy is slower than adaptive `default` on call-heavy kernels - 382.17 µs
versus 39.60 µs on `RecursiveFib(20)` - so `default` is the tier to quote.

> **Environment**: August 13, 2026 - Apple M4 Pro - darwin/arm64 - Go 1.26.2 - CPython 3.13
>
> **Statistics**: every comparison table below is the median of `-benchtime=300ms -count=3`.
> All runtimes execute the same fixture and pass the same correctness check.

This document does not reduce a runtime to one aggregate score. Each operation is
compared directly against every control that can run it, and `ns/op`, `B/op`, and
`allocs/op` are recorded together.

## 1. Controls

| Tier | Control | Meaning |
|---|---|---|
| Interpreter | minivm `threaded` | `WithThreshold(-1)`. Generated threaded execution, JIT disabled. |
| Interpreter | CPython, Tengo, GopherLua, Goja, gpython, Yaegi | Bytecode or AST interpreters with no native code generation. |
| Native | minivm `default` | Default adaptive execution; hot entries promote to the native tier. |
| Native | minivm `jit` | `WithThreshold(0)`. Eager profiling and compilation policy. |
| Native | Wazero | WebAssembly runtime using its optimizing compiler backend on arm64. |
| Reference | Native Go | The same kernel written directly in Go. A lower bound, not a peer. |

A cell reads `—` when that runtime has no fixture for the operation. Bold marks the
fastest runtime **within its own tier**.

## 2. Reading these results

Read the tables row by row rather than through an aggregate. Each row is an
independent statement with its own tier controls and memory behavior.

Compare within a tier. An interpreter losing to a compiler is not a finding, and
`threaded` beating `default` on a kernel is a statement about promotion policy, not
about interpreters. `ClosureCounter`, `AllocationGraph`, `PermutationFlips`,
`StructTreeWalk`, and `BinaryTrees` currently run fastest under `threaded`.

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
| Interpreter | minivm `threaded` | **510.6 ns** | 0 | 0 |
|  | Tengo | 9.27 µs | 90,592 | 61 |
|  | GopherLua | 651.0 ns | 160 | 0 |
|  | Goja | 2.21 µs | 368 | 20 |
|  | gpython | 2.57 µs | 2,448 | 88 |
|  | Yaegi | 2.84 µs | 2,036 | 101 |
| Native | minivm `default` | **40.5 ns** | 0 | 0 |
|  | minivm `jit` | 51.6 ns | 0 | 0 |
|  | Wazero | 52.8 ns | 8 | 1 |
| Reference | Native Go | 9.6 ns | 0 | 0 |

#### `Sieve(256)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **11.40 µs** | 1,048 | 2 |
|  | Tengo | 57.17 µs | 122,504 | 1,611 |
|  | GopherLua | 22.85 µs | 18,416 | 44 |
|  | Goja | 43.29 µs | 1,872 | 25 |
|  | gpython | 35.62 µs | 5,704 | 30 |
|  | Yaegi | 18.92 µs | 1,800 | 37 |
| Native | minivm `default` | 1.27 µs | 1,048 | 2 |
|  | minivm `jit` | 1.28 µs | 1,048 | 2 |
|  | Wazero | **677.0 ns** | 8 | 1 |
| Reference | Native Go | 268.6 ns | 0 | 0 |

### Calls

#### `RecursiveFib(20)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **310.71 µs** | 0 | 0 |
|  | CPython | 562.79 µs | 26 | 0 |
|  | Tengo | 930.21 µs | 319,347 | 28,655 |
|  | GopherLua | 1.07 ms | 704 | 2 |
|  | Goja | 1.52 ms | 4,680 | 39 |
|  | gpython | 3.89 ms | 9,807,919 | 109,494 |
|  | Yaegi | 4.50 ms | 8,302,177 | 192,840 |
| Native | minivm `default` | 39.60 µs | 0 | 0 |
|  | minivm `jit` | 382.17 µs | 0 | 0 |
|  | Wazero | **33.20 µs** | 8 | 1 |
| Reference | Native Go | 14.61 µs | 0 | 0 |

#### `IndirectRecursiveFib`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **589.99 µs** | 0 | 0 |
|  | Tengo | 944.71 µs | 319,359 | 28,655 |
|  | GopherLua | 941.72 µs | 704 | 2 |
|  | Goja | 1.37 ms | 4,680 | 39 |
|  | gpython | 3.90 ms | 10,158,202 | 109,494 |
|  | Yaegi | 10.98 ms | 13,059,853 | 394,041 |
| Native | minivm `default` | 487.40 µs | 0 | 0 |
|  | minivm `jit` | 695.33 µs | 0 | 0 |
|  | Wazero | **42.34 µs** | 8 | 1 |
| Reference | Native Go | 15.72 µs | 0 | 0 |

#### `ClosureCounter(128)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **2.52 µs** | 64 | 2 |
|  | Tengo | 13.59 µs | 92,272 | 261 |
|  | GopherLua | 5.88 µs | 151 | 3 |
|  | Goja | 10.11 µs | 1,264 | 13 |
|  | gpython | 27.29 µs | 58,312 | 659 |
|  | Yaegi | 33.69 µs | 34,784 | 786 |
| Native | minivm `default` | 3.35 µs | 64 | 2 |
|  | minivm `jit` | **3.32 µs** | 64 | 2 |
| Reference | Native Go | 34.9 ns | 0 | 0 |

#### `NQueens(7)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 325.88 µs | 120 | 6 |
|  | CPython | **226.15 µs** | 11 | 0 |
|  | gpython | 782.30 µs | 363,441 | 4,156 |
| Native | minivm `default` | **154.25 µs** | 120 | 6 |
|  | minivm `jit` | 155.75 µs | 120 | 6 |
| Reference | Native Go | 4.21 µs | 0 | 0 |

#### `Fannkuch(6)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 539.64 µs | 34,608 | 1,442 |
|  | CPython | **430.81 µs** | 20 | 0 |
|  | gpython | 1.57 ms | 1,367,678 | 16,944 |
| Native | minivm `default` | **522.91 µs** | 34,608 | 1,442 |
|  | minivm `jit` | 706.67 µs | 34,608 | 1,442 |
| Reference | Native Go | 17.54 µs | 17,280 | 720 |

### Memory and data structures

#### `TypedArraySum(256)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **3.00 µs** | 0 | 0 |
|  | Tengo | 15.83 µs | 94,208 | 513 |
|  | GopherLua | 3.42 µs | 4,000 | 15 |
|  | Goja | 13.29 µs | 2,080 | 238 |
|  | gpython | 7.63 µs | 2,496 | 246 |
|  | Yaegi | 4.23 µs | 296 | 8 |
| Native | minivm `default` | 314.9 ns | 0 | 0 |
|  | minivm `jit` | 509.3 ns | 0 | 0 |
|  | Wazero | **161.0 ns** | 8 | 1 |
| Reference | Native Go | 70.8 ns | 0 | 0 |

#### `AllocationGraph(128)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **5.27 µs** | 1,024 | 128 |
|  | Tengo | 13.60 µs | 96,288 | 388 |
|  | GopherLua | 6.45 µs | 14,376 | 256 |
|  | Goja | 25.84 µs | 78,016 | 770 |
|  | gpython | 5.84 µs | 5,712 | 266 |
|  | Yaegi | 13.81 µs | 1,492 | 142 |
| Native | minivm `default` | 7.19 µs | 1,024 | 128 |
|  | minivm `jit` | **7.09 µs** | 1,024 | 128 |
| Reference | Native Go | 944.1 ns | 1,024 | 128 |

#### `PermutationFlips(24,64)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 114.89 µs | 14,336 | 128 |
|  | Tengo | 321.12 µs | 292,858 | 9,856 |
|  | GopherLua | **101.03 µs** | 78,808 | 451 |
|  | Goja | 262.24 µs | 122,504 | 765 |
|  | gpython | 228.35 µs | 115,560 | 2,496 |
|  | Yaegi | 174.84 µs | 112,600 | 5,591 |
| Native | minivm `default` | 151.37 µs | 14,336 | 128 |
|  | minivm `jit` | **134.93 µs** | 14,336 | 128 |
| Reference | Native Go | 1.08 µs | 0 | 0 |

#### `StructTreeWalk(9)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **158.75 µs** | 768 | 8 |
|  | Tengo | 280.54 µs | 458,379 | 5,114 |
|  | GopherLua | 545.66 µs | 818,512 | 11,253 |
|  | Goja | 448.41 µs | 558,961 | 6,149 |
|  | gpython | 1.26 ms | 2,570,669 | 34,797 |
|  | Yaegi | 846.99 µs | 1,422,624 | 35,306 |
| Native | minivm `default` | **178.06 µs** | 768 | 8 |
|  | minivm `jit` | 178.08 µs | 768 | 8 |
| Reference | Native Go | 12.97 µs | 16,368 | 1,023 |

#### `BinaryTrees(4..6)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 1.23 ms | 768 | 8 |
|  | CPython | **991.16 µs** | 45 | 0 |
|  | gpython | 9.87 ms | 19,457,623 | 280,714 |
| Native | minivm `default` | 1.35 ms | 768 | 8 |
|  | minivm `jit` | **1.34 ms** | 768 | 8 |
| Reference | Native Go | 118.71 µs | 201,936 | 8,414 |

#### `SortStress(128,2)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **323.05 µs** | 5,136 | 512 |
|  | CPython | 339.27 µs | 16 | 0 |
|  | gpython | 870.26 µs | 23,448 | 2,034 |
| Native | minivm `default` | **73.14 µs** | 5,136 | 512 |
|  | minivm `jit` | 75.04 µs | 5,136 | 512 |
| Reference | Native Go | 4.38 µs | 1,024 | 2 |

#### `StringBuild(512)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 383.91 µs | 85,408 | 4,107 |
|  | CPython | **368.74 µs** | 17 | 0 |
|  | gpython | 1.25 ms | 2,104,720 | 21,456 |
| Native | minivm `default` | 392.13 µs | 85,408 | 4,107 |
|  | minivm `jit` | **287.92 µs** | 85,408 | 4,107 |
| Reference | Native Go | 138.19 µs | 855,892 | 5,001 |

### Numeric

#### `BranchTree(96)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **618.7 ns** | 0 | 0 |
|  | Tengo | 16.98 µs | 95,384 | 660 |
|  | GopherLua | 8.56 µs | 2,464 | 9 |
|  | Goja | 13.91 µs | 1,992 | 196 |
|  | gpython | 12.75 µs | 2,168 | 203 |
|  | Yaegi | 10.56 µs | 1,832 | 308 |
| Native | minivm `default` | 253.3 ns | 0 | 0 |
|  | minivm `jit` | 247.3 ns | 0 | 0 |
|  | Wazero | **167.0 ns** | 16 | 1 |
| Reference | Native Go | 78.4 ns | 0 | 0 |

#### `NBody(5,100)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 319.44 µs | 504 | 14 |
|  | CPython | **230.89 µs** | 11 | 0 |
|  | gpython | 1.17 ms | 382,762 | 34,975 |
| Native | minivm `default` | **100.73 µs** | 504 | 14 |
|  | minivm `jit` | 100.78 µs | 504 | 14 |
| Reference | Native Go | 3.39 µs | 0 | 0 |

#### `SpectralNorm(24,2)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **279.86 µs** | 648 | 6 |
|  | CPython | 415.89 µs | 20 | 0 |
|  | gpython | 1.94 ms | 2,457,137 | 52,718 |
| Native | minivm `default` | **41.84 µs** | 648 | 6 |
|  | minivm `jit` | 41.87 µs | 648 | 6 |
| Reference | Native Go | 2.79 µs | 576 | 3 |

#### `Mandelbrot(16x16)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **144.06 µs** | 0 | 0 |
|  | CPython | 181.52 µs | 9 | 0 |
|  | gpython | 684.61 µs | 324,977 | 23,643 |
| Native | minivm `default` | 49.06 µs | 0 | 0 |
|  | minivm `jit` | **48.84 µs** | 0 | 0 |
| Reference | Native Go | 2.99 µs | 0 | 0 |

#### `MatMul(16)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **176.90 µs** | 6,216 | 6 |
|  | CPython | 210.72 µs | 10 | 0 |
|  | gpython | 701.09 µs | 90,712 | 9,350 |
| Native | minivm `default` | **16.01 µs** | 6,216 | 6 |
|  | minivm `jit` | 15.98 µs | 6,216 | 6 |
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

Within minivm, the strongest signal is the threaded-to-native gap on tight arithmetic:
`IterativeFib` runs 510.6 ns threaded versus 40.5 ns under `default`, and `Sieve`
11.40 µs versus 1.27 µs. That gap does not appear on call-heavy or allocation-heavy
kernels, where `threaded` is often the fastest minivm tier.

In the interpreter tier the six losses cluster by shape. CPython leads `NQueens`,
`NBody`, `Fannkuch`, and `BinaryTrees` - all dominated by allocating and discarding
short-lived objects, where CPython's free lists beat a reference-counted heap on top
of Go's allocator. That is the clearest remaining target, and it is a memory-management
problem rather than a dispatch problem.

In the native tier, Wazero is ahead on five of the six kernels it shares. minivm's
native tier only lowers a subset of opcodes and falls back to threaded execution for
the rest, so kernels touching unlowered opcodes never stay native. `jit`'s eager
policy also compiles paths that adaptive `default` correctly declines, which is why
it loses badly on `RecursiveFib(20)`.

The tables above predate the cooling and retirement rules (`docs/profile.md`,
`docs/jit-internals.md`) and have not been re-measured as a full cross-runtime
sweep since. Two measured effects those rules address, both reproducible with
the commands in section 10:

- A function that can never compile used to keep paying for the attempt. A CPU
  profile of `PermutationFlips/default` attributed 11.9% to `Interpreter.backedge`
  and 10.5% to `Interpreter.trace` with no native code ever installed;
  `StringBuild` performed about 1,020 rejected trace captures per run.
- A capture taken at an unlucky moment could install an entry that exits
  `trace-cut` on half its invocations and never be replaced. `RecursiveFib(35)`
  recorded 29.9M native entries against 14.9M trace-cut exits and ran 2.64x
  slower under `default` than under `threaded`; interleaved `-benchtime=1x
  -count=3` after retirement gives 760 ms versus 676 ms, a 1.13x residual.

The `Sieve` and `MatMul` native rows above were re-measured with this section's
own `-benchtime=300ms -count=3` after loop-header plans stopped carrying blocks
their own root cannot reach (`prune`, see `docs/jit-internals.md`). Every other
row, minivm and cross-runtime alike, is from the earlier sweep.

The before/after evidence for that change is a separate A/B measurement and is
quoted here at its own settings, not the table's: two full 19-kernel sweeps at
`-benchtime=200ms -count=3`, run back to back on one machine so both halves see
the same thermal state, compared with `benchstat`. It puts `Sieve` at 2.04 µs to
1.29 µs and `MatMul` at 22.10 µs to 16.08 µs, every other kernel inside
run-to-run noise, -2.6% geomean. Those figures differ slightly from the table
rows above (1.27 µs and 16.01 µs) because they come from that separate sweep at
a shorter `benchtime`; the table rows are the canonical ones. Only two kernels
move, and both are the ones whose hot function holds more than one loop header.
Reproduce the A/B with:

```bash
cd benchmarks
go test -run='^$' -bench='.' -benchtime=200ms -count=3 . > new.txt
git stash push && go test -run='^$' -bench='.' -benchtime=200ms -count=3 . > base.txt; git stash pop
benchstat base.txt new.txt
```

Allocation results are bounded: object-heavy kernels such as `StructTreeWalk` and
`BinaryTrees` stay at 768 B/op and 8 allocs/op. `StringBuild` holds 85,408 B/op, and
its 4,107 allocs/op come from the per-token UTF-32 array and string cells rather than
from joining.

## 8. Benchmark fixture inventory

| Benchmark | Fixture | Main signal |
|---|---|---|
| `IterativeFib` | n=30 | integer arithmetic, locals, loops, branches |
| `Sieve` | n=256 | typed-array allocation and indexed mutation |
| `RecursiveFib` | n=20,35 | call frames, recursion, returns |
| `IndirectRecursiveFib` | fixed recursive workload | first-class function references |
| `ClosureCounter` | 128 iterations | closure creation, captures, calls |
| `NQueens` | n=7 | recursive backtracking and array state |
| `Fannkuch` | n=6 | recursive permutation search and array slices |
| `TypedArraySum` | 256 elements | indexed loads and accumulation |
| `AllocationGraph` | depth=128 | allocation, linking, traversal, release |
| `PermutationFlips` | size=24, depth=64 | boxed array allocation and mutation |
| `StructTreeWalk` | depth=9 | struct allocation and recursive traversal |
| `BinaryTrees` | depth=4..6 | recursive object construction and checksum |
| `SortStress` | n=128, rounds=2 | integer arithmetic and in-place sorting |
| `StringBuild` | 512 tokens | string allocation, concat, length, UTF-32 encoding |
| `BranchTree` | 96 nodes, input=37 | comparisons and branch-heavy control flow |
| `NBody` | 5 bodies, 100 steps | f64 arithmetic and function calls |
| `SpectralNorm` | n=24, 2 iterations | f64 division and nested loops |
| `Mandelbrot` | 16×16, max_iter=50 | tight f64 loop and early return |
| `MatMul` | n=16 | f64 multiply-accumulate |

## 9. Methodology

- Canonical workloads validate their fixed result/checksum before timing and validate the result after timing.
- Program construction, bytecode verification, JIT warmup, reset, cleanup, and expected-result computation stay outside the measured operation unless they are the benchmark's named operation.
- Inputs are deterministic.
- Canonical comparison tables use `-benchtime=300ms -count=3` and report the median of three sequential samples.
- Direct interpreter/API tables use a `100ms × 3` protocol.
- An *interleaved* A/B measurement runs the two variants back to back on one machine, whole sweep against whole sweep, and compares them with `benchstat`. This machine swings by several percent across minutes, so a before/after claim taken from two runs separated in time is not evidence. Interleaved A/B figures are quoted at their own `benchtime` and are never mixed into the canonical tables.
- Every runtime in a comparison table ran the same fixture in the same command and passed the same correctness check.
- `make benchmark-core` remains the repository-wide smoke command, but the full command includes many non-runtime benchmarks and can exceed interactive command limits.
- `RecursiveFib(35)` is excluded from the comparison tables: at roughly 1.2 s per operation the slower external runtimes make a `count=3` sweep impractical.
- CPython's `B/op` and `allocs/op` reflect only the Go side of the harness, so only its `ns/op` is comparable.
- Cross-runtime comparisons must not be inferred from an incomplete run.

## 10. Reproduction commands

### Canonical VM kernels

```bash
cd benchmarks
# RecursiveFib/35 is excluded to match the published comparison protocol.
go test -run='^$' \
  -bench='^(BenchmarkControl|BenchmarkMemory|BenchmarkNumeric|BenchmarkCall_(ClosureCounter|NQueens|Fannkuch|IndirectRecursiveFib))' \
  -benchmem -benchtime=300ms -count=3 .
go test -run='^$' \
  -bench='^BenchmarkCall_RecursiveFib$/^20$' \
  -benchmem -benchtime=300ms -count=3 .
```

### Recursive Fibonacci

```bash
cd benchmarks
go test -run='^$' \
  -bench='^BenchmarkCall_RecursiveFib/(20|35)$' \
  -benchmem -benchtime=100ms -count=3 ./...
```

### Interpreter/API

```bash
go test -run='^$' \
  -bench='^(BenchmarkNew|BenchmarkInterpreter_(Run|Reset|Push|Pop|PopBoxed|Peek|Alloc|Retain|Release|StructGetLocalFusion|ArrayGetContainerFusion)|BenchmarkPool_(Get|Put))$' \
  -benchmem -benchtime=100ms -count=3 ./interp
```

### Reference traversal

```bash
go test -run='^$' \
  -bench='^Benchmark(Array|Struct|TypedMap|Map)_Refs$' \
  -benchmem -benchtime=100ms -count=3 ./types
```

### External comparison

```bash
cd benchmarks
# RecursiveFib/35 is excluded: at ~1.2 s/op the slower runtimes make count=3 impractical.
go test -tags=compare -run='^$' \
  -bench='^(BenchmarkControl|BenchmarkMemory|BenchmarkNumeric|BenchmarkCall_(ClosureCounter|NQueens|Fannkuch|IndirectRecursiveFib))' \
  -benchmem -benchtime=300ms -count=3 .
go test -tags=compare -run='^$' \
  -bench='^BenchmarkCall_RecursiveFib$/^20$' \
  -benchmem -benchtime=300ms -count=3 .
```

## 11. Ownership

| Location | Responsibility |
|---|---|
| `interp/*_test.go` | interpreter construction, execution, reset, stack/heap, pool, JIT lifecycle |
| `types/*_test.go` | reference traversal contracts |
| `benchmarks/` | runtime-neutral kernel workloads and optional external comparisons |

A benchmark belongs next to the public behavior it measures. Every benchmark fixture should retain a correctness check and deterministic input.
