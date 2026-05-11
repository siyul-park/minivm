# AGENTS.md

This file guides AI coding agents working in this repository (`Claude Code`, `Codex`, `Cursor`, etc.).

---

## Quick Commands

```bash
make test                      # go test -race ./...
make benchmark                 # go test -run="-" -bench=".*" -benchmem ./...
make lint                      # goimports -w . && go vet ./...
make coverage                  # generate coverage.out
make build                     # build ./dist/minivm

go test -race ./...
go test -race -run TestFoo ./interp/...

./dist/minivm                  # interactive assembly REPL
```

---

## Documentation Index

Read only what is relevant to the task.

| Document                                                              | Read when…                                        |
| --------------------------------------------------------------------- | ------------------------------------------------- |
| [docs/architecture.md](../docs/architecture.md)                       | tracing execution flow, debugging across packages |
| [docs/value-representation.md](../docs/value-representation.md)       | modifying boxed values, JIT value passing         |
| [docs/memory-model.md](../docs/memory-model.md)                       | touching refs, closures, GC, host functions       |
| [docs/instruction-set.md](../docs/instruction-set.md)                 | adding or debugging opcodes                       |
| [docs/jit-internals.md](../docs/jit-internals.md)                     | modifying threaded/JIT compilation                |
| [docs/pass-system.md](../docs/pass-system.md)                         | adding optimization or analysis passes            |
| [docs/coding-patterns.md](../docs/coding-patterns.md)                 | writing any new code                              |
| [docs/guides/add-opcode.md](../docs/guides/add-opcode.md)             | implementing a new instruction                    |
| [docs/guides/add-architecture.md](../docs/guides/add-architecture.md) | adding a new JIT backend                          |
| [docs/guides/repl.md](../docs/guides/repl.md)                         | extending or debugging the REPL                   |

---

## Architecture Overview

minivm is a bytecode VM with an adaptive JIT.

```text
program.Program
    │
    ▼
threadedCompiler
    │
    ▼
[]func(*Interpreter)
    │
    ▼
Interpreter.Run()
    │
    ├─ executes threaded closures
    └─ promotes hot blocks to JIT
           │
           ▼
      native ARM64
```

Hot-block compilation:

* execution counters update every 128 ticks
* JIT threshold defaults to 4096 ticks
* compiled native handlers replace threaded closures in-place

---

## Package Responsibilities

| Package       | Responsibility                                               |
| ------------- | ------------------------------------------------------------ |
| `program/`    | bytecode + constants container                               |
| `instr/`      | opcode definitions, encoding, parsing, formatting            |
| `types/`      | boxed values, arrays, structs, strings, NaN boxing           |
| `interp/`     | interpreter, threaded compiler, JIT driver                   |
| `asm/`        | virtual-register IR, register allocation, executable buffers |
| `asm/arm64/`  | ARM64 encoder, ABI, trampolines                              |
| `pass/`       | generic pass pipeline                                        |
| `analysis/`   | shared analysis passes                                       |
| `transform/`  | optimization transforms                                      |
| `optimize/`   | optimization pipeline wiring                                 |
| `cmd/repl/`   | interactive REPL                                             |
| `cmd/minivm/` | CLI entrypoint                                               |

---

## Key Invariants

These rules cause silent corruption or invalid execution when violated.

### Runtime

* Heap index `0` is permanently `Null`.
* `release()` must remain iterative, never recursive.
* Threaded closure errors should `panic`; `interp.Run()` recovers and annotates `at=<ip>`.

### Threaded Compiler

* Advance `c.ip` during compile time.
* Advance `f.ip` during runtime execution.
* Missing either causes invalid execution or infinite loops.

### JIT

* JIT handlers must call `c.ip++` first.
* On type mismatch, return `false, false`.
* Never coerce mismatched types.
* Branch terminators return `true, true`.
* JIT emits only when `count > 4`.

### Executable Buffers

Always follow this order:

```text
Unseal → Append → Seal → Call
```

Incorrect ordering crashes on Apple Silicon.

### Optimization

* `ConstantFoldingPass` pads folded regions using left-aligned NOPs.
* Do not strip folded padding.
* Threaded NOP handlers absorb gaps during compilation.

---

## Coding Expectations

* Match existing package structure and naming.
* Prefer small, cohesive packages.
* Avoid unnecessary abstractions.
* Keep opcode handlers explicit and predictable.
* Preserve interpreter/JIT behavioral parity.
* Prefer table-driven tests where practical.
* Avoid hidden control flow.

---

## Documentation Maintenance

Update docs when:

* an invariant caused a bug
* a command became outdated
* architecture changed
* a recurring pitfall was discovered
* a new important doc should be indexed

Guidelines:

* keep edits terse and factual
* document only current behavior
* avoid speculative notes
* preserve formatting consistency
* verify Markdown after edits
