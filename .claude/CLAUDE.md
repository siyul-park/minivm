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
jitCompiler → native ARM64               replaces closures in-place for hot blocks
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
- **JIT handler: call `c.ip++` first, then `return false` on type mismatch** — never coerce mismatched types; return false to split the sub-block.
- **`Buffer`: `Unseal` before `Append`, `Seal` before `Call`** — wrong order triggers a signal on Apple Silicon.
- **`ConstantFoldingPass` pads with NOPs left-aligned** — threaded NOP handler absorbs the gap at compile time; do not strip this padding.
- **JIT emits a sub-block only when `count > 8`** — exactly 8 JIT-able instructions is not enough; threshold is strictly greater.
- **`release()` is iterative, not recursive** — it uses an explicit stack to walk `Traceable.Refs()`.
- **Errors in threaded closures should `panic`** — `interp.Run()` recovers and appends `at=<ip>` via `i.error(r)`.

## Known Gaps

| Gap | Impact |
|-----|--------|
| `SELECT` unimplemented | no threaded or JIT handler; panics if executed |
| JIT excludes control flow, calls, variable access | loops and function calls always run threaded |
| No x86-64 backend | JIT inactive on Linux/Windows servers |
| No benchmark suite | 4096-tick JIT threshold is unvalidated |
| REPL stack transfer unsafe for `KindRef` values | heap pointers from one interpreter step are invalid in the next; ref-producing instructions behave incorrectly in the REPL |

## Coding Conventions (summary)

Full details in [docs/coding-patterns.md](../docs/coding-patterns.md). Quick reference:

**Naming**
- Constructors: `New<Type>` — always returns the concrete type or its primary interface
- Options: `With<Name>() func(*option)` — functional options pattern only, no config structs
- Errors: `Err<PascalCase> = errors.New(...)` — declared at package level, grouped together

**Interfaces**
- Define interfaces in the consuming package, not the implementing one
- Declare `var _ Interface = (*Type)(nil)` immediately after type declaration
- Unexported type + exported instance for singleton-value types (e.g. `TypeI32 = i32Type{}`)

**Errors**
- Wrap with context: `fmt.Errorf("%w: context", ErrBase)` — always use `%w` for sentinel
- Hot-path errors `panic`; boundaries `recover` (see `interp.Run`)

**Testing**
- One test file per source file: `foo.go` → `foo_test.go`
- One `Test<Type>_<Method>` per public method; all cases inside as a table using `t.Run`
- Constructors: `TestNew<Type>`; package-level functions: `Test<FuncName>`
- Subtest name: `t.Run(fmt.Sprint(tt.<primary_input>), ...)` or a descriptive string for error paths
- Never write helper functions in test files — inline all setup directly in each test or subtest
- Always `require` (never `assert`) — fails fast
- `defer resource.Free()` immediately after successful allocation
