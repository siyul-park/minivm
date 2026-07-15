# minivm

[![CI](https://github.com/siyul-park/minivm/actions/workflows/ci.yml/badge.svg)](https://github.com/siyul-park/minivm/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/siyul-park/minivm/branch/main/graph/badge.svg)](https://codecov.io/gh/siyul-park/minivm)
[![Go Reference](https://pkg.go.dev/badge/github.com/siyul-park/minivm.svg)](https://pkg.go.dev/github.com/siyul-park/minivm)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**English** · [한국어](README.ko.md)

## A compact, embeddable bytecode VM for Go

Run dynamic logic inside your Go application without giving up control over
performance, resources, or host integration.

- **Bounded execution** — limit stack, heap, call depth, fuel, hooks, and context.
- **Direct host integration** — call Go through typed, reflection-free host functions.
- **Adaptive performance** — start in a threaded interpreter and promote hot ARM64
  functions and loops to native code.

```bash
go get github.com/siyul-park/minivm
```

> Requires Go 1.26.2+. The VM core uses only the Go standard library.

## Quick Start

Build and run a bytecode program that calculates `6 × 7`:

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

minivm keeps the execution model explicit: bytecode in, controlled runtime,
typed value out.

## Why minivm

| Capability | What it gives you |
|---|---|
| Embeddable runtime | First-class functions, locals, globals, closures, refs, strings, arrays, structs, maps, coroutines, and structured errors |
| Host integration | Typed `HostFunction` calls plus `Marshal` and `Unmarshal` for ordinary Go values |
| Resource control | Stack, heap, frame, fuel, context, hook, and debugger controls |
| Fast baseline | Closure-threaded dispatch with low steady-state allocation on core workloads |
| Hot-path acceleration | Adaptive ARM64 trace JIT for supported functions and loops |
| Safe admission | Static bytecode verification before execution |

### Built for

- **Scripting engines** that execute user-defined behavior under host policy
- **Rule engines** that change runtime decisions without redeployment
- **DSL runtimes** that need a compact execution layer
- **Plugin systems** that isolate extension logic from the host application

## Call Go from Bytecode

Expose Go behavior through a typed host function:

```go
lookup := interp.NewHostFunction(
    &types.FunctionType{
        Params:  []types.Type{types.TypeI32},
        Returns: []types.Type{types.TypeI32},
    },
    func(vm *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error) {
        price := db.GetPrice(int(params[0].I32()))
        return []types.Boxed{types.BoxI32(price)}, nil
    },
)
```

Parameters and results stay in typed `[]types.Boxed` values. The direct path does
not require reflection or `interface{}` boxing.

See [Host Integration](docs/host-integration.md) for marshaling, host objects, and
lifetime rules.

## Performance

minivm is designed to be useful before JIT compilation and faster when repeated
execution makes native traces worthwhile.

Representative medians measured July 15, 2026, on Apple M4 Pro,
`darwin/arm64`, Go 1.26.2 (`ns/op`, lower is better):

| Runtime | Iterative Fib (30) | Recursive Fib (35) | Sieve (256) | Branch Tree (96) |
|---|---:|---:|---:|---:|
| native Go | 8.337 | 20,957,448 | 247.8 | 77.39 |
| wazero | 49.84 | 46,785,131 | 645.4 | 156.9 |
| **minivm/default** | **71.83** | **48,426,669** | **5,052** | **228.0** |
| minivm/threaded | 730.9 | 512,675,498 | 15,385 | 986.4 |

`minivm/default` uses the adaptive ARM64 trace-JIT policy. Results vary by
workload: unsupported paths remain in the threaded interpreter, and some
workloads do not benefit from tracing yet.

See [Benchmarks](docs/benchmarks.md) for the full matrix, memory results,
measurement boundaries, and reproduction commands.

## Runtime Tooling

### Verify untrusted bytecode

```go
if err := program.Verify(prog); err != nil {
    log.Fatal(err)
}
```

The verifier rejects malformed control flow, invalid stack behavior, and type
mismatches before execution. The `run` CLI verifies loaded programs by default.

### Optimize ahead of execution

```go
prog, err := optimize.NewOptimizer(optimize.O2).Optimize(prog)
```

Optimization levels range from local constant folding and deduplication to
dead-code elimination and cross-block global value numbering.

### Control execution

```go
vm := interp.New(prog,
    interp.WithStack(4096),
    interp.WithHeap(512),
    interp.WithFrame(256),
    interp.WithFuel(10_000),
    interp.WithThreshold(4096),
    interp.WithTick(128),
)
```

Use hooks for policy checks and `NewDebugger` with `WithDebugger` for
instruction-accurate breakpoints and stepping.

## Architecture

```text
Program -> verifier / optimizer -> threaded interpreter -> ARM64 trace JIT
                                   |                    |
                                   +-- always valid ----+-- hot paths only
```

The threaded interpreter is the complete execution engine. The trace JIT is an
adaptive acceleration layer: supported hot paths compile to native ARM64 code,
while every unsupported or cold path continues in the interpreter.

The instruction set is WebAssembly-inspired but intentionally custom. It uses
one-byte opcodes with fixed-width or length-prefixed operands.

- [Architecture](docs/architecture.md)
- [Instruction Set](docs/instruction-set.md)
- [JIT Internals](docs/jit-internals.md)
- [Memory Model](docs/memory-model.md)

## Status

| Feature | Status |
|---|---|
| Threaded interpreter | ✅ Available |
| Static bytecode verifier | ✅ Available |
| AOT optimizer (`O1`-`O3`) | ✅ Available |
| ARM64 trace JIT | ✅ Available |
| Debugger and profiler | ✅ Available |
| x86-64 JIT | 🔲 Planned |

The x86-64 assembler package currently provides a non-emitting placeholder.
See the [Roadmap](docs/roadmap.md) for current priorities.

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
