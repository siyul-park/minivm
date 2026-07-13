# Architecture

This document describes minivm's package boundaries, ownership model, and execution flow.

## When to Read

Read this document when a change crosses package boundaries or touches runtime state, optimization, JIT, debugging, profiling, or bytecode verification.

For detailed behavior, follow the related topic docs instead of duplicating the same explanation here.

## Related Docs

| Area | Also read |
|---|---|
| Runtime state, heap ownership, refs | `memory-model.md`, `value-representation.md` |
| Opcode semantics | `instruction-set.md` |
| JIT internals | `jit-internals.md`, `profile.md` |
| Optimizer and analyses | `pass-system.md` |
| Static bytecode validation | `verification.md` |
| Host functions and marshaling | `host-integration.md` |
| Debugger and REPL | `debugging.md`, `guides/repl.md` |
| Platforms and build constraints | `compatibility.md` |

## Boundary Rules

- `instr` should remain leaf-like.
- `types` must not import `interp`.
- Optimizer code should flow through `pass.Pipeline` and `pass.Manager`.
- `program/verify.go` intentionally avoids importing `analysis` or `pass` to prevent dependency cycles.
- Architecture-specific native code should stay under `asm/<arch>/` and `interp/jit_<arch>.go`.

## Package Dependency Graph

Import direction: `A → B` means `A` imports `B`.

```text
types   → instr
program → instr, types
prof    → instr
asm/amd64 → asm
asm/arm64 → asm
interp  → program, instr, types, asm, asm/arm64, pass, analysis, prof
debug   → interp
analysis → pass, types, instr
transform → analysis, pass, types, instr, program
optimize → transform, analysis, pass, program
cli → debug, instr, interp, prof, program, types, cobra
cmd/minivm → cli
```

## Package Responsibilities

| Package | Responsibility |
|---|---|
| `program/` | bytecode, constants, types, handlers, builder, and verifier entry point |
| `instr/` | opcode definitions, encoding, parsing, formatting, and metadata |
| `types/` | VM values, type descriptors, boxed representation, arrays, structs, maps, strings, functions, closures, and errors |
| `interp/` | interpreter state, threaded dispatch, host APIs, coroutines, tracing, JIT driver, and pooling |
| `debug/` | bytecode-level debugger API |
| `prof/` | execution samples and JIT metrics |
| `asm/` | architecture-neutral native-code interfaces, buffers, linking, and executable memory |
| `asm/arm64/` | active ARM64 encoder, ABI bridge, and register conventions |
| `asm/amd64/` | placeholder backend; does not emit native code yet |
| `pass/` | generic analysis and transform infrastructure |
| `analysis/` | reusable static analyses |
| `transform/` | optimization transforms |
| `optimize/` | optimization pipeline wiring |
| `cli/` | command tree, run command, REPL, and value formatting |
| `cmd/minivm/` | executable entrypoint |

## Core Runtime Model

`program.Program` is the hand-off format between bytecode producers and the VM.

```go
type Program struct {
    Code      []byte
    Locals    []types.Type
    Constants []types.Value
    Types     []types.Type
}
```

`program.Builder` is the preferred construction API. It handles labels, branch offsets, constant and type interning, and stable pool indexes.

`interp.New` compiles bytecode to threaded dispatch closures. The threaded interpreter is the source of correctness. The JIT is an optimization layered on top of it and must always preserve threaded fallback behavior.

## Execution Flow

A typical execution follows this path:

```text
1. Build program
   └─ program.New(...) or program.Builder

2. Verify when input is not trusted
   └─ program.Verify(...)

3. Optimize when requested
   └─ optimize.NewOptimizer(level).Optimize(...)

4. Construct interpreter
   └─ interp.New(...)

5. Run
   ├─ threaded dispatch executes bytecode
   ├─ tick path handles context, fuel, hooks, and samples
   └─ ARM64 JIT may compile hot traces

6. Close or reset
   └─ release runtime resources
```

