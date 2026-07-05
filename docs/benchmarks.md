# Benchmarks

This document summarizes minivm benchmark results, execution characteristics, and JIT performance.

Unless stated otherwise, benchmarks were run on:

* `darwin/arm64`
* Apple M4 Pro, 12 cores
* Go 1.26.2

minivm currently provides ARM64 JIT support only. On ARM64, the default `interp.New` can compile hot recorded traces, including function entries and loop headers, into native code. Pure interpreter benchmarks disable the JIT with `WithThreshold(-1)`.

## Running Benchmarks

```bash
# Full benchmark suite
make benchmark

# Pure threaded interpreter only
go test -run="-" -bench="BenchmarkInterpreter_Run/threaded" -benchmem ./interp/...

# Cross-runtime comparison
cd benchmarks && go test -run="-" -bench="BenchmarkFib35" -benchmem -benchtime=2s ./...

# ARM64 JIT coverage workloads
cd benchmarks && go test -run="^$" -bench="BenchmarkJITIssue60" -benchmem -benchtime=2s ./...
```

## Summary

The main benchmark findings are:

* minivm’s threaded interpreter is allocation-light and faster than the compared script VMs on recursive numeric workloads.
* The ARM64 JIT significantly improves hot recursive and loop-heavy code.
* On `fib(35)`, the JIT improves minivm by about **13×** over the threaded interpreter.
* The JIT brings minivm close to native-code runtimes such as wazero, while still preserving minivm’s boxed value model and deoptimization support.
* Most scalar instructions in the pure threaded interpreter dispatch in roughly **11–13 ns/op** with zero allocations.

## Cross-Runtime Comparison — `fib(35)`

This benchmark computes recursive `fib(35)` without memoization.

* Result: `9,227,465`
* Recursive calls: about `29.8M`
* Source: `benchmarks/fib_test.go`
* Timing excludes runtime setup where applicable

| Runtime        |          ns/op |        B/op |  allocs/op | vs native Go | Execution model                          |
| -------------- | -------------: | ----------: | ---------: | -----------: | ---------------------------------------- |
| native Go      |     19,324,275 |           0 |          0 |           1× | compiled                                 |
| wazero         |     44,409,757 |          16 |          2 |         2.3× | WASM to native JIT                       |
| **minivm JIT** | **51,911,961** |   **4,918** |     **45** |     **2.7×** | threaded interpreter + tracing ARM64 JIT |
| minivm interp  |    669,343,195 |         288 |          2 |          35× | threaded interpreter                     |
| tengo          |  1,138,199,604 | 312,799,988 | 39,088,179 |          59× | bytecode VM                              |
| gopher-lua     |  1,462,044,917 |     971,008 |      3,793 |          76× | register VM                              |
| goja           |  2,052,722,000 |     383,488 |     46,384 |         106× | bytecode VM                              |

On this workload, the ARM64 JIT reduces minivm execution time from about **669 ms** to **52 ms**. This makes the JIT path about **13× faster** than the threaded interpreter.

Compared with other script VMs, minivm JIT is about **22–40× faster** on this benchmark. The pure threaded interpreter also remains competitive, while staying allocation-light.

minivm JIT is still slower than wazero by about **1.2×**. This is expected: minivm keeps NaN-boxed values and deoptimization state, while wazero compiles WASM to unboxed native code without the same fallback requirements.

## Threaded Interpreter Throughput

The following results measure the pure threaded interpreter with JIT disabled.

Each row represents a full `Interpreter.Run` + `Reset` cycle. Setup instructions are included, so the numbers reflect practical dispatch overhead rather than isolated opcode cost.

### Scalar Operations

Primitive arithmetic and comparison instructions generally dispatch in **~11–13 ns/op** with zero allocations.

| Operation                                                     |  ns/op |
| ------------------------------------------------------------- | -----: |
| `nop`                                                         |      8 |
| constants: `i32.const`, `i64.const`, `f32.const`, `f64.const` |      9 |
| i32 arithmetic, bitwise, comparison                           |    ~11 |
| i64 arithmetic and comparison                                 | ~12–13 |
| f32 / f64 arithmetic and comparison                           |    ~11 |
| numeric conversions                                           | ~11–12 |

### Control Flow

