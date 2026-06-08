# Benchmarks

Unless a section names another environment: `darwin/arm64`, Apple M4 Pro (12 cores), Go 1.26.2.
minivm JIT targets ARM64 only. On this machine a default `interp.New` interpreter promotes hot
numeric segments to native code, so the cross-runtime number below reflects the JIT. The
instruction-throughput tables force the JIT off with `WithThreshold(-1)`, so they reflect the pure
threaded interpreter.

```bash
# Full suite (interpreter + cross-runtime)
make benchmark

# Pure threaded interpreter only (JIT disabled)
go test -run="-" -bench="BenchmarkInterpreter_Run/threaded" -benchmem ./interp/...

# Cross-runtime comparison (benchmarks/ module)
cd benchmarks && go test -run="-" -bench="BenchmarkFib35" -benchmem -benchtime=5s ./...
```

---

## Cross-Runtime Comparison — fib(35)

Recursive `fib(35)` (= 9,227,465), 29.8M recursive calls. End-to-end per call, no memoization. Source: `benchmarks/fib_test.go` (`-benchtime=5s`). minivm is measured twice from the same program: **interp** is the pure threaded interpreter (`WithThreshold(-1)`), **JIT** is the default `interp.New`, which on ARM64 promotes the hot recursive segment to native code.

| Runtime | ns/op | B/op | allocs/op | vs native Go | execution model |
|---|---|---|---|---|---|
| native Go | 18,912,368 | 0 | 0 | 1× | compiled |
| wazero | 42,690,476 | 16 | 2 | 2.3× | WASM → native JIT (AOT at load) |
| **minivm (JIT)** | **71,776,495** | **1,727** | **21** | **3.8×** | **threaded interpreter + ARM64 JIT** |
| minivm (interp) | 750,025,292 | 139 | 0 | 40× | threaded interpreter |
| tengo | 1,142,455,492 | 312,798,644 | 39,088,178 | 60× | bytecode VM |
| gopher-lua | 1,422,659,979 | 971,072 | 3,793 | 75× | register VM |
| goja | 2,032,321,458 | 379,440 | 46,376 | 107× | bytecode VM |

The JIT is worth **10× on this workload** — it cuts minivm from 750 ms to 72 ms per call by replacing threaded dispatch over the hot numeric segment with native code.

Among pure interpreters, minivm (interp) leads and is effectively allocation-free: **1.5× faster than tengo, 1.9× than gopher-lua, 2.7× than goja**, at 0 allocs/op where tengo reaches 312 MB and 39M allocs at fib(35). With the JIT on, minivm joins wazero as the only runtimes that reach native code, pulling **16× ahead of tengo, 20× of gopher-lua, 28× of goja**.

The JIT compiles whole functions, not just straight-line numeric segments: a static `const.get; call` (how fib recurses) is fused into a native branch-and-link to the callee's framed entry, and `return` lowers to a native return, so the entire recursion runs in native code. Only indirect calls, host calls, and ref-holding globals stay threaded.

minivm (JIT) still trails wazero by 1.7×, but not because of threaded calls — it is the bookkeeping wazero does without. minivm keeps the interpreter's NaN-boxed value representation (tag/mask per op) instead of raw native integers, and every fused call guards a frame-budget check and records a deopt-journal entry so a trap can rebuild interpreter frames. wazero AOT-compiles to unboxed native code with no fallback path. That per-run JIT bookkeeping is also why the JIT path allocates a small fixed amount (21 allocs/op) where the pure interpreter does not — the cost is per run, not per call.

---

## Threaded Interpreter — Instruction Throughput

Each row is one complete `Interpreter.Run` + `Reset` cycle. Setup instructions are included, so numbers reflect real dispatch overhead, not isolated opcode cost. Benchmarked via `BenchmarkInterpreter_Run/threaded` (`-benchtime=1s`). The `/threaded` benchmark reuses the existing test-program corpus with `WithThreshold(-1)`, so no hot segment is promoted to JIT code during measurement.

