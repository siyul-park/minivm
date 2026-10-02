# Benchmarks

Comparisons here are tier-matched.

This document owns performance evidence; `testing.md` owns test contracts.

minivm `threaded` is a bytecode interpreter and is compared against interpreters. Rows labelled `jit` are the native tier under default options (the automatic `interp.WithThreshold(0)`); `threaded` rows pass `interp.WithThreshold(-1)`. The JIT compiles a hot `*types.Function` to ARM64 native code from an interpreted `CALL`, and compiles a hot loop header to ARM64 native code on-stack (OSR) — module-level loops included — from the interpreter's own threaded dispatch. An operation, call, or terminator `internal/jit/arm64` does not lower bridges into the interpreter or deoptimizes back to threaded execution (see `instruction-set.md` for per-opcode status). `jit` numbers are compared against Wazero's compiler backend; Native Go is a reference bound, not a peer.

| Kernel | `jit` | `threaded` | Wazero |
|---|---:|---:|---:|
| `RecursiveFib(35)` | 45.27 ms | 465.11 ms | 45.43 ms |
| `RecursiveFib(20)` | 33.60 µs | 345.40 µs | 33.68 µs |

> **Methodology**: Apple M4 Pro · darwin/arm64 · Go 1.26.2 · measured 2026-09-27 at commit `e60bedb`, `go test -tags=compare -run='^$' -bench='^(BenchmarkControl|BenchmarkCall|BenchmarkMemory|BenchmarkNumeric)' -benchmem -benchtime=300ms -count=3`, median of 3.

## PR Smoke

`make benchmark-pr` (Apple M4 Pro, darwin/arm64, Go 1.26.2, 2026-09-27). The command uses its default `benchmark-pr-time=100ms`; these are smoke measurements, not canonical comparison rows.

| Kernel | threaded | jit |
|---|---:|---:|
| `IterativeFib(30)` | 527.3 ns/op | 36.12 ns/op |
| `TypedArraySum(256)` | 2,675 ns/op | 183.4 ns/op |
| `BranchTree(96)` | 493.2 ns/op | 65.18 ns/op |
| `RecursiveFib(20)` | 363.6 µs/op | 32.1 µs/op |
| `RecursiveFib(35)` | 452.8 ms/op | 44.1 ms/op |

## Controls

| Tier | Control | Meaning |
|---|---|---|
| Interpreter | minivm `threaded` | Generated threaded execution. |
| Interpreter | CPython, Tengo, GopherLua, Goja, gpython, Yaegi | Bytecode or AST interpreters with no native code generation. |
| Native | minivm `jit` | default options: the JIT compiling to ARM64 native code. |
| Native | Wazero | WebAssembly runtime using its optimizing compiler backend on arm64. |
| Reference | Native Go | The same kernel written directly in Go. A lower bound, not a peer. |

A cell reads `—` when that runtime has no fixture for the operation. Bold marks the
fastest runtime **within its own tier**.

## Reading Results

Comparison `MUST` stay within the same tier. For external runtimes, `B/op` and `allocs/op` describe the Go harness; CPython comparisons use `ns/op` only. A performance change `MUST` reproduce the same row and protocol.

## Canonical VM Operations

### Control

#### `IterativeFib(30)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 501.1 ns | 0 | 0 |
|  | Tengo | 9.97 µs | 90,592 | 61 |
|  | GopherLua | **498.7 ns** | 160 | 0 |
|  | Goja | 2.16 µs | 368 | 20 |
|  | gpython | 2.47 µs | 2,448 | 88 |
|  | Yaegi | 2.80 µs | 2,036 | 101 |
| Native | minivm `jit` | 61.4 ns | 0 | 0 |
|  | Wazero | **49.4 ns** | 8 | 1 |
| Reference | Native Go | 8.9 ns | 0 | 0 |

`jit` is OSR: the loop is module-level code, not a `CALL`-native candidate.

#### `Sieve(256)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **7.92 µs** | 1,048 | 2 |
|  | Tengo | 52.71 µs | 122,504 | 1,611 |
|  | GopherLua | 22.25 µs | 18,416 | 44 |
|  | Goja | 42.59 µs | 1,872 | 25 |
|  | gpython | 34.70 µs | 5,704 | 30 |
|  | Yaegi | 18.02 µs | 1,800 | 37 |
| Native | minivm `jit` | **647.9 ns** | 9,592 | 72 |
|  | Wazero | 657.4 ns | 8 | 1 |
| Reference | Native Go | 241.4 ns | 0 | 0 |

