# AGENTS.md

This file provides guidance to AI coding agents (Claude Code, Codex, Cursor, etc.) working in this repository.

---

## Quick Commands

```bash
make test                                        # go test -race ./...
go test -race -run TestFoo ./interp/...          # run a single test
make benchmark                                   # go test -run="-" -bench=".*" -benchmem ./...
make lint                                        # goimports -w . && go vet ./...
make coverage                                    # produces coverage.out
make build                                       # builds ./dist/minivm
./dist/minivm                                    # launch the interactive assembly REPL
```

---

## Documentation Index

**Read docs on demand — do not pre-load all of them.** Each entry states exactly when it is relevant.

| Document | Read when… |
|---|---|
| [docs/architecture.md](../docs/architecture.md) | tracing execution flow, debugging across packages, understanding component boundaries |
| [docs/value-representation.md](../docs/value-representation.md) | working in `types/`, passing values through JIT, debugging boxing/unboxing |
| [docs/memory-model.md](../docs/memory-model.md) | writing threaded closures that touch refs, implementing host functions, modifying GC |
| [docs/instruction-set.md](../docs/instruction-set.md) | adding or modifying opcodes, checking JIT coverage, debugging specific instructions |
| [docs/jit-internals.md](../docs/jit-internals.md) | modifying `threaded.go` or `jit_arm64.go`, writing new opcode handlers |
| [docs/pass-system.md](../docs/pass-system.md) | adding optimization passes, modifying `transform/` or `analysis/` |
| [docs/coding-patterns.md](../docs/coding-patterns.md) | writing **any** new code in this repository |
| [docs/guides/add-opcode.md](../docs/guides/add-opcode.md) | adding a new instruction end-to-end |
| [docs/guides/add-architecture.md](../docs/guides/add-architecture.md) | adding a new JIT backend (e.g. x86-64) |
| [docs/guides/repl.md](../docs/guides/repl.md) | using or extending the interactive REPL (`cmd/minivm`, `cmd/repl`) |

---

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

### Package Responsibilities

| Package | Responsibility |
|---|---|
| `program/` | Bytecode + constants container; entry point for program construction |
| `instr/` | Opcode definitions, variable-width encoding/decoding, text formatter (`Format`), text parser (`Parse`/`ParseAll`) |
| `types/` | Value types (`Boxed`, `Function`, `Array`, `Struct`, `String`) and NaN boxing |
| `interp/` | Interpreter state, threaded compiler, JIT driver, host function bridge |
| `asm/` | Virtual-register IR, linear-scan register allocator, executable memory buffer |
| `asm/arm64/` | ARM64 encoder, ABI, caller trampoline |
| `pass/` | Reflection-based pipeline (`Manager`, `Pass[T]`) |
| `analysis/` | `BasicBlocksPass` (shared by JIT and optimizer) |
| `transform/` | `ConstantFoldingPass`, `ConstantDeduplicationPass`, `DeadCodeEliminationPass` |
| `optimize/` | `Optimizer` that wires passes into O0/O1 levels |
| `cmd/repl/` | Interactive assembly REPL (`REPL` type, `Run` loop) |
| `cmd/minivm/` | CLI entry point (cobra root command) |

---

## Self-Maintenance: Keeping Docs Accurate

When a user reports a mistake or an agent discovers a gap during a task, **consider** updating the relevant documentation. This is a judgment call — not every mistake requires a doc change.

### When a doc update is worth it

- The same mistake is likely to happen again without a written reminder.
- A command, invariant, or package description in this file is factually wrong or outdated.
- A useful doc exists but is missing from the [Documentation Index](#documentation-index).
- A non-obvious constraint was discovered that no existing doc covers.

### Which file to update

There is no requirement to edit `AGENTS.md` specifically. Update whichever file is the most natural home for the information:

- **Recurring gotcha or silent bug** → [Key Invariants](#key-invariants) in this file, or the relevant `docs/` page.
- **Wrong or missing command** → [Quick Commands](#quick-commands) in this file.
- **Architectural detail** → `docs/architecture.md` or the relevant doc listed in the index.
- **Coding pattern or style rule** → `docs/coding-patterns.md`.
- **New doc worth consulting conditionally** → add a row to the [Documentation Index](#documentation-index).

If no existing doc is a good fit, add a minimal note here. If the fix is minor and self-evident, skip the doc update entirely.

### Style rules when editing

- Match the terse, imperative style of existing content.
- One entry per distinct fact — do not bundle unrelated changes.
- Document only what is concretely true in the current codebase; no speculative notes.
- After editing, verify the file is valid Markdown (tables aligned, code fences closed).

---

## Key Invariants

> ⚠️ These are non-obvious rules that cause **silent bugs** when violated. Read before touching any related code.

- **Heap index 0 is permanently `Null`** — allocated in `interp.New()`, never free it.

- **Threaded closure: advance `c.ip` at compile time, advance `f.ip` at runtime** — missing either causes wrong execution or infinite loops.

- **JIT handler: call `c.ip++` first, then `return false, false` on type mismatch** — never coerce mismatched types; the second bool signals termination (`true, true` for branch terminators).

- **`Buffer` ordering: `Unseal` → `Append` → `Seal` → `Call`** — wrong order triggers a signal on Apple Silicon.

- **`ConstantFoldingPass` pads with NOPs left-aligned** — the threaded NOP handler absorbs the gap at compile time; do not strip this padding.

- **JIT emits a segment only when `count > 4`** — exactly 4 consecutive JIT-able instructions is not enough; threshold is strictly greater-than.

- **`release()` is iterative, not recursive** — it uses an explicit stack to walk `Traceable.Refs()`.

- **Errors in threaded closures should `panic`** — `interp.Run()` recovers and appends `at=<ip>` via `i.error(r)`.