| Operation  | ns/op |
| ---------- | ----: |
| `br`       |    10 |
| `br_if`    |    14 |
| `br_table` |    14 |
| `select`   |    16 |

### Variables

| Operation                        | ns/op |
| -------------------------------- | ----: |
| `global.set`                     |    13 |
| `global.tee`                     |    13 |
| `global.set` + `global.get`      |    16 |
| call → `local.set`               |    19 |
| call → `local.tee`               |    19 |
| call → `local.set` + `local.get` |    22 |

### Function Calls

| Operation                | ns/op | B/op | allocs/op |
| ------------------------ | ----: | ---: | --------: |
| bytecode call            |    15 |    0 |         0 |
| bytecode call + `return` |    16 |    0 |         0 |
| host function call       |    18 |    8 |         1 |
| closure call             |    62 |   56 |         3 |

Bytecode calls are allocation-free. Host calls allocate one `[]Boxed` slice for parameter and return passing. Closure calls also allocate the closure object and captured upvalues.

### References

| Operation                           | ns/op | B/op | allocs/op |
| ----------------------------------- | ----: | ---: | --------: |
| `ref.null`                          |     9 |    0 |         0 |
| `ref.test`                          |    12 |    0 |         0 |
| `ref.cast`                          |    13 |    0 |         0 |
| `ref.new` + `ref.get`               |    22 |    0 |         0 |
| `ref.new` + `ref.set` + `ref.get`   |    31 |    0 |         0 |
| `ref.is_null` with string ref       |    41 |   16 |         1 |
| `ref.eq` / `ref.ne` with string ref |    49 |   16 |         1 |

The string-ref cases include the cost of loading a boxed string constant.

### Strings

| Operation                   | ns/op | B/op | allocs/op |
| --------------------------- | ----: | ---: | --------: |
| `string.len`                |    54 |   16 |         1 |
| `string.eq` / `string.ne`   |   ~66 |   16 |         1 |
| string ordering comparisons |  ~100 |   32 |         2 |
| `string.encode_utf32`       |    81 |   56 |         3 |
| `string.new_utf32`          |   129 |   80 |         5 |
| `string.concat`             |   138 |   56 |         4 |

### Heap Objects

Heap object benchmarks allocate on each `Run`, because the interpreter re-executes the program from scratch for every iteration.

#### Arrays

Array operations generally take **~30–44 ns/op** and use **2 allocations/op**.

| Operation           |  ns/op | allocs/op |
| ------------------- | -----: | --------: |
| `array.new_default` |    ~30 |         2 |
| `array.new`         |    ~32 |         2 |
| `array.get`         |    ~39 |         2 |
| `array.set`         |    ~41 |         2 |
| `array.fill`        |    ~43 |         2 |
| `array.copy`        |     44 |         2 |
| `array.len`         | ~38–44 |         2 |

#### Structs

Struct operations use **1 allocation/op** and about **64 B/op**.

| Operation            | ns/op | B/op | allocs/op |
| -------------------- | ----: | ---: | --------: |
| `struct.new_default` |    25 |   64 |         1 |
| `struct.new`         |    29 |   64 |         1 |
| `struct.get`         |   ~36 |   64 |         1 |
| `struct.set`         |   ~39 |   64 |         1 |

#### Maps

| Operation                          | ns/op | B/op | allocs/op |
| ---------------------------------- | ----: | ---: | --------: |
| `map.new_default` + `map.len`      |    55 |   72 |         2 |
| `map.new` with 1 entry + `map.len` |    87 |  216 |         3 |
| `map.get` / `map.lookup` with i32  |   ~92 |  216 |         3 |
| `map.set`                          |    83 |  216 |         3 |
| `map.delete` + `map.len`           |   114 |  216 |         3 |
| `map.clear` + `map.len`            |   139 |  216 |         3 |

Creating a map with an initial entry is about **1.6×** more expensive than creating an empty map. String- and i64-valued maps require additional boxing and therefore allocate more.

## Heap Lifecycle and Traversal

Lifecycle benchmarks use public heap APIs and include forced cyclic GC.