`jit` is OSR; B/op and allocs/op vary sample to sample, same async-compile-timing cause as `NBody/jit` below.

### Calls

#### `RecursiveFib(20)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **345.40 µs** | 0 | 0 |
|  | CPython | 554.26 µs | 30 | 0 |
|  | Tengo | 886.28 µs | 319,345 | 28,655 |
|  | GopherLua | 1.10 ms | 704 | 2 |
|  | Goja | 1.57 ms | 4,680 | 39 |
|  | gpython | 3.87 ms | 9,807,929 | 109,494 |
|  | Yaegi | 4.06 ms | 8,302,125 | 192,840 |
| Native | minivm `jit` | **33.60 µs** | 0 | 0 |
|  | Wazero | 33.68 µs | 8 | 1 |
| Reference | Native Go | 14.57 µs | 0 | 0 |

#### `IndirectRecursiveFib`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **601.10 µs** | 0 | 0 |
|  | Tengo | 921.12 µs | 319,346 | 28,655 |
|  | GopherLua | 931.18 µs | 704 | 2 |
|  | Goja | 1.35 ms | 4,680 | 39 |
|  | gpython | 3.85 ms | 10,158,191 | 109,494 |
|  | Yaegi | 10.84 ms | 13,059,857 | 394,041 |
| Native | minivm `jit` | **39.51 µs** | 0 | 0 |
|  | Wazero | 42.25 µs | 8 | 1 |
| Reference | Native Go | 15.78 µs | 0 | 0 |

#### `TailSum(1000)` and `TailPingPong(1000)`

The only kernels whose bytecode holds a `RETURN_CALL`; every native entry deoptimizes at it (`RETURN_CALL` has no native lowering, see `instruction-set.md`). No external runtimes for this pair.

| Kernel | Tier | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| `TailSum` | minivm `threaded` | 9.78 µs | 0 | 0 |
|  | minivm `jit` | **9.76 µs** | 3,240 | 81 |
| `TailPingPong` | minivm `threaded` | **9.95 µs** | 0 | 0 |
|  | minivm `jit` | 9.96 µs | 3,424 | 73 |

#### `ClosureCounter(128)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **2.60 µs** | 64 | 2 |
|  | Tengo | 13.02 µs | 92,272 | 261 |
|  | GopherLua | 5.79 µs | 151 | 3 |
|  | Goja | 9.85 µs | 1,264 | 13 |
|  | gpython | 26.65 µs | 58,312 | 659 |
|  | Yaegi | 33.01 µs | 34,784 | 786 |
| Native | minivm `jit` | 2.62 µs | 64 | 2 |
| Reference | Native Go | 34.0 ns | 0 | 0 |

#### `NQueens(7)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **202.60 µs** | 120 | 6 |
|  | CPython | 221.82 µs | 12 | 0 |
|  | gpython | 710.35 µs | 363,441 | 4,156 |
| Native | minivm `jit` | 15.05 µs | 26,968 | 223 |
| Reference | Native Go | 4.11 µs | 0 | 0 |

Measured over 50 rounds at `interp.WithThreshold(0)` (`vm_jit_compiles_total`, `vm_jit_exits_total`), at e60bedb:

| Metric | Count |
|---|---:|
| Baseline compiles | 1 |
| Optimized compiles | 1 |
| Bridge exits | 9 |
| Deopt exits | 0 |

#### `Fannkuch(6)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **403.23 µs** | 34,608 | 1,442 |
|  | CPython | 427.60 µs | 22 | 0 |
|  | gpython | 1.45 ms | 1,367,677 | 16,944 |
| Native | minivm `jit` | 279.19 µs | 34,608 | 1,442 |
| Reference | Native Go | 16.98 µs | 17,280 | 720 |

Measured over 50 rounds at `interp.WithThreshold(0)`, at e60bedb:

| Metric | Count |
|---|---:|
| Baseline compiles | 2 |
| Optimized compiles | 3 |
| Bridge exits | 8 |
| Call exits | 16 |
| Release exits | 30,733 |
| Deopt exits | 0 |

#### `I64WideFib(25)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 13.07 ms | 2,913,440 | 364,178 |
| Native | minivm `jit` | 455.01 µs | 16 | 1 |

