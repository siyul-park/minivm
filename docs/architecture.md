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
| `internal/jit` | architecture-neutral plans and driver |
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

Place behavior by dominant ownership, not import convenience. Extend an owner before adding a coordinator.

## Dependencies

- `instr` and `internal/graph` remain leaf-like.
- `internal/ssa` and `internal/ssa/transform` do not depend on runtime, JIT, or target packages.
- `internal/jit/frontend` does not depend on `interp`, `asm`, or backend packages.
- `internal/jit/backend` does not depend on target packages.
- Target code stays under `internal/asm/<arch>` and `internal/jit/<arch>`.
- `internal/jit` imports no target package; arch selection belongs in `interp`.
- `program.Verify` is independent of runtime and optimization policy.

## Execution

```text
program → Verify → optimize? → interp → threaded
                                         ↓ hot root
                                      JIT compile
                                         ↓
                              native ↔ threaded fallback
```text

Threaded execution is the semantic baseline. Native execution must preserve observable behavior.

## Runtime

`interp.Interpreter` owns stack, frames, globals, heap, reference counts, threaded dispatch, tracing, and JIT installation. A shared `Pool` owns compile coordination; an interpreter owns its execution state and dispatch table.

Execution is single-goroutine-owned. Background compilation consumes immutable input and does not mutate live interpreter state.

## Invariants

- Heap index `0` is permanent null.
- Only `KindRef` participates in reference counting.
- Heap indexes are stable; reference cleanup is iterative.
- A frame distinguishes function address from callable reference.
- External bytecode is verified before execution.
- Native fallback materializes exactly the state required by threaded execution.
- Debugger mode disables JIT and preserves bytecode boundaries.

## JIT Boundary

- Architecture-neutral policy stays in `internal/jit`.
- Target mechanics stay in `internal/jit/<arch>` and `internal/asm/<arch>`.
- Unsupported lowering declines without partial IR/state mutation.
- Guards deopt before native code executes unsupported behavior.
- Published code is immutable and interpreter dispatch remains interpreter-owned.
- `internal/asm` owns allocation, linking, and executable memory.

## Optimization

Bytecode transforms repair position-sensitive metadata or leave the function unchanged. SSA transforms re-emit from SSA and decline when the result cannot be encoded safely.

## Related

- `instruction-set.md`
- `verification.md`
- `memory-model.md`
- `value-representation.md`
- `jit-internals.md`
- `pass-system.md`