| Benchmark                    | ns/op | B/op | allocs/op |
| ---------------------------- | ----: | ---: | --------: |
| `Alloc/free_slot_reuse`      |  10.1 |    0 |         0 |
| `Alloc/small_heap_cyclic_gc` |  48.1 |   40 |         2 |
| `Release/primitive_struct`   |  28.6 |   64 |         1 |
| `Release/ref_array`          |  52.7 |   48 |         4 |
| `Release/ref_struct`         |  54.4 |   72 |         3 |
| `Release/ref_valued_map`     | 155.0 |  224 |         5 |

Reference traversal avoids allocation when no child references are present.

| Workload     | ns/op | B/op | allocs/op |
| ------------ | ----: | ---: | --------: |
| `no_refs`    |   1.0 |    0 |         0 |
| `inline_i64` |  25.1 |    0 |         0 |
| `child_refs` |  32.4 |    8 |         1 |

The same behavior applies to arrays, structs, and maps: traversal stays allocation-free when there are no nested references, and allocates once after the first child reference is found.

## Marshal

`BenchmarkInterpreter_Marshal` measures conversion from ordinary Go values into VM values.

| Value                 | ns/op | B/op | allocs/op |
| --------------------- | ----: | ---: | --------: |
| `i32`                 |  39.2 |   80 |         2 |
| `string`              |  49.9 |   96 |         3 |
| `function`            |  64.4 |  144 |         4 |
| `slice_i32`           |  81.5 |  136 |         4 |
| `host_object`         | 139.6 |  324 |         7 |
| `struct_plain`        | 141.8 |  200 |         6 |
| `nested_slice_struct` | 479.2 |  426 |        13 |
| `map_string_i32`      | 547.3 |  712 |        18 |

Simple scalar and string values are inexpensive to marshal. Nested structures and maps are more expensive because they require additional heap objects and reflection work.

## Recursive Workloads

These results use the threaded interpreter with JIT disabled.

| Program                                           |   ns/op | B/op | allocs/op |
| ------------------------------------------------- | ------: | ---: | --------: |
| `fib(20)` — i32 recursive                         | 570,765 |    0 |         0 |
| `factorial(10)` — i64 with early exit via `br_if` |     310 |    0 |         0 |

For the deep-recursion `fib(35)` result with JIT enabled, see the cross-runtime comparison above.

## ARM64 JIT

On ARM64, minivm compiles hot recorded traces to native code. Supported paths include:

* numeric operations
* direct calls
* small guarded indirect function dispatch
* ref-counted locals, globals, and upvalues
* selected read-only heap operations such as `ref.get`, `array.len`, selected `array.get`, selected `struct.get`, and `error.get`
* loops with native back-edges and safepoint polling

Unsupported paths either deoptimize or continue through the threaded interpreter. These include allocation, mutation, host calls, heap-promoted i64 values, and unsupported heap shapes.

The default threshold is `4096` executed instructions, which is about 32 samples at the default tick interval of 128.

### JIT Coverage Workloads

`BenchmarkJITIssue60` tracks key JIT coverage workloads.

| Workload                      | interp ns/op | JIT ns/op | Effect |
| ----------------------------- | -----------: | --------: | -----: |
| `indirect_call_fib_via_local` |      810,879 |    57,000 |    14× |
| `closure_counter_loop`        |       17,983 |     1,059 |    17× |
| `typed_array_sum`             |       34,059 |     2,780 |    12× |

Loop-anchored trace compilation lets hot loop bodies run in native code between safepoints instead of deoptimizing on every iteration. Recursive function references through locals can also remain native when guards succeed.

On x86-64, JIT is not implemented yet. The runtime falls back to threaded execution.

## Methodology

* Threaded interpreter, lifecycle, marshal, and traversal benchmarks use `-benchtime=1s`.
* Cross-runtime and JIT coverage benchmarks use `-benchtime=2s`.
* `BenchmarkInterpreter_Run/threaded` always runs with `WithThreshold(-1)`, so it measures the pure threaded interpreter.
* The `minivm JIT` rows use default `interp.New`, which enables ARM64 trace compilation.
* `Interpreter.Reset()` is called between iterations.
* `interp.New()` is called once outside the timed loop.
* Cross-runtime benchmarks live in the separate `benchmarks/` Go module.
* wazero uses its default compiler runtime, with module instantiation excluded from timing.
* Cross-runtime library versions:

  * wazero v1.12.0
  * gopher-lua v1.1.2
  * tengo v2.17.0
  * goja v0.0.0-20260311135729
