# minivm

[![CI](https://github.com/siyul-park/minivm/actions/workflows/ci.yml/badge.svg)](https://github.com/siyul-park/minivm/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/siyul-park/minivm/branch/main/graph/badge.svg)](https://codecov.io/gh/siyul-park/minivm)
[![Go Reference](https://pkg.go.dev/badge/github.com/siyul-park/minivm.svg)](https://pkg.go.dev/github.com/siyul-park/minivm)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**Ship a scripting engine without writing a compiler.**

minivm gives your Go service a lightweight programmable runtime: assemble bytecode, expose Go functions, run. Hot paths auto-upgrade from threaded interpreter to native ARM64 — no flags, no warmup, no config.

```bash
go get github.com/siyul-park/minivm
```

> Requires Go 1.26.2+. VM core uses only Go standard library; CLI and tests use small Go module dependencies.

---

## What you can build

- **Scripting engines** — let users write logic your application executes under your host policy
- **Rule engines** — evaluate complex conditions at runtime without redeployment
- **DSL runtimes** — define your own instruction set on top of a proven VM foundation
- **Plugin systems** — run application-defined bytecode in an isolated, GC-managed environment

## Designed for Go embedding

minivm is shaped around Go services:

- **Simple embedding** — build programs with Go APIs and run in-process
- **Typed host calls** — expose Go functions through `[]Boxed` bridge without reflection
- **Compact runtime model** — small custom bytecode format with GC-managed heap
- **Automatic tiering** — start in threaded interpreter and promote hot ARM64 numeric paths

Instruction set borrows from WebAssembly while staying focused on Go-native scripting, rules, and DSL execution. See [docs/roadmap.md](docs/roadmap.md) for current direction.

---

## Performance

Recursive `fib(20)` — end-to-end per call, linux/amd64:

| Runtime | ns/op | allocs/op | execution model |
|---|---|---|---|
| native Go | 37,968 | 0 | compiled |
| wazero | 62,219 | 2 | WASM JIT |
| **minivm** | **1,157,136** | **0** | **threaded interpreter** |
| tengo | 2,000,364 | 28,657 | bytecode VM |
| gopher-lua | 2,942,015 | 2 | register VM |
| goja | 3,964,702 | 39 | bytecode VM |

Among **interpreters without ahead-of-time JIT**, minivm runs with **zero heap allocation** and outperforms tengo (~1.7×), gopher-lua (~2.5×), and goja (~3.4×). wazero's lead comes from JIT-compiling WebAssembly to native x86-64 at load time — minivm closes this gap on ARM64 once hot segments promote to native code.

Single-instruction throughput (threaded interpreter, same hardware):

| Workload | ns/op |
|---|---|
| i32/i64/f32/f64 arithmetic | ~20–22 |
| branch (`br`, `br_if`) | ~20–24 |
| bytecode function call | ~26–29 |
| host function call | ~36 |
| array/struct operations | ~90–140 |

Full results: [`docs/benchmarks.md`](docs/benchmarks.md)

---

## Usage

### Execute bytecode

Every stack value is `uint64`. VM manages memory; you manage bytecode.

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

Bridge between application and guest code. Any Go function becomes callable from bytecode:

```go
lookup := interp.NewHostFunction(
    &types.FunctionType{
        Params:  []types.Type{types.TypeI32},
        Returns: []types.Type{types.TypeI32},
    },
    func(vm *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error) {
        id := params[0].I32()
        price := db.GetPrice(int(id)) // your Go code
        return []types.Boxed{types.BoxI32(price)}, nil
    },
)

prog := program.New(
    []instr.Instruction{
        instr.New(instr.I32_CONST, 42), // product id
        instr.New(instr.CONST_GET, 0),  // push lookup function
        instr.New(instr.CALL),          // call it
    },
    program.WithConstants(lookup),
)
```

Host functions receive typed params as `[]Boxed`, return `[]Boxed` — no reflection, no `interface{}` boxing.

### Define reusable functions

Functions are first-class constants. Build with `FunctionBuilder` API:

```go
factorial := types.NewFunctionBuilder(&types.FunctionType{
    Params:  []types.Type{types.TypeI32},
    Returns: []types.Type{types.TypeI32},
}).WithLocals(types.TypeI32).Emit(
    instr.New(instr.LOCAL_GET, 0),
    instr.New(instr.I32_CONST, 1),
    instr.New(instr.I32_LT_S),
    instr.New(instr.BR_IF, 14),        // base case: n < 1 → return 1
    instr.New(instr.LOCAL_GET, 0),
    instr.New(instr.I32_CONST, 1),
    instr.New(instr.I32_SUB),
    instr.New(instr.CONST_GET, 0),
    instr.New(instr.CALL),             // factorial(n-1)
    instr.New(instr.LOCAL_GET, 0),
    instr.New(instr.I32_MUL),          // n * factorial(n-1)
    instr.New(instr.RETURN),
    instr.New(instr.I32_CONST, 1),     // base case value
    instr.New(instr.RETURN),
).Build()

prog := program.New(
    []instr.Instruction{
        instr.New(instr.I32_CONST, 10),
        instr.New(instr.CONST_GET, 0),
        instr.New(instr.CALL),
    },
    program.WithConstants(factorial),
)
```

### Optimize before running

Before handing bytecode to VM, collapse compile-time-known expressions and strip unreachable branches:

```go
prog, err := optimize.NewOptimizer(optimize.O1).Optimize(prog)
```

`O1` applies across every function:
- **Constant folding** — `I32_CONST 3, I32_CONST 4, I32_ADD` collapses to `I32_CONST 7`
- **Constant deduplication** — identical constant values share a single slot
- **Dead code elimination** — unreachable basic blocks are removed

---

## How the JIT works

minivm runs a **two-tier pipeline** — no decisions required:

```
                startup
bytecode ──────────────────► threaded closures
                                    │
                            every 128 instructions:
                            record a function/IP sample
                                    │
                    samples reach threshold (default 4096 ticks)
                                    │
                                    ▼
                          jit compiler runs
                          emits native ARM64
                          replaces closures in-place
```

JIT compiles numeric computation — arithmetic, bitwise ops, comparisons, and type conversions across i32/i64/f32/f64 — to native code. Also handles selected stack operations, locals, constants, `select`, and branch instructions when current stack shape can be represented by native segment signature. Function calls, globals, references, and heap-object operations continue through threaded tier. Threaded interpreter uses closure dispatch rather than a switch table, so it's fast even before JIT.

**In practice:** compute-heavy loop runs in interpreter for ~4096 executed instructions, then hot native segments take over. Nothing to tune.

---

## Instruction set

WebAssembly-inspired, intentionally custom. Every opcode is one byte; operands are fixed-width or length-prefixed.

| Category | |
|---|---|
| Stack | `NOP` `DROP` `DUP` `SWAP` `SELECT` |
| Control | `BR` `BR_IF` `BR_TABLE` `CALL` `RETURN` `UNREACHABLE` |
| Variables | `LOCAL_GET/SET/TEE` &nbsp; `GLOBAL_GET/SET/TEE` &nbsp; `CONST_GET` |
| Integers | `I32_CONST` `I64_CONST` — add, sub, mul, div, rem, shl, shr, and, or, xor, comparisons, conversions |
| Floats | `F32_CONST` `F64_CONST` — add, sub, mul, div, comparisons, conversions |
| References | `REF_NULL` `REF_TEST` `REF_CAST` `REF_IS_NULL` `REF_EQ` `REF_NE` |
| Strings | `STRING_NEW_UTF32` `STRING_LEN` `STRING_CONCAT` and comparisons |
| Arrays | `ARRAY_NEW` `ARRAY_NEW_DEFAULT` `ARRAY_LEN` `ARRAY_GET/SET` `ARRAY_FILL/COPY` |
| Structs | `STRUCT_NEW` `STRUCT_NEW_DEFAULT` `STRUCT_GET/SET` |

---

## Options

```go
vm := interp.New(prog,
    interp.WithStack(4096),      // value stack slots    (default: 1024)
    interp.WithHeap(512),        // initial heap capacity (default: 128)
    interp.WithFrame(256),       // max call depth        (default: 128)
    interp.WithThreshold(4096),  // ticks before JIT; 0 = first sample, negative disables JIT
    interp.WithTick(128),        // sample/poll cadence   (default: 128)
    interp.WithFuel(10_000),     // instruction budget   (default: unlimited)
    interp.WithHook(func(vm *interp.Interpreter) error {
        return nil              // inspect or enforce host policy inline
    }),
    interp.WithCutoff(4),          // min JIT segment ops   (default: 4)
)
```

`WithTick` controls profiling samples, context-cancellation polling, hook cadence, and fuel consumption. `WithFuel` accepts instruction budget and rounds up to nearest tick interval; `WithFuel(0)` is unlimited. Hooks run synchronously on `Run` goroutine; avoid concurrent interpreter access and preserve VM invariants.

For bytecode-level debugging, use `NewDebugger` with `WithDebugger`. Provides breakpoints plus `Step`, `Next`, `Finish`; disables JIT. See [`docs/debugging.md`](docs/debugging.md).

For profile snapshots, JIT counters, and REPL `.profile` output, see [`docs/profile.md`](docs/profile.md).

---

## Status

| | |
|---|---|
| Threaded interpreter | ✅ |
| AOT optimizer (O1) | ✅ |
| ARM64 JIT — numeric ops, locals, branches | ✅ |
| ARM64 JIT — calls, globals, refs | 🔲 planned |
| x86-64 JIT | 🔲 planned |

See [docs/roadmap.md](docs/roadmap.md) for roadmap priorities and future direction.

---

## License

[MIT](LICENSE)
