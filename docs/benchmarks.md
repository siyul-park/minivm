# Benchmarks

Environment: `linux/amd64`, Intel Xeon @ 2.80 GHz, Go 1.26.2.
minivm JIT targets ARM64 only; all numbers below reflect the threaded interpreter.

```bash
make benchmark
go test -bench="BenchmarkInterpreter_Run" -benchmem ./interp/...
```

---

## Cross-Runtime Comparison — fib(35)

Recursive `fib(35)` (= 9,227,465), 29.8M recursive calls. End-to-end per call, no memoization.

| Runtime | ns/op | B/op | allocs/op | vs native Go | execution model |
|---|---|---|---|---|---|
| native Go | 51,947,220 | 0 | 0 | 1× | compiled |
| wazero | 84,807,148 | 16 | 2 | 1.6× | WASM → native JIT |
| **minivm** | **1,672,707,295** | **288** | **1** | **32×** | **threaded interpreter** |
| tengo | 2,665,298,176 | 312,800,180 | 39,088,180 | 51× | bytecode VM |
| gopher-lua | 4,081,167,978 | 971,008 | 3,793 | 79× | register VM |
| goja | 5,427,175,850 | 383,488 | 46,384 | 105× | bytecode VM |

Among interpreters that don't JIT, minivm leads: **1.6× tengo, 2.4× gopher-lua, 3.2× goja**. Its allocation count stays near zero regardless of recursion depth — tengo reaches 39M allocs at fib(35).

wazero's advantage is structural: it compiles the WebAssembly module to native x86-64 at load time. minivm closes this gap on ARM64, where JIT promotes hot numeric segments to native code.

---

## Threaded Interpreter — Instruction Throughput

Each row is one complete `Interpreter.Run` + `Reset` cycle. Setup instructions are included, so numbers reflect real dispatch overhead, not isolated opcode cost.

### Scalar operations

All primitive arithmetic and comparison instructions — i32, i64, f32, f64 — dispatch in **~20–22 ns, 0 allocs**.

| Operation | ns/op |
|---|---|
| `nop` | 16 |
| `i32.const` / `i64.const` / `f32.const` / `f64.const` | 17 |
| arithmetic: `add` `sub` `mul` `div` `rem` | 21 |
| bitwise: `shl` `shr` `and` `or` `xor` | 21 |
| comparison: `eq` `ne` `lt` `gt` `le` `ge` `eqz` | 21 |
| type conversion (`i32.to_i64`, `f64.to_i32`, …) | 21 |

### Control flow

| Operation | ns/op |
|---|---|
| `br` (unconditional) | 20 |
| `br_if` | 23 |
| `br_table` | 24 |
| `select` | 26 |

### Variables

| Operation | ns/op |
|---|---|
| `global.set` | 25 |
| `global.tee` | 24 |
| `global.set` + `global.get` | 29 |
| call → `local.set` | 30 |
| call → `local.tee` | 31 |
| call → `local.set` + `local.get` | 34 |

### Function calls

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| bytecode call | 26 | 0 | 0 |
| bytecode call + `return` | 29 | 0 | 0 |
| host function call | 36 | 8 | 1 |

Host calls allocate one `[]Boxed` slice per call for parameter and return passing.

### Ref operations

| Operation | ns/op |
|---|---|
| `ref.null` | 16 |
| `ref.test` / `ref.is_null` | 23 |
| `ref.eq` / `ref.ne` | 27 |
| `ref.cast` | 28 |

### Heap objects

Allocations happen on every `Run` since the interpreter re-executes from scratch each call.

**Arrays**

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `array.new_default` (i32, len=1) | 88 | 24 | 2 |
| `array.get` (i32) | 88 | 24 | 2 |
| `array.set` / `array.fill` (i32) | ~100 | 28 | 2 |
| `array.get` ([]ref) | 137 | 48 | 4 |
| `array.len` ([]ref, len=4) | 176 | 96 | 4 |

**Structs**

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `struct.new_default` | 96 | 48 | 3 |
| `struct.get` / `struct.set` | ~139 | 48 | 4 |

**Maps**

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `map.new_default` + `map.len` | 135 | 64 | 2 |
| `map.new` (1 entry) + `map.len` | 420 | 480 | 3 |
| `map.get` / `map.set` | ~440 | 480 | 3 |
| `map.delete` / `map.clear` | ~520 | 480 | 3 |

`map.new` with an initial entry is ~3× more expensive than `map.new_default` due to upfront insertion and a larger backing allocation.

### Recursive workloads

| Program | ns/op | B/op | allocs/op |
|---|---|---|---|
| `fib(35)` — i32 recursive, 29.8M calls | 1,672,707,295 | 288 | 1 |
| `factorial(10)` — i64, early exit via `br_if` | 556 | 0 | 0 |

---

## JIT on ARM64

On ARM64, JIT compiles hot numeric segments to native code, eliminating threaded dispatch overhead for compute-heavy loops. Threshold defaults to 4096 executed instructions (~32 samples at tick=128); nothing to configure.

On x86-64, JIT is not yet implemented. Running with `WithTick(1)` + `WithThreshold(1)` falls back to threaded execution with per-instruction polling — slower than the default tick=128 cadence for deep-recursive workloads.

---

## Methodology

- `-benchtime=1s` for the threaded-interpreter suite; `-benchtime=5s` for cross-runtime comparison.
- `Interpreter.Reset()` called between iterations; `New()` called once outside the timed loop.
- wazero uses its default compiler runtime (JIT); module instantiation excluded from timing.
- Cross-runtime library versions: wazero v1.11.0, gopher-lua v1.1.2, tengo v2.17.0, goja v0.0.0-20260311135729.