### Scalar operations

All primitive arithmetic and comparison instructions — i32, i64, f32, f64 — dispatch in **~11–13 ns, 0 allocs**.

| Operation | ns/op |
|---|---|
| `nop` | 8 |
| `i32.const` / `i64.const` / `f32.const` / `f64.const` | 9 |
| i32 arithmetic: `add` `sub` `mul` `div` `rem` | ~11 |
| i32 bitwise: `shl` `shr` `and` `or` `xor` | ~11 |
| i32 comparison: `eq` `ne` `lt` `gt` `le` `ge` `eqz` | ~11 |
| i64 arithmetic / comparison | ~12–13 |
| f32 / f64 arithmetic / comparison | ~11 |
| type conversion (`i32.to_i64`, `f64.to_i32`, …) | ~11–12 |

### Control flow

| Operation | ns/op |
|---|---|
| `br` (unconditional) | 10 |
| `br_if` | 14 |
| `br_table` | 14 |
| `select` | 16 |

### Variables

| Operation | ns/op |
|---|---|
| `global.set` | 13 |
| `global.tee` | 13 |
| `global.set` + `global.get` | 16 |
| call → `local.set` | 19 |
| call → `local.tee` | 19 |
| call → `local.set` + `local.get` | 22 |

### Function calls

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| bytecode call | 15 | 0 | 0 |
| bytecode call + `return` | 16 | 0 | 0 |
| host function call | 18 | 8 | 1 |
| closure call | 62 | 56 | 3 |

Host calls allocate one `[]Boxed` slice per call for parameter and return passing. A closure call additionally allocates the closure object and its captured upvalues.

### Ref operations

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `ref.null` | 9 | 0 | 0 |
| `ref.test` (integer ref) | 12 | 0 | 0 |
| `ref.cast` (integer ref) | 13 | 0 | 0 |
| `ref.new` + `ref.get` | 22 | 0 | 0 |
| `ref.new` + `ref.set` + `ref.get` | 31 | 0 | 0 |
| `ref.is_null` (string ref) | 41 | 16 | 1 |
| `ref.eq` / `ref.ne` (string ref) | 49 | 16 | 1 |

`ref.is_null` and `ref.eq` are benchmarked against a boxed string constant; the added cost is the `const.get` load rather than the ref check itself.

### Strings

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `string.len` | 54 | 16 | 1 |
| `string.eq` / `string.ne` | ~66 | 16 | 1 |
| `string.lt` / `string.gt` / `string.le` / `string.ge` | ~100 | 32 | 2 |
| `string.encode_utf32` | 81 | 56 | 3 |
| `string.new_utf32` | 129 | 80 | 5 |
| `string.concat` | 138 | 56 | 4 |

### Heap objects

Allocations happen on every `Run` since the interpreter re-executes from scratch each call.

**Arrays** — all kinds dispatch in 2 allocs/op; `B/op` varies by element type (i8 ≈ 25, i32 ≈ 28, i64/f64 ≈ 32, ref ≈ 40).

| Operation | ns/op | allocs/op |
|---|---|---|
| `array.new_default` | ~30 | 2 |
| `array.new` | ~32 | 2 |
| `array.get` | ~39 | 2 |
| `array.set` | ~41 | 2 |
| `array.fill` | ~43 | 2 |
| `array.copy` | 44 | 2 |
| `array.len` | ~38–44 | 2 |

**Structs** — `B/op` = 64, 1 alloc/op across all field kinds.

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `struct.new_default` | 25 | 64 | 1 |
| `struct.new` | 29 | 64 | 1 |
| `struct.get` | ~36 | 64 | 1 |
| `struct.set` | ~39 | 64 | 1 |

**Maps**

