# minivm

[![CI](https://github.com/siyul-park/minivm/actions/workflows/ci.yml/badge.svg)](https://github.com/siyul-park/minivm/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/siyul-park/minivm/branch/main/graph/badge.svg)](https://codecov.io/gh/siyul-park/minivm)
[![Go Reference](https://pkg.go.dev/badge/github.com/siyul-park/minivm.svg)](https://pkg.go.dev/github.com/siyul-park/minivm)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**Ship a scripting engine without writing a compiler.**

minivm gives your Go service a programmable runtime: assemble bytecode, expose Go functions to it, and run. Hot paths automatically upgrade from a threaded interpreter to native ARM64 code — no flags, no warmup, no configuration.

```bash
go get github.com/siyul-park/minivm
```

> Requires Go 1.25+. Zero external dependencies.

---

## What you can build

- **Scripting engines** — let users write logic your application executes safely
- **Rule engines** — evaluate complex conditions at runtime without redeployment  
- **DSL runtimes** — define your own instruction set on top of a proven VM foundation
- **Plugin systems** — run untrusted bytecode in an isolated, GC-managed environment

---

## Usage

### Execute bytecode

Every value on the stack is a `uint64`. The VM manages memory; you manage the bytecode.

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

The bridge between your application and guest code. Any Go function becomes callable from bytecode:

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

Host functions receive typed parameters as `[]Boxed` and return `[]Boxed` — no reflection, no interface{} boxing overhead.

### Define reusable functions

Functions are first-class constants. Build them with the fluent `FunctionBuilder` API:

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

Before handing bytecode to the VM, collapse compile-time-known expressions and strip unreachable branches:

```go
prog, err := optimize.NewOptimizer(optimize.O1).Optimize(prog)
```

`O1` applies across every function in the program:
- **Constant folding** — `I32_CONST 3, I32_CONST 4, I32_ADD` collapses to `I32_CONST 7`
- **Constant deduplication** — identical constant values share a single slot
- **Dead code elimination** — unreachable basic blocks are removed

---

## How the JIT works

minivm runs a **two-tier pipeline** that requires no decisions from you:

```
                startup
bytecode ──────────────────► threaded closures
                                    │
                            every 128 iterations:
                            count hits per basic block
                                    │
                    hits reach threshold (default 4096 ticks)
                                    │
                                    ▼
                          jit compiler runs
                          emits native ARM64
                          replaces closures in-place
```

The JIT compiles numeric computation — arithmetic, bitwise ops, comparisons, and type conversions across i32/i64/f32/f64 — to native code. Control flow, function calls, and reference operations continue through the threaded tier. The threaded interpreter itself uses closure dispatch rather than a switch table, so it's fast even before JIT kicks in.

**What this means in practice:** a compute-heavy loop will be slow for its first ~4096 iterations, then native-speed thereafter. There is nothing to tune unless you want to.

---

## Instruction set

Inspired by WebAssembly. Every opcode is one byte; operands are fixed-width or length-prefixed.

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
    interp.WithThreshold(8192),  // ticks before JIT      (default: 4096)
)
```

---

## Status

| | |
|---|---|
| Threaded interpreter | ✅ |
| AOT optimizer (O1) | ✅ |
| ARM64 JIT — numeric ops | ✅ |
| ARM64 JIT — control flow, calls | 🔲 planned |
| x86-64 JIT | 🔲 planned |

---

## License

[MIT](LICENSE)