### Memory and data structures

#### `TypedArraySum(256)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **2.74 µs** | 0 | 0 |
|  | Tengo | 15.25 µs | 94,208 | 513 |
|  | GopherLua | 3.33 µs | 4,000 | 15 |
|  | Goja | 12.88 µs | 2,080 | 238 |
|  | gpython | 7.23 µs | 2,496 | 246 |
|  | Yaegi | 4.11 µs | 296 | 8 |
| Native | minivm `jit` | 204.6 ns | 0 | 0 |
|  | Wazero | **152.8 ns** | 8 | 1 |
| Reference | Native Go | 64.5 ns | 0 | 0 |

`jit` hoists the array's shape guard out of the loop — a quiet loop with no calls or allocations (see `pass-system.md`) — so `array.get` runs natively across iterations without a per-iteration guard.

#### `AllocationGraph(128)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **4.23 µs** | 0 | 0 |
|  | Tengo | 13.69 µs | 96,288 | 388 |
|  | GopherLua | 6.10 µs | 14,376 | 256 |
|  | Goja | 24.77 µs | 78,016 | 770 |
|  | gpython | 5.53 µs | 5,712 | 266 |
|  | Yaegi | 11.87 µs | 1,492 | 142 |
| Native | minivm `jit` | 4.27 µs | 192 | 8 |
| Reference | Native Go | 905.9 ns | 1,024 | 128 |

`jit` and `threaded` are close: this kernel is allocation-bound, not loop-bound.

#### `XorShiftI64(256)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 51.00 µs | 18,424 | 2,046 |
| Native | minivm `jit` | 809.0 ns | 2,088 | 3 |

#### `PermutationFlips(24,64)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **52.97 µs** | 7,680 | 128 |
|  | Tengo | 230.74 µs | 292,857 | 9,856 |
|  | GopherLua | 97.36 µs | 78,808 | 451 |
|  | Goja | 253.57 µs | 122,504 | 765 |
|  | gpython | 226.96 µs | 115,560 | 2,496 |
|  | Yaegi | 172.45 µs | 112,600 | 5,591 |
| Native | minivm `jit` | 14.30 µs | 24,336 | 341 |
| Reference | Native Go | 999.3 ns | 0 | 0 |

`array.new_default` bridges and resumes native execution (see `instruction-set.md`); measured over 50 rounds, one Baseline compile produced 2,214 bridge exits paired with 2,214 release exits and zero deopts. The extra B/op and allocs/op are that bridge's cost, not a threaded fallback.

#### `StructTreeWalk(9)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **142.82 µs** | 768 | 8 |
|  | Tengo | 272.52 µs | 458,380 | 5,114 |
|  | GopherLua | 533.39 µs | 818,513 | 11,253 |
|  | Goja | 438.19 µs | 558,962 | 6,149 |
|  | gpython | 1.22 ms | 2,570,671 | 34,797 |
|  | Yaegi | 837.77 µs | 1,422,625 | 35,306 |
| Native | minivm `jit` | 105.27 µs | 768 | 8 |
| Reference | Native Go | 12.50 µs | 16,368 | 1,023 |

Measured over 50 rounds at `interp.WithThreshold(0)`, at e60bedb:

| Metric | Count |
|---|---:|
| Baseline compiles | 2 |
| Optimized compiles | 2 |
| Bridge exits | 9 |
| Call exits | 8 |
| Release exits | 45 |
| Deopt exits | 0 |

#### `BinaryTrees(4..6)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 985.81 µs | 768 | 8 |
|  | CPython | **985.69 µs** | 53 | 0 |
|  | gpython | 9.51 ms | 19,457,610 | 280,714 |
| Native | minivm `jit` | 737.83 µs | 768 | 8 |
| Reference | Native Go | 112.61 µs | 201,936 | 8,414 |

Measured over 50 rounds at `interp.WithThreshold(0)`, at e60bedb:

| Metric | Count |
|---|---:|
| Baseline compiles | 2 |
| Optimized compiles | 3 |
| Bridge exits | 9 |
| Call exits | 12 |
| Deopt exits | 0 |

#### `SortStress(128,2)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **190.75 µs** | 5,136 | 512 |
|  | CPython | 337.37 µs | 18 | 0 |
|  | gpython | 830.77 µs | 23,448 | 2,034 |
| Native | minivm `jit` | 18.37 µs | 44,552 | 1,258 |
| Reference | Native Go | 3.65 µs | 1,024 | 2 |

