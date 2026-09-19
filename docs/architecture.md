# Architecture

Package ownership, dependencies, execution flow, and runtime/JIT boundaries.

## Instruction Levels

| Level | Owner |
|---|---|
| Bytecode | `instr` — `Instruction`, `Opcode` |
| SSA | `internal/ssa` — `Operation`, `Function` |
| Machine | `internal/asm/<arch>` — machine instructions |

Bytecode defines semantics. SSA adds compiler state/control-flow concepts. Machine IR is target-specific.

## Package Ownership

| Package | Owns |
|---|---|
| `instr` | opcode vocabulary, widths, encoding, metadata |
| `types` | VM values, kinds, boxed values, heap types |
| `program` | bytecode, builders, verification boundary |
| `interp` | runtime state, threaded execution, host calls, JIT installation |
| `internal/codegen` | generated threaded handlers and fusion |
| `internal/ssa` | SSA IR and verification |
| `internal/ssa/transform` | target-independent SSA transforms |
| `internal/asm` | machine IR, allocation, linking, executable memory |
| `internal/asm/<arch>` | ISA encoding and ABI mechanics |
| `internal/jit` | architecture-neutral plans, target contract, and driver |
| `internal/jit/frontend` | bytecode/trace → SSA |
| `internal/jit/backend` | SSA → machine orchestration, target-neutral metadata |
| `internal/jit/<arch>` | native lowering |
| `internal/journal` | native/interpreter frame-journal ABI |
| `internal/jit/compile` | compile admission and published code store |
| `internal/jit/tier` | tiering and retirement policy |
| `analysis` | reusable read-only facts |
| `transform` | bytecode transforms |
| `optimize` | optimization composition |
| `pass` | pass lifecycle and analysis cache |
| `prof` | profiling and aggregation |
| `debug` | debugging policy |
| `cli` | command parsing and presentation |

The agent `MUST` place behavior by dominant ownership, not import convenience. It `MUST` extend an owner before adding a coordinator.

## Dependencies

- `instr` and `internal/graph` `MUST` remain leaf-like.
- `internal/ssa` and `internal/ssa/transform` `MUST NOT` depend on runtime, JIT, or target packages.
- `internal/jit/frontend` `MUST NOT` depend on `interp`, `asm`, or backend packages.
- `internal/jit/backend` `MUST NOT` depend on target packages.
- Target code `MUST` stay under `internal/asm/<arch>` and `internal/jit/<arch>`.
- `internal/jit` `MUST NOT` import any target package; arch selection belongs in `interp`.
- `program.Verify` `MUST` stay independent of runtime and optimization policy.

## Execution

```text
program → Verify → optimize? → interp → threaded
                                         ↓ hot root
                                      JIT compile
                                         ↓
                              native ↔ threaded fallback
```

Threaded execution is the semantic baseline. Native execution `MUST` preserve observable behavior.

## Runtime

`interp.Interpreter` owns stack, frames, globals, heap, reference counts, threaded dispatch, tracing, and JIT installation. A shared `Pool` owns compile coordination; an interpreter owns its execution state and dispatch table.

Execution is single-goroutine-owned. Background compilation consumes immutable input and `MUST NOT` mutate live interpreter state.

## Invariants

The following invariants `MUST` hold, and the agent `MUST` preserve them:

- Heap index `0` is permanent null.
- Only `KindRef` participates in reference counting.
- Heap indexes are stable; reference cleanup is iterative.
- A frame distinguishes function address from callable reference.
- External bytecode is verified before execution.
- Native fallback materializes exactly the state required by threaded execution.
- Debugger mode disables JIT and preserves bytecode boundaries.

## JIT Boundary

- Architecture-neutral policy `MUST` stay in `internal/jit`.
- Target mechanics `MUST` stay in `internal/jit/<arch>` and `internal/asm/<arch>`.
- Unsupported lowering `MUST` decline without partial IR/state mutation.
- Guards `MUST` deopt before native code executes unsupported behavior.
- Published code is immutable and interpreter dispatch remains interpreter-owned.
- `internal/asm` owns allocation, linking, and executable memory.

## Optimization

A size-changing bytecode transform `MUST` repair all position-sensitive metadata or leave the function unchanged. An SSA transform `MUST` re-emit from SSA and `MUST` decline when the result cannot be encoded safely.

## Related

- `instruction-set.md`
- `verification.md`
- `memory-model.md`
- `value-representation.md`
- `jit-internals.md`
- `pass-system.md`
