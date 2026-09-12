# JIT Internals

Current contracts for the ARM64 JIT and its interpreter boundary.

## When to Read

Read when changing `internal/jit`, `internal/jit/frontend`, `internal/jit/backend`, `internal/jit/arm64`, tracing, tiering, deoptimization, or native installation.

## Source of Truth

| Concern | Owner |
|---|---|
| Opcode semantics | `instruction-set.md`, `instr/type.go` |
| Threaded execution | `interp/threaded.go` |
| Trace recording | `interp/trace.go` |
| JIT planning | `internal/jit/` |
| SSA frontend | `internal/jit/frontend/` |
| SSA backend seam | `internal/jit/backend/` |
| ARM64 lowering | `internal/jit/arm64/` |
| Compile coordination | `internal/jit/compile/` |
| Tiering and retirement | `interp/tier.go`, `internal/jit/tier/` |
| Frame journal | `internal/journal/` |
| Callable ABI | `internal/asm/` |
| Value representation | `value-representation.md` |
| Hotness policy | `profile.md` |

## Model

The threaded interpreter is the correctness baseline. JIT execution is optional and every native path has a threaded fallback.

```text
bytecode
  ↓
threaded handlers
  ↓ hot root
trace snapshot / static plan
  ↓
SSA frontend
  ↓
backend.Machine
  ↓
ARM64 native code
  ↕
threaded fallback / bridge
```

Native compilation reads an immutable `jit.Input`. Recording, snapshot creation, installation, and interpreter state mutation stay on the interpreter goroutine. Compilation itself may run synchronously or on the shared compile worker.

## Roots

| Root | Meaning |
|---|---|
| module entry | program start |
| function entry | function start |
| loop header | hot backward-branch target |

Entry and loop callables have different frame ownership. An entry owns and tears down its frame; a loop re-enters a live frame and must not unwind it.

## Compilation

`jit.Compiler` tries the SSA backend first. If the machine declines or emitted code cannot build, the assembler is discarded and the existing plan pipeline remains available. A compiler never mutates live interpreter state.

Two frontends produce the same plan model:

- `StaticPlan` uses verified bytecode and a forward dataflow pass.
- `TracePlan` uses immutable recorded execution.

The plan contains blocks, entry state, operations, and explicit edges. Build, layout, metadata, validation, and publication belong to the compiler/backend boundary.

## Static Planning

Static planning resolves only facts available without execution:

- stack kinds
- known constants and reference provenance
- declared array and struct types
- direct call targets
- dynamic arities whose counts are statically known

Runtime shape, type, bounds, and kind checks remain guards. A root is rejected when a required fact cannot be proven.

Loop plans are pruned to blocks reachable from their own root. This prevents unrelated blocks from increasing code size, register pressure, and bridge state.

## Trace Planning

Trace recording clones the interpreter and runs threaded handlers until return, a loop boundary, branch exit, unsupported operation, trace limit, or abort. The live interpreter is never mutated by speculative execution.

Recorded observations provide specialized call targets and heap shapes. A recursive call from a non-entry loop trace is a fallback boundary because the recorder cannot model the recursive callee safely. Aborted recordings are never published.

Trace snapshots are immutable. Shared tracers synchronize tree publication; compilation consumes snapshots without accessing live heap state.

## SSA Backend

`internal/jit/backend` owns target-neutral compilation state:

- block layout
- value-to-register binding
- block-parameter moves
- `Deopt` metadata
- bridge resume points

`backend.Machine` owns target decisions:

| Hook | Role |
|---|---|
| `Lowers` | opcode has native lowering |
| `Traps` | lowering ends the block in threaded control |
| `Open` | creates per-compile lowering state |
| `Enter` | emits callable prologue |
| `Lower` | lowers operations and may fuse adjacent operations |
| `Term` | lowers the block terminator |
| `Leave` | emits deferred cold paths |

The backend emits no architecture instructions itself.

## ARM64 Representation

Native computation uses the representation implied by `ssa.Type`:

| Type | Native representation |
|---|---|
| `i1`, `i8`, `i32` | 32-bit integer lane |
| `i64` | 64-bit integer lane |
| `f32` | 32-bit float lane |
| `f64` | 64-bit float lane |
| `ref` | boxed 64-bit value |

Interpreter-visible values are always boxed.

`I64_ADD` can produce a result outside the inline boxed range. Its own lowering guards the result immediately after computing it and exits through the add's own pre-op state on overflow - the add's two operands, not its unboxed sum - so the flush is ordinary in-range boxed i64 values and the interpreter resumes at the add's own IP to redo it and heap-promote the result (see Guards and Deoptimization). No raw, out-of-range i64 value is ever assigned to a live SSA value, so every downstream boundary that boxes an i64 - a store, a return, module completion, a deopt flush - only ever boxes one a producer already proved in range.

Only `I64_ADD` compiles through this shape today. `I64_SUB`, `I64_MUL`, `I64_SHL`, `I64_SHR_U`, division, remainder, and float-to-i64 conversion can also leave the boxed range and still decline to the plan pipeline; each can adopt the identical guard once proven.

## Guards and Deoptimization

A guard has two products:

1. hot-path proof that allows native execution;
2. `OpState` metadata sufficient to reconstruct interpreter state on failure.

`backend.Deopt` describes the state. ARM64 emits the journal stores and cold stub.

A deoptimization materializes live VM slots, frame records, stack pointer, resume IP, trap state, and required retains. Interpreter code then resumes threaded execution.

## Bridge

A bridge transfers one operation to threaded execution and resumes native execution afterward.

`IsBridgeable` covers operations the current native backend cannot lower but whose stack effect can still be modeled. The bridged block resumes at the block after the operation.

## Frame Journal

`internal/journal` is the ABI between native code and the interpreter. Header cells carry stack/global/frame pointers, entry and resume IPs, trap state, exit metadata, and native runtime state. Frame records describe inlined frames.

## Calls, Loops, and Suspension

Native calls use interpreter-owned native-entry slots and fall back when the target is not installed. Native loop back-edges commit the state required by a future deoptimization and use a safepoint budget.

Suspension is a terminal fallback boundary. Native code never resumes in the middle of a suspended native frame.

`RETURN_CALL` remains a threaded boundary for the SSA backend because it changes frame identity rather than simply changing control flow inside one SSA function.

## Ownership

Native values are borrowed from VM storage unless the IR explicitly owns them. Cold paths must restore the same ownership the interpreter expects.

- references loaded from slots remain borrowed
- produced references are owned
- `OpRetain` / `OpRelease` describe ownership transitions
- deopt state records ownership per stack entry
- a deferred reference cannot cross a committing loop back-edge without being materialized

## Native Coverage

The ARM64 backend currently supports the native operations listed in `instruction-set.md`. Unsupported operations either bridge when their stack effect is modelable or remain on the plan/threaded path. A machine decline never changes interpreter semantics.

## Testing Contract

- frontend tests compare static/trace acceptance with their plan counterparts and run `ssa.Verify`
- backend tests assert layout, register bindings, moves, deopt metadata, and bridge resume points
- ARM64 tests use exact instruction goldens
- interpreter tests compare JIT and threaded observable behavior
- mutations must make the corresponding test fail

## Related Docs

- `architecture.md`
- `instruction-set.md`
- `value-representation.md`
- `memory-model.md`
- `testing.md`
- `profile.md`
