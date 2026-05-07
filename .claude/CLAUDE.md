# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make test                                        # go test -race ./...
go test -race -run TestFoo ./interp/...          # single test
make benchmark                                   # go test -run="-" -bench=".*" -benchmem ./...
make lint                                        # goimports -w . && go vet ./...
make coverage                                    # produces coverage.out
make build                                       # builds ./dist/minivm
./dist/minivm                                    # launch the interactive assembly REPL
```

## Documentation

Read these docs on demand — do not pre-load all of them. Each entry states when it is relevant.

| Document | Read when… |
|---|---|
| [docs/architecture.md](../docs/architecture.md) | tracing execution flow, debugging across packages, understanding component boundaries |
| [docs/value-representation.md](../docs/value-representation.md) | working in `types/`, passing values through JIT, debugging boxing/unboxing |
| [docs/memory-model.md](../docs/memory-model.md) | writing threaded closures that touch refs, implementing host functions, modifying GC |
| [docs/instruction-set.md](../docs/instruction-set.md) | adding or modifying opcodes, checking JIT coverage, debugging specific instructions |
| [docs/jit-internals.md](../docs/jit-internals.md) | modifying `threaded.go` or `jit_arm64.go`, writing new opcode handlers |
| [docs/pass-system.md](../docs/pass-system.md) | adding optimization passes, modifying `transform/` or `analysis/` |
| [docs/coding-patterns.md](../docs/coding-patterns.md) | writing any new code in this repository |
| [docs/guides/add-opcode.md](../docs/guides/add-opcode.md) | adding a new instruction end-to-end |
| [docs/guides/add-architecture.md](../docs/guides/add-architecture.md) | adding a new JIT backend (e.g. x86-64) |
| [docs/guides/repl.md](../docs/guides/repl.md) | using or extending the interactive REPL (`cmd/minivm`, `cmd/repl`) |

## Architecture Overview

minivm is a bytecode VM with an adaptive JIT. Every function runs through a two-tier pipeline:

```
program.Program (bytecode []byte + constants)
    │
    ▼ interp.New()
threadedCompiler → []func(*Interpreter)   one closure per byte offset, always
    │
    ▼ Interpreter.Run() — every 128 ticks, count hot-block hits
    │ when hits reach threshold (default 4096 ticks):
    ▼
jitCompiler → native ARM64               replaces closures in-place for hot segments
```

**Package responsibilities (one line each):**
- `program/` — bytecode + constants container; entry point for program construction
- `instr/` — opcode definitions, variable-width encoding/decoding, text formatter (`Format`), text parser (`Parse`/`ParseAll`)
- `types/` — value types (`Boxed`, `Function`, `Array`, `Struct`, `String`) and NaN boxing
- `interp/` — interpreter state, threaded compiler, JIT driver, host function bridge
- `asm/` — virtual-register IR, linear-scan register allocator, executable memory buffer
- `asm/arm64/` — ARM64 encoder, ABI, caller trampoline
- `pass/` — reflection-based pipeline (`Manager`, `Pass[T]`)
- `analysis/` — `BasicBlocksPass` (shared by JIT and optimizer)
- `transform/` — `ConstantFoldingPass`, `ConstantDeduplicationPass`, `DeadCodeEliminationPass`
- `optimize/` — `Optimizer` that wires passes into O0/O1 levels
- `cmd/repl/` — interactive assembly REPL (`REPL` type, `Run` loop)
- `cmd/minivm/` — CLI entry point (cobra root command)

## Key Invariants

These are the non-obvious rules that cause silent bugs when violated:

- **Heap index 0 is permanently `Null`** — allocated in `interp.New()`, never free it.
- **Threaded closure: advance `c.ip` at compile time, advance `f.ip` at runtime** — missing either causes wrong execution or infinite loops.
- **JIT handler: call `c.ip++` first, then `return false, false` on type mismatch** — never coerce mismatched types; the second bool signals termination (`true, true` for branch terminators).
- **`Buffer`: `Unseal` before `Append`, `Seal` before `Call`** — wrong order triggers a signal on Apple Silicon.
- **`ConstantFoldingPass` pads with NOPs left-aligned** — threaded NOP handler absorbs the gap at compile time; do not strip this padding.
- **JIT emits a segment only when `count > 4`** — exactly 4 consecutive JIT-able instructions is not enough; threshold is strictly greater.
- **`release()` is iterative, not recursive** — it uses an explicit stack to walk `Traceable.Refs()`.
- **Errors in threaded closures should `panic`** — `interp.Run()` recovers and appends `at=<ip>` via `i.error(r)`.
