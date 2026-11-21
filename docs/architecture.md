# Architecture

Package ownership, dependencies, execution flow, and the current threaded runtime boundary.

## Instruction Levels

| Level | Owner | Meaning |
|---|---|---|
| Bytecode | `instr` | VM semantics and encoding |
| SSA | `internal/ssa` | compiler state and control flow |
| Machine | `internal/asm/<arch>` | target instructions |

Bytecode is what the VM executes, SSA is the meaning to optimize, and the machine form lowers SSA meaning to target instructions. Semantics originate in bytecode; SSA and machine forms refine representation, not meaning.

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
| `internal/transform` | bytecode↔SSA conversion, SSA transforms, JIT feedback model |
| `internal/asm` | machine IR, register allocation, encoding, linking, executable memory |
| `internal/asm/arm64` | ARM64 encoding |
| `internal/jit` | native runtime contract shared by the interpreter and the compiler (`Context`): native stack and entry/exit trampoline (`State`, `Enter`, `Resume`); publishes and invalidates native code (`Code`, `Cache`) |
| `internal/jit/compile` | SSA to machine rows: block layout, value registers, edge moves, loop budget; compiles a unit by tier and queues compiles (`Compile`, `Queue`) |
| `internal/jit/arm64` | ARM64 lowering of SSA operations; the runtime's register roles (`Ctx`, `Machine.Reserve`) |
| `pass` | pass API, lifecycle, pipelines, analysis cache |
| `analysis` | reusable read-only facts |
| `optimize` | optimization composition |
| `prof` | execution sampling, metrics, aggregation |
| `debug` | debugging policy |
| `cli` | command parsing and presentation |

Behavior `MUST` follow dominant ownership, not import convenience; an owner `MUST` be extended before adding a coordinator.

## Dependencies

- `instr` and `internal/graph` `MUST` remain leaf-like.
- `internal/ssa` and `internal/transform` `MUST NOT` depend on runtime or target packages.
- `internal/asm` `MUST` remain below the runtime and compiler layers and `MUST NOT` own any runtime convention: native stack, entry, exit, suspension, or pinned registers.
- `internal/jit` MUST NOT import `interp`.
- ARM64 encoding MUST stay under `internal/asm/arm64`.
- `internal/jit/compile` MUST NOT name a physical register or target instruction; `internal/jit/arm64` MUST NOT walk SSA control flow.
- The native compiler MUST NOT become a dependency of `internal/ssa*` or `internal/transform`.
- `program.Verify` `MUST` stay independent of runtime and optimization policy.

## Execution

```text
program → optimize? → interp.New (Verify of a Clone) → threaded ⇄ native
```

Threaded execution is the semantic baseline. On ARM64 the JIT, on by default (`WithThreshold`), may compile hot functions; native execution returns to threaded execution at unsupported or non-native boundaries.

## Runtime

`interp.Interpreter` owns stack, frames, globals, the embedded `types.Heap` (heap/RC), threaded dispatch, tracing, and native installation. Interpreters with the JIT on build their JIT runtime at the first Run and a `jit.Context` at the first native entry; pooled interpreters share published code and compile state through `Pool`.

Execution is single-goroutine-owned. Background compilation consumes immutable input and `MUST NOT` mutate live interpreter state.

## Invariants

The following invariants `MUST` hold:

- Heap index `0` is permanent null.
- Only `KindRef` participates in reference counting.
- Heap indexes are stable; reference cleanup is iterative.
- A frame distinguishes function address from callable reference.
- External bytecode is verified before execution.
- Debugger mode disables JIT and preserves bytecode boundaries.

## Native Boundary

The native tier consumes `internal/transform`, `internal/ssa`, and `internal/asm`; `internal/jit` owns the native stack and trampoline, and `interp` owns when to enter, exit service, materialization, and tiering.

## Related

- `jit-internals.md`
- `coding-patterns.md`
- `memory-model.md`
- `value-representation.md`