`jit`'s `array.get`/`array.set` loop lowers and runs natively; the array's own allocation op bridges to threaded and resumes native execution rather than lowering (`array.new_default`, see `instruction-set.md`), which is the source of the extra B/op and allocs/op.

#### `StringBuild(512)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **345.32 µs** | 85,408 | 4,107 |
|  | CPython | 365.58 µs | 19 | 0 |
|  | gpython | 1.21 ms | 2,104,719 | 21,456 |
| Native | minivm `jit` | 303.65 µs | 85,408 | 4,107 |
| Reference | Native Go | 139.09 µs | 855,891 | 5,001 |

`string.concat` bridges to threaded and resumes native code rather than deopting (`interp.bridgeable`, see `instruction-set.md`), so `jit` lands close to `threaded` with a modest gain from the surrounding loop running natively.

### Numeric

#### `BranchTree(96)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **503.4 ns** | 0 | 0 |
|  | Tengo | 16.98 µs | 95,384 | 660 |
|  | GopherLua | 8.28 µs | 2,464 | 9 |
|  | Goja | 13.48 µs | 1,992 | 196 |
|  | gpython | 11.65 µs | 2,168 | 203 |
|  | Yaegi | 10.43 µs | 1,832 | 308 |
| Native | minivm `jit` | **57.8 ns** | 0 | 0 |
|  | Wazero | 161.3 ns | 16 | 1 |
| Reference | Native Go | 77.9 ns | 0 | 0 |

`BranchTree` is loop-free module code: the Module entry tier compiles it to native at ip 0 (`max(n, 2)` runs, see `jit-internals.md`), rather than through OSR or a `CALL`. That is why `jit` runs near an order of magnitude below `threaded` here instead of tracking it closely.

#### `NBody(5,100)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 312.06 µs | 504 | 14 |
|  | CPython | **228.69 µs** | 13 | 0 |
|  | gpython | 1.14 ms | 382,762 | 34,975 |
| Native | minivm `jit` | 17.59 µs | 112,448 | 1,025 |
| Reference | Native Go | 3.33 µs | 0 | 0 |

`NBody/jit`'s ns/op, B/op, and allocs/op all vary sample to sample because `advance` runs mostly natively but its allocation count depends on whether the async compile queue is still draining during a given sample's timed loop.

#### `SpectralNorm(24,2)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **275.57 µs** | 648 | 6 |
|  | CPython | 412.12 µs | 22 | 0 |
|  | gpython | 1.79 ms | 2,457,137 | 52,718 |
| Native | minivm `jit` | 16.76 µs | 15,184 | 184 |
| Reference | Native Go | 2.75 µs | 576 | 3 |

#### `Mandelbrot(16x16)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **139.51 µs** | 0 | 0 |
|  | CPython | 180.43 µs | 10 | 0 |
|  | gpython | 653.70 µs | 324,978 | 23,643 |
| Native | minivm `jit` | 6.06 µs | 26,112 | 240 |
| Reference | Native Go | 2.92 µs | 0 | 0 |

#### `FNV1a64(1024)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | 52.47 µs | 16,400 | 2,049 |
| Native | minivm `jit` | 1.04 µs | 16 | 2 |

#### `MatMul(16)`

| Tier | Runtime | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Interpreter | minivm `threaded` | **171.53 µs** | 6,216 | 6 |
|  | CPython | 206.51 µs | 12 | 0 |
|  | gpython | 668.55 µs | 90,712 | 9,350 |
| Native | minivm `jit` | 9.31 µs | 21,480 | 99 |
| Reference | Native Go | 2.60 µs | 6,144 | 3 |

`jit` is OSR: the loop is module-level code, not a `CALL`-native candidate; B/op and allocs/op vary sample to sample, same cause as `NBody/jit`.

## Direct Interpreter Operations

These measure the cost of a public operation itself. Unlike the workload tables there is no `threaded`/`jit` axis for an API that has none, so each operation lists its own contrast cases instead.

