# Architecture

Package boundaries, ownership, and execution flow.

## When to Read

Read when changing package boundaries, runtime state, verification, optimization, JIT, profiling, or debugging.

## Instruction Levels

| Level | Owner | Form |
|---|---|---|
| Bytecode | `instr` | `instr.Instruction` / `instr.Opcode` |
| SSA | `internal/ssa` | `ssa.Operation` / `ssa.Function` |
| Machine | `internal/asm/<arch>` | `asm.Instruction` / architecture opcode |

Bytecode opcodes remain the semantic vocabulary. SSA adds only compiler concepts such as guards, state, ownership, and control-flow edges. Machine IR is architecture-specific.

## Package Boundaries

| Package | Responsibility |
|---|---|
| `program` | bytecode, builders, constants, types, verification entry point |
| `instr` | opcode definitions, encoding, decoding, metadata |
| `types` | values, boxed representation, heap object types |
| `interp` | execution state, threaded dispatch, host calls, tracing, JIT installation |
| `internal/jit` | architecture-neutral JIT plans and driver |
| `internal/jit/frontend` | snapshot → SSA frontend translation |
| `internal/jit/backend` | SSA backend orchestration and target-neutral metadata |
| `internal/jit/<arch>` | target lowering |
| `internal/ssa` | SSA IR and verification |
| `internal/ssa/transform` | target-independent SSA passes |
| `internal/asm` | native-code abstraction, allocation, linking, executable memory |
| `internal/journal` | interpreter/native frame-journal ABI |
| `internal/codegen` | threaded-handler generation and fusion |
| `pass` / `analysis` / `transform` / `optimize` | analysis and optimization infrastructure |
| `debug` / `prof` / `cli` | debugging, profiling, user-facing commands |

## Dependency Rules

- `instr` and `internal/graph` stay leaf-like.
- `internal/ssa` and `internal/ssa/transform` do not depend on `interp`, JIT, or architecture packages.
- `internal/jit/frontend` does not depend on `interp`, `asm`, or a backend.
- `internal/jit/backend` depends on no architecture package.
- Architecture code stays under `internal/asm/<arch>` and `internal/jit/<arch>`.
- `program.Verify` stays independent of `analysis` and `pass` to avoid cycles.

## Execution

```text
program.Builder / program.New
        ↓
program.Verify (untrusted input)
        ↓
optimize (optional)
        ↓
interp.New
        ↓
threaded execution
        ↓
hot trace / loop event
        ↓
JIT compile on ARM64
        ↓
native execution ↔ threaded fallback
```

The threaded interpreter is the semantic baseline. Native execution is an optimization and must preserve observable behavior.

## Runtime State

`Interpreter` owns the operand stack, frames, globals, heap, reference counts, threaded dispatch table, tracing state, and JIT installation state. A `Pool` shares compile coordination while each interpreter keeps its own execution state and dispatch table.

The runtime is single-goroutine-owned during execution. Background compilation reads an immutable snapshot and never mutates live interpreter state.

## Core Invariants

- Heap index `0` is the permanent null sentinel.
- Only `KindRef` participates in reference counting.
- Heap indices are stable.
- Reference cleanup is iterative.
- External bytecode is verified before execution.
- A frame distinguishes function address from callable reference.
- A native fallback materializes the state required by threaded execution.
- Debugger mode disables JIT and preserves bytecode instruction boundaries.

## Optimization

Bytecode transforms must repair all position-sensitive data or leave the function unchanged. SSA transforms re-emit code from the SSA layout and decline the whole function when the result cannot be encoded safely.

## Related Docs

- `instruction-set.md` — opcode semantics and backend status
- `verification.md` — bytecode validation
- `value-representation.md` — boxed values and computational types
- `memory-model.md` — ownership and heap lifecycle
- `jit-internals.md` — JIT contracts
- `pass-system.md` — analyses and transforms
- `compatibility.md` — platform support