| Operation | ns/op | B/op | allocs/op |
|---|---|---|---|
| `map.new_default` + `map.len` | 55 | 72 | 2 |
| `map.new` (1 entry) + `map.len` | 87 | 216 | 3 |
| `map.get` / `map.lookup` (i32) | ~92 | 216 | 3 |
| `map.set` | 83 | 216 | 3 |
| `map.delete` + `map.len` | 114 | 216 | 3 |
| `map.clear` + `map.len` | 139 | 216 | 3 |

`map.new` with an initial entry is ~1.6× more expensive than `map.new_default` due to upfront insertion. String- and i64-valued maps add a second pair of allocations (≈6 allocs/op) for key/value boxing.

### Heap lifecycle and traversal

Lifecycle benchmarks use public heap APIs and include forced cyclic GC (`BenchmarkInterpreter_Alloc` / `BenchmarkInterpreter_Release`, `-benchtime=1s`).

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `Alloc/free_slot_reuse` | 10.1 | 0 | 0 |
| `Alloc/small_heap_cyclic_gc` | 48.1 | 40 | 2 |
| `Release/primitive_struct` | 28.6 | 64 | 1 |
| `Release/ref_array` | 52.7 | 48 | 4 |
| `Release/ref_struct` | 54.4 | 72 | 3 |
| `Release/ref_valued_map` | 155.0 | 224 | 5 |

`Map.Refs()` traversal (`BenchmarkMap_Refs`, `types` package) confirms no-child traversal stays allocation-free; a slice is allocated only after the first child ref:

| Workload | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `no_refs` | 1.0 | 0 | 0 |
| `inline_i64` | 25.1 | 0 | 0 |
| `child_refs` | 32.4 | 8 | 1 |

The same no-child-is-allocation-free property holds for `Array`, `Struct`, and `Map`: 0 allocs/op with no child refs, 1 alloc/op once a child ref is present.

### Marshal

`BenchmarkInterpreter_Marshal` converts ordinary Go values into heap objects (`-benchtime=1s`).

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

### Recursive workloads (threaded, JIT disabled)

| Program | ns/op | B/op | allocs/op |
|---|---|---|---|
| `fib(20)` — i32 recursive | 570,765 | 0 | 0 |
| `factorial(10)` — i64, early exit via `br_if` | 310 | 0 | 0 |

For the deep-recursion comparison at `fib(35)` with the JIT active, see the [Cross-Runtime Comparison](#cross-runtime-comparison--fib35) section above.

---

## JIT on ARM64

On ARM64, JIT compiles hot numeric segments to native code, eliminating threaded dispatch overhead for compute-heavy loops. Threshold defaults to 4096 executed instructions (~32 samples at tick=128); nothing to configure. The [Cross-Runtime Comparison](#cross-runtime-comparison--fib35) isolates its effect — the `minivm (interp)` and `minivm (JIT)` rows run the same program with the JIT off and on, a 10× gap on fib(35).

On x86-64, JIT is not yet implemented. Running with `WithTick(1)` + `WithThreshold(1)` falls back to threaded execution with per-instruction polling — roughly 2× the default-threaded cost for simple dispatch benchmarks.

---

## Methodology

- `-benchtime=1s` for the threaded-interpreter, lifecycle, Marshal, and `Map_Refs` suites; `-benchtime=5s` for the cross-runtime comparison.
- `BenchmarkInterpreter_Run/threaded` runs with `WithThreshold(-1)` on every architecture, so it measures the pure threaded interpreter — the same configuration as the cross-runtime `minivm (interp)` row. The `minivm (JIT)` row uses a default `New`, which promotes hot segments to native code on ARM64.
- `Interpreter.Reset()` called between iterations; `New()` called once outside the timed loop.
- Cross-runtime benchmark code lives in `benchmarks/` (a separate Go module with its own `go.mod`). Run `make benchmark` to execute both suites, or `cd benchmarks && go test ...` for the cross-runtime suite alone.
- wazero uses its default compiler runtime (JIT); module instantiation excluded from timing.
- Cross-runtime library versions: wazero v1.12.0, gopher-lua v1.1.2, tengo v2.17.0, goja v0.0.0-20260311135729.
