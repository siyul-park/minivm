# Benchmarks

Measured performance of minivm on `linux/amd64` (Intel Xeon @ 2.80 GHz, Go 1.26.2).

minivm JIT targets ARM64 only; all results below use the threaded interpreter unless noted.

Run benchmarks yourself:

```bash
make benchmark                              # all packages
go test -bench="BenchmarkFibo20" -benchmem ./interp/...
go test -bench="BenchmarkInterpreter_Run" -benchmem ./interp/...
```

## Fibonacci(20) — Cross-Runtime Comparison

Recursive `fib(20)` returning `6765`. Measures end-to-end execution per call.

| Runtime | ns/op | B/op | allocs/op | vs native Go | execution model |
|---|---|---|---|---|---|
| native Go | 37,968 | 0 | 0 | 1× | compiled |
| wazero | 62,219 | 16 | 2 | 1.6× | WASM JIT (compiler runtime) |
| **minivm threaded** | **1,157,136** | **0** | **0** | **30×** | **threaded interpreter** |
| tengo | 2,000,364 | 319,474 | 28,657 | 53× | tree-walking + bytecode |
| gopher-lua | 2,942,015 | 703 | 2 | 77× | register-based VM |
| goja | 3,964,702 | 3,643 | 39 | 104× | AST + bytecode |

Among **threaded/bytecode interpreters without JIT**, minivm allocates zero heap per call and outperforms tengo (~1.7×), gopher-lua (~2.5×), and goja (~3.4×).

wazero is faster because it JIT-compiles WebAssembly to native x86-64 at module instantiation time, paying a one-time compile cost. minivm's threaded interpreter closes this gap on ARM64 once JIT promotes hot segments.

Source: `interp/compare_bench_test.go · BenchmarkFibo20`

## Instruction Throughput — Threaded Interpreter

Single-instruction or short programs on a fresh `Interpreter.Run` + `Reset` cycle.
Programs include setup instructions (constants, arithmetic to build inputs) so totals
reflect realistic opcode dispatch rather than isolated opcode cost.

### Stack

| Program | ns/op | B/op | allocs/op |
|---|---|---|---|
| `nop` | 16 | 0 | 0 |
| `i32.const` + `drop` | 22 | 0 | 0 |
| `i32.const` + `dup` | 22 | 0 | 0 |
| `i32.const` + `i32.const` + `swap` | 23 | 0 | 0 |

### Control Flow

| Program | ns/op | B/op | allocs/op |
|---|---|---|---|
| `br` (unconditional) | 20 | 0 | 0 |
| `br_if` (taken) | 23 | 0 | 0 |
| `br_table` | 24 | 0 | 0 |
| `select` | 26 | 0 | 0 |

### Calls

| Program | ns/op | B/op | allocs/op |
|---|---|---|---|
| bytecode function call | 26 | 0 | 0 |
| bytecode call with `return` | 29 | 0 | 0 |
| host function call | 36 | 8 | 1 |

Host function calls allocate a `[]Boxed` slice for parameters and returns.

### Globals

| Program | ns/op | B/op | allocs/op |
|---|---|---|---|
| `global.set` | 25 | 0 | 0 |
| `global.tee` | 24 | 0 | 0 |
| `global.set` + `global.get` | 29 | 0 | 0 |

### Locals

| Program | ns/op | B/op | allocs/op |
|---|---|---|---|
| call → `local.set` | 30 | 0 | 0 |
| call → `local.tee` | 31 | 0 | 0 |
| call → `local.set` + `local.get` | 34 | 0 | 0 |

### Ref Operations

| Program | ns/op | B/op | allocs/op |
|---|---|---|---|
| `ref.null` | 16 | 0 | 0 |
| `ref.test` | 23 | 0 | 0 |
| `ref.cast` | 28 | 0 | 0 |
| `ref.is_null` | 23 | 0 | 0 |
| `ref.eq` | 27 | 0 | 0 |

### i32 Arithmetic

| Opcode | ns/op | B/op | allocs/op |
|---|---|---|---|
| `i32.const` | 17 | 0 | 0 |
| `i32.add` | 21 | 0 | 0 |
| `i32.sub` | 21 | 0 | 0 |
| `i32.mul` | 21 | 0 | 0 |
| `i32.div_s` | 21 | 0 | 0 |
| `i32.div_u` | 21 | 0 | 0 |
| `i32.rem_s` | 22 | 0 | 0 |
| `i32.rem_u` | 22 | 0 | 0 |
| `i32.shl` | 21 | 0 | 0 |
| `i32.shr_s` | 21 | 0 | 0 |
| `i32.shr_u` | 21 | 0 | 0 |
| `i32.xor` | 21 | 0 | 0 |
| `i32.and` | 20 | 0 | 0 |
| `i32.or` | 21 | 0 | 0 |
| `i32.eqz` | 22 | 0 | 0 |
| `i32.eq` | 21 | 0 | 0 |
| `i32.ne` | 21 | 0 | 0 |
| `i32.lt_s` | 21 | 0 | 0 |
| `i32.gt_s` | 21 | 0 | 0 |
| `i32.le_s` | 20 | 0 | 0 |
| `i32.ge_s` | 21 | 0 | 0 |

