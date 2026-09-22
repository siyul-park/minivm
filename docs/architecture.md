# Architecture

Package ownership, dependencies, execution flow, and the current threaded runtime boundary.

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
| `interp` | runtime state, threaded execution, host calls |
| `internal/codegen` | generated threaded handlers and fusion |
| `internal/graph` | CFG analysis |
| `internal/ssa` | SSA IR and verification |
| `internal/asm` | machine IR, register allocation, encoding, linking, executable memory, native stack and trampoline |
| `internal/asm/arm64` | ARM64 encoding |
| `internal/jit` | native runtime contract shared by the interpreter and the compiler; publishes and retires native code (`Code`, `Store`) |
| `internal/jit/compile` | SSA to machine rows: block layout, value registers, edge moves, loop budget; compiles a unit by tier and queues compiles (`Compile`, `Queue`) |
| `internal/jit/arm64` | ARM64 lowering of SSA operations |
| `pass` | pass lifecycle, pipelines, analysis cache |
| `analysis` | reusable read-only facts |
| `transform` | bytecode transforms, bytecode↔SSA conversion, SSA transforms |
| `optimize` | optimization composition |
| `prof` | execution sampling, metrics, aggregation |
| `debug` | debugging policy |
| `cli` | command parsing and presentation |

Behavior `MUST` follow dominant ownership, not import convenience; an owner `MUST` be extended before adding a coordinator.

## Dependencies

- `instr` and `internal/graph` `MUST` remain leaf-like.
- `internal/ssa` and `transform` `MUST NOT` depend on runtime or target packages.
- `internal/asm` MUST remain below the runtime and compiler layers and MUST NOT know how native code is used: no trap, exit, or interpreter vocabulary.
- `internal/jit` MUST NOT import `interp`.
- ARM64 encoding MUST stay under `internal/asm/arm64`.
- `internal/jit/compile` MUST NOT name a physical register or target instruction; `internal/jit/arm64` MUST NOT walk SSA control flow.
- The native compiler MUST NOT become a dependency of `internal/ssa*` or `transform`.
- `program.Verify` `MUST` stay independent of runtime and optimization policy.

## Execution

```text
program → Verify → optimize? → interp → threaded ⇄ native
```

Threaded execution is the semantic baseline. A hot `*types.Function`, entered at ip 0 from an interpreted `CALL`, `MAY` run compiled native code instead (`interp.WithThreshold`, arm64 only); every other call stays threaded.

## Runtime

`interp.Interpreter` owns stack, frames, globals, heap, reference counts, threaded dispatch, tracing, and JIT installation. An interpreter built with `WithThreshold` owns its native execution context and tiering counts; its published code and compile queue are its own, or its `Pool`'s when pooled (see `jit-internals.md` Runtime).

Execution is single-goroutine-owned. Background compilation consumes immutable input and `MUST NOT` mutate live interpreter state.

## Invariants

The following invariants `MUST` hold, and the agent `MUST` preserve them:

- Heap index `0` is permanent null.
- Only `KindRef` participates in reference counting.
- Heap indexes are stable; reference cleanup is iterative.
- A frame distinguishes function address from callable reference.
- External bytecode is verified before execution.
- Debugger mode disables JIT and preserves bytecode boundaries.

## Native Boundary

The native tier consumes `transform`, `internal/ssa`, and `internal/asm`; `interp` owns native entry, exit, materialization, and tiering.

## Related

- `jit-internals.md`
- `coding-patterns.md`
- `memory-model.md`
- `value-representation.md`
