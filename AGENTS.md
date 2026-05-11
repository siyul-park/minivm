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

## Agent Workflow

1. Check `git status --short`; do not overwrite unrelated user changes.
2. Read only the docs listed for the package you will touch.
3. Locate existing tests near the edited package and mirror their style.
4. Update docs when behavior, invariants, commands, or recurring pitfalls change.
5. Run the narrow test first, then `go test ./...` or `make test` when risk is broader.

## Task Router

| Task | Start here | Usually edit | Verify |
| ---- | ---------- | ------------ | ------ |
| Opcode semantics | `docs/instruction-set.md`, `docs/guides/add-opcode.md` | `instr/`, `interp/threaded.go`, `interp/jit_arm64.go` | `go test ./instr ./interp` |
| Runtime/stack/frame bug | `docs/architecture.md`, `docs/memory-model.md` | `interp/`, `types/` | `go test ./interp ./types` |
| Ref/GC/host function | `docs/memory-model.md`, `docs/value-representation.md` | `interp/host.go`, `interp/threaded.go`, `types/` | `go test ./interp ./types` |
| JIT/ARM64 backend | `docs/jit-internals.md`, `docs/value-representation.md` | `interp/jit*.go`, `asm/`, `asm/arm64/` | `go test ./asm/... ./interp` |
| Optimizer/pass | `docs/pass-system.md` | `analysis/`, `transform/`, `optimize/`, `pass/` | `go test ./analysis ./transform ./optimize ./pass` |
| REPL/CLI | `docs/guides/repl.md` | `cmd/repl/`, `cmd/minivm/`, `instr/parse.go` | `go test ./cmd/repl ./cmd/minivm ./instr` |
| Style-only code change | `docs/coding-patterns.md` | touched package | package tests |

---

## Documentation Index

Read only what is relevant to the task.

| Document                                                              | Read when…                                        |
| --------------------------------------------------------------------- | ------------------------------------------------- |
| [docs/architecture.md](docs/architecture.md)                         | tracing execution flow, debugging across packages |
| [docs/value-representation.md](docs/value-representation.md)         | modifying boxed values, JIT value passing         |
| [docs/memory-model.md](docs/memory-model.md)                         | touching refs, closures, GC, host functions       |
| [docs/instruction-set.md](docs/instruction-set.md)                   | adding or debugging opcodes                       |
| [docs/jit-internals.md](docs/jit-internals.md)                       | modifying threaded/JIT compilation                |
| [docs/pass-system.md](docs/pass-system.md)                           | adding optimization or analysis passes            |
| [docs/coding-patterns.md](docs/coding-patterns.md)                   | writing any new code                              |
| [docs/guides/add-opcode.md](docs/guides/add-opcode.md)               | implementing a new instruction                    |
| [docs/guides/add-architecture.md](docs/guides/add-architecture.md) | adding a new JIT backend                          |
| [docs/guides/repl.md](docs/guides/repl.md)                           | extending or debugging the REPL                   |

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
    └─ promotes sampled hot segments to JIT
           │
           ▼
      native ARM64
```

Hot-segment compilation:

* profile samples record `(function, ip)` every 128 executed instructions
* JIT threshold defaults to 4096 ticks, i.e. 32 profile samples
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

* JIT handlers must advance `c.ip` before every return path.
* On type mismatch, return `false, false`.
* Never coerce mismatched types.
* Branch terminators return `true, true`.
* Completed JIT segments emit when `count >= emit` (default 4); truncated segments emit only when `count > 4`.

### Executable Buffers

Always follow this order:

```text
Unseal → Append → Seal → Call
```

Incorrect ordering crashes on Apple Silicon.

### Optimization

* `ConstantFoldingPass` right-aligns folded instructions and pads the left side with NOPs.
* Preserve folded byte ranges until `DeadCodeEliminationPass` compacts bytecode and rewrites branches.
* Threaded NOP handlers absorb consecutive gaps with one runtime dispatch.

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