All i32 arithmetic and comparison instructions: **~20–22 ns/op, 0 allocs**.

### i64 Arithmetic

| Opcode | ns/op | B/op | allocs/op |
|---|---|---|---|
| `i64.const` | 17 | 0 | 0 |
| `i64.add` | 21 | 0 | 0 |
| `i64.sub` | 21 | 0 | 0 |
| `i64.mul` | 21 | 0 | 0 |
| `i64.div_s` | 21 | 0 | 0 |
| `i64.eqz` | 22 | 0 | 0 |
| `i64.eq` | 22 | 0 | 0 |
| `i64.lt_s` | 22 | 0 | 0 |
| `i64.le_s` | 22 | 0 | 0 |

### f32 / f64 Arithmetic

| Opcode | ns/op | B/op | allocs/op |
|---|---|---|---|
| `f32.const` | 17 | 0 | 0 |
| `f32.add` | 21 | 0 | 0 |
| `f32.mul` | 21 | 0 | 0 |
| `f32.div` | 21 | 0 | 0 |
| `f32.eq` | 21 | 0 | 0 |
| `f64.const` | 17 | 0 | 0 |
| `f64.add` | 21 | 0 | 0 |
| `f64.mul` | 21 | 0 | 0 |
| `f64.eq` | 22 | 0 | 0 |

### Heap Objects

Heap objects allocate on every `Run` because the interpreter re-executes from scratch; allocation cost is per-program-execution.

#### Arrays

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `array.new` (i32, len=1) | 96 | 24 | 2 |
| `array.new_default` (i32, len=1) | 88 | 24 | 2 |
| `array.get` (i32) | 88 | 24 | 2 |
| `array.set` (i32) | 98 | 28 | 2 |
| `array.fill` (i32) | 102 | 28 | 2 |
| `array.get` ([]ref) | 137 | 48 | 4 |
| `array.len` (i32, len=3) | 104 | 40 | 2 |
| `array.len` ([]ref, len=4) | 176 | 96 | 4 |

Ref-type arrays allocate an extra heap object per element.

#### Structs

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `struct.new` (i32) | 107 | 48 | 3 |
| `struct.new_default` (i32) | 96 | 48 | 3 |
| `struct.get` (i32) | 140 | 48 | 4 |
| `struct.set` (i32) | 138 | 48 | 4 |

#### Maps

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `map.new` (1 entry) + `map.len` | 420 | 480 | 3 |
| `map.new_default` + `map.len` | 135 | 64 | 2 |
| `map.get` (i32 key) | 472 | 480 | 3 |
| `map.set` | 406 | 480 | 3 |
| `map.delete` + `map.len` | 509 | 480 | 3 |
| `map.clear` + `map.len` | 534 | 480 | 3 |

`map.new` with an initial entry is expensive relative to `map.new_default` due to map insertion cost and larger allocation.

### Recursive Programs

| Program | ns/op | B/op | allocs/op |
|---|---|---|---|
| `fib(20)` — i32 recursive | 1,157,136 | 0 | 0 |
| `factorial(10)` — i64 recursive | 556 | 0 | 0 |

`fib(20)` triggers 21,891 recursive calls; `factorial(10)` is tail-call reducible and terminates early via `br_if`.

## JIT Mode (x86_64 — Threaded Fallback)

JIT compilation targets ARM64 only. On x86_64, `WithTick(1)` + `WithThreshold(1)` mode falls back to threaded execution with per-instruction context polling, adding overhead to each dispatch compared to the default tick=128 cadence.

`fib(20)` in JIT-mode settings on x86_64: **4,093 µs/op** vs **1,157 µs/op** default — the tick=1 overhead dominates for deep-recursive workloads.

On ARM64, JIT-compiled segments eliminate threaded dispatch overhead for hot numeric loops; native segments run at hardware speed.

## Methodology

- Benchmarks run via `go test -bench -benchmem` with `-benchtime=1s` (threaded suite) and `-benchtime=5s` (cross-runtime comparison).
- `Interpreter.Reset()` called between iterations to reuse allocated state.
- Each sub-benchmark measures one program execution: compile-time is amortized across `New()` outside the timed loop.
- Host function benchmark includes `[]Boxed` allocation inherent to the host call ABI.
- Cross-runtime versions: goja v0.0.0-20260311135729, tengo v2.17.0, gopher-lua v1.1.2, wazero v1.11.0.
- wazero uses its default compiler runtime (JIT to native x86-64); module instantiation cost is excluded from the timed loop.
- Native Go baseline uses the same recursive algorithm without memoization.
- Native Go baseline uses the same recursive algorithm without memoization.
