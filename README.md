# minivm

[![CI](https://github.com/siyul-park/minivm/actions/workflows/ci.yml/badge.svg)](https://github.com/siyul-park/minivm/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/siyul-park/minivm/branch/main/graph/badge.svg)](https://codecov.io/gh/siyul-park/minivm)
[![Go Reference](https://pkg.go.dev/badge/github.com/siyul-park/minivm.svg)](https://pkg.go.dev/github.com/siyul-park/minivm)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**Fast bytecode VM that embeds anywhere.**

minivm lets Go programs load tiny bytecode programs, call back into host functions, and run under explicit stack, heap, fuel, and hook limits. It starts as a fast threaded interpreter and promotes hot ARM64 segments to native code automatically.

```bash
go get github.com/siyul-park/minivm
```

> Requires Go 1.26.2+. The VM core depends only on the Go standard library.

## Why minivm

| Need | What minivm gives you |
|---|---|
| Embed runtime behavior | bytecode programs with first-class functions, locals, globals, refs, arrays, structs, and strings |
| Call host code | zero-reflection `HostFunction` path plus `Marshal` / `Unmarshal` for ordinary Go values |
| Keep execution bounded | stack, heap, frame, fuel, context, and hook controls |
| Stay fast before JIT | closure-threaded dispatch with near-zero allocations on recursive workloads |
| Get native speed where it matters | adaptive ARM64 JIT for hot numeric segments |

## Build With It

- **Scripting engines** — execute user-defined logic under your host policy
- **Rule engines** — evaluate complex conditions at runtime without redeployment
- **DSL runtimes** — define a custom instruction set on a proven VM foundation
- **Plugin systems** — run sandboxed bytecode in a GC-managed environment

## Performance

Recursive `fib(35)` — darwin/arm64, Apple M4 Pro, Go 1.26.2. minivm is measured twice: **interp** is the pure threaded interpreter, **JIT** is the default `New`, which promotes the hot recursive segment to native code on ARM64:

| Runtime | ns/op | B/op | allocs/op | vs native Go | execution model |
|---|---|---|---|---|---|
| native Go | 20,120,613 | 0 | 0 | 1× | compiled |
| wazero | 44,474,194 | 16 | 2 | 2.2× | WASM → native JIT |
| **minivm (JIT)** | **62,877,686** | **3,589** | **47** | **3.1×** | **threaded interpreter + ARM64 JIT** |
| minivm (interp) | 670,005,945 | 288 | 2 | 33× | threaded interpreter |
| tengo | 1,139,041,250 | 312,797,912 | 39,088,180 | 57× | bytecode VM |
| gopher-lua | 1,428,272,792 | 971,008 | 3,793 | 71× | register VM |
| goja | 2,022,279,709 | 383,488 | 46,384 | 100× | bytecode VM |

The JIT is worth **10.7× on this workload** (670 ms → 63 ms per call). Among pure interpreters, minivm (interp) leads and is effectively allocation-light: **1.7× faster than tengo, 2.1× gopher-lua, 3.0× goja**, while tengo reaches 312 MB and 39M allocs. With the JIT on, minivm joins wazero as the only runtimes reaching native code, pulling **18–32× ahead** of the script VMs.

minivm's JIT compiles whole functions, not just numeric segments: fib's recursive `const.get; call` fuses into a native branch-and-link to the callee, so the recursion runs entirely in native code. It still trails wazero by 1.4× because of bookkeeping wazero skips — minivm keeps values NaN-boxed and guards each call with a frame-budget check and a deopt-journal record, while wazero AOT-compiles to unboxed native code with no fallback path.

Single-instruction throughput (threaded interpreter, JIT disabled):

| Workload | ns/op |
|---|---|
| i32/i64/f32/f64 arithmetic | ~11–13 |
| branches (`br`, `br_if`) | ~10–14 |
| bytecode function call | ~15–16 |
| host function call | ~18 |
| array / struct operations | ~30–44 |

Full results: [`docs/benchmarks.md`](docs/benchmarks.md)

## Usage

### Execute bytecode

```go
prog := program.New([]instr.Instruction{
    instr.New(instr.I32_CONST, 6),
    instr.New(instr.I32_CONST, 7),
    instr.New(instr.I32_MUL),
})

vm := interp.New(prog)
defer vm.Close()

if err := vm.Run(context.Background()); err != nil {
    log.Fatal(err)
}

result, _ := vm.Pop() // types.I32(42)
```

### Call Go from bytecode

Expose Go code as a bytecode-callable function:

```go
lookup := interp.NewHostFunction(
    &types.FunctionType{
        Params:  []types.Type{types.TypeI32},
        Returns: []types.Type{types.TypeI32},
    },
    func(vm *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error) {
        id := params[0].I32()
        price := db.GetPrice(int(id))
        return []types.Boxed{types.BoxI32(price)}, nil
    },
)

prog := program.New(
    []instr.Instruction{
        instr.New(instr.I32_CONST, 42), // product id
        instr.New(instr.CONST_GET, 0),  // push function
        instr.New(instr.CALL),
    },
    program.WithConstants(lookup),
)
```

Parameters arrive as typed `[]Boxed`: no reflection, no `interface{}` boxing.

### Define reusable functions

Functions are first-class constants built with `FunctionBuilder`:

```go
factorial := types.NewFunctionBuilder(&types.FunctionType{
    Params:  []types.Type{types.TypeI32},
    Returns: []types.Type{types.TypeI32},
}).WithLocals(types.TypeI32).Emit(
    instr.New(instr.LOCAL_GET, 0),
    instr.New(instr.I32_CONST, 1),
    instr.New(instr.I32_LT_S),
    instr.New(instr.BR_IF, 14),     // n < 1 → return 1
    instr.New(instr.LOCAL_GET, 0),
    instr.New(instr.I32_CONST, 1),
    instr.New(instr.I32_SUB),
    instr.New(instr.CONST_GET, 0),
    instr.New(instr.CALL),          // factorial(n-1)
    instr.New(instr.LOCAL_GET, 0),
    instr.New(instr.I32_MUL),       // n * factorial(n-1)
    instr.New(instr.RETURN),
    instr.New(instr.I32_CONST, 1),
    instr.New(instr.RETURN),
).Build()
```

### Optimize before running

Fold constants and strip dead branches before the VM sees them:

```go
prog, err := optimize.NewOptimizer(optimize.O1).Optimize(prog)
```

`O1` applies three passes across every function:

- **Constant folding** — `I32_CONST 3, I32_CONST 4, I32_ADD` → `I32_CONST 7`
- **Constant deduplication** — identical values share a single constant slot
- **Dead code elimination** — unreachable basic blocks are removed

## How the JIT works

minivm runs a two-tier pipeline by default; thresholds and sampling cadence remain configurable:

```
           startup
bytecode ──────────► threaded interpreter
                           │
                     every 128 instructions:
                     sample function + IP
                           │
                     threshold reached
                     (default 4096 ticks)
                           │
                           ▼
                     JIT compiles hot segments
                     emits native ARM64
                     replaces closures in-place
```

JIT covers numeric computation: arithmetic, bitwise operations, comparisons, and type conversions across i32/i64/f32/f64. Stack ops, locals, constants, `select`, and branches compile when the stack shape fits a native segment signature. Direct static calls (`const.get; call`) fuse into a native branch-and-link to the callee's compiled entry, and `return` lowers to a native return, so whole call trees — recursion included — run in native code. Indirect and host calls, ref-holding globals, and heap-object ops stay threaded. The threaded interpreter uses closure dispatch rather than a switch table, so it stays useful before JIT kicks in.

## Instruction set

WebAssembly-inspired, intentionally custom. Opcodes are one byte; operands are fixed-width or length-prefixed.

| Category | Instructions |
|---|---|
| Stack | `NOP` `DROP` `DUP` `SWAP` `SELECT` |
| Control | `BR` `BR_IF` `BR_TABLE` `CALL` `RETURN` `UNREACHABLE` |
| Variables | `LOCAL_GET/SET/TEE` &nbsp; `GLOBAL_GET/SET/TEE` &nbsp; `CONST_GET` |
| Integers | `I32_CONST` `I64_CONST` — arithmetic, bitwise, comparisons, conversions |
| Floats | `F32_CONST` `F64_CONST` — arithmetic, comparisons, conversions |
| References | `REF_NULL` `REF_TEST` `REF_CAST` `REF_IS_NULL` `REF_EQ` `REF_NE` |
| Strings | `STRING_NEW_UTF32` `STRING_LEN` `STRING_CONCAT` and comparisons |
| Arrays | `ARRAY_NEW` `ARRAY_NEW_DEFAULT` `ARRAY_LEN` `ARRAY_GET/SET` `ARRAY_FILL/COPY` |
| Structs | `STRUCT_NEW` `STRUCT_NEW_DEFAULT` `STRUCT_GET/SET` |

## Options

```go
vm := interp.New(prog,
    interp.WithStack(4096),     // value stack capacity   (default: 1024)
    interp.WithHeap(512),       // initial heap capacity  (default: 128)
    interp.WithFrame(256),      // max call depth         (default: 128)
    interp.WithThreshold(4096), // ticks before JIT; 0 = first sample, <0 = disabled
    interp.WithTick(128),       // sample/poll cadence    (default: 128)
    interp.WithFuel(10_000),    // instruction budget     (default: unlimited)
    interp.WithHook(func(vm *interp.Interpreter) error {
        return nil // called every tick — inspect state or enforce policy
    }),
    interp.WithCutoff(4),       // min ops per JIT segment (default: 4)
)
```

`WithTick` governs profiling, context-cancellation checks, hook cadence, and fuel consumption together. `WithFuel(0)` is unlimited; non-zero values round up to the nearest tick interval. Hooks execute synchronously on the `Run` goroutine.

For instruction-accurate debugging (breakpoints, `Step`, `Next`, `Finish`), use `NewDebugger` + `WithDebugger` — this disables JIT. See [`docs/debugging.md`](docs/debugging.md).

For profile snapshots and JIT counters, see [`docs/profile.md`](docs/profile.md).

## Status

| Feature | |
|---|---|
| Threaded interpreter | ✅ |
| AOT optimizer (O1) | ✅ |
| ARM64 JIT — numeric ops, locals, branches | ✅ |
| ARM64 JIT — calls, globals, refs | 🔲 planned |
| x86-64 JIT | 🔲 planned |

See [docs/roadmap.md](docs/roadmap.md) for priorities and future direction.

## License

[MIT](LICENSE)
