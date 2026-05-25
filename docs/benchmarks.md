# Benchmarks

Environment: `linux/amd64`, Intel Xeon @ 2.10 GHz, Go 1.26.2.
minivm JIT targets ARM64 only; all numbers below reflect the threaded interpreter unless noted.

```bash
# Full suite (interpreter + cross-runtime)
make benchmark

# Interpreter only
go test -run="-" -bench="BenchmarkInterpreter_Run" -benchmem ./interp/...

# Cross-runtime comparison (benchmarks/ module)
cd benchmarks && go test -run="-" -bench="BenchmarkFib35" -benchmem -benchtime=5s ./...
```

---

## Cross-Runtime Comparison — fib(35)

Recursive `fib(35)` (= 9,227,465), 29.8M recursive calls. End-to-end per call, no memoization. Source: `benchmarks/fib_test.go` (`-benchtime=5s`).

| Runtime | ns/op | B/op | allocs/op | vs native Go | execution model |
|---|---|---|---|---|---|
| native Go | 56,441,552 | 0 | 0 | 1× | compiled |
| wazero | 84,601,941 | 16 | 2 | 1.5× | WASM → native JIT |
| **minivm** | **1,320,092,108** | **244** | **1** | **23×** | **threaded interpreter** |
| tengo | 2,276,648,719 | 312,797,200 | 39,088,175 | 40× | bytecode VM |
| gopher-lua | 3,002,897,021 | 971,008 | 3,793 | 53× | register VM |
| goja | 3,962,089,181 | 380,400 | 46,377 | 70× | bytecode VM |

Among interpreters that don't JIT, minivm leads: **1.7× tengo, 2.3× gopher-lua, 3.0× goja**. Its allocation count stays near zero regardless of recursion depth — tengo reaches 39M allocs at fib(35).

wazero's advantage is structural: it compiles the WebAssembly module to native x86-64 at load time. minivm closes this gap on ARM64, where JIT promotes hot numeric segments to native code.

---

## Threaded Interpreter — Instruction Throughput

Each row is one complete `Interpreter.Run` + `Reset` cycle. Setup instructions are included, so numbers reflect real dispatch overhead, not isolated opcode cost. Benchmarked via `BenchmarkInterpreter_Run/default` (`-benchtime=1s`).

### Scalar operations

All primitive arithmetic and comparison instructions — i32, i64, f32, f64 — dispatch in **~17–25 ns, 0 allocs**.

| Operation | ns/op |
|---|---|
| `nop` | 17 |
| `i32.const` / `i64.const` / `f32.const` / `f64.const` | 17 |
| i32 arithmetic: `add` `sub` `mul` `div` `rem` | ~20 |
| i32 bitwise: `shl` `shr` `and` `or` `xor` | ~20 |
| i32 comparison: `eq` `ne` `lt` `gt` `le` `ge` `eqz` | ~21 |
| i64 arithmetic / comparison | ~22–25 |
| f32 / f64 arithmetic / comparison | ~18–21 |
| type conversion (`i32.to_i64`, `f64.to_i32`, …) | ~19–23 |

### Control flow

| Operation | ns/op |
|---|---|
| `br` (unconditional) | 20 |
| `br_if` | 23 |
| `br_table` | 23 |
| `select` | 26 |

### Variables

| Operation | ns/op |
|---|---|
| `global.set` | 24 |
| `global.tee` | 24 |
| `global.set` + `global.get` | 28 |
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
| `ref.test` (integer ref) | 22 |
| `ref.cast` (integer ref) | 29 |
| `ref.is_null` (string ref) | 81 |
| `ref.eq` / `ref.ne` (string ref) | 93 |

`ref.is_null` and `ref.eq` are benchmarked against a boxed string constant; the added cost is the `const.get` load rather than the ref check itself.

### Strings

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `string.len` | 107 | 16 | 1 |
| `string.eq` / `string.ne` | ~130 | 16 | 1 |
| `string.lt` / `string.gt` / `string.le` / `string.ge` | ~202 | 32 | 2 |
| `string.encode_utf32` | 172 | 56 | 3 |
| `string.concat` | 305 | 56 | 4 |

### Heap objects

Allocations happen on every `Run` since the interpreter re-executes from scratch each call.

**Arrays**

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `array.new_default` (i32, len=1) | 63 | 28 | 2 |
| `array.new_default` (i64/f64, len=1) | 58 | 32 | 2 |
| `array.new_default` (ref, len=1) | 62 | 40 | 2 |
| `array.get` (i32) | 75 | 28 | 2 |
| `array.get` (ref) | 98 | 48 | 3 |
| `array.set` / `array.fill` (i32) | ~83 | 28 | 2 |
| `array.set` / `array.fill` (ref) | ~97–106 | 48 | 3 |
| `array.len` (i32, len=3) | 82 | 40 | 2 |
| `array.len` (ref, len=4) | 117 | 80 | 3 |

**Structs**

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `struct.new_default` | 54 | 64 | 1 |
| `struct.get` | ~92 | 68 | 2 |
| `struct.set` | ~94 | 68 | 2 |

**Maps**

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `map.new_default` + `map.len` | 106 | 72 | 2 |
| `map.new` (1 entry) + `map.len` | 187 | 216 | 3 |
| `map.get` / `map.lookup` | ~185 | 216 | 3 |
| `map.set` | 177 | 216 | 3 |
| `map.delete` | 226 | 216 | 3 |
| `map.clear` | 271 | 216 | 3 |

`map.new` with an initial entry is ~1.8× more expensive than `map.new_default` due to upfront insertion.

### Recursive workloads

| Program | ns/op | B/op | allocs/op |
|---|---|---|---|
| `fib(20)` — i32 recursive | 856,421 | 0 | 0 |
| `factorial(10)` — i64, early exit via `br_if` | 501 | 0 | 0 |

For the deep-recursion comparison at `fib(35)`, see the [Cross-Runtime Comparison](#cross-runtime-comparison--fib35) section above.

---

## JIT on ARM64

On ARM64, JIT compiles hot numeric segments to native code, eliminating threaded dispatch overhead for compute-heavy loops. Threshold defaults to 4096 executed instructions (~32 samples at tick=128); nothing to configure.

On x86-64, JIT is not yet implemented. Running with `WithTick(1)` + `WithThreshold(1)` falls back to threaded execution with per-instruction polling — roughly 2× the default-threaded cost for simple dispatch benchmarks.

---

## Methodology

- `-benchtime=1s` for the threaded-interpreter suite; `-benchtime=5s` for cross-runtime comparison.
- `Interpreter.Reset()` called between iterations; `New()` called once outside the timed loop.
- Cross-runtime benchmark code lives in `benchmarks/` (a separate Go module with its own `go.mod`). Run `make benchmark` to execute both suites, or `cd benchmarks && go test ...` for the cross-runtime suite alone.
- wazero uses its default compiler runtime (JIT); module instantiation excluded from timing.
- Cross-runtime library versions: wazero v1.11.0, gopher-lua v1.1.2, tengo v2.17.0, goja v0.0.0-20260311135729.
