# minivm

[![CI](https://github.com/siyul-park/minivm/actions/workflows/ci.yml/badge.svg)](https://github.com/siyul-park/minivm/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/siyul-park/minivm/branch/main/graph/badge.svg)](https://codecov.io/gh/siyul-park/minivm)
[![Go Reference](https://pkg.go.dev/badge/github.com/siyul-park/minivm.svg)](https://pkg.go.dev/github.com/siyul-park/minivm)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**English** · [한국어](README.ko.md)

**Fast bytecode VM that embeds anywhere.**

minivm lets Go programs load tiny bytecode programs, call back into host functions, and run under explicit stack, heap, fuel, and hook limits. It starts as a fast threaded interpreter and compiles hot functions and loops to native ARM64 code automatically with a trace JIT.

```bash
go get github.com/siyul-park/minivm
```

> Requires Go 1.26.2+. The VM core depends only on the Go standard library.

Full docs: [`docs/README.md`](docs/README.md)

## Why minivm

| Need | What minivm gives you |
|---|---|
| Embed runtime behavior | bytecode programs with first-class functions, locals, globals, refs, arrays, structs, maps, strings, coroutines, and structured errors |
| Call host code | zero-reflection `HostFunction` path plus `Marshal` / `Unmarshal` for ordinary Go values |
| Keep execution bounded | stack, heap, frame, fuel, context, and hook controls |
| Stay fast before JIT | closure-threaded dispatch with near-zero allocations on recursive workloads |
| Get native speed where it matters | adaptive ARM64 trace JIT for hot functions and loops |

## Build With It

- **Scripting engines** — execute user-defined logic under your host policy
- **Rule engines** — evaluate complex conditions at runtime without redeployment
- **DSL runtimes** — define a custom instruction set on a proven VM foundation
- **Plugin systems** — run sandboxed bytecode in a GC-managed environment

## Performance

Recursive `fib(35)` — darwin/arm64, Apple M4 Pro, Go 1.26.2. minivm is measured twice: **interp** is the pure threaded interpreter, **JIT** is the default `New`, which records hot functions and loops and compiles them to native code on ARM64:

| Runtime | ns/op | B/op | allocs/op | vs native Go | execution model |
|---|---|---|---|---|---|
| native Go | 19,324,275 | 0 | 0 | 1× | compiled |
| wazero | 44,409,757 | 16 | 2 | 2.3× | WASM → native JIT |
| **minivm (JIT)** | **51,911,961** | **4,918** | **45** | **2.7×** | **threaded interpreter + ARM64 trace JIT** |
| minivm (interp) | 669,343,195 | 288 | 2 | 35× | threaded interpreter |
| tengo | 1,138,199,604 | 312,799,988 | 39,088,179 | 59× | bytecode VM |
| gopher-lua | 1,462,044,917 | 971,008 | 3,793 | 76× | register VM |
| goja | 2,052,722,000 | 383,488 | 46,384 | 106× | bytecode VM |

The JIT is worth **13× on this workload** (669 ms → 52 ms per call). Among pure interpreters, minivm is allocation-light and faster than the script VMs measured here.

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

### Validate untrusted bytecode

Verify bytecode from untrusted or external producers before constructing an interpreter:

```go
if err := program.Verify(prog); err != nil {
    log.Fatal(err)
}
```

The `run` CLI command performs this check before execution. See [`docs/verification.md`](docs/verification.md) for the verifier model and limits.

### Optimize before running

Fold constants before the VM sees them:

```go
prog, err := optimize.NewOptimizer(optimize.O1).Optimize(prog)
```

`O1` applies two cheap local passes across every function:

- **Constant folding** — `I32_CONST 3, I32_CONST 4, I32_ADD` → `I32_CONST 7`
- **Constant deduplication** — identical values share a single constant slot

Use `O2` when you also want algebraic simplification and dead-code elimination, or `O3` for cross-block global value numbering.

## How the JIT works

minivm runs a two-tier pipeline by default; thresholds and sampling cadence remain configurable:

```
           startup
bytecode ──────────► threaded interpreter
                           │
                     every tick: sample function + IP
                           │
                     function or loop header hot
                           │
                           ▼
                     record the live hot path → trace
                     compile trace to native ARM64
                     install at the entry / loop header
                           │
                     guard fails ──► deopt to interpreter
```

The JIT records the hot path through a function entry or loop header, compiles that trace to native code, and deopts back to the interpreter when a guard fails. See [`docs/jit-internals.md`](docs/jit-internals.md) for details.

## Instruction set

WebAssembly-inspired, intentionally custom. Opcodes are one byte; operands are fixed-width or length-prefixed.

| Category | Instructions |
|---|---|
| Stack | `NOP` `DROP` `DUP` `SWAP` `SELECT` |
| Control | `BR` `BR_IF` `BR_TABLE` `CALL` `RETURN` `RETURN_CALL` `UNREACHABLE` |
| Coroutines | `YIELD` `RESUME` `CORO_DONE` `CORO_VALUE` |
| Variables | `LOCAL_GET/SET/TEE` &nbsp; `GLOBAL_GET/SET/TEE` &nbsp; `UPVAL_GET/SET` &nbsp; `CONST_GET` |
| Integers | `I32_CONST` `I64_CONST` — arithmetic, bitwise, comparisons, conversions |
| Floats | `F32_CONST` `F64_CONST` — arithmetic, comparisons, conversions |
| References | `REF_NULL` `REF_TEST` `REF_CAST` `REF_IS_NULL` `REF_EQ/NE` `REF_NEW/GET/SET` |
| Strings | `STRING_NEW_UTF32` `STRING_ENCODE_UTF32` `STRING_ITER` `STRING_LEN` `STRING_CONCAT` and comparisons |
| Arrays | `ARRAY_NEW` `ARRAY_NEW_DEFAULT` `ARRAY_LEN` `ARRAY_GET/SET` `ARRAY_FILL/COPY` `ARRAY_APPEND/DELETE/SLICE` |
| Structs | `STRUCT_NEW` `STRUCT_NEW_DEFAULT` `STRUCT_GET/SET` |
| Maps | `MAP_NEW` `MAP_NEW_DEFAULT` `MAP_LEN` `MAP_GET/LOOKUP` `MAP_SET/DELETE/CLEAR` `MAP_KEYS/ITER` |
| Closures | `CLOSURE_NEW` |
| Errors | `THROW` `ERROR_NEW` `ERROR_GET` `ERROR_CODE` |

Complete opcode reference: [`docs/instruction-set.md`](docs/instruction-set.md)

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
)
```

`WithTick` governs profiling, context-cancellation checks, hook cadence, and fuel consumption together. `WithFuel(0)` is unlimited; non-zero values round up to the nearest tick interval. Hooks execute synchronously on the `Run` goroutine.

For instruction-accurate debugging (breakpoints, `Step`, `Next`, `Finish`), use `NewDebugger` + `WithDebugger` — this disables JIT. See [`docs/debugging.md`](docs/debugging.md).

## Status

| Feature | |
|---|---|
| Threaded interpreter | ✅ |
| AOT optimizer (O1-O3) | ✅ |
| ARM64 trace JIT — numerics, locals, globals, branches | ✅ |
| ARM64 trace JIT — calls, upvalues, refs, heap reads, loops | ✅ |
| x86-64 JIT | 🔲 planned (`asm/amd64` is a non-emitting placeholder) |

See [`docs/roadmap.md`](docs/roadmap.md) for priorities and future direction.

## License

[MIT](LICENSE)