| Operation | Case | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| `New` | Empty | 4,133 | 35,290 | 28 |
| `New` | Program | 4,376 | 35,368 | 31 |
| `Reset` | Scalar | 47.19 | 0 | 0 |
| `Reset` | Heap | 84.72 | 8 | 1 |
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
| `Pool.Get` | ParallelRoundTrip | 327.0 ns | 1 | 0 |
| `Pool.Put` | Uncontended | 136.3 ns | 0 | 0 |
| `StructGetLocalFusion` | — | 227.112 ms | 221,640 | 33 |
| `ArrayGetContainerFusion` | global | 3.619 ms | 16 | 2 |
| `ArrayGetContainerFusion` | upvalue | 3.719 ms | 72 | 4 |

## Reference Traversal Operations

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

## Interpreter Execution Primitives

Each `BenchmarkInterpreter_Run` row is the time to execute a whole bytecode program, not the latency of a single opcode. Setup, reset, and result validation stay outside the timer.

| Operation | Threaded | Fused | pending-native | B/op | allocs/op |
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

## Interpretation

`jit` rows enter native code three ways: an interpreted `CALL` to a `*types.Function` (Baseline, then Optimized once entries — interpreted or native-to-native — reach the promote threshold), on-stack at a hot loop header (OSR), module-level code included, with no call boundary, compiling directly at Optimized, or at ip 0 for loop-free module code (Module entry, `max(n, 2)` runs, also direct to Optimized). A called function that lowers cleanly runs natively; an unsupported operation, `RETURN_CALL`, or an entry that cannot compile returns to threaded execution and still pays native entry bookkeeping.

Results `MUST` be read by row and tier; unlike tiers `MUST NOT` be aggregated.

## Benchmark Fixture Inventory

| Benchmark | Fixture | Main signal |
|---|---|---|
| `IterativeFib` | n=30 | arithmetic and loops |
| `Sieve` | n=256 | typed-array access |
| `RecursiveFib` | n=20,35 | calls and recursion |
| `IndirectRecursiveFib` | fixed recursive workload | indirect calls |
| `TailSum` | n=1000 | a tail call back to the same function |
| `TailPingPong` | n=1000 | tail calls that morph into another function |
| `ClosureCounter` | 128 iterations | closures and calls |
| `NQueens` | n=7 | recursive state |
| `Fannkuch` | n=6 | permutation search |
| `I64WideFib` | n=25 | wide-i64 recursion |
| `TypedArraySum` | 256 elements | indexed loads |
| `AllocationGraph` | depth=128 | allocation and release |
| `XorShiftI64` | n=256 | i64 bitwise loop |
| `PermutationFlips` | size=24, depth=64 | array mutation |
| `StructTreeWalk` | depth=9 | struct access |
| `BinaryTrees` | depth=4..6 | object construction |
| `SortStress` | n=128, rounds=2 | sorting |
| `StringBuild` | 512 tokens | string operations |
| `BranchTree` | 96 nodes | branch-heavy control |
| `NBody` | 5 bodies, 100 steps | f64 arithmetic |
| `SpectralNorm` | n=24, 2 iterations | f64 loops |
| `Mandelbrot` | 16x16 | tight f64 loop |
| `FNV1a64` | n=1024 | i64 hash loop |
| `MatMul` | n=16 | f64 multiply-accumulate |

## Methodology

Canonical measurements `MUST`:

- use deterministic inputs and correctness checks;
- exclude setup, verification, warmup, reset, cleanup, and result calculation from timing;
- use `-benchtime=300ms -count=3` and report the median;
- use interleaved A/B runs with `benchstat` for variants;
- compare CPython on `ns/op` only; `B/op` and `allocs/op` belong to the Go harness.

## Reproduction

```bash
cd benchmarks
go test -run='^$' -bench='^(BenchmarkControl|BenchmarkMemory|BenchmarkNumeric|BenchmarkCall)' \
  -benchmem -benchtime=300ms -count=3 .

# External runtimes
go test -tags=compare -run='^$' -bench='^(BenchmarkControl|BenchmarkMemory|BenchmarkNumeric|BenchmarkCall)' \
  -benchmem -benchtime=300ms -count=3 .
```

## Ownership

| Location | Responsibility |
|---|---|
| `interp/*_test.go` | interpreter benchmarks (`threaded` only; no native-tier benchmarks live here) |
| `types/*_test.go` | reference traversal benchmarks |
| `benchmarks/` | runtime-neutral kernel workloads (`threaded` and `jit`) and external comparisons |

## Related

- `profile.md`
- `testing.md`
- `roadmap.md`