`program.New` and `interp.New` trust their inputs. Use `program.Verify` before constructing an interpreter for bytecode loaded from external sources.

## Runtime State

`interp.Interpreter` owns execution state.

| State | Purpose |
|---|---|
| `instrs` | raw bytecode per function slot |
| `code` | threaded dispatch closures or native wrappers |
| `tracer` | profile samples and trace recording |
| `frames` | call stack |
| `stack` | operand stack |
| `heap`, `rc`, `free`, `trial`, `work` | heap storage, exact counts, reusable slots, and cycle-collection scratch |
| `globals` | global slots |
| `compiler` | private JIT compiler for standalone interpreters |
| `cache` | shared native-code cache used by pools |

Each `Interpreter` is single-goroutine-owned during use. `Pool` lets multiple goroutines borrow separate interpreters while sharing profile and native-code cache data.

## Key Invariants

### Heap and Values

- Heap index `0` is permanently `Null`.
- Only `KindRef` values participate in reference counting.
- Heap indices are stable and must not move.
- Values that contain refs must implement `types.Traceable`.
- Large `i64` values may spill to the heap while preserving bytecode semantics.

See `memory-model.md` and `value-representation.md` for the detailed rules.

### Frames

A frame separates the function/template address from the callable reference.

| Field | Meaning |
|---|---|
| `addr` | function/template slot used for code, profiling, and JIT |
| `ref` | callable heap ref released on return |

For plain functions, `addr == ref`. For closures, `addr` points to the function template and `ref` points to the closure object.

### Threaded Dispatch

- Compile-time handlers advance `c.ip`.
- Runtime handlers advance `i.fr.ip`.
- Runtime traps panic internally and are recovered by `interp.Run`.
- Debugging with `WithDebugger` disables JIT and preserves bytecode instruction boundaries.

### JIT


The interpreter requests native compilation through `compiler.Compile(i, addr)` only. The compiler runs the static and trace frontends internally; both produce the same flat, backend-neutral plan with block-ID edges, and installation depends only on the entry ABI kind.

- Native code is speculative and guarded.
- Blocks with declared entry state carry no register state across edges; stack and dirty locals are materialized in VM memory.
- Native-call slots are fixed for an interpreter lifetime and published atomically on function-entry installation.
- Unsupported paths must fall back to threaded execution.
- JIT handlers must not duplicate complex interpreter behavior unless they can own all semantics.
- Guard failure materializes VM state and resumes threaded dispatch.
- ARM64 label branches are range-checked and relaxed only to replacements that are already in range; an unreachable target falls back to threaded execution.
- Spill frames use a stable base register, and every internal call resume point must restore the active spill-frame depth. Loop traces and terminal mutation traces disable spilling when control flow cannot preserve that contract.

See `jit-internals.md` for trace recording, journal layout, calls, branches, loops, and fallback rules.

### Optimization

Optimization passes mutate `*program.Program` through the pass system.

Bytecode length changes must repair all position-sensitive data, including branch offsets and exception handler ranges. If repair cannot preserve behavior, the transform should leave the function unchanged.

See `pass-system.md` for optimizer levels and rewrite rules.

## Focus Areas

| Area | Direction |
|---|---|
| JIT coverage | Expand reliable native lowering for hot numeric, call, and read-only heap paths |
| Architecture support | Keep ARM64 stable; add other backends only with clear user and benchmark value |
| Benchmarks | Keep benchmark claims tied to `docs/benchmarks.md` |
| Program format | Keep `instr` and `program` compact and Go-native |
| Host integration | Keep `HostFunction`, `Marshal`, and `Unmarshal` explicit about ownership |
| Resource policy | Keep context, fuel, hooks, stack, heap, frame limits, and host policy easy to reason about |

## Maintenance Notes

When changing architecture-level behavior:

- update the owning topic document rather than repeating details here
- keep package boundaries explicit
- preserve interpreter/JIT semantic parity
- keep public APIs small
- prefer local, simple ownership rules
- keep examples current with the code
