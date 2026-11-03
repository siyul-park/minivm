# minivm

[![CI](https://github.com/siyul-park/minivm/actions/workflows/ci.yml/badge.svg)](https://github.com/siyul-park/minivm/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/siyul-park/minivm/branch/main/graph/badge.svg)](https://codecov.io/gh/siyul-park/minivm)
[![Go Reference](https://pkg.go.dev/badge/github.com/siyul-park/minivm.svg)](https://pkg.go.dev/github.com/siyul-park/minivm)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**English** · [한국어](README.ko.md)

## A compact, embeddable bytecode VM for Go

Run dynamic logic inside Go with explicit resource limits, typed host calls, and a threaded semantic baseline.

- **Bounded execution** — stack, heap, frame depth, fuel, hooks, context.
- **Go interop** — Go values and funcs convert both ways; a typed `HostFunction` path for hot calls.
- **Native tier** — ARM64 execution, on by default, with threaded fallback.

```bash
go get github.com/siyul-park/minivm
```

> Requires Go 1.26.2+. The VM core uses only the Go standard library.

## Quick Start

Build and run a bytecode program that calculates `6 × 7`:

```go
prog, err := program.NewBuilder().
    Emit(instr.I32_CONST, 6).
    Emit(instr.I32_CONST, 7).
    Emit(instr.I32_MUL).
    Build()
if err != nil {
    log.Fatal(err)
}

vm, err := interp.New(prog) // verifies prog before it can run
if err != nil {
    log.Fatal(err)
}
defer vm.Close()

if err := vm.Run(context.Background()); err != nil {
    log.Fatal(err)
}

result, err := vm.Pop()
if err != nil {
    log.Fatal(err)
}
fmt.Println(result) // 42
```

minivm keeps the execution model explicit: bytecode in, verified runtime, typed value out. Fuel and heap are unlimited by default; set `interp.WithFuel` and `interp.WithHeapLimit` (plus a cancellable context) for untrusted bytecode. Runnable versions of every example here are in [`interp/example_test.go`](interp/example_test.go).

## Why minivm

| Capability | What it gives you |
|---|---|
| Embeddable runtime | First-class functions, locals, globals, closures, refs, strings, arrays, structs, maps, coroutines, and structured errors |
| Host integration | Typed `HostFunction` calls plus `Marshal` and `Unmarshal` for ordinary Go values |
| Resource control | Stack, heap, frame, fuel, context, hook, and debugger controls |
| Fast baseline | Closure-threaded dispatch with low steady-state allocation |
| Semantic baseline | Threaded interpreter with explicit resource controls |
| Safe admission | Static bytecode verification before execution |

### Built for

- **Scripting engines** that execute user-defined behavior under host policy
- **Rule engines** that change runtime decisions without redeployment
- **DSL runtimes** that need a compact execution layer
- **Plugin systems** that isolate extension logic from the host application

## Call Go from Bytecode

`Marshal` turns an ordinary Go func into a VM function. A leading `context.Context` receives the running context; a trailing `error` becomes a VM error.

```go
add := func(ctx context.Context, a, b int64) (int64, error) { return a + b, nil }
typ := &types.FunctionType{
    Params:  []types.Type{types.TypeI64(), types.TypeI64()},
    Returns: []types.Type{types.TypeI64()},
}
prog, err := program.NewBuilder().
    Globals(typ).
    Emit(instr.I64_CONST, 2).
    Emit(instr.I64_CONST, 3).
    Emit(instr.GLOBAL_GET, 0).
    Emit(instr.CALL).
    Build()
if err != nil {
    log.Fatal(err)
}

vm, err := interp.New(prog)
if err != nil {
    log.Fatal(err)
}
defer vm.Close()

fn, err := vm.Marshal(add)
if err != nil {
    log.Fatal(err)
}
addr, err := vm.Alloc(fn) // the caller owns this reference
if err != nil {
    log.Fatal(err)
}
if err := vm.SetGlobal(0, types.BoxRef(addr)); err != nil { // the global takes it
    log.Fatal(err)
}
if err := vm.Run(context.Background()); err != nil {
    log.Fatal(err)
}
result, err := vm.Pop()
if err != nil {
    log.Fatal(err)
}
fmt.Println(result) // 5
```

For hot calls, `interp.NewHostFunction` takes and returns `[]types.Boxed` directly, with no reflection.

## Call Bytecode from Go

`Unmarshal` turns a VM function into a typed Go func:

```go
ctx := context.Background()

// addr holds a VM function; see ExampleRegistry_Unmarshal in interp/example_test.go for the full setup.
var inc func(context.Context, int64) (int64, error)
if err := vm.Unmarshal(types.BoxRef(addr), &inc); err != nil {
    log.Fatal(err)
}
n, err := inc(ctx, 41)
if err != nil {
    log.Fatal(err)
}
fmt.Println(n) // 42
```

Declare a trailing `error` on the func type: without one, a failed call panics. Calling it while `vm.Run` is active returns `interp.ErrInterpreterBusy`.

See [Host Integration](docs/host-integration.md) for the conversion table, ownership of each API, and the low-level `HostFunction` path.

## Performance

Threaded execution is the semantic baseline. ARM64 native execution is on by default and tuned or disabled with `interp.WithThreshold`; measurements and reproduction live in [Benchmarks](docs/benchmarks.md).

## Runtime Tooling

### Verify untrusted bytecode

`interp.New` and `interp.NewPool` verify a snapshot of the program and run only that snapshot, so later changes to `prog` cannot bypass verification. The snapshot covers code and constants; type objects (for example function signatures) are shared with `prog` and MUST NOT be mutated after construction (see [Verification](docs/verification.md)). To check bytecode without building an interpreter:

```go
if err := program.Verify(prog); err != nil {
    log.Fatal(err)
}
```

The verifier rejects malformed control flow, invalid stack behavior, and type mismatches.

### Optimize bytecode

```go
prog, err := optimize.New(optimize.O2).Optimize(prog)
if err != nil {
    log.Fatal(err)
}
```

The bytecode optimizer rewrites a program before it runs. It does not verify and may panic on malformed input, so run `program.Verify` first. Levels range from constant folding and dead-code elimination to common-subexpression elimination, promotion, and loop-invariant hoisting.

### Control execution

```go
vm, err := interp.New(prog,
    interp.WithStack(4096),
    interp.WithHeap(512),
    interp.WithFrame(256),
    interp.WithFuel(10_000),
    interp.WithTick(128),
)
if err != nil {
    log.Fatal(err)
}
defer vm.Close()
```

Use hooks for policy checks and `debug.NewDebugger` with `WithHook(dbg.Hook)` and `WithTick(1)` for instruction-accurate breakpoints and stepping ([Debugging](docs/debugging.md)).

## Architecture

```text
Program → Verify → optimize? → interp.New (verify) → threaded ⇄ native (ARM64)
```

The threaded interpreter is the semantic baseline and complete execution engine. On ARM64 the JIT adds native execution for hot functions once a program has run long enough to repay a compile (`WithThreshold`); unsupported paths return to threaded execution.

The instruction set is WebAssembly-inspired but intentionally custom. It uses one-byte opcodes with fixed-width or length-prefixed operands.

- [Architecture](docs/architecture.md)
- [Instruction Set](docs/instruction-set.md)
- [JIT Internals](docs/jit-internals.md)
- [Memory Model](docs/memory-model.md)

## Status

| Feature | Status |
|---|---|
| Threaded interpreter | ✅ Available |
| Static bytecode verifier | ✅ Available |
| Bytecode optimizer (`optimize.O1`-`O3`) | ✅ Available |
| ARM64 native tier (on by default, `interp.WithThreshold`) | ✅ Available |
| Debugger and profiler | ✅ Available |
| x86-64 native backend | 🔲 Not implemented |

The x86-64 assembler backend is not present. See the [Roadmap](docs/roadmap.md) for current priorities.

## Documentation

| Guide | Use it for |
|---|---|
| [Documentation Index](docs/README.md) | Browse all project documentation |
| [Compatibility](docs/compatibility.md) | Check Go, platform, CGO, and build-tag support |
| [Host Integration](docs/host-integration.md) | Connect bytecode with Go values and functions |
| [Verification](docs/verification.md) | Understand static admission checks and limits |
| [Debugging](docs/debugging.md) | Use breakpoints, stepping, and inspection |
| [Testing](docs/testing.md) | Understand executable specifications and gates |
| [Benchmarks](docs/benchmarks.md) | Reproduce performance and allocation measurements |

## License

[MIT](LICENSE)